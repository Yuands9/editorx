package editor

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
)

type compareSource struct {
	tab  *editorTab
	path string
}
type textComparePicker struct {
	open    bool
	sources [2]compareSource
	problem string
	read    *ui.EffectState[localHistoryComparison, int]
}

func (state *historyCompareState) currentTab() *editorTab {
	if state.generic {
		return state.tab
	}
	return state.owner.tabAtPath(state.source)
}

func (m *editorApp) openTextComparePicker() {
	state := &textComparePicker{open: true}
	if historyEditableTab(m.active) {
		state.sources[1].tab = m.active
	}
	for _, tab := range m.tabs {
		if tab != m.active && historyEditableTab(tab) {
			state.sources[0].tab = tab
			break
		}
	}
	m.comparePicker = state
}

func (source compareSource) snapshot() (document, error) {
	if source.tab == nil {
		return document{path: source.path}, nil
	}
	if !historyEditableTab(source.tab) || source.tab.source.text.CompositionActive() {
		return document{}, errors.New("请先完成输入，并选择普通文本编辑区")
	}
	doc := source.tab.doc
	doc.saved = source.tab.source.text.CommittedString()
	return doc, historyCompareSize(doc.saved)
}

func (m *editorApp) startTextCompare() {
	s := m.comparePicker
	if s == nil || s.read != nil {
		return
	}
	var docs [2]document
	for i, source := range s.sources {
		if source.tab == nil && source.path == "" {
			s.problem = "请选择两份文本"
			return
		}
		doc, err := source.snapshot()
		if err != nil {
			s.problem = err.Error()
			return
		}
		docs[i] = doc
	}
	sources := s.sources
	s.problem = ""
	s.read = ui.NewEffect[localHistoryComparison, int](func(ctx context.Context) (localHistoryComparison, error) {
		for i, source := range sources {
			if source.tab == nil {
				var err error
				docs[i], err = readHistoryCurrentFile(ctx, source.path)
				if err != nil {
					return localHistoryComparison{}, err
				}
			}
		}
		comparison := localHistoryComparison{old: docs[0], current: docs[1], fromEditor: true}
		comparison.compareLines()
		return comparison, ctx.Err()
	})
}

func (m *editorApp) openTextComparison(comparison localHistoryComparison, tab *editorTab, leftLabel string) {
	m.closeHistoryComparison()
	state := &historyCompareState{owner: m, generic: true, leftLabel: leftLabel, source: tab.doc.path, tab: tab,
		comparison: &comparison, selection: -1, block: -1, caret: -1}
	state.diff.Pointer, state.diff.Text, state.diff.Key = state.pickDiffBlock, state.textInput, state.editingKey
	state.input.OnEvent = state.editUnopenedFile
	m.historyCompare = state
	state.syncCurrent()
	state.selectBlock(0)
	if m.application != nil {
		window, err := ui.OpenWindow(m.application, state.windowConfig(), state.pageConfig())
		if err != nil {
			m.historyCompare = nil
			m.fail(fmt.Errorf("无法打开对比窗口：%w", err))
			return
		}
		state.window = window
	}
}

func (m *editorApp) textCompareDialog(c ui.C) {
	s := m.comparePicker
	if s == nil {
		return
	}
	if s.read != nil {
		ui.Effect(c.Part("read"), s.read, 0)
		if comparison, err, done := s.read.Take(); done {
			s.read = nil
			if err != nil {
				s.problem = err.Error()
			} else {
				right := s.sources[1].tab
				if right == nil {
					right = m.tabAtPath(s.sources[1].path)
				}
				if right == nil {
					right = m.openLoadedFile(openedEditorFile{doc: comparison.current})
				}
				if !historyEditableTab(right) {
					s.problem = "右侧文件当前不能编辑"
					return
				}
				label := filepath.Base(s.sources[0].path) + " · 磁盘快照"
				if s.sources[0].tab != nil {
					label = s.sources[0].tab.tabTitle() + " · 打开时的快照"
				}
				m.comparePicker = nil
				m.openTextComparison(comparison, right, label)
				return
			}
		}
	}
	busy := s.read != nil
	var choices []*editorTab
	choices = append(choices, nil)
	for _, tab := range m.tabs {
		if historyEditableTab(tab) {
			choices = append(choices, tab)
		}
	}
	dismissed := ui.ModalWith(c, &s.open, "通用分屏对比", ui.ModalSpec{Width: min(620, c.Bounds().Width-32)}, func() {
		ui.Column(c).FillWidth().Gap(12).Children(func() {
			for i, label := range []string{"左侧 · 参考文本", "右侧 · 当前编辑"} {
				source := &s.sources[i]
				ui.Text(c, label).FontSize(13).Bold()
				ui.Select(c.Part("source").KeyInt(i), &source.tab, choices, func(tab *editorTab) string {
					if tab == nil {
						return "选择磁盘文件…"
					}
					return tab.tabTitle()
				}).Label(label).FillWidth().Height(34).Disabled(busy)
				if source.tab == nil {
					ui.Row(c.Part("file").KeyInt(i)).FillWidth().Items(ui.Center).Gap(8).Children(func() {
						ui.Text(c, source.path).FontSize(12).Muted().Grow(1).MinWidth(0).Ellipsis()
						if ui.Button(c, "选择"+label).Icon(folderIcon).Ghost().Disabled(busy).Clicked() {
							c.After(func() {
								paths, err := app.OpenFiles(app.OpenFileOptions{Title: label})
								if err == nil && len(paths) > 0 {
									source.path = paths[0]
								}
							})
						}
					})
				}
			}
			ui.Wrap(c, "支持两份文本或打开标签（含未保存内容）。左侧保留快照；右侧可编辑、逐块合并并撤销。每份最多 32 MiB。").FontSize(12).Muted()
			if s.problem != "" {
				ui.Wrap(c, s.problem).Color(m.theme.Danger)
			}
		})
	}, func() {
		label := "开始对比"
		if busy {
			label = "正在读取并对比…"
		}
		if ui.Button(c, label).Disabled(busy).Clicked() {
			c.After(m.startTextCompare)
		}
	})
	if dismissed || !s.open {
		m.comparePicker = nil
	}
}

func (m *editorApp) hasTab(tab *editorTab) bool { return slices.Contains(m.tabs, tab) }
