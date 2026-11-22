package main

import (
	"fmt"
	"strings"

	"github.com/vivaui/viva/ui"
)

var jsonExtraActions = [...]string{"Form-Data", "转义", "去转义", "Unicode 编码", "Unicode 解码"}

func (m *editorApp) jsonToolbar(c ui.C, t *editorTab, blocked bool) {
	ui.Row(c).FillWidth().Height(48).PadX(12).Gap(4).Items(ui.Center).Children(func() {
		if !t.jsonCompare {
			ui.Row(c).Pad(3).Gap(2).Radius(16).Background(m.theme.Card).Children(func() {
				for _, action := range []string{"格式化", "压缩", "键排序"} {
					button := ui.Button(c, action).Ghost().Height(28).FontSize(12).Disabled(blocked)
					if t.jsonAction == action {
						button.Secondary()
					}
					if button.Clicked() {
						c.After(func() { m.jsonAction(t, action) })
					}
				}
			})
			if ui.Button(c, "转 XML").Ghost().Height(30).FontSize(12).Disabled(blocked).Clicked() {
				c.After(func() { m.jsonAction(t, "转 XML") })
			}
		}
		compare := ui.Button(c, "对比").Ghost().Height(30).FontSize(12).Disabled(blocked).Tooltip("比较两个 JSON 的字段、类型和值")
		if t.jsonCompare {
			compare.Secondary().Tooltip("退出 JSON 对比")
		}
		if compare.Clicked() {
			c.After(func() {
				t.jsonCompare = !t.jsonCompare
				t.diffReady = false
				t.jsonMenu, t.diffMenus = false, [2]bool{}
				t.source.text.CancelComposition()
				t.comparison.text.CancelComposition()
				t.source.focus, t.comparison.focus = !t.jsonCompare, t.jsonCompare
			})
		}
		if !t.jsonCompare {
			more := ui.Button(c, "更多").Icon(chevronDownIcon).Ghost().Height(30).FontSize(12).Disabled(blocked)
			if more.Clicked() {
				t.jsonMenu = !t.jsonMenu
			}
			ui.Menu(c, more, &t.jsonMenu, func(menu *ui.MenuItems) {
				menu.WidthRange(200, 240)
				menu.Label("转换与编码")
				for _, action := range jsonExtraActions {
					if menu.Item(action).Checked(t.jsonAction == action).Clicked() {
						c.After(func() { m.jsonAction(t, action) })
					}
				}
			})
		}
		ui.Slot(c).Grow(1)
		for _, indent := range []int{2, 4} {
			button := ui.Button(c.Part("json-indent").KeyInt(indent), fmt.Sprintf("%d 空格", indent)).Ghost().Height(28).FontSize(11).Disabled(blocked).Tooltip("格式化缩进")
			if t.jsonIndent == indent {
				button.Secondary()
			}
			if button.Clicked() {
				c.After(func() { t.jsonIndent, t.resultReady = indent, false })
			}
		}
	})
}

