package editor

import (
	"fmt"
	"strings"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"github.com/vivaui/viva/ui/codeeditor"
)

func (state *historyCompareState) view(c ui.C) {
	m := state.owner
	if m.historyCompare != state || state.detached() {
		if m.historyCompare == state {
			c.After(m.closeHistoryComparison)
		}
		return
	}
	m.updateHistoryComparison(c, state)
	state.syncCurrent()
	state.updateCaret()
	blocked := state.blocked()
	state.ownerBlocked = blocked
	if (state.editor == nil || !state.editor.CompositionActive()) && ui.Shortcut(c.Part("close"), 0, "escape") {
		c.After(m.closeHistoryComparison)
	}
	ui.Column(c).Fill().MinHeight(0).Pad(workspaceInset).Gap(8).Background(m.theme.Card).Children(func() {
		ui.Row(c.Part("panes")).FillWidth().Grow(1).MinHeight(0).Gap(8).Children(func() {
			m.workspacePanel(c.Part("timeline")).Width(174).Shrink(0).Children(func() {
				m.historyVersionList(c.Part("versions"), state, blocked)
			})
			m.workspacePanel(c.Part("comparison")).Grow(1).MinWidth(0).Children(func() {
				m.historyComparisonToolbar(c.Part("toolbar"), state, blocked)
				m.historyComparisonBody(c.Part("body"), state, blocked)
			})
		})
		ui.Row(c.Part("footer")).FillWidth().MinHeight(34).PadX(4).Gap(8).Items(ui.Center).Children(func() {
			if state.export != nil {
				ui.Text(c, "正在另存历史副本…").FontSize(12).Muted().Grow(1).MinWidth(0)
			} else if state.problem != "" {
				ui.Wrap(c, state.problem).FontSize(12).Color(m.theme.Danger).Grow(1).MinWidth(0)
			} else if state.notice != "" {
				ui.Wrap(c, state.notice).FontSize(12).Muted().Grow(1).MinWidth(0)
			} else {
				ui.Slot(c).Grow(1)
			}
			canMerge := m.canMergeHistory(state)
			if ui.Button(c.Part("restore"), "恢复整个版本").Ghost().Height(30).FontSize(12).Disabled(!canMerge).Clicked() {
				state.queueMerge(c, state.block, true)
			}
			if ui.Button(c.Part("combine"), "合并此区别").Height(30).FontSize(12).Disabled(!canMerge).Clicked() {
				state.queueMerge(c, state.block, false)
			}
		})
	})
}

func (state *historyCompareState) queueMerge(c ui.C, block int, whole bool) {
	c.After(func() {
		if state.mergeNeedsReview {
			state.notice = historyChangedNotice
			state.mergeNeedsReview = false
			return
		}
		state.block = block
		state.owner.mergeHistoryChange(state, whole)
	})
}

func (state *historyCompareState) updateCaret() {
	if !state.canEditCurrent() {
		state.caretFocused = false
		return
	}
	editor := state.editor
	if caret := editor.Caret(); caret != state.caret {
		state.caret, state.revealCaret = caret, state.revealCaret || state.caretFocused
	}
	if !state.revealCaret || !state.onlyChanges {
		return
	}
	line := strings.Count(editor.String()[:editor.ByteOffset(state.caret)], "\n")
	for _, row := range state.comparison.diff.changes.Rows {
		if row.RightLine == line {
			return
		}
	}
	state.onlyChanges = false
}

func (m *editorApp) historyVersionList(c ui.C, state *historyCompareState, blocked bool) {
	heading := ui.Text(c, "历史版本").FontSize(14).Bold().Height(43).PadX(13).PadY(12)
	if state.source != "" {
		heading.Hit(ui.ID(c.Part("source-path")), state.source).Tooltip(state.source)
	}
	if state.catalog != nil {
		ui.Text(c, "正在读取版本…").Muted().FontSize(12).PadX(13)
		return
	}
	ui.List(c.Part("timeline-list"), &state.versionList, len(state.versions), func(item ui.ListRow) {
		version := state.versions[item.Index]
		selected := item.Index == state.selection
		label := version.at.Local().Format(time.DateTime)
		item.Element.Height(54).PadX(6).PadBottom(3).Children(func() {
			c := item.C.Part("version")
			row := ui.Column(c).Fill().MinWidth(0).PadX(9).PadY(6).Gap(4).Radius(8).
				HitWith(ui.ID(c), label, gfx.HitOptions{Role: gfx.RoleButton, Disabled: blocked, Selected: selected}).
				Hover(ui.Look{}.Background(m.theme.Hover)).Focus(ui.Look{}.Border(m.theme.Ring))
			if selected {
				row.Background(m.theme.Secondary)
			}
			row.Children(func() {
				ui.Text(c, label).FontSize(12).Ellipsis()
				ui.Text(c, historySize(version.bytes)).FontSize(11).Muted()
			})
			if row.Clicked() && !selected {
				c.After(func() {
					state.notice = ""
					m.selectHistoryVersion(state, item.Index)
				})
			}
		})
	}).FillWidth().Grow(1).MinHeight(0).Label("历史版本列表")
}

