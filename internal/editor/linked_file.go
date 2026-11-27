package editor

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/app/proxy"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"github.com/vivaui/viva/ui"
)

const linkedFileLimit = 128 << 20

var linkedFileHTTP = func() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = func(request *http.Request) (*url.URL, error) {
		return proxy.SystemProxy(request.URL)
	}
	return &http.Client{
		Transport: transport, Timeout: 2 * time.Minute,
		CheckRedirect: func(request *http.Request, previous []*http.Request) error {
			if len(previous) >= 10 || request.URL.User != nil ||
				(request.URL.Scheme != "http" && request.URL.Scheme != "https") {
				return errors.New("链接跳转无效")
			}
			request.Header.Del("Referer")
			return nil
		},
	}
}()

var linkedFileExtensions = map[string]string{
	"application/pdf":  ".pdf",
	"application/json": ".json", "text/json": ".json",
	"text/markdown": ".md", "text/x-markdown": ".md",
	"text/javascript": ".js", "application/javascript": ".js", "application/x-javascript": ".js",
	"text/html": ".html", "text/css": ".css",
	"application/xml": ".xml", "text/xml": ".xml",
	"application/yaml": ".yaml", "text/yaml": ".yaml", "application/x-yaml": ".yaml",
	"text/csv": ".csv", "text/tab-separated-values": ".tsv",
	"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/bmp": ".bmp",
	"image/webp": ".webp", "image/tiff": ".tiff", "image/svg+xml": ".svg",
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": ".docx",
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":       ".xlsx",
}

var linkIcon = gfx.Icon{ViewBox: 24, Stroke: 1.7, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(10, 8), gfx.LineTo(13, 5), gfx.CubicTo(17, 1, 23, 7, 19, 11), gfx.LineTo(16, 14),
	gfx.MoveTo(14, 16), gfx.LineTo(11, 19), gfx.CubicTo(7, 23, 1, 17, 5, 13), gfx.LineTo(8, 10),
	gfx.MoveTo(9, 15), gfx.LineTo(15, 9),
}}

type linkedFileState struct {
	open, focus, save bool
	address           ui.Field
	request           *ui.EffectState[linkedFileResult, int]
	problem           string
}

type linkedFileResult struct {
	file openedEditorFile
	data []byte
}

func linkedFileURL(value string) (*url.URL, error) {
	address, err := url.Parse(strings.TrimSpace(value))
	if err != nil || address.Hostname() == "" || address.User != nil ||
		(address.Scheme != "http" && address.Scheme != "https") {
		return nil, errors.New(uiText("请输入有效的 HTTP 或 HTTPS 文件地址"))
	}
	address.Fragment = ""
	return address, nil
}

func (m *editorApp) openLinkWebShortcut(letter rune, modifiers gfx.Modifiers) func() {
	if m.appSettings.open || m.interactionBlocked() {
		return nil
	}
	chord, _ := keymap.FromEvent(gfx.KeyEvent{Text: string(letter), Modifiers: modifiers})
	if m.keyBindings.actionFor(chord) != shortcutOpenLink {
		return nil
	}
	return func() {
		if m.runEditorShortcut(shortcutOpenLink) {
			m.invalidator.Invalidate()
		}
	}
}

func (m *editorApp) openLinkedFileDialog() {
	m.linkedFile = linkedFileState{open: true, focus: true}
	if clipboard, err := app.ClipboardText(); err == nil {
		if _, err := linkedFileURL(clipboard); err == nil {
			m.linkedFile.address.SetText(strings.TrimSpace(clipboard))
			m.linkedFile.address.Select(0, m.linkedFile.address.GetEditor().RuneCount())
		}
	}
}

func (m *editorApp) closeLinkedFileDialog() {
	m.linkedFile = linkedFileState{}
	m.focusActiveSource()
}

