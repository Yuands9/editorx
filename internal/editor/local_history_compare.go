package editor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"github.com/vivaui/viva/ui/codeeditor"
	"github.com/vivaui/viva/window"
)

const historyMergePart = 5 // The first five diff parts are its scrollbars, gutters and viewport.
const historyChangedNotice = "当前内容已更新。请查看重新比对后的区别。"
const historyMergeUndoRunes = 4 << 20 // Viva text.Editor's single-edit undo budget.

type historyCompareState struct {
	generic             bool
	leftLabel           string
	owner               *editorApp
	window              *app.Window
	invalidator         app.Invalidator
	store               *localHistoryStore
	source              string
	fileID              string
	preferred           string
	versions            []localHistoryVersion
	catalog             *ui.EffectState[[]localHistoryVersion, int]
	read                *ui.EffectState[localHistoryComparison, int]
	export              *ui.EffectState[string, int]
	selection           int
	comparison          *localHistoryComparison
	block               int
	onlyChanges, reveal bool
	problem, notice     string
	versionList         ui.ListState
	diff                codeeditor.Diff
	tab                 *editorTab
	editor              *text.Editor
	revision            uint64
	input               window.TextInput
	caret               int
	caretFocused        bool
	revealCaret         bool
	limitProblem        error
	ownerBlocked        bool
	ownerChanged        bool
	mergeNeedsReview    bool
	actionRows          *components.CodeDiffRow
	actionScope         gfx.Scope
	mergeActions        []components.CodeDiffGutterAction
}

func (m *editorApp) openHistoryComparison(path string, version localHistoryVersion) {
	if m.dataDirectory == "" {
		directory, err := editorDataDirectory()
		if err != nil {
			m.fail(err)
			return
		}
		m.dataDirectory = directory
	}
	store := m.documentHistory()
	if store == nil || path == "" && version.fileID == "" {
		return
	}
	id := version.fileID
	if id == "" {
		id = localHistoryFileID(path)
	}
	if previous := m.historyCompare; previous != nil {
		if previous.fileID == id && previous.window != nil && !previous.window.Closed() {
			if version.name != "" {
				previous.preferred = version.name
				if index := slices.IndexFunc(previous.versions, func(v localHistoryVersion) bool { return v.name == version.name }); index >= 0 && index != previous.selection {
					m.selectHistoryVersion(previous, index)
				}
			}
			previous.window.Show()
			previous.invalidator.Invalidate()
			return
		}
		m.closeHistoryComparison()
	}
	state := &historyCompareState{owner: m, store: store, source: path, fileID: id, preferred: version.name, selection: -1, block: -1, caret: -1}
	state.catalog = ui.NewEffect[[]localHistoryVersion, int](func(ctx context.Context) ([]localHistoryVersion, error) {
		return store.versionsFor(id)
	})
	state.diff.Pointer = state.pickDiffBlock
	state.diff.Text = state.textInput
	state.diff.Key = state.editingKey
	state.input.OnEvent = state.editUnopenedFile
	m.historyCompare = state
	if m.application != nil {
		window, err := ui.OpenWindow(m.application, state.windowConfig(), state.pageConfig())
		if err != nil {
			m.historyCompare = nil
			m.fail(fmt.Errorf("无法打开历史比对窗口：%w", err))
			m.invalidator.Invalidate()
			return
		}
		state.window = window
	}
}

func (m *editorApp) closeHistoryComparison() {
	state := m.historyCompare
	if state == nil {
		return
	}
	m.historyCompare = nil
	if state.window != nil {
		state.window.Close()
	}
}

func historyEditableTab(tab *editorTab) bool {
	return aiTextTab(tab) && !tab.source.readOnly
}

func (m *editorApp) selectHistoryVersion(state *historyCompareState, index int) {
	state.selection, state.block, state.problem = index, -1, ""
	state.diff.Code.ScrollTo(0, 0)
	m.readHistoryComparison(state)
}

func (m *editorApp) readHistoryComparison(state *historyCompareState) {
	if state.generic {
		state.syncCurrent()
		return
	}
	state.comparison = nil
	version := state.versions[state.selection]
	var working *document
	if tab := m.tabAtPath(state.source); historyEditableTab(tab) {
		doc := tab.doc
		doc.saved = tab.source.text.CommittedString()
		working = &doc
	}
	state.read = ui.NewEffect[localHistoryComparison, int](func(ctx context.Context) (localHistoryComparison, error) {
		return historyComparison(ctx, state.store, state.source, version, working), ctx.Err()
	})
}

