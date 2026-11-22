package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

var (
	jsonMergeLeftIcon = gfx.Icon{ViewBox: 24, Stroke: 1.7, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
		gfx.MoveTo(19, 12), gfx.LineTo(5, 12), gfx.MoveTo(11, 6), gfx.LineTo(5, 12), gfx.LineTo(11, 18),
	}}
	jsonMergeRightIcon = gfx.Icon{ViewBox: 24, Stroke: 1.7, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
		gfx.MoveTo(5, 12), gfx.LineTo(19, 12), gfx.MoveTo(13, 6), gfx.LineTo(19, 12), gfx.LineTo(13, 18),
	}}
	jsonPasteIcon = gfx.Icon{ViewBox: 24, Stroke: 1.6, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
		gfx.MoveTo(8, 5), gfx.LineTo(5, 5), gfx.LineTo(5, 21), gfx.LineTo(19, 21), gfx.LineTo(19, 5), gfx.LineTo(16, 5),
		gfx.MoveTo(8, 3), gfx.LineTo(16, 3), gfx.LineTo(16, 7), gfx.LineTo(8, 7), gfx.LineTo(8, 3),
		gfx.MoveTo(8, 12), gfx.LineTo(16, 12), gfx.MoveTo(8, 16), gfx.LineTo(14, 16),
	}}
)

func (m *editorApp) jsonDiffView(c ui.C, t *editorTab, blocked bool) {
	t.updateDiff()
	t.comparison.view.Snapshot.Language = "JSON"
	ui.Column(c.Part("json-comparison")).FillWidth().Grow(1).MinHeight(0).PadX(10).PadBottom(10).Clip().Children(func() {
		ui.Row(c).FillWidth().Height(t.diffSplit.Pos).Shrink(0).MinHeight(0).Children(func() {
			ui.Column(c).Width(t.split.Pos).FillHeight().MinWidth(0).Radius(16).Background(m.theme.Card).Clip().Children(func() {
				m.jsonDiffEditor(c.Part("json-left"), t, t.source, blocked)
			})
			ui.Splitter(c, &t.split).Mins(176, 176).SplitterThickness(10)
			ui.Column(c).Grow(1).FillHeight().MinWidth(0).Radius(16).Background(m.theme.Card).Clip().Children(func() {
				m.jsonDiffEditor(c.Part("json-right"), t, t.comparison, blocked)
			})
		})
		ui.Splitter(c, &t.diffSplit).Mins(112, 132).SplitterThickness(14).Label("调整 JSON 比对结果高度").DrawOver(func(d *ui.DrawContext) {
			r := d.Rect
			color := m.theme.MutedForeground
			if t.diffSplit.Dragging() || d.Canvas.Hovered() == t.diffSplit.ID {
				color = m.theme.Ring
			}
			d.Canvas.Rounded(gfx.Rect{X: r.X + (r.Width-36)/2, Y: r.Y + (r.Height-3)/2, Width: 36, Height: 3}, 2, color)
		})
		m.jsonDiffResults(c, t, blocked)
	})
}

func (m *editorApp) jsonDiffEditor(c ui.C, t *editorTab, pane *editorPane, blocked bool) {
	index, title, side, editorLabel := 0, "原始 JSON · 当前文件", "左侧", "代码编辑区"
	if pane == t.comparison {
		index, title, side, editorLabel = 1, "待比较 JSON", "右侧", "JSON 对比输入"
		pane.view.Snapshot.Placeholder = "粘贴或载入第二份 JSON，自动比较"
	}
	ui.Row(c).FillWidth().Height(40).PadX(8).Gap(4).Items(ui.Center).Children(func() {
		ui.Text(c, title).FontSize(12).Bold().Grow(1).MinWidth(0).Ellipsis()
		m.jsonDiffTools(c.Part("json-tools"), t, pane, &t.diffMenus[index], side, blocked)
		if pane == t.comparison {
			if ui.Button(c, "载入对比文件").Icon(fileIcon).IconOnly().Ghost().Size(28, 28).Tooltip("载入对比文件").Disabled(blocked).Clicked() {
				c.After(func() { m.loadComparison(t) })
			}
			if ui.Button(c, "粘贴对比内容").Icon(jsonPasteIcon).IconOnly().Ghost().Size(28, 28).Tooltip("粘贴对比内容").Disabled(blocked).Clicked() {
				c.After(func() {
					value, err := app.ClipboardText()
					if err != nil {
						m.fail(err)
						return
					}
					t.comparison.replace(value)
					t.source.focus, t.comparison.focus = false, true
				})
			}
		}
	})
	pane.declare(c, editorLabel, !blocked).FillWidth().Grow(1)
}

