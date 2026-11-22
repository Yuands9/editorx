package main

import (
	"fmt"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

func (m *editorApp) searchDialog(c ui.C, t *editorTab) {
	s := &t.search
	if !s.open {
		return
	}
	viewport := c.Bounds()
	width, height := min(float32(680), viewport.Width-24), float32(350)
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
				// 标题区包含顶部、两侧和分隔线上方的留白；后绘制的关闭按钮优先响应。
				ui.Control(c, "拖动查找窗口", &s.header).Grow(1).Height(26).HitOutset(16, 44, 10, 16).Draw(func(d *ui.DrawContext) {
					d.Canvas.Label("查找", d.Rect, gfx.TextStyle{Size: 16, Weight: gfx.WeightBold, Color: m.theme.Foreground})
				})
				if ui.Button(c, "关闭搜索").Icon(closeIcon).IconOnly().Ghost().Width(28).Height(26).Tooltip("关闭 · Esc").Clicked() {
					c.After(s.close)
				}
			})
			ui.Slot(c).Height(1).FillWidth().Background(m.theme.Border)
			ui.Row(c).FillWidth().Grow(1).MinHeight(0).Gap(20).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(12).Children(func() {
					scope := "当前文档"
					if s.target == t.result {
						scope = "JSON 处理结果"
					} else if s.target == t.comparison {
						scope = "JSON 对比输入"
					}
					ui.Text(c, "查找目标").FontSize(12).Muted()
					field := ui.TextField(c, &s.field, "搜索内容", "输入关键词").FillWidth().Height(34).OnKey(s.key)
					if s.focus {
						field.AutoFocus()
						s.focus = false
					}
					ui.Text(c, "范围："+t.title()+" · "+scope).FontSize(12).Muted().FillWidth().Ellipsis()
					ui.Row(c).FillWidth().Gap(18).Children(func() {
						ui.Checkbox(c, &s.options.WholeWord, "全词匹配").FontSize(12)
						ui.Checkbox(c, &s.options.CaseSensitive, "区分大小写").FontSize(12)
					})
					ui.Checkbox(c, &s.wrap, "循环查找").FontSize(12)
					ui.Column(c).FillWidth().Pad(10).Radius(10).Background(m.theme.Background).Gap(8).Children(func() {
						ui.Text(c, "搜索模式").FontSize(12).Muted()
						ui.Row(c).Gap(18).Children(func() {
							ui.Radio(c, &s.options.Regexp, false, "普通文本").FontSize(12)
							ui.Radio(c, &s.options.Regexp, true, "正则表达式").FontSize(12).Tooltip("Go 正则语法；(?s) 允许 . 匹配换行")
						})
					})
				})
				ui.Column(c).Width(176).Gap(8).Children(func() {
					_, composing := s.query.Composition()
					disabled := composing || s.query.String() == "" || s.errorText != ""
					if ui.Button(c, "下一个匹配").FillWidth().Height(32).Disabled(disabled).Clicked() {
						c.After(func() { s.step(1) })
					}
					if ui.Button(c, "上一个匹配").Outline().FillWidth().Height(32).Disabled(disabled).Clicked() {
						c.After(func() { s.step(-1) })
					}
					if ui.Button(c, "计数").Outline().FillWidth().Height(32).Disabled(disabled).Clicked() {
						c.After(func() { s.refresh(); s.message = fmt.Sprintf("当前范围共有 %d 个匹配", len(s.matches)) })
					}
					if ui.Button(c, "查找当前文档全部").Outline().FillWidth().Height(32).FontSize(12).Disabled(disabled).Clicked() {
						c.After(func() { m.findAll(t, false) })
					}
					if ui.Button(c, "查找全部已打开文档").Outline().FillWidth().Height(32).FontSize(12).Disabled(disabled).Clicked() {
						c.After(func() { m.findAll(t, true) })
					}
				})
			})
			status := "输入关键词 · Enter 下一处 · Shift+Enter 上一处"
			color := m.theme.MutedForeground
			if s.pattern != "" {
				status = fmt.Sprintf("%d / %d", s.current+1, len(s.matches))
			}
			if s.message != "" {
				status = s.message
			}
			if s.truncated {
				status += " · 仅显示前 10,000 个匹配"
			}
			if s.errorText != "" {
				status = s.errorText
				color = m.theme.Danger
			} else if s.pattern != "" && len(s.matches) == 0 {
				status = "没有找到匹配内容"
				color = m.theme.Danger
			}
			ui.Text(c, status).FontSize(12).Color(color).Height(20).FillWidth().Ellipsis()
		})
	})
}
