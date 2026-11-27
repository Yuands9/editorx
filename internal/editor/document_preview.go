package editor

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"encoding/json"
	"encoding/xml"
	"errors"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
)

type documentPreview struct {
	kind, issue string
	mindMap     []mindMapSheet
	word        wordPreviewContent
	view        *app.WebView
	server      *http.Server
	pdf         *os.File
	pdfBytes    []byte
	ready       bool
}

type mindMapSheet struct {
	Title string
	Root  mindMapTopic
}

type mindMapTopic struct {
	Title, Note string
	Children    []mindMapTopic
}

type xmindJSONTopic struct {
	Title    string `json:"title"`
	Children struct {
		Attached []xmindJSONTopic `json:"attached"`
		Detached []xmindJSONTopic `json:"detached"`
	} `json:"children"`
	Notes struct {
		Plain struct {
			Content string `json:"content"`
		} `json:"plain"`
	} `json:"notes"`
}

type xmindXMLTopic struct {
	Title    string          `xml:"title"`
	Note     string          `xml:"notes>plain"`
	Children []xmindXMLTopic `xml:"children>topics>topic"`
}

const xmindContentLimit = 24 << 20
const xmindTopicLimit = 16000

//go:embed assets/xmind-preview.html
var xmindPage string

var xmindTemplate = template.Must(template.New("xmind").Parse(xmindPage))

func previewFormat(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pdf":
		return "PDF"
	case ".xmind":
		return "XMind"
	case ".docx":
		if !hasWordEditor {
			return "Word"
		}
	}
	return ""
}

func readDocumentPreview(path string) (*documentPreview, error) {
	preview := &documentPreview{kind: previewFormat(path)}
	if preview.kind == "Word" {
		word, err := readWordPreview(path)
		preview.word = word
		return preview, err
	}
	if preview.kind == "XMind" {
		mindMap, err := readMindMap(path)
		preview.mindMap = mindMap
		return preview, err
	}
	file, err := os.Open(path)
	if err != nil {
		return preview, err
	}
	defer file.Close()
	var head [1024]byte
	n, err := io.ReadFull(file, head[:])
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return preview, err
	}
	return preview, validatePDFHeader(head[:n])
}

func validatePDFHeader(data []byte) error {
	if !bytes.Contains(data[:min(len(data), 1024)], []byte("%PDF-")) {
		return errors.New(uiText("文件缺少 PDF 标识"))
	}
	return nil
}

func readMindMap(path string) ([]mindMapSheet, error) {
	book, err := zip.OpenReader(path)
	if book != nil {
		defer book.Close()
	}
	if err != nil {
		return nil, errors.New(uiText("无法读取 XMind 压缩包"))
	}
	return decodeMindMap(&book.Reader)
}

func decodeMindMap(book *zip.Reader) ([]mindMapSheet, error) {
	var modern, legacy *zip.File
	for _, file := range book.File {
		switch file.Name {
		case "content.json":
			modern = file
		case "content.xml":
			legacy = file
		}
	}
	dataFile := modern
	if dataFile == nil {
		dataFile = legacy
	}
	if dataFile == nil || dataFile.UncompressedSize64 > xmindContentLimit {
		return nil, errors.New(uiText("XMind 主题数据缺失或过大"))
	}
	stream, err := dataFile.Open()
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	data, err := io.ReadAll(io.LimitReader(stream, xmindContentLimit+1))
	if err != nil || len(data) > xmindContentLimit {
		return nil, errors.New(uiText("无法读取完整的 XMind 主题"))
	}
	var sheets []mindMapSheet
	left := xmindTopicLimit
	if modern != nil {
		type sheet struct {
			Title string         `json:"title"`
			Root  xmindJSONTopic `json:"rootTopic"`
		}
		var current []sheet
		if err := json.Unmarshal(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}), &current); err != nil {
			return nil, errors.New(uiText("XMind 的 content.json 格式无效"))
		}
		for _, item := range current {
			topic := item.Root.tree(0, &left)
			sheets = append(sheets, mindMapSheet{Title: mindMapName(item.Title, topic.Title), Root: topic})
		}
	} else {
		var legacy struct {
			Sheets []struct {
				Title string        `xml:"title"`
				Root  xmindXMLTopic `xml:"topic"`
			} `xml:"sheet"`
		}
		if err := xml.Unmarshal(data, &legacy); err != nil {
			return nil, errors.New(uiText("XMind 的 content.xml 格式无效"))
		}
		for _, item := range legacy.Sheets {
			topic := item.Root.tree(0, &left)
			sheets = append(sheets, mindMapSheet{Title: mindMapName(item.Title, topic.Title), Root: topic})
		}
	}
	if left < 0 {
		return nil, errors.New(uiText("XMind 主题数或层级超出了预览范围"))
	}
	if len(sheets) == 0 {
		return nil, errors.New(uiText("XMind 文件中没有画布"))
	}
	return sheets, nil
}