func (m *editorApp) jsonDiffTools(c ui.C, t *editorTab, pane *editorPane, open *bool, side string, blocked bool) {
	disabled := blocked || strings.TrimSpace(pane.text.String()) == ""
	ui.Row(c).Pad(2).Gap(1).Shrink(0).Radius(10).Background(m.theme.Background).Items(ui.Center).Children(func() {
		if ui.Button(c, "格式化"+side).Ghost().Height(26).FontSize(11).Disabled(disabled).Clicked() {
			c.After(func() { m.jsonDiffAction(t, pane, side, "格式化") })
		}
		label := side + " JSON 菜单"
		menuButton := ui.Button(c, label).Icon(chevronDownIcon).IconOnly().Ghost().Size(26, 26).Tooltip(label).Disabled(blocked)
		if menuButton.Clicked() {
			*open = !*open
		}
		ui.Menu(c, menuButton, open, func(menu *ui.MenuItems) {
			menu.WidthRange(184, 228)
			menu.Label(label)
			for _, action := range []string{"格式化", "压缩", "键排序"} {
				if menu.Item(action).Disabled(disabled).Clicked() {
					c.After(func() { m.jsonDiffAction(t, pane, side, action) })
				}
			}
			menu.Separator()
			if menu.Item("转 XML").Disabled(disabled).Clicked() {
				c.After(func() { m.jsonDiffAction(t, pane, side, "转 XML") })
			}
			for _, action := range jsonExtraActions {
				if menu.Item(action).Disabled(disabled).Clicked() {
					c.After(func() { m.jsonDiffAction(t, pane, side, action) })
				}
			}
		})
	})
}

func (m *editorApp) jsonDiffAction(t *editorTab, pane *editorPane, side, action string) {
	if _, composing := pane.text.Composition(); composing {
		m.status = "请先完成中文选词，再处理" + side + " JSON"
		return
	}
	source := pane.text.String()
	output, kind, err := transformJSON(source, action, t.jsonIndent)
	if err != nil {
		message, _ := jsonErrorMessage(source, err)
		m.fail(fmt.Errorf("%s JSON：%s", side, message))
		return
	}
	if kind != "JSON" {
		result := m.addTab(document{eol: "CRLF"}, "转换结果"+jsonResultExtension(kind))
		result.source.replace(output)
		m.status = side + "转换结果已放入新标签"
		return
	}
	if output != source {
		pane.replace(output)
	}
	t.source.focus, t.comparison.focus = pane == t.source, pane == t.comparison
	m.status = side + " JSON · " + action
}

