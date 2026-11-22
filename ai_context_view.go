package main

import (
	"errors"
	"path/filepath"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

func (m *editorApp) aiAttachmentChips(c ui.C, blocked bool) {
	if len(m.ai.attachments) == 0 {
		return
	}
	ui.Scroll(c).Sideways().Scrollbars().FillWidth().Height(30).Children(func() {
		ui.Row(c).Gap(5).Items(ui.Center).Children(func() {
			for i, item := range m.ai.attachments {
				place := c.KeyInt(i)
				label, detail, selection := aiPathName(item.path), item.path, false
				icon := fileIcon
				switch {
				case item.directory:
					icon = folderIcon
				case item.tabID != 0:
					tab := m.aiTab(item.tabID)
					if tab == nil {
						continue
					}
					label, detail = tab.title(), tab.doc.path
					selected := tab.source.text.Selection()
					selection = item.automatic && selected.Start != selected.End
					if selection {
						detail = "正文选区 · " + label
						icon = codeIcon
					}
				}
				chip := ui.Row(place).Height(26).PadLeft(7).Gap(4).Items(ui.Center).Radius(8).Background(m.theme.CardElevated)
				chip.HitWith(ui.ID(place.Part("chip")), label, gfx.HitOptions{Role: gfx.RoleGroup, SkipFocus: true}).Tooltip(detail)
				chip.Children(func() {
					ui.Icon(place, icon).Size(13, 13).Color(m.theme.MutedForeground)
					caption := label
					if selection {
						caption += " · 选区"
					}
					ui.Text(place, caption).MaxWidth(118).Ellipsis().FontSize(11)
					if ui.Button(place, "移除附件 "+label).Icon(closeIcon).IconOnly().Ghost().Size(22, 22).Disabled(blocked).Clicked() {
						c.After(func() { m.removeAIAttachment(item) })
					}
				})
			}
		})
	})
}

func (m *editorApp) aiContextPicker(c ui.C, blocked bool) {
	a := &m.ai
	add := ui.Button(c, "添加文件上下文").Icon(plusIcon).IconOnly().Outline().Size(32, 32).Radius(16).
		Tooltip("添加文件或文件夹").Disabled(blocked)
	if add.Clicked() {
		a.contextMenu = !a.contextMenu
	}
	ui.PopoverWith(c, add, ui.PopoverSpec{
		Side: ui.PopoverTop, Align: ui.Start, Width: min(340, c.Bounds().Width-24), Offset: 8, Padding: 10, Radius: 14, Instant: true,
	}, &a.contextMenu, func() {
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			ui.Row(c).FillWidth().Gap(6).Children(func() {
				if ui.Button(c, "选择文件…").Icon(fileIcon).Outline().Height(32).Grow(1).Clicked() {
					c.After(func() { m.pickAIAttachments(false) })
				}
				if ui.Button(c, "选择文件夹…").Icon(folderIcon).Outline().Height(32).Grow(1).Clicked() {
					c.After(func() { m.pickAIAttachments(true) })
				}
			})
			count := 0
			for _, tab := range m.tabs {
				if aiTextTab(tab) {
					count++
				}
			}
			if count > 0 {
				ui.Text(c, "已打开的文件").FontSize(12).Muted()
				ui.Scroll(c).Height(min(155, float32(count)*30)).FillWidth().Scrollbars().Children(func() {
					ui.Column(c).FillWidth().Children(func() {
						for _, tab := range m.tabs {
							if !aiTextTab(tab) {
								continue
							}
							item := aiAttachment{tabID: tab.id}
							checked := m.aiAttachmentIndex(item) >= 0
							if ui.Checkbox(c.KeyInt(tab.id), &checked, tab.title()).Height(30).FillWidth().
								FontSize(13).Tooltip(tab.doc.path).Changed() {
								c.After(func() {
									if checked {
										m.addAIAttachment(item)
									} else {
										m.removeAIAttachment(item)
									}
								})
							}
						}
					})
				})
				if ui.Button(c, "完成选择").Ghost().Height(30).FillWidth().Clicked() {
					a.contextMenu, a.focusPrompt = false, true
				}
			}
		})
	})
}

func (m *editorApp) pickAIAttachments(directory bool) {
	a := &m.ai
	a.contextMenu, a.focusPrompt = false, true
	var paths []string
	var err error
	if directory {
		paths, err = app.OpenDirectories(app.OpenDirectoryOptions{Title: "选择要附加的文件夹"})
	} else {
		paths, err = app.OpenFiles(app.OpenFileOptions{Title: "选择要附加的文件", AllowMultiple: true})
	}
	if errors.Is(err, app.ErrCanceled) {
		return
	}
	if err != nil {
		a.message = err.Error()
		return
	}
	a.message = ""
	for _, selected := range paths {
		path, err := filepath.Abs(selected)
		if err != nil {
			a.message = err.Error()
			continue
		}
		item := aiAttachment{path: path, directory: directory}
		if !directory {
			if !aiTextPath(path) {
				a.message = "AI 助手支持 UTF-8 文本文件 · " + filepath.Base(path)
				continue
			}
			if tab := m.tabAtPath(path); aiTextTab(tab) {
				item = aiAttachment{tabID: tab.id}
			}
		}
		m.addAIAttachment(item)
	}
}