func mindMapName(title, fallback string) string {
	if strings.TrimSpace(title) != "" {
		return title
	}
	return fallback
}

func (topic xmindJSONTopic) tree(depth int, left *int) mindMapTopic {
	*left--
	if *left < 0 || depth > 96 {
		*left = -1
		return mindMapTopic{}
	}
	node := mindMapTopic{Title: mindMapName(topic.Title, "未命名主题"), Note: topic.Notes.Plain.Content}
	for _, children := range [][]xmindJSONTopic{topic.Children.Attached, topic.Children.Detached} {
		for _, child := range children {
			node.Children = append(node.Children, child.tree(depth+1, left))
			if *left < 0 {
				return node
			}
		}
	}
	return node
}

func (topic xmindXMLTopic) tree(depth int, left *int) mindMapTopic {
	*left--
	if *left < 0 || depth > 96 {
		*left = -1
		return mindMapTopic{}
	}
	node := mindMapTopic{Title: mindMapName(topic.Title, "未命名主题"), Note: topic.Note}
	for _, child := range topic.Children {
		node.Children = append(node.Children, child.tree(depth+1, left))
		if *left < 0 {
			break
		}
	}
	return node
}

func (preview *documentPreview) close() {
	if preview == nil {
		return
	}
	if preview.view != nil {
		preview.view.Close()
		preview.view = nil
	}
	preview.ready = false
	if preview.server != nil {
		_ = preview.server.Close()
		preview.server = nil
	}
	if preview.pdf != nil {
		_ = preview.pdf.Close()
		preview.pdf = nil
	}
}

func (m *editorApp) openDocumentPreview(t *editorTab) {
	preview := t.preview
	if !m.webReady || preview == nil || preview.view != nil || preview.issue != "" {
		return
	}
	if preview.kind == "PDF" {
		m.openPDFPreview(t)
		return
	}
	var html bytes.Buffer
	var err error
	if preview.kind == "Word" {
		content := preview.word
		content.Title, content.Light = t.title(), m.light
		err = wordPreviewTemplate.Execute(&html, content)
	} else {
		err = xmindTemplate.Execute(&html, struct{ Sheets []mindMapSheet }{preview.mindMap})
	}
	if err != nil {
		preview.issue = uiText("无法生成 ") + preview.kind + uiText(" 预览：") + err.Error()
		return
	}
	const location = "editorx-document://local/index.html"
	view, err := m.webHost.Open(app.WebViewConfig{
		URL: location, Ephemeral: true, BridgeMainFrameOnly: true, TopCornerRadius: 12,
		Scheme: &app.WebScheme{Name: "editorx-document", Serve: func(path string) (string, []byte, bool) {
			if path != "index.html" {
				return "", nil, false
			}
			return "text/html; charset=utf-8", html.Bytes(), true
		}},
		LetterShortcut: m.openLinkWebShortcut,
		Navigate: func(nav app.WebNavigation) bool {
			address, _, _ := strings.Cut(nav.URL, "#")
			return strings.EqualFold(address, location)
		},
		OnLoad: func(load app.WebLoad) {
			if load.Phase != app.WebLoadFinished || preview.view == nil {
				return
			}
			preview.ready = true
			preview.view.Eval(previewThemeJS(m.light))
			preview.view.Eval(previewLanguageJS(currentUILanguage()))
		},
	})
	if err != nil {
		preview.close()
		preview.issue = uiText("无法打开内嵌预览：") + err.Error()
		return
	}
	preview.view = view
	m.invalidator.Invalidate()
}

func previewThemeJS(light bool) string {
	if light {
		return `document.documentElement.classList.add("light")`
	}
	return `document.documentElement.classList.remove("light")`
}

func (m *editorApp) writePreviewFile(content string) (string, error) {
	dir := filepath.Join(m.dataDirectory, editorPreviewCacheDirectory)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	file, err := createEditorPreviewCache(dir, editorPreviewCachePattern)
	if err != nil {
		return "", err
	}
	_, err = file.WriteString(content)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		removeEditorPreviewCache(file.Name())
		return "", err
	}
	return file.Name(), nil
}

func (m *editorApp) documentPreviewView(c ui.C, t *editorTab, blocked bool) {
	preview := t.preview
	if preview.view != nil && !blocked {
		ui.Slot(c).FillWidth().Grow(1).MinHeight(0).NativeView(preview.view.ID())
		return
	}
	if m.webReady && preview.view == nil && preview.issue == "" && !blocked {
		c.After(func() { m.openDocumentPreview(t) })
	}
	message := "正在打开 " + preview.kind + " 预览…"
	if preview.issue != "" {
		message = preview.issue
	}
	ui.Wrap(c, message).FillWidth().Pad(20).Muted()
}
