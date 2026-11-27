package editor

import (
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"github.com/vivaui/viva/ui"
	htmlparser "golang.org/x/net/html"
)

type htmlPreview struct {
	view      *app.WebView
	file      string
	source    string
	path      string
	errorText string
	scroll    gfx.Point
	ready     bool
}

func fileURL(path string) string {
	return (&url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(path)}).String()
}

// 快速排除常见的无 base 页面。可能出现在注释或脚本中的候选仍交给分词器判断。
func mayContainHTMLBase(source string) bool {
	for {
		index := strings.IndexByte(source, '<')
		if index < 0 || len(source)-index < len("<base") {
			return false
		}
		source = source[index+1:]
		if (source[0] == 'b' || source[0] == 'B') && strings.EqualFold(source[:len("base")], "base") {
			return true
		}
	}
}

// 仅改预览副本的 base，不改编辑内容。分词器避免把注释或脚本字符串中的标签当成 head/base。
func previewHTML(source, originalPath string) string {
	base, _ := url.Parse(fileURL(originalPath))
	tokenizer := htmlparser.NewTokenizer(strings.NewReader(source))
	offset, insert, baseStart, baseEnd := 0, 0, -1, 0
	hasHead, hasBase := false, false
	replacement := ""
	for {
		kind := tokenizer.Next()
		if kind == htmlparser.ErrorToken {
			break
		}
		start := offset
		offset += len(tokenizer.Raw())
		if kind == htmlparser.DoctypeToken && !hasHead {
			insert = offset
		}
		if kind != htmlparser.StartTagToken && kind != htmlparser.SelfClosingTagToken {
			continue
		}
		name, moreAttributes := tokenizer.TagName()
		if string(name) == "html" && !hasHead {
			insert = offset
		}
		if string(name) == "head" && !hasHead {
			insert, hasHead = offset, true
			if hasBase || !mayContainHTMLBase(source[offset:]) {
				break
			}
		}
		if string(name) != "base" || hasBase {
			continue
		}
		token := htmlparser.Token{Type: kind, Data: "base"}
		for moreAttributes {
			var key, value []byte
			key, value, moreAttributes = tokenizer.TagAttr()
			token.Attr = append(token.Attr, htmlparser.Attribute{Key: string(key), Val: string(value)})
		}
		for i, attr := range token.Attr {
			if attr.Key != "href" {
				continue
			}
			hasBase = true
			reference, err := url.Parse(attr.Val)
			if err != nil || originalPath == "" {
				break
			}
			token.Attr[i].Val = base.ResolveReference(reference).String()
			baseStart, baseEnd, replacement = start, offset, token.String()
			break
		}
		if hasHead && hasBase {
			break
		}
	}
	if baseStart >= 0 {
		source = source[:baseStart] + replacement + source[baseEnd:]
		if insert >= baseEnd {
			insert += len(replacement) - (baseEnd - baseStart)
		}
	}
	tag := `<meta charset="utf-8">`
	if !hasBase && originalPath != "" {
		tag += `<base href="` + html.EscapeString(base.String()) + `">`
	}
	return source[:insert] + tag + source[insert:]
}