func (m *editorApp) linkedFileDialog(c ui.C) {
	state := &m.linkedFile
	if !state.open {
		return
	}
	busy := state.request != nil
	submitted := false
	dismissed := ui.ModalWith(c, &state.open, uiText("打开链接文件"), ui.ModalSpec{Width: 520}, func() {
		field := ui.TextField(c, &state.address, uiText("文件链接"), uiText("粘贴 HTTP 或 HTTPS 链接")).
			FillWidth().Height(38).PadX(12).Radius(9).Disabled(busy)
		if state.focus {
			field.AutoFocus()
			state.focus = false
		}
		submitted = field.Submitted()
		if busy {
			ui.Text(c, uiText("正在打开链接文件…")).Muted().FontSize(12)
			ui.Progress(c.Part("progress"), 0).FillWidth().Indeterminate()
		} else if state.problem != "" {
			ui.Wrap(c, state.problem).FillWidth().FontSize(12).Color(m.theme.Danger)
		} else {
			ui.Text(c, uiText("直接打开时只在内存中保留文件。")).Muted().FontSize(12)
		}
	}, func() {
		if ui.Button(c, uiText("取消")).Ghost().Clicked() {
			c.After(m.closeLinkedFileDialog)
		}
		_, composing := state.address.GetEditor().Composition()
		ready := !busy && !composing && strings.TrimSpace(state.address.Text()) != ""
		if ui.Button(c, uiText("保存并打开…")).Outline().Disabled(!ready).Clicked() {
			c.After(func() { m.startLinkedFile(true) })
		}
		if ui.Button(c, uiText("打开")).Icon(linkIcon).Disabled(!ready).Clicked() || ready && submitted {
			c.After(func() { m.startLinkedFile(false) })
		}
	})
	if dismissed {
		m.closeLinkedFileDialog()
	}
}

func (m *editorApp) startLinkedFile(save bool) {
	state := &m.linkedFile
	address, err := linkedFileURL(state.address.Text())
	if err != nil {
		state.problem = err.Error()
		return
	}
	state.save, state.problem = save, ""
	state.request = ui.NewEffect[linkedFileResult, int](func(ctx context.Context) (linkedFileResult, error) {
		result, err := readLinkedFile(ctx, linkedFileHTTP, address)
		if !save {
			result.data = nil
		}
		return result, err
	})
}

func (m *editorApp) updateLinkedFile(c ui.C) {
	state := &m.linkedFile
	if state.request == nil {
		return
	}
	request := state.request
	ui.Effect(c, request, 0)
	result, err, done := request.Take()
	if !done {
		return
	}
	if err != nil {
		state.request, state.problem, state.focus = nil, err.Error(), true
		return
	}
	open := func() {
		if state.request != request || m.windowClosed || (m.desktop != nil && m.desktop.exiting) {
			return
		}
		defer m.invalidator.Invalidate()
		if state.save {
			m.saveLinkedFile(result)
			return
		}
		m.closeLinkedFileDialog()
		tab := m.openLoadedFile(result.file)
		if tab.format() == "HTML" {
			tab.htmlMode = "编辑"
		}
		if tab.doc.path == "" {
			m.status = uiText("已临时打开链接文件 · ") + tab.title()
		} else {
			m.status = uiText("已保存并打开 · ") + tab.title()
		}
	}
	if m.post != nil {
		m.post(open)
	} else {
		open()
	}
}

func (m *editorApp) saveLinkedFile(result linkedFileResult) {
	state := &m.linkedFile
	state.request, state.save, state.focus = nil, false, true
	name := result.file.name
	path, err := app.SaveFile(app.SaveFileOptions{
		Title: uiText("保存链接文件"), SuggestedName: name, DefaultExtension: strings.TrimPrefix(filepath.Ext(name), "."),
		Filters: []app.FileFilter{{Name: uiText("所有文件"), Pattern: "*.*"}},
	})
	if errors.Is(err, app.ErrCanceled) {
		return
	}
	if err == nil {
		path, err = filepath.Abs(path)
	}
	if err != nil {
		state.problem = err.Error()
		return
	}
	if m.tabAtPath(path) != nil {
		state.problem = uiText("该文件已在编辑器中打开，请选择其他保存位置")
		return
	}
	state.request = ui.NewEffect[linkedFileResult, int](func(ctx context.Context) (linkedFileResult, error) {
		err := writeFileAtomically(path, func(output io.Writer) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if _, err := output.Write(result.data); err != nil {
				return err
			}
			return ctx.Err()
		})
		if err != nil {
			return linkedFileResult{}, fmt.Errorf(uiText("无法保存链接文件：%w"), err)
		}
		result.data, result.file.doc.path = nil, path
		if result.file.preview != nil {
			result.file.preview.pdfBytes = nil
		}
		return result, nil
	})
}

