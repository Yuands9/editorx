package editor

import (
	"slices"
	"strings"

	"github.com/vivaui/viva/ui"
)

func (m *editorApp) projectRemovalDialog(c ui.C) {
	root := m.projectRemoval
	if root == nil {
		return
	}
	open := true
	cancel := func() {
		m.projectRemoval = nil
		m.focusActiveSource()
	}
	ui.ModalWith(c, &open, uiText("移除项目？"), ui.ModalSpec{Width: 460}, func() {
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			ui.Wrap(c, root.path).FillWidth()
			ui.Wrap(c, uiText("移除项目会关闭该项目的标签页。项目文件仍保留在磁盘上。")).FillWidth().Muted().FontSize(12)
		})
	}, func() {
		if ui.Button(c.Part("cancel"), uiText("取消")).Outline().AutoFocus().Clicked() {
			c.After(cancel)
		}
		if ui.Button(c.Part("remove"), uiText("确认移除")).Clicked() {
			c.After(func() {
				m.projectRemoval = nil
				m.closeProjectTabsAndRemove(root)
			})
		}
	})
	if !open {
		cancel()
	}
}

func (m *editorApp) closeProjectTabsAndRemove(root *fileNode) {
	if m.workspace == nil || !slices.Contains(m.workspace.roots, root) {
		return
	}
	var tabs []*editorTab
	for _, tab := range m.tabs {
		if strings.EqualFold(m.projectRoot(tab.doc.path), root.path) {
			tabs = append(tabs, tab)
		}
	}
	// Keep every tab open until all save/discard choices are made. Cancel abandons the removal.
	m.reviewTabClosures(tabs, false, nil, func() {
		for _, tab := range tabs {
			m.removeTab(tab)
		}
		m.removeProject(root)
	})
}