func (m *editorApp) historyComparisonToolbar(c ui.C, state *historyCompareState, blocked bool) {
	count := 0
	if state.comparison != nil && state.limitProblem == nil {
		count = len(state.comparison.diff.blocks)
	}
	ui.Row(c.Part("actions")).FillWidth().Height(43).PadX(8).Items(ui.Center).Gap(4).Children(func() {
		if ui.Button(c.Part("previous"), "上一个区别").Icon(historyUpIcon).IconOnly().Ghost().Size(30, 30).
			Tooltip("上一个区别").Disabled(blocked || count == 0).Clicked() {
			c.After(func() { state.selectBlock((state.block + count - 1) % count) })
		}
		if ui.Button(c.Part("next"), "下一个区别").Icon(historyDownIcon).IconOnly().Ghost().Size(30, 30).
			Tooltip("下一个区别").Disabled(blocked || count == 0).Clicked() {
			c.After(func() { state.selectBlock((state.block + 1) % count) })
		}
		progress := "0 处区别"
		if state.block >= 0 && count > 0 {
			progress = fmt.Sprintf("%d / %d 处区别", state.block+1, count)
		} else if state.comparison != nil && state.comparison.oldProblem == "" && state.comparison.diskProblem == "" && state.limitProblem == nil {
			progress = "两份内容一致"
		}
		ui.Text(c, progress).FontSize(12).Muted().Shrink(0).PadLeft(4)
		if state.comparison != nil && state.comparison.diff.full.Stats.Coarse {
			ui.Text(c, "部分区别按整块展示").FontSize(11).Muted().MinWidth(0).Ellipsis().PadLeft(4)
		}
		ui.Slot(c).Grow(1).MinWidth(0)
		ui.Row(c.Part("modes")).Items(ui.Center).Gap(2).Pad(2).Radius(8).Background(m.theme.Card).Children(func() {
			for _, mode := range []struct {
				label string
				icon  gfx.Icon
				only  bool
			}{{"展示全文件", expandIcon, false}, {"只看区别", collapseIcon, true}} {
				button := ui.Button(c.Part("mode").Key(mode.label), mode.label).Icon(mode.icon).IconOnly().Ghost().Size(30, 30).
					Tooltip(mode.label).Disabled(blocked || state.comparison == nil || state.comparison.oldProblem != "" || state.limitProblem != nil)
				if state.onlyChanges == mode.only {
					button.Secondary()
				}
				if button.Clicked() && state.onlyChanges != mode.only {
					c.After(func() {
						state.onlyChanges = mode.only
						state.reveal = state.block >= 0
					})
				}
			}
		})
		if ui.Button(c.Part("export"), "另存副本…").Icon(exportIcon).IconOnly().Ghost().Size(30, 30).
			Tooltip("另存副本…").Disabled(blocked || state.selection < 0 || state.export != nil).Clicked() {
			c.After(func() { m.exportHistoryComparison(state) })
		}
	})
}