func readLinkedFile(ctx context.Context, client *http.Client, address *url.URL) (linkedFileResult, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address.String(), nil)
	if err != nil {
		return linkedFileResult{}, err
	}
	response, err := client.Do(request)
	if err != nil {
		var requestError *url.Error
		if errors.As(err, &requestError) {
			err = requestError.Err
		}
		return linkedFileResult{}, fmt.Errorf(uiText("无法读取链接文件：%w"), err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return linkedFileResult{}, fmt.Errorf(uiText("链接返回 HTTP %d"), response.StatusCode)
	}
	if response.ContentLength > linkedFileLimit {
		return linkedFileResult{}, errors.New(uiText("链接文件超过 128 MiB"))
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, linkedFileLimit+1))
	if err != nil {
		return linkedFileResult{}, fmt.Errorf(uiText("无法读取链接文件：%w"), err)
	}
	if len(data) > linkedFileLimit {
		return linkedFileResult{}, errors.New(uiText("链接文件超过 128 MiB"))
	}
	if err := ctx.Err(); err != nil {
		return linkedFileResult{}, err
	}
	name := linkedFileName(address, response, data[:min(len(data), 512)])
	if ext := filepath.Ext(name); (ext == "" || strings.EqualFold(ext, ".bin")) && bytes.HasPrefix(data, []byte("PK")) {
		if kind := linkedOfficeExtension(data); kind != "" {
			name = strings.TrimSuffix(name, ext) + kind
		}
	}
	file, err := decodeLinkedFile(name, data)
	if err != nil {
		return linkedFileResult{}, fmt.Errorf(uiText("无法打开链接文件：%w"), err)
	}
	if err := ctx.Err(); err != nil {
		return linkedFileResult{}, err
	}
	return linkedFileResult{file: file, data: data}, nil
}

func decodeLinkedFile(name string, data []byte) (openedEditorFile, error) {
	file := openedEditorFile{name: name}
	var err error
	kind := previewFormat(name)
	ext := strings.ToLower(filepath.Ext(name))
	switch {
	case kind == "PDF":
		file.preview = &documentPreview{kind: kind, pdfBytes: data}
		err = validatePDFHeader(data)
	case kind == "Word" || kind == "XMind":
		var book *zip.Reader
		book, err = zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return openedEditorFile{}, err
		}
		file.preview = &documentPreview{kind: kind}
		if kind == "Word" {
			file.preview.word, err = decodeWordPreview(book)
		} else {
			file.preview.mindMap, err = decodeMindMap(book)
		}
	case officeKind(name) != "":
		file.doc, file.office, err = decodeOffice(name, data)
	case ext == ".xls" || ext == ".doc":
		err = errLegacyOfficeFormat
	case isImageFile(name):
		file.picture, err = decodeImage(bytes.NewReader(data), int64(len(data)))
	default:
		file.doc, err = decodeDocument("", data)
	}
	return file, err
}

func linkedFileName(address *url.URL, response *http.Response, firstBytes []byte) string {
	_, params, _ := mime.ParseMediaType(response.Header.Get("Content-Disposition"))
	name := params["filename"]
	explicit := name != ""
	if !explicit {
		name = path.Base(address.Path)
		if response.Request != nil && response.Request.URL != nil {
			final := path.Base(response.Request.URL.Path)
			if filepath.Ext(final) != "" || filepath.Ext(name) == "" {
				name = final
			}
		}
	}
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune("<>:\"/\\|?*", r) {
			return '_'
		}
		return r
	}, name)
	name = strings.Trim(name, " .")
	if name == "" || name == "_" || name == "." || name == ".." {
		name = "链接文件"
	}

	contentType, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	extension := linkedFileExtensions[contentType]
	if extension == "" && (contentType == "" || contentType == "application/octet-stream") {
		detected, _, _ := mime.ParseMediaType(http.DetectContentType(firstBytes))
		extension = linkedFileExtensions[detected]
	}
	oldExtension := filepath.Ext(name)
	if extension != "" && (oldExtension == "" || strings.EqualFold(oldExtension, ".bin") || !explicit) {
		name = strings.TrimSuffix(name, oldExtension) + extension
	}
	if !filepath.IsLocal(name) {
		name = "_" + name
	}
	ext := filepath.Ext(name)
	if len(ext) > 20 {
		ext = ""
	}
	stem := []rune(strings.TrimSuffix(name, ext))
	return string(stem[:min(len(stem), 100)]) + ext
}

func linkedOfficeExtension(data []byte) string {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return ""
	}
	for _, file := range archive.File {
		switch file.Name {
		case "word/document.xml":
			return ".docx"
		case "xl/workbook.xml":
			return ".xlsx"
		}
	}
	return ""
}
