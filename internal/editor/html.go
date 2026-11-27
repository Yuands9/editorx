package editor

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
)

func (m *editorApp) openHTML(t *editorTab) {
	if t.format() != "HTML" || m.browser != nil {
		return
	}
	if _, composing := t.source.text.Composition(); composing {
		m.fail(errors.New(uiText("请先完成中文选词，再打开页面")))
		return
	}
	if t.doc.path == "" || t.modified() {
		if !m.saveTab(t, false) {
			return
		}
	}
	path := t.doc.path
	// 浏览器启动可能等待系统响应，交给 Viva 的后台 effect，避免阻塞编辑和拖动。
	m.browser = ui.NewEffect[string, int](func(ctx context.Context) (string, error) {
		if err := ctx.Err(); err != nil {
			return path, err
		}
		return path, app.OpenPathInBrowser(path)
	})
}

func (m *editorApp) browserUpdate(c ui.C) {
	if m.browser == nil {
		return
	}
	ui.Effect(c, m.browser, 0)
	if path, err, done := m.browser.Take(); done {
		m.browser = nil
		if err != nil {
			m.fail(err)
			return
		}
		m.status = uiText("已在浏览器打开 ") + filepath.Base(path)
	}
}
