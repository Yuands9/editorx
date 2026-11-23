package editor

import (
	"archive/zip"
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
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
	open, focus bool
	address     ui.Field
	request     *ui.EffectState[openedEditorFile, int]
	problem     string
}

func linkedFileURL(value string) (*url.URL, error) {
	address, err := url.Parse(strings.TrimSpace(value))
	if err != nil || address.Hostname() == "" || address.User != nil ||
		(address.Scheme != "http" && address.Scheme != "https") {
		return nil, errors.New("请输入有效的 HTTP 或 HTTPS 文件地址")
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
	dismissed := ui.ModalWith(c, &state.open, "打开链接文件", ui.ModalSpec{Width: 520}, func() {
		field := ui.TextFieldBase(c, &state.address, "文件链接", "粘贴 HTTP 或 HTTPS 链接").
			FillWidth().Height(38).PadX(12).Radius(9).Background(m.theme.Background).Disabled(busy)
		if state.focus {
			field.AutoFocus()
			state.focus = false
		}
		submitted = field.Submitted()
		if busy {
			ui.Text(c, "正在下载并打开文件…").Muted().FontSize(12)
			ui.Progress(c.Part("progress"), 0).FillWidth().Indeterminate()
		} else if state.problem != "" {
			ui.Wrap(c, state.problem).FillWidth().FontSize(12).Color(m.theme.Danger)
		}
	}, func() {
		if ui.Button(c, "取消").Ghost().Clicked() {
			c.After(m.closeLinkedFileDialog)
		}
		_, composing := state.address.GetEditor().Composition()
		ready := !busy && !composing && strings.TrimSpace(state.address.Text()) != ""
		if ui.Button(c, "打开").Icon(linkIcon).Disabled(!ready).Clicked() || ready && submitted {
			c.After(m.startLinkedFile)
		}
	})
	if dismissed {
		m.closeLinkedFileDialog()
	}
}

func (m *editorApp) startLinkedFile() {
	state := &m.linkedFile
	address, err := linkedFileURL(state.address.Text())
	if err != nil {
		state.problem = err.Error()
		return
	}
	directory := m.dataDirectory
	if directory == "" {
		directory, err = editorDataDirectory()
		if err != nil {
			state.problem = err.Error()
			return
		}
	}
	state.problem = ""
	state.request = ui.NewEffect[openedEditorFile, int](func(ctx context.Context) (openedEditorFile, error) {
		return readLinkedFile(ctx, linkedFileHTTP, address, filepath.Join(directory, "downloads"))
	})
}

func (m *editorApp) updateLinkedFile(c ui.C) {
	state := &m.linkedFile
	if state.request == nil {
		return
	}
	request := state.request
	ui.Effect(c, request, 0)
	file, err, done := request.Take()
	if !done {
		return
	}
	if err != nil {
		state.request, state.problem, state.focus = nil, err.Error(), true
		return
	}
	open := func() {
		if state.request != request || m.windowClosed || (m.desktop != nil && m.desktop.exiting) {
			_ = os.Remove(file.doc.path)
			_ = os.Remove(filepath.Dir(file.doc.path))
			return
		}
		m.closeLinkedFileDialog()
		tab := m.openLoadedFile(file)
		if tab.format() == "HTML" {
			tab.htmlMode = "编辑"
		}
		m.status = "已打开链接文件 · " + tab.title()
		m.invalidator.Invalidate()
	}
	if m.post != nil {
		m.post(open)
	} else {
		open()
	}
}

func readLinkedFile(ctx context.Context, client *http.Client, address *url.URL, directory string) (openedEditorFile, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address.String(), nil)
	if err != nil {
		return openedEditorFile{}, err
	}
	response, err := client.Do(request)
	if err != nil {
		var requestError *url.Error
		if errors.As(err, &requestError) {
			err = requestError.Err
		}
		return openedEditorFile{}, fmt.Errorf("下载文件失败：%w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return openedEditorFile{}, fmt.Errorf("链接返回 HTTP %d", response.StatusCode)
	}
	if response.ContentLength > linkedFileLimit {
		return openedEditorFile{}, errors.New("链接文件超过 128 MiB")
	}

	stream := bufio.NewReader(response.Body)
	firstBytes, _ := stream.Peek(512)
	name := linkedFileName(address, response, firstBytes)
	isZIP := len(firstBytes) >= 2 && firstBytes[0] == 'P' && firstBytes[1] == 'K'
	if err := os.MkdirAll(directory, 0700); err != nil {
		return openedEditorFile{}, err
	}
	folder, err := os.MkdirTemp(directory, "link-")
	if err != nil {
		return openedEditorFile{}, err
	}
	filename := filepath.Join(folder, name)
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(filename)
			_ = os.Remove(folder)
		}
	}()
	file, err := os.Create(filename)
	if err != nil {
		return openedEditorFile{}, err
	}
	size, err := io.Copy(file, io.LimitReader(stream, linkedFileLimit+1))
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return openedEditorFile{}, errors.Join(err, closeErr)
	}
	if size > linkedFileLimit {
		return openedEditorFile{}, errors.New("链接文件超过 128 MiB")
	}
	if ext := strings.ToLower(filepath.Ext(name)); (ext == "" || ext == ".bin") && isZIP {
		if kind := linkedOfficeExtension(filename); kind != "" {
			typed := strings.TrimSuffix(filename, filepath.Ext(filename)) + kind
			if err := os.Rename(filename, typed); err != nil {
				return openedEditorFile{}, err
			}
			filename = typed
		}
	}
	if err := ctx.Err(); err != nil {
		return openedEditorFile{}, err
	}
	result, err := readEditorFile(filename)
	if err != nil {
		return openedEditorFile{}, fmt.Errorf("无法打开下载的文件：%w", err)
	}
	if err := ctx.Err(); err != nil {
		return openedEditorFile{}, err
	}
	keep = true
	return result, nil
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

func linkedOfficeExtension(filename string) string {
	archive, err := zip.OpenReader(filename)
	if archive != nil {
		defer archive.Close()
	}
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