func (m *editorApp) updateHistoryComparison(c ui.C, state *historyCompareState) {
	if state.catalog != nil {
		ui.Effect(c.Part("versions"), state.catalog, 0)
		if versions, err, ready := state.catalog.Take(); ready {
			state.catalog, state.versions = nil, versions
			if err != nil {
				state.problem = "无法读取历史版本：" + err.Error()
			} else if len(versions) != 0 {
				index := slices.IndexFunc(versions, func(v localHistoryVersion) bool { return v.name == state.preferred })
				m.selectHistoryVersion(state, max(0, index))
			}
		}
	}
	if state.read != nil {
		ui.Effect(c.Part("comparison"), state.read, 0)
		if comparison, err, ready := state.read.Take(); ready {
			state.read = nil
			if err != nil {
				state.problem = "无法读取历史比对：" + err.Error()
			} else {
				state.comparison = &comparison
				state.selectBlock(max(0, state.block))
			}
		}
	}
	if state.export != nil {
		ui.Effect(c.Part("export"), state.export, 0)
		if path, err, ready := state.export.Take(); ready {
			state.export = nil
			if err != nil {
				state.problem = "无法另存历史版本：" + err.Error()
			} else {
				state.notice = "已另存副本 · " + path
			}
		}
	}
}

func (state *historyCompareState) selectBlock(index int) {
	state.block = min(index, len(state.comparison.diff.blocks)-1)
	state.reveal = state.block >= 0
	state.mergeNeedsReview = false
}

func (state *historyCompareState) pickDiffBlock(event gfx.PointerEvent) gfx.EventResult {
	if state.comparison == nil || event.Kind != gfx.PointerDown || event.Button != gfx.PointerButtonPrimary {
		return gfx.EventResult{}
	}
	snapshot := &state.diff.Snapshot
	if event.Target != snapshot.LeftGutterID && event.Target != snapshot.RightGutterID {
		return gfx.EventResult{}
	}
	row, _, _, ok := state.diff.Code.GutterAt(gfx.Point{X: event.X, Y: event.Y})
	if !ok {
		return gfx.EventResult{}
	}
	for index, block := range state.comparison.diff.blocks {
		visible := block.visibleRange(state.onlyChanges)
		if row >= visible.Start && row < visible.End {
			state.block = index
			state.mergeNeedsReview = false
			return gfx.EventResult{Handled: true, Repaint: true}
		}
	}
	return gfx.EventResult{}
}

func (state *historyCompareState) mergeActionsFor(result components.CodeDiffResult, scope gfx.Scope) []components.CodeDiffGutterAction {
	if state.actionRows == &result.Rows[0] && state.actionScope == scope {
		return state.mergeActions
	}
	state.actionRows, state.actionScope = &result.Rows[0], scope
	state.mergeActions = state.mergeActions[:0]
	for index, block := range state.comparison.diff.blocks {
		label := fmt.Sprintf("合并第 %d 处历史改动", index+1)
		if state.generic {
			label = fmt.Sprintf("合并第 %d 处改动", index+1)
		}
		state.mergeActions = append(state.mergeActions, components.CodeDiffGutterAction{
			Row: block.visibleRange(state.onlyChanges).Start, Left: true, ID: scope.At(historyMergePart + index),
			Label: label,
		})
	}
	return state.mergeActions
}

func (m *editorApp) canMergeHistory(state *historyCompareState) bool {
	return state.canEditCurrent() && state.block >= 0 && !state.editor.CompositionActive()
}

func (m *editorApp) mergeHistoryChange(state *historyCompareState, whole bool) {
	if !m.canMergeHistory(state) {
		return
	}
	comparison := state.comparison
	if !state.attachCurrentTab() {
		return
	}
	tab, editor := state.tab, state.editor
	current := editor.CommittedString()
	if current != comparison.current.saved || tab.doc.eol != comparison.current.eol {
		state.notice = historyChangedNotice
		m.readHistoryComparison(state)
		return
	}
	start, end, value := 0, len(current), ""
	if whole {
		value = strings.Join(comparison.diff.full.Left, "\n")
	} else {
		start, end, value = comparison.diff.replacement(comparison.diff.blocks[state.block], len(current))
	}
	start, end, value = trimHistoryReplacement(current, start, end, value)
	removed := current[start:end]
	if len(removed)+len(value) > historyMergeUndoRunes && utf8.RuneCountInString(removed)+utf8.RuneCountInString(value) > historyMergeUndoRunes {
		state.problem = "这次合并太大，无法保留撤销记录。可以另存历史副本。"
		if whole && len(comparison.diff.blocks) > 1 {
			state.problem = "这次恢复太大，无法保留撤销记录。可以逐块合并，或另存历史副本。"
		}
		return
	}
	editor.ReplaceRange(text.Range{Start: editor.RuneIndex(start), End: editor.RuneIndex(end)}, value)
	state.syncCurrent()
	state.revealCaret, state.mergeNeedsReview = true, false
	state.problem, state.notice = "", "已合并到编辑区 · Ctrl+Z 撤回"
	m.status = "已合并改动 · 可撤销"
}

func (m *editorApp) exportHistoryComparison(state *historyCompareState) {
	version := state.versions[state.selection]
	path, err := chooseHistoryCopyPath(state.store, state.source, version)
	if errors.Is(err, app.ErrCanceled) {
		return
	}
	if err != nil {
		state.problem = "无法另存历史版本：" + err.Error()
		return
	}
	state.problem, state.notice = "", ""
	state.export = ui.NewEffect[string, int](func(ctx context.Context) (string, error) {
		err := writeFileAtomically(path, func(output io.Writer) error {
			return state.store.copyVersion(ctx, version, output)
		})
		return path, err
	})
}
