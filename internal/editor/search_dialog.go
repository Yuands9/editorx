package editor

import (
	"fmt"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

func (m *editorApp) searchDialog(c ui.C, t *editorTab) {
	s := &t.search
	large := t.hasLargeEdit()
	if !s.open || !large && !t.canSearchText() {
		return
	}
	if large {
		t.refreshLargeEditSearch()
	}
	viewport := c.Bounds()
	width, height := min(float32(680), viewport.Width-24), float32(350)
	title := uiText("查找")
	if s.replacing {
		height = 430
		title = uiText("查找与替换")
	}
	if s.centerOnOpen || s.bounds.Width == 0 {
		s.bounds = gfx.Rect{X: (viewport.Width - width) / 2, Y: (viewport.Height - height) / 2}
		s.centerOnOpen = false
	}
	s.bounds.Width, s.bounds.Height = width, height
	s.bounds.X = max(12, min(s.bounds.X, viewport.Width-width-12))
	s.bounds.Y = max(12, min(s.bounds.Y, viewport.Height-height-12))
	ui.Overlay(c.Part("find-dialog"), ui.OverlaySpec{Band: ui.LayerPaneChrome, Bounds: s.bounds}).Children(func() {
		ui.Column(c).Fill().Radius(18).Border(1, m.theme.Border).Background(m.theme.Card).Clip().Pad(16).Gap(10).Draw(func(d *ui.DrawContext) { d.Canvas.Block(d.Rect) }).Children(func() {
			ui.Row(c).FillWidth().Height(26).Items(ui.Center).Children(func() {
				ui.Control(c, uiText("拖动查找窗口"), &s.header).Grow(1).Height(26).HitOutset(16, 44, 10, 16).Draw(func(d *ui.DrawContext) {
					d.Canvas.Label(title, d.Rect, gfx.TextStyle{Size: 16, Weight: gfx.WeightBold, Color: m.theme.Foreground})
				})
				if ui.Button(c, uiText("关闭搜索")).Icon(closeIcon).IconOnly().Ghost().Width(28).Height(26).Tooltip(uiText("关闭 · Esc")).Clicked() {
					c.After(s.close)
				}
			})
			ui.Slot(c).Height(1).FillWidth().Background(m.theme.Border)
			ui.Row(c).FillWidth().Grow(1).MinHeight(0).Gap(20).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(9).Children(func() {
					ui.Row(c.Part("find-mode")).Gap(5).Children(func() {
						findMode := ui.Button(c.Part("find"), uiText("查找")).Ghost().Height(28).MinWidth(60)
						replaceMode := ui.Button(c.Part("replace"), uiText("替换")).Ghost().Height(28).MinWidth(60)
						if s.replacing {
							replaceMode.Secondary()
						} else {
							findMode.Secondary()
						}
						if !s.replacing && replaceMode.Clicked() {
							c.After(t.openReplace)
						}
						if s.replacing && findMode.Clicked() {
							c.After(t.openSearch)
						}
					})
					s.field.Hint = "Enter 查找下一处"
					if binding := m.keyBindings.chord(shortcutSearchNewline).Label(); binding != "" {
						s.field.Hint = binding + " 换行，" + s.field.Hint
					}
					field := ui.TextArea(c, &s.field, uiText("搜索内容"), uiText("输入查找内容")).FillWidth().Height(57).OnKey(s.inputKey)
					if s.focus {
						field.AutoFocus()
						s.focus = false
					}
					if s.replacing {
						s.replaceField.Hint = "支持多行，留空表示删除匹配内容"
						ui.TextArea(c, &s.replaceField, uiText("替换为"), uiText("替换为（留空删除）")).FillWidth().Height(57).OnKey(s.inputKey)
					}
					scope := t.title()
					if s.target == t.result {
						scope += " · JSON 处理结果"
					} else if s.target == t.comparison {
						scope += " · JSON 对比输入"
					}
					ui.Text(c, scope).FontSize(12).Muted().FillWidth().Ellipsis()
					ui.Row(c).FillWidth().Gap(13).Children(func() {
						ui.Checkbox(c, &s.options.WholeWord, uiText("全词匹配")).FontSize(12)
						ui.Checkbox(c, &s.options.CaseSensitive, uiText("区分大小写")).FontSize(12)
						ui.Checkbox(c, &s.wrap, uiText("循环查找")).FontSize(12)
					})
					ui.Row(c).FillWidth().Gap(12).Pad(8).Radius(10).Background(m.theme.Background).Children(func() {
						ui.Radio(c, &s.options.mode, searchText, "普通文本").FontSize(12)
						ui.Radio(c, &s.options.mode, searchEscapes, "转义字符").FontSize(12).Tooltip(uiText("支持 \\n、\\r\\n、\\t 和 \\\\"))
						ui.Radio(c, &s.options.mode, searchRegexp, "正则表达式").FontSize(12).Tooltip(uiText("Go 正则支持 \\n，替换可用 $1 或命名分组"))
					})
				})
				ui.Column(c).Width(180).Gap(8).Children(func() {
					disabled := s.composing() || s.query.String() == "" || s.errorText != "" || large && t.largeText.busyEditing()
					if ui.Button(c, uiText("下一个匹配")).FillWidth().Height(32).Disabled(disabled).Tooltip(m.shortcutHint(uiText("下一个匹配"), shortcutFindNext)).Clicked() {
						c.After(func() { m.stepSearch(t, 1) })
					}
					if ui.Button(c, uiText("上一个匹配")).Outline().FillWidth().Height(32).Disabled(disabled).Tooltip(m.shortcutHint(uiText("上一个匹配"), shortcutFindPrevious)).Clicked() {
						c.After(func() { m.stepSearch(t, -1) })
					}
					if s.replacing {
						if ui.Button(c, uiText("替换当前匹配")).Outline().FillWidth().Height(32).Disabled(disabled || s.target.readOnly).Clicked() {
							c.After(func() {
								if large {
									t.replaceInLargeEdit(false)
								} else {
									s.replaceCurrent()
								}
							})
						}
						if ui.Button(c, uiText("替换当前文件全部")).Outline().FillWidth().Height(32).FontSize(12).Disabled(disabled || s.target.readOnly).Clicked() {
							c.After(func() { m.replaceAll(t, false) })
						}
						if ui.Button(c, uiText("替换全部已打开文档")).Outline().FillWidth().Height(32).FontSize(12).Disabled(disabled).Clicked() {
							c.After(func() { m.replaceAll(t, true) })
						}
					} else {
						if ui.Button(c, uiText("计数")).Outline().FillWidth().Height(32).Disabled(disabled).Clicked() {
							c.After(func() {
								if large {
									t.countLargeEditMatches()
								} else {
									s.refresh()
									s.message = fmt.Sprintf(uiText("当前范围共有 %d 个匹配"), len(s.matches))
								}
							})
						}
						if ui.Button(c, uiText("查找当前文档全部")).Outline().FillWidth().Height(32).FontSize(12).Disabled(disabled).Clicked() {
							c.After(func() { m.findAll(t, false) })
						}
						if ui.Button(c, uiText("查找全部已打开文档")).Outline().FillWidth().Height(32).FontSize(12).Disabled(disabled).Clicked() {
							c.After(func() { m.findAll(t, true) })
						}
					}
				})
			})
			status := uiText("Enter 下一处 · Shift+Enter 上一处")
			if binding := m.keyBindings.chord(shortcutSearchNewline).Label(); binding != "" {
				status = binding + uiText(" 换行 · ") + status
			}
			color := m.theme.MutedForeground
			if !large && s.pattern != "" {
				status = fmt.Sprintf("%d / %d", s.current+1, len(s.matches))
				if s.truncated {
					status += uiText(" · 仅显示前 10,000 个匹配")
				}
			}
			if s.errorText != "" {
				status, color = s.errorText, m.theme.Danger
			} else if s.message != "" {
				status = s.message
			} else if !large && s.pattern != "" && len(s.matches) == 0 {
				status, color = uiText("没有找到匹配内容"), m.theme.Danger
			}
			ui.Text(c, status).FontSize(12).Color(color).Height(20).FillWidth().Ellipsis()
		})
	})
}
