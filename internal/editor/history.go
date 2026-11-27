package editor

import (
	"path/filepath"
	"slices"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

func (m *editorApp) historyDialog(c ui.C) {
	if !m.showHistory || m.store == nil {
		return
	}
	ui.ModalWith(c, &m.showHistory, uiText("最近打开"), ui.ModalSpec{Width: 760, Description: uiText("最近 30 个文件或文件夹；点击重新打开")}, func() {
		ui.Scroll(c).FillWidth().Height(340).Children(func() {
			ui.Column(c).FillWidth().Gap(4).Children(func() {
				for i, item := range m.store.history {
					ui.Row(c.KeyInt(i)).FillWidth().Gap(8).Items(ui.Center).Children(func() {
						row := ui.Row(c).Grow(1).MinWidth(0).Height(56).PadX(10).Gap(10).Items(ui.Center).Radius(10)
						row.HitWith(ui.ID(c), uiText("打开历史 ")+item.Path, gfx.HitOptions{Role: gfx.RoleButton}).Hover(ui.Look{}.Background(m.theme.Hover)).Tooltip(item.Path)
						row.Children(func() {
							icon, color := m.fileAppearance(item.Path)
							if item.Directory {
								icon = folderIcon
							}
							ui.Icon(c, icon).Width(18).Height(18).Color(color)
							ui.Column(c).Grow(1).MinWidth(0).Children(func() {
								ui.Text(c, filepath.Base(item.Path)).FontSize(14).Ellipsis().FillWidth()
								ui.Text(c, item.Path).FontSize(11).Muted().Ellipsis().FillWidth()
							})
							ui.Text(c, item.OpenedAt.Format("01-02 15:04")).FontSize(11).Muted()
						})
						if row.Clicked() {
							c.After(func() {
								m.showHistory = false
								if item.Directory {
									m.addProjects(item.Path)
								} else {
									m.load(item.Path)
								}
							})
						}
						if ui.Button(c, uiText("移除历史 ")+item.Path).Icon(closeIcon).IconOnly().Ghost().Width(28).Tooltip(uiText("移除此记录")).Clicked() {
							c.After(func() { m.store.history = slices.Delete(m.store.history, i, i+1) })
						}
					})
				}
				if len(m.store.history) == 0 {
					ui.Text(c, uiText("暂无打开记录")).Pad(20).Muted()
				}
			})
		})
	}, func() {
		if ui.Button(c, uiText("清空打开历史")).Outline().Disabled(len(m.store.history) == 0).Clicked() {
			m.store.history = nil
		}
		if ui.Button(c, uiText("关闭历史")).Clicked() {
			m.showHistory = false
		}
	})
}

func (m *editorApp) openingPage(c ui.C, blocked bool) {
	hint := m.shortcutHint(uiText("打开已有文件或新建文件"), shortcutNewFile)
	if m.active != nil {
		hint = uiText("直接输入内容，或打开已有文件")
	}
	ui.Column(c).Fill().Center().Pad(24).Children(func() {
		ui.Column(c).Width(520).MaxWidthPercent(100).Gap(8).Items(ui.Center).Children(func() {
			ui.Text(c, uiText("开始编辑")).FontSize(22).Bold()
			ui.Text(c, hint).FontSize(13).Muted()
			ui.Flow(c).FillWidth().Justify(ui.Center).Gap(10).LineGap(8).MarginTop(10).MarginBottom(12).Children(func() {
				if ui.Button(c, uiText("打开项目")).Icon(folderIcon).Outline().Height(40).PadX(16).Radius(12).
					Disabled(blocked).Tooltip(m.shortcutHint(uiText("打开项目"), shortcutOpenProject)).Clicked() {
					c.After(m.openFolder)
				}
				if ui.Button(c, uiText("打开文件")).Icon(fileIcon).Outline().Height(40).PadX(16).Radius(12).Disabled(blocked).Tooltip(m.shortcutHint(uiText("打开文件"), shortcutOpenFile)).Clicked() {
					c.After(m.open)
				}
				if ui.Button(c, uiText("最近打开文件")).Icon(folderIcon).Outline().Height(40).PadX(16).Radius(12).Disabled(blocked || m.store == nil).
					Tooltip(m.shortcutHint(uiText("最近打开文件"), shortcutRecentFiles)).Clicked() {
					m.showHistory = true
				}
			})
			if m.quickRecentFiles(c.Part("recent"), 52, blocked) == 0 {
				ui.Text(c, uiText("打开的文件会显示在这里")).FontSize(12).Muted()
			}
		})
	})
}

func (m *editorApp) quickRecentFiles(c ui.C, rowHeight float32, blocked bool) int {
	if m.store == nil {
		return 0
	}
	count := 0
	for _, item := range m.store.history {
		if item.Directory {
			continue
		}
		place := c.KeyInt(count)
		row := ui.Row(place).FillWidth().Height(rowHeight).PadX(12).Gap(12).Items(ui.Center).Radius(12)
		row.HitWith(ui.ID(place), uiText("最近文件 ")+item.Path, gfx.HitOptions{Role: gfx.RoleButton, Disabled: blocked}).Tooltip(item.Path).
			Hover(ui.Look{}.Background(m.theme.Hover)).Focus(ui.Look{}.Background(m.theme.Hover))
		row.Children(func() {
			icon, color := m.fileAppearance(item.Path)
			ui.Icon(place, icon).Size(18, 18).Color(color).Shrink(0)
			ui.Column(place).Grow(1).MinWidth(0).Gap(3).Children(func() {
				ui.Text(place, filepath.Base(item.Path)).FontSize(14).FillWidth().Ellipsis()
				ui.Text(place, filepath.Dir(item.Path)).FontSize(11).Muted().Ellipsis().FillWidth()
			})
			ui.Icon(place, chevronRightIcon).Size(14, 14).Color(m.theme.MutedForeground).Shrink(0)
		})
		if row.Clicked() && !blocked {
			c.After(func() { m.load(item.Path) })
		}
		count++
		if count == 3 {
			break
		}
	}
	return count
}
