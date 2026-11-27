package editor

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
)

func (m *editorApp) projectPathActions(c ui.C, menu *ui.MenuItems, path string) {
	menu.Sub(m.menuText("复制路径"), func(items *ui.MenuItems) {
		root := m.projectRoot(path)
		relative, _ := filepath.Rel(root, path)
		for _, option := range []struct {
			label, value string
			disabled     bool
		}{
			{"完整路径", path, false}, {"相对项目路径", relative, root == ""}, {"文件名", filepath.Base(path), false},
		} {
			if items.Item(m.menuText(option.label)).Icon(copyIcon).Disabled(option.disabled).Clicked() {
				c.After(func() {
					if err := app.SetClipboardText(option.value); err != nil {
						m.fail(err)
					}
				})
			}
		}
	}).Icon(copyIcon)
}

func (m *editorApp) revealProjectFile(path string) {
	root := m.projectRoot(path)
	if root == "" {
		m.status = uiText("当前文件不在已打开的项目中")
		return
	}
	m.showFiles = true
	m.workspace.project(root).expanded = true
	m.refreshProject(m.workspace.project(root), path)
}

func (m *editorApp) previewProjectFile(path string) {
	now := time.Now()
	if tab := m.tabAtPath(path); tab != nil {
		if strings.EqualFold(path, m.lastPreviewPath) && now.Sub(m.lastPreviewAt) < 500*time.Millisecond {
			tab.projectPreview = false
		}
		m.lastPreviewPath, m.lastPreviewAt = path, now
		m.activate(tab)
		return
	}
	m.lastPreviewPath, m.lastPreviewAt = path, now
	m.loadTextFile(path, "", func(tab *editorTab) {
		if tab == nil {
			return
		}
		for _, old := range append([]*editorTab(nil), m.tabs...) {
			if old == tab || !old.projectPreview {
				continue
			}
			if old.modified() || old.hasResultDraft() || old.pinned || old.group != nil || old.savingText != nil {
				old.projectPreview = false
			} else {
				m.removeTab(old)
			}
		}
		tab.projectPreview = !tab.modified() && !tab.pinned
	})
}