func (m *editorApp) jsonView(c ui.C, t *editorTab, blocked bool) {
	if t.jsonCompare {
		m.jsonDiffView(c, t, blocked)
		return
	}
	t.source.view.Snapshot.MarkLayers = nil
	t.updateResult()
	t.result.updateFolds()
	foldable := t.resultError == "" && t.resultKind == "JSON" && len(t.result.folds) > 0
	ui.Row(c).FillWidth().Grow(1).MinHeight(0).PadX(10).PadBottom(10).Children(func() {
		ui.Column(c).Width(t.split.Pos).FillHeight().MinWidth(0).Radius(16).Background(m.theme.Card).Clip().Children(func() {
			ui.Row(c).FillWidth().Height(38).PadX(14).Gap(8).Items(ui.Center).Children(func() {
				ui.Text(c, "源文件").FontSize(12).Bold()
				ui.Slot(c).Grow(1)
				ui.Text(c, "可编辑").FontSize(11).Muted()
			})
			t.source.declare(c, "代码编辑区", !blocked).FillWidth().Grow(1)
			ui.Row(c).FillWidth().Height(28).PadX(12).Items(ui.Center).Children(func() {
				ui.Text(c, fmt.Sprintf("%d 字符 · %d 行", t.source.text.RuneCount(), strings.Count(t.source.text.String(), "\n")+1)).FontSize(11).Muted()
			})
		})
		ui.Splitter(c, &t.split).Mins(210, 260).SplitterThickness(10)
		ui.Column(c).Grow(1).FillHeight().MinWidth(0).Radius(16).Background(m.theme.Card).Clip().Children(func() {
			ui.Row(c).FillWidth().Height(38).PadX(10).Gap(2).Items(ui.Center).Children(func() {
				title := t.jsonAction + "结果"
				ui.Text(c, title).FontSize(12).Bold().Grow(1).MinWidth(0).Ellipsis().Hit(ui.ID(c), title).Tooltip(title)
				if ui.Button(c, "复制").Icon(copyIcon).IconOnly().Ghost().Width(28).Height(28).Disabled(blocked || t.resultError != "").Tooltip("复制完整结果").Clicked() {
					c.After(func() { m.copyResult(t) })
				}
				if ui.Button(c, "应用").Icon(applyIcon).IconOnly().Ghost().Width(28).Height(28).Disabled(blocked || t.resultError != "").Tooltip("应用结果 · JSON 替换原文，其他格式新建标签").Clicked() {
					c.After(func() { m.applyResult(t) })
				}
				if ui.Button(c, "另存").Icon(exportIcon).IconOnly().Ghost().Width(28).Height(28).Disabled(blocked || t.resultError != "").Tooltip("另存处理结果").Clicked() {
					c.After(func() { m.exportResult(t) })
				}
				ui.Slot(c).Width(1).Height(14).Background(m.theme.Border)
				if ui.Button(c, "展开").Icon(expandIcon).IconOnly().Ghost().Width(28).Height(28).Disabled(blocked || !foldable).Tooltip("展开全部结构（仅多行 JSON）").Clicked() {
					c.After(func() { t.result.foldAll(false) })
				}
				if ui.Button(c, "折叠").Icon(collapseIcon).IconOnly().Ghost().Width(28).Height(28).Disabled(blocked || !foldable).Tooltip("折叠全部结构（仅多行 JSON）").Clicked() {
					c.After(func() { t.result.foldAll(true) })
				}
			})
			if t.resultError != "" {
				ui.Column(c).FillWidth().Pad(18).Gap(12).Children(func() {
					ui.Text(c, "JSON 处理失败").FontSize(15).Color(m.theme.Danger)
					ui.Text(c, t.resultError).FontSize(12).FillWidth().Ellipsis().Hit(ui.ID(c), t.resultError).Tooltip(t.resultError)
					ui.Text(c, "修正左侧原文后自动更新").FontSize(12).Muted()
					if ui.Button(c, "定位错误").Secondary().Disabled(blocked || t.resultErrorOffset < 0).Clicked() {
						c.After(func() {
							t.source.foldAll(false)
							t.source.text.MoveTo(t.source.text.RuneIndex(t.resultErrorOffset), false)
							t.source.focus = true
						})
					}
				})
				ui.Slot(c).Grow(1)
			} else {
				t.result.declare(c, "JSON 处理结果", !blocked).FillWidth().Grow(1)
			}
			ui.Row(c).FillWidth().Height(28).PadX(12).Gap(6).Items(ui.Center).Children(func() {
				status := fmt.Sprintf("有效 · %d 字符", t.result.text.RuneCount())
				if t.resultError != "" {
					status = "等待修正"
				} else if t.resultKind == "Form-Data" {
					status = "key:value · 嵌套值保留 JSON"
				}
				if folded := len(t.result.view.Snapshot.FoldedLines); folded > 0 {
					status += fmt.Sprintf(" · %d 节点已折叠", folded)
				}
				ui.Text(c, status).FontSize(11).Muted().Grow(1).MinWidth(0).Ellipsis().Hit(ui.ID(c), status).Tooltip(status)
				wrap := ui.Button(c, "结果折行").Icon(wrapIcon).IconOnly().Ghost().Width(24).Height(24).Tooltip("结果自动折行（仅影响显示）").Disabled(blocked)
				if t.result.view.Snapshot.Wrap {
					wrap.Secondary()
				}
				if wrap.Clicked() {
					t.result.view.Snapshot.Wrap = !t.result.view.Snapshot.Wrap
				}
			})
		})
	})
}