// 预览可从编辑缓冲重建，不必像用户文件那样等待磁盘持久化；原子替换仍避免浏览器读到半份内容。
func writeHTMLPreview(path, source string) error {
	file, err := createEditorPreviewCache(filepath.Dir(path), editorPreviewWritePattern)
	if err != nil {
		return err
	}
	temporaryPath := file.Name()
	defer removeEditorPreviewCache(temporaryPath)
	if _, err = file.WriteString(source); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func (m *editorApp) queueHTMLPreview(t *editorTab) {
	if !m.webReady || m.post == nil || !t.needsHTMLPreview() {
		return
	}
	// Wait until a batch of file opens finishes, so only its selected tab starts a preview.
	m.post(func() {
		if !m.windowClosed && m.active == t && t.needsHTMLPreview() {
			m.refreshHTML(t)
			m.invalidator.Invalidate()
		}
	})
}

func (t *editorTab) needsHTMLPreview() bool {
	return t != nil && t.htmlMode != "编辑" && t.html.view == nil && t.html.errorText == "" && t.format() == "HTML"
}

func (m *editorApp) refreshHTML(t *editorTab) {
	if !m.webReady || t.format() != "HTML" {
		return
	}
	if _, composing := t.source.text.Composition(); composing {
		m.status = uiText("请先完成中文选词，再刷新预览")
		return
	}
	source := t.source.text.String()
	if t.html.file == "" {
		file, err := m.writePreviewFile(previewHTML(source, t.doc.path))
		if err != nil {
			t.html.errorText = err.Error()
			return
		}
		t.html.file = file
	} else if source != t.html.source || t.doc.path != t.html.path {
		if err := writeHTMLPreview(t.html.file, previewHTML(source, t.doc.path)); err != nil {
			t.html.errorText = err.Error()
			return
		}
	}
	t.html.source, t.html.path = source, t.doc.path
	t.html.errorText, t.html.ready = "", false
	address := fileURL(t.html.file)
	if t.html.view == nil {
		view, err := m.webHost.Open(app.WebViewConfig{URL: address, Ephemeral: true, TopCornerRadius: 12, BridgeMainFrameOnly: true,
			LetterShortcut: func(letter rune, modifiers gfx.Modifiers) func() {
				if m.active != t || m.appSettings.open || m.interactionBlocked() {
					return nil
				}
				chord, _ := keymap.FromEvent(gfx.KeyEvent{Text: string(letter), Modifiers: modifiers})
				action := m.keyBindings.actionFor(chord)
				if action == shortcutOpenLink {
					return m.openLinkWebShortcut(letter, modifiers)
				}
				if action != shortcutFind && action != shortcutReplace {
					return nil
				}
				return func() {
					if m.active == t && m.runEditorShortcut(action) {
						m.invalidator.Invalidate()
					}
				}
			},
			OnMessage: func(message string) {
				var position struct {
					Kind string
					X, Y float32
				}
				if json.Unmarshal([]byte(message), &position) == nil && position.Kind == "scroll" {
					t.html.scroll = gfx.Point{X: max(0, position.X), Y: max(0, position.Y)}
				}
			},
			OnLoad: func(load app.WebLoad) {
				if load.Phase != app.WebLoadFinished || t.html.view == nil || t.html.file == "" {
					return
				}
				t.html.ready = true
				t.html.view.Eval(fmt.Sprintf(`window.scrollTo(%g,%g);(()=>{let pending=false;const report=()=>window.viva.postMessage(JSON.stringify({Kind:"scroll",X:scrollX,Y:scrollY}));addEventListener("scroll",()=>{if(!pending){pending=true;setTimeout(()=>{pending=false;report()},100)}},{passive:true});report();})();`, t.html.scroll.X, t.html.scroll.Y))
			},
		})
		if err != nil {
			t.html.errorText = fmt.Sprintf(uiText("无法启动内嵌预览，请检查 Microsoft Edge WebView2 Runtime：%v"), err)
			return
		}
		t.html.view = view
	} else {
		t.html.view.Load(address)
	}
	m.status = uiText("已刷新 HTML 预览")
}

func (preview *htmlPreview) close() {
	preview.source, preview.path = "", ""
	preview.ready = false
	if preview.view != nil {
		preview.view.Close()
		preview.view = nil
	}
	if preview.file != "" {
		removeEditorPreviewCache(preview.file)
		preview.file = ""
	}
}

func (m *editorApp) htmlView(c ui.C, t *editorTab, blocked bool) {
	if m.webReady && t.needsHTMLPreview() {
		c.After(func() {
			if t.needsHTMLPreview() {
				m.refreshHTML(t)
			}
		})
	}
	ui.Row(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
		if t.htmlMode != "预览" {
			column := ui.Column(c).FillHeight().MinWidth(0)
			if t.htmlMode == "分屏" {
				column.Width(t.split.Pos)
			} else {
				column.Grow(1)
			}
			column.Children(func() { m.sourceEditor(c, t, blocked).FillWidth().Grow(1) })
		}
		if t.htmlMode == "分屏" {
			ui.Splitter(c, &t.split).Mins(180, 180).SplitterThickness(1)
		}
		if t.htmlMode != "编辑" {
			ui.Column(c).Grow(1).MinWidth(0).FillHeight().Children(func() {
				if t.html.view != nil {
					ui.Slot(c).Fill().NativeView(t.html.view.ID())
					return
				}
				message := "正在准备网页预览…"
				if t.html.errorText != "" {
					message = t.html.errorText
				}
				ui.Text(c, message).FillWidth().Pad(20).FontSize(13).Muted()
				if t.html.errorText != "" && ui.Button(c, uiText("重试网页预览")).MarginX(20).Clicked() {
					c.After(func() { m.refreshHTML(t) })
				}
			})
		}
	})
}