func (m *editorApp) jsonDiffResults(c ui.C, t *editorTab, blocked bool) {
	ui.Column(c.Part("json-differences")).FillWidth().Grow(1).MinHeight(0).Pad(10).Gap(6).Radius(16).Background(m.theme.Card).Clip().Children(func() {
		ui.Row(c).FillWidth().Height(28).Gap(8).Items(ui.Center).Children(func() {
			ui.Text(c, fmt.Sprintf("差异 %d", len(t.differences))).FontSize(13).Bold()
			ui.Text(c, "对象忽略键顺序 · 数组按位置比较").FontSize(11).Muted().Grow(1).MinWidth(0).Ellipsis()
			if len(t.differences) != 0 {
				ui.Text(c, "逐项合并").FontSize(11).Muted()
			}
			if ui.Button(c, "复制差异").Icon(copyIcon).IconOnly().Ghost().Size(28, 28).Tooltip("复制完整差异报告").
				Disabled(blocked || t.diffError != "" || strings.TrimSpace(t.diffRight) == "").Clicked() {
				c.After(func() { m.copyDiff(t) })
			}
		})
		if t.diffError != "" {
			ui.Text(c, t.diffError).Color(m.theme.Danger).FontSize(12).FillWidth().Ellipsis().Hit(ui.ID(c), t.diffError).Tooltip(t.diffError)
			return
		}
		if strings.TrimSpace(t.diffRight) == "" {
			ui.Text(c, "在右侧粘贴或载入待比较的 JSON。").FontSize(12).Muted().FillWidth().Ellipsis()
			return
		}
		if len(t.differences) == 0 {
			ui.Text(c, "两份 JSON 结构和值一致").FontSize(13).Color(m.theme.Success)
			return
		}
		ui.List(c, &t.diffList, len(t.differences), func(item ui.ListRow) {
			difference := t.differences[item.Index]
			item.Element.Height(34).MarginBottom(3).PadX(8).Gap(7).Radius(8).Background(m.theme.Background).Items(ui.Center).Clip().Children(func() {
				m.jsonDiffRow(item.C, t, difference, blocked)
			})
		}).FillWidth().Grow(1).MinHeight(0).Label("JSON 差异列表")
		if index, submitted := t.diffList.Submitted(); submitted && !blocked {
			difference := t.differences[index]
			c.After(func() { t.locateDifference(difference) })
		}
	})
}

func (m *editorApp) jsonDiffRow(c ui.C, t *editorTab, difference jsonDifference, blocked bool) {
	color := gfx.RGB(219, 181, 114)
	if m.light {
		color = gfx.RGB(151, 84, 22)
	}
	if difference.kind == "新增" {
		color = m.theme.Success
	} else if difference.kind == "删除" {
		color = m.theme.Danger
	}
	path := jsonDisplayPath(difference.path)
	ui.Text(c, difference.kind).Color(color).Width(53).FontSize(12)
	locate := ui.Text(c, path).Grow(1).MinWidth(0).FontSize(12).Ellipsis().
		HitWith(ui.ID(c), "定位 "+path, gfx.HitOptions{Role: gfx.RoleButton, Disabled: blocked}).Tooltip("点击定位 " + path)
	if locate.Clicked() {
		c.After(func() { t.locateDifference(difference) })
	}
	ui.Text(c, difference.before).Grow(1).MinWidth(0).FontSize(12).Ellipsis().Hit(ui.ID(c), difference.before).Tooltip(difference.before)
	ui.Text(c, "→").Muted().FontSize(12)
	ui.Text(c, difference.after).Grow(1).MinWidth(0).FontSize(12).Ellipsis().Hit(ui.ID(c), difference.after).Tooltip(difference.after)
	for _, action := range []struct {
		side              string
		icon              gfx.Icon
		intoLeft, missing bool
		sourceMissing     bool
	}{
		{"左侧", jsonMergeLeftIcon, true, difference.left.End == difference.left.Start, difference.right.End == difference.right.Start},
		{"右侧", jsonMergeRightIcon, false, difference.right.End == difference.right.Start, difference.left.End == difference.left.Start},
	} {
		label := "合并到" + action.side
		hint := label + " " + path
		if action.sourceMissing {
			hint = "删除" + action.side + "的 " + path
		} else if action.missing {
			hint = "添加到" + action.side + " " + path
		}
		if action.missing && difference.arrayGap {
			hint = "请先合并前面缺失的数组项"
		}
		if ui.Button(c, label+" "+path).Icon(action.icon).IconOnly().Ghost().Size(26, 26).
			Tooltip(hint).Disabled(blocked || action.missing && difference.arrayGap).Clicked() {
			c.After(func() { m.mergeJSONDifference(t, difference, action.intoLeft) })
		}
	}
}

func (m *editorApp) loadComparison(t *editorTab) {
	paths, err := app.OpenFiles(app.OpenFileOptions{Title: "载入待比较 JSON", Filters: []app.FileFilter{{Name: "所有文件", Pattern: "*.*"}}})
	t.comparison.focus = true
	if errors.Is(err, app.ErrCanceled) || err == nil && len(paths) == 0 {
		return
	}
	if err != nil {
		m.fail(err)
		return
	}
	doc, err := readDocument(paths[0])
	if err != nil {
		m.fail(err)
		return
	}
	t.comparison.replace(doc.saved)
}