func (m *editorApp) historyComparisonBody(c ui.C, state *historyCompareState, blocked bool) {
	if state.catalog != nil || state.read != nil {
		ui.Text(c, "正在比对历史…").Muted().FontSize(13).Pad(14)
		return
	}
	if state.comparison == nil {
		if state.problem == "" {
			ui.Text(c, "这个文件还没有历史版本").Muted().FontSize(13).Pad(14)
		}
		return
	}
	comparison := state.comparison
	if state.limitProblem != nil {
		ui.Wrap(c, "已暂停比对："+state.limitProblem.Error()+"。可在主编辑器调整内容后继续比对。")
		return
	}
	if comparison.oldProblem != "" {
		ui.Wrap(c, comparison.oldProblem).FillWidth().Muted().FontSize(13).Pad(14)
		return
	}
	editable := state.canEditCurrent()
	ui.Row(c.Part("columns")).FillWidth().Height(30).PadX(9).Gap(8).Items(ui.Center).Children(func() {
		ui.Text(c, "历史内容").FontSize(12).Bold().Grow(1).Basis(0).MinWidth(0)
		current := "当前版本"
		if editable {
			current += " · 可编辑"
		}
		ui.Text(c, current).FontSize(12).Bold().Grow(1).Basis(0).MinWidth(0)
	})
	if comparison.diskProblem != "" {
		ui.Wrap(c.Part("problem"), comparison.diskProblem).FillWidth().FontSize(12).Muted().PadX(9).PadBottom(6)
	}
	result := comparison.diff.full
	var separators map[int]string
	if state.onlyChanges && len(comparison.diff.blocks) != 0 {
		result = comparison.diff.changes
		separators = comparison.diff.separators
	}
	language := documentFormat(state.source)
	if tab := m.tabAtPath(state.source); tab != nil {
		language = tab.format()
	}
	state.diff.Snapshot = components.CodeDiffSnapshot{
		Result: result, Value: comparison.current.saved, ReadOnly: !editable, Disabled: blocked, CenterGutters: true,
		Language: language, Highlight: m.highlightHistoryLine, RowSeparators: separators,
		FontSize: float32(m.editorPrefs.FontSize), TabWidth: m.editorPrefs.TabWidth, MonoFace: gfx.MonoFamily(m.editorPrefs.FontFamily),
		VerticalScrollbarID: gfx.Item(1), HorizontalScrollbarID: gfx.Item(2),
	}
	if editable {
		editor, snapshot := state.editor, &state.diff.Snapshot
		selection := editor.Selection()
		snapshot.ShowCaret, snapshot.CaretWidth, snapshot.Caret = true, 1, editor.ByteOffset(editor.Caret())
		snapshot.SelectionStart, snapshot.SelectionEnd = editor.ByteOffset(selection.Start), editor.ByteOffset(selection.End)
		if composition, ok := editor.Composition(); ok {
			snapshot.CompositionStart, snapshot.CompositionEnd = editor.ByteOffset(composition.Start), editor.ByteOffset(composition.End)
		}
	}
	if state.block >= 0 {
		state.diff.Snapshot.ActiveRows = comparison.diff.blocks[state.block].visibleRange(state.onlyChanges)
	}
	const editLabel = "当前版本编辑区"
	view := codeeditor.DiffView(c.Part("diff"), editLabel, &state.diff).FillWidth().Grow(1).MinHeight(0)
	// Let the painted right pane provide the text bounds and caret placement.
	view.HitWith(view.N().ID, editLabel, gfx.HitOptions{Role: gfx.RoleGroup, SkipFocus: true})
	parts := state.diff.Parts()
	state.diff.Snapshot.LeftGutterID, state.diff.Snapshot.RightGutterID = parts.At(2), parts.At(3)
	state.diff.Snapshot.ViewportID = parts.At(4)
	if !blocked && m.canMergeHistory(state) {
		state.diff.Snapshot.Actions = state.mergeActionsFor(result, parts)
		if canvas := c.Canvas(); canvas != nil {
			if part, clicked := canvas.ClickedIn(parts); clicked && part >= historyMergePart && part-historyMergePart < len(comparison.diff.blocks) {
				state.queueMerge(c, part-historyMergePart, false)
			}
		}
	}
	view.DrawOver(func(d *ui.DrawContext) {
		state.caretFocused = d.Canvas.Focused() == d.Node.ID
		if state.reveal && state.block >= 0 {
			state.reveal = false
			rows := state.diff.Snapshot.ActiveRows
			state.diff.Code.RevealRow(min(rows.End+historyContextLines-1, len(result.Rows)-1))
			state.diff.Code.RevealRow(max(0, rows.Start-historyContextLines))
			state.diff.Code.RevealRow(rows.Start)
			d.Canvas.RequestFrame()
		}
		if state.revealCaret && editable {
			state.revealCaret = false
			state.diff.Code.RevealCaret(state.diff.Snapshot.Value, state.diff.Snapshot.Caret)
			d.Canvas.RequestFrame()
		}
	})
}

func (m *editorApp) highlightHistoryLine(language, line string, runs []components.CodeEditorRun) []components.CodeEditorRun {
	highlight := syntaxLine{syntaxPalette: syntaxColors(m.light), runs: runs}
	highlight.scan(language, line, configLexState{})
	return highlight.runs
}
