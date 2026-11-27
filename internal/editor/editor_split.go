package editor

import (
	"slices"

	"github.com/vivaui/viva/ui"
)

type editorSplitState struct {
	left, right  *editorTab
	mirror       *editorPane
	divider      ui.SplitterState
	rightFocused bool
}

func (m *editorApp) openEditorSplit(tab *editorTab) {
	if !historyEditableTab(tab) {
		return
	}
	m.activate(tab)
	state := &editorSplitState{left: tab, right: tab}
	for _, other := range m.tabs {
		if other != tab && historyEditableTab(other) {
			state.right = other
			break
		}
	}
	m.editorSplit = state
	for _, item := range []*editorTab{state.left, state.right} {
		if item.format() == "JSON" {
			item.updateResult()
			item.result.focused = false
		}
	}
}

func (m *editorApp) splitEditingPane() *editorPane {
	if s := m.editorSplit; s != nil && s.rightFocused && s.right == m.active && s.left == s.right && s.mirror != nil {
		return s.mirror
	}
	if m.active == nil {
		return nil
	}
	if m.active.format() == "JSON" && m.active.result.focused && m.editorSplit == nil {
		return m.active.result
	}
	return m.active.source
}

func (m *editorApp) splitDocumentView(c ui.C, tab *editorTab, blocked bool) {
	s := m.editorSplit
	if s == nil {
		m.documentView(c, tab, blocked)
		return
	}
	if !historyEditableTab(s.left) || !historyEditableTab(s.right) || !slices.Contains(m.tabs, s.left) || !slices.Contains(m.tabs, s.right) {
		m.editorSplit = nil
		m.documentView(c, tab, blocked)
		return
	}
	if s.divider.Pos == 0 {
		s.divider.Pos = c.Bounds().Width / 2
	}
	var choices []*editorTab
	for _, candidate := range m.tabs {
		if historyEditableTab(candidate) {
			choices = append(choices, candidate)
		}
	}
	ui.Row(c.Part("ordinary-split")).FillWidth().Grow(1).MinHeight(0).Children(func() {
		for _, right := range []bool{false, true} {
			if right {
				ui.Splitter(c.Part("divider"), &s.divider).Mins(140, 140).SplitterThickness(6).Label("调整分屏宽度")
			}
			part := c.Key("left")
			target := s.left
			if right {
				part = c.Key("right")
				target = s.right
			}
			column := ui.Column(part).FillHeight().MinWidth(0)
			if right {
				column.Grow(1)
			} else {
				column.Width(s.divider.Pos)
			}
			column.Children(func() {
				ui.Row(part.Part("header")).FillWidth().Height(32).Gap(4).Items(ui.Center).Children(func() {
					selected := target
					label := "左侧文件"
					if right {
						label = "右侧文件"
					}
					if ui.Select(part, &selected, choices, func(item *editorTab) string { return item.tabTitle() }).Label(label).Grow(1).MinWidth(0).Height(28).Disabled(blocked).Changed() {
						c.After(func() {
							if right {
								s.right = selected
								s.mirror = nil
							} else {
								s.left = selected
							}
							s.rightFocused = right
							m.activate(selected)
						})
					}
					if right && ui.Button(part, "关闭分屏").Icon(closeIcon).IconOnly().Ghost().Size(28, 28).Disabled(blocked).Clicked() {
						c.After(func() { m.editorSplit = nil; m.focusActiveSource() })
					}
				})
				pane := target.source
				if right && s.left == s.right {
					if s.mirror == nil || s.mirror.text != target.source.text {
						s.mirror = newEditorPane("", false)
						s.mirror.text = target.source.text
						s.mirror.input.Editor = target.source.text
						s.mirror.view.Document = target.source.text
					}
					pane = s.mirror
				}
				pane.applyEditorPreferences(m.editorPrefs)
				pane.view.Snapshot.Wrap = target.source.view.Snapshot.Wrap
				pane.view.Snapshot.Language = target.format()
				pane.light = m.light
				pane.onFocused = func() {
					if !blocked && (m.active != target || s.rightFocused != right) {
						c.After(func() { s.rightFocused = right; m.activate(target); target.source.focus = false; pane.focus = true })
					}
				}
				box := pane.declare(part.Part("editor"), labelForSplit(right), !blocked).FillWidth().Grow(1).MinHeight(0)
				if !blocked {
					m.editorContextMenu(part.Part("menu"), box, target, pane)
				}

			})
		}
	})
}

func labelForSplit(right bool) string {
	if right {
		return "右侧编辑区"
	}
	return "左侧编辑区"
}
