package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/vivaui/viva/app"
)

func (m *editorApp) openPDFPreview(t *editorTab) {
	preview := t.preview
	file, err := os.Open(t.doc.path)
	if err != nil {
		preview.issue = err.Error()
		return
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		preview.issue = err.Error()
		return
	}
	assets, _ := fs.Sub(officeAssets, "assets/office")
	server, address, err := serveLocalDocument(assets, func(w http.ResponseWriter, r *http.Request, path string) bool {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self' 'wasm-unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self' blob: data:; connect-src 'self'; worker-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return true
		}
		switch path {
		case "", "index.html":
			serveEditorPage(w, r, assets, "pdf.html")
		case "document.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			http.ServeContent(w, r, "document.pdf", info.ModTime(), io.NewSectionReader(file, 0, info.Size()))
		default:
			return false
		}
		return true
	})
	if err != nil {
		_ = file.Close()
		preview.issue = err.Error()
		return
	}
	preview.server, preview.pdf = server, file
	location := address
	if m.light {
		location += "?theme=light"
	}
	view, err := m.webHost.Open(app.WebViewConfig{
		URL: location, Ephemeral: true, BridgeMainFrameOnly: true, TopCornerRadius: 12,
		Navigate: func(nav app.WebNavigation) bool {
			page, _, _ := strings.Cut(nav.URL, "#")
			return page == location
		},
		NewWindow: func(address string) bool {
			return strings.HasPrefix(address, "https://") || strings.HasPrefix(address, "http://")
		},
		OnMessage: func(raw string) {
			var message struct{ Kind, Text string }
			if json.Unmarshal([]byte(raw), &message) != nil {
				return
			}
			switch message.Kind {
			case "ready":
				preview.ready = true
				preview.view.Eval(previewThemeJS(m.light))
				m.status = "PDF 预览已就绪"
			case "error":
				preview.issue = message.Text
				m.status = "PDF：" + message.Text
			case "external":
				if err := app.OpenPath(t.doc.path); err != nil {
					m.fail(err)
				}
			case "save-copy":
				m.savePDFCopy(t, file, info.Size())
			}
		},
	})
	if err != nil {
		preview.close()
		preview.issue = "无法打开 PDF 预览：" + err.Error()
		return
	}
	preview.view = view
	m.invalidator.Invalidate()
}

func (m *editorApp) savePDFCopy(t *editorTab, source io.ReaderAt, size int64) {
	path, err := app.SaveFile(app.SaveFileOptions{
		Title: "另存 PDF 原件", SuggestedName: t.title(), Filters: []app.FileFilter{{Name: "PDF 文档", Pattern: "*.pdf"}},
	})
	if errors.Is(err, app.ErrCanceled) {
		return
	}
	if err != nil {
		m.fail(err)
		return
	}
	if filepath.Ext(path) == "" {
		path += ".pdf"
	}
	if strings.EqualFold(filepath.Clean(path), t.doc.path) {
		m.status = "PDF 已在原位置"
		return
	}
	if err := writeFileFrom(path, io.NewSectionReader(source, 0, size), size); err != nil {
		m.fail(fmt.Errorf("无法保存 PDF 原件：%w", err))
		return
	}
	m.status = "已保存 " + filepath.Base(path)
}
