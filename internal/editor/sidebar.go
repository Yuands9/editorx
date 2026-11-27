package editor

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

func (m *editorApp) toggleFiles() {
	m.showFiles = !m.showFiles
	m.fileMenu = false
	m.focusActiveSource()
}

func (m *editorApp) sidebar(c ui.C, t *editorTab, blocked bool) {
	ui.Row(c).FillWidth().Height(60).PadX(8).Gap(8).Items(ui.Center).Children(func() {
		m.mainMenu(c, t, blocked)
		ui.Text(c, applicationName).FontSize(15).Bold().Grow(1)
		if ui.Button(c, m.menuText("收起文件栏")).Icon(leftPanelIcon).IconOnly().Ghost().Width(32).Height(32).Tooltip(m.shortcutHint(uiText("关闭左侧主菜单"), shortcutFiles)).Disabled(blocked).Clicked() {
			c.After(m.toggleFiles)
		}
	})
	ui.Column(c).FillWidth().PadX(10).Gap(6).Children(func() {
		for _, action := range []struct {
			label    string
			shortcut editorShortcut
			icon     gfx.Icon
			run      func()
		}{
			{"打开文件", shortcutOpenFile, fileIcon, m.open},
			{"添加项目", shortcutOpenProject, folderIcon, m.openFolder},
		} {
			label := m.menuText(action.label)
			row := ui.Row(c).FillWidth().Height(42).PadX(10).Gap(10).Items(ui.Center).Radius(14)
			row.HitWith(ui.ID(c), label, gfx.HitOptions{Role: gfx.RoleButton, Disabled: blocked}).Tooltip(m.shortcutHint(label, action.shortcut)).Hover(ui.Look{}.Background(m.theme.Hover)).Focus(ui.Look{}.Background(m.theme.Hover))
			row.Children(func() {
				ui.Icon(c, action.icon).Width(18).Height(18).Color(m.theme.Foreground)
				ui.Text(c, label).FontSize(14).Grow(1).MinWidth(0).Ellipsis()
				if shortcut := m.keyBindings.chord(action.shortcut).Label(); shortcut != "" {
					ui.Text(c, shortcut).FontSize(11).Muted()
				}
			})
			if row.Clicked() && !blocked {
				c.After(action.run)
			}
		}
	})
	ui.Slot(c).Height(12)
	m.filePanel(c, blocked)
}
