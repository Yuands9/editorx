package editor

import (
	"context"
	"path/filepath"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"github.com/vivaui/viva/window"
)

func (state *historyCompareState) windowConfig() app.Config {
	owner := state.owner
	title := "比对历史"
	if state.source != "" {
		title += " · " + filepath.Base(state.source)
	}
	const width, height = 1240, 780
	var origin *gfx.Point
	if bounds, ok := owner.nativeWindow.Frame(); ok {
		origin = &gfx.Point{X: bounds.X + (bounds.Width-width)/2, Y: bounds.Y + (bounds.Height-height)/2}
	}
	return app.Config{
		Title: title, Icon: themedApplicationIcon(owner.light), Width: width, Height: height,
		MinWidth: 760, MinHeight: 440, Origin: origin, Invalidator: &state.invalidator,
		ClearColor: app.Color{Red: float64(owner.theme.Card.Red), Green: float64(owner.theme.Card.Green), Blue: float64(owner.theme.Card.Blue), Alpha: 1},
		OnActivate: state.invalidator.Invalidate,
		OnShortcut: state.shortcut,
		OnKey:      state.shortcut,
		OnClosed: func() {
			if owner.historyCompare == state {
				owner.historyCompare = nil
			}
			comparison := state.comparison
			if owner.nativeWindow != nil && (state.read != nil || comparison != nil &&
				len(comparison.old.saved)+len(comparison.current.saved) >= closedTextCollectionThreshold) {
				releaseClosedTextMemory()
			}
		},
	}
}

func (state *historyCompareState) pageConfig() ui.PageConfig {
	return ui.PageConfig{View: state.view, Theme: &state.owner.theme, Styles: &editorControlStyles}
}

func (state *historyCompareState) blocked() bool {
	owner := state.owner
	return owner.historyCompare != state || owner.windowClosed || owner.windowClosePending || owner.confirm ||
		owner.desktop != nil && owner.desktop.exiting
}

func (state *historyCompareState) detached() bool {
	return state.tab != nil && state.owner.tabAtPath(state.source) != state.tab
}

func (m *editorApp) updateHistoryWindow(c ui.C) {
	state := m.historyCompare
	if state == nil {
		return
	}
	if state.detached() {
		c.After(func() {
			if m.historyCompare == state {
				m.closeHistoryComparison()
			}
		})
		return
	}
	tab := m.tabAtPath(state.source)
	changed := historyEditableTab(tab) &&
		(state.tab != tab || state.editor != tab.source.text || state.revision != tab.source.text.TrackEdits() ||
			state.comparison != nil && state.comparison.current.eol != tab.doc.eol)
	state.ownerChanged = state.ownerChanged || changed
	if state.blocked() != state.ownerBlocked || changed {
		state.invalidator.Invalidate()
	}
}

func (state *historyCompareState) syncCurrent() {
	state.limitProblem = nil
	comparison := state.comparison
	if comparison == nil || comparison.oldProblem != "" {
		return
	}
	tab := state.owner.tabAtPath(state.source)
	if !historyEditableTab(tab) {
		if tab != nil || comparison.diskProblem != "" {
			state.editor = nil
			return
		}
		if state.editor == nil || state.editor.String() != comparison.current.saved {
			state.editor = text.NewEditor(comparison.current.saved)
			state.editor.MoveTo(0, false)
		}
		return
	}
	editor := tab.source.text
	revision := editor.TrackEdits()
	if state.tab == tab && state.editor == editor && state.revision != revision {
		if tab.markdownMode == "预览" {
			tab.markdownMode = "分屏"
		}
		if tab.htmlMode == "预览" {
			tab.htmlMode = "分屏"
		}
		state.owner.invalidator.Invalidate()
	}
	state.tab, state.editor, state.revision = tab, editor, revision
	comparison.fromEditor = true
	value := editor.String()
	if comparison.current.saved != value || comparison.current.eol != tab.doc.eol || comparison.diskProblem != "" {
		state.limitProblem = historyCompareSize(value)
		if state.limitProblem != nil {
			return
		}
		state.mergeNeedsReview = state.mergeNeedsReview || state.ownerChanged || !state.caretFocused
		comparison.current, comparison.diskProblem = tab.doc, ""
		comparison.current.saved = value
		comparison.compareLines()
		state.block = min(max(0, state.block), len(comparison.diff.blocks)-1)
	}
	state.ownerChanged = false
}

func (state *historyCompareState) canEditCurrent() bool {
	comparison := state.comparison
	return !state.blocked() && !state.detached() && state.limitProblem == nil && state.editor != nil && comparison != nil &&
		comparison.oldProblem == "" && comparison.diskProblem == "" && (state.tab == nil || historyEditableTab(state.tab))
}

func (state *historyCompareState) attachCurrentTab() bool {
	if !state.canEditCurrent() {
		return false
	}
	if state.tab != nil {
		return true
	}
	owner, comparison := state.owner, state.comparison
	tab := owner.tabAtPath(state.source)
	var current document
	var err error
	if tab == nil {
		current, err = readHistoryCurrentFile(context.Background(), state.source)
	} else if historyEditableTab(tab) {
		current = tab.doc
		current.saved = tab.source.text.CommittedString()
	} else {
		state.problem = "当前文件的编辑区不可用，可另存历史副本。"
		return false
	}
	if err != nil || current.saved != comparison.current.saved || current.eol != comparison.current.eol {
		state.notice = historyChangedNotice
		owner.readHistoryComparison(state)
		return false
	}
	if tab == nil {
		tab = owner.openLoadedFile(openedEditorFile{doc: current})
	}
	selection := state.editor.Selection()
	if state.editor.Caret() == selection.Start {
		selection.Start, selection.End = selection.End, selection.Start
	}
	tab.source.text.SetSelection(selection)
	state.tab, state.editor = tab, tab.source.text
	state.revision = state.editor.TrackEdits()
	owner.invalidator.Invalidate()
	return true
}

func (state *historyCompareState) textInput(window.TextInputContext) *window.TextInput {
	if !state.canEditCurrent() {
		return nil
	}
	state.input.MonoFace = gfx.MonoFamily(state.owner.editorPrefs.FontFamily)
	if state.tab != nil {
		state.input.Editor = state.editor
	} else {
		state.input.Editor = nil
		state.input.State = app.TextStateOf(state.editor)
	}
	return &state.input
}

func (state *historyCompareState) editUnopenedFile(event window.TextEvent) (window.TextState, error) {
	if state.editor == nil {
		return window.TextState{}, nil
	}
	if state.canEditCurrent() && (!historyTextChanges(event) || state.attachCurrentTab()) {
		event.Apply(state.editor)
		state.revealCaret = true
	}
	return app.TextStateOf(state.editor), nil
}

func historyTextChanges(event window.TextEvent) bool {
	if event.CompositionPhase != text.CompositionNone {
		return true
	}
	switch event.Kind {
	case window.TextInsert, window.TextPaste, window.TextSetMarked, window.TextUnmark:
		return true
	case window.TextCommandEvent:
		switch event.Command {
		case window.TextDeleteBackward, window.TextDeleteForward, window.TextDeleteWordBackward, window.TextDeleteWordForward,
			window.TextDeleteToLineStart, window.TextDeleteToLineEnd, window.TextInsertNewline, window.TextInsertLineBreak, window.TextInsertTab:
			return true
		}
	}
	return false
}

func (state *historyCompareState) editingKey(event gfx.KeyEvent) gfx.EventResult {
	if event.Release || !state.canEditCurrent() || state.editor.CompositionActive() {
		return gfx.EventResult{}
	}
	if command, ok := components.CodeEditorCommandForKey(event); ok {
		if state.attachCurrentTab() {
			command.Apply(state.editor)
		}
		state.revealCaret = true
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	movement, extend := components.TextareaMovementForKey(event.Key, event.Modifiers)
	if movement == components.TextareaMoveNone {
		return gfx.EventResult{}
	}
	editor := state.editor
	to := state.diff.Code.Move(editor.String(), editor.ByteOffset(editor.Caret()), movement)
	editor.MoveTo(editor.RuneIndex(to), extend)
	state.revealCaret = true
	return gfx.EventResult{Handled: true, Repaint: true}
}

func (state *historyCompareState) shortcut(event gfx.KeyEvent) gfx.EventResult {
	if event.Release || state.blocked() || state.detached() {
		return gfx.EventResult{}
	}
	chord, _ := keymap.FromEvent(event)
	if !chord.Valid() {
		return gfx.EventResult{}
	}
	editor, tab := state.editor, state.tab
	if tab != nil {
		if !historyEditableTab(tab) {
			return gfx.EventResult{}
		}
		editor = tab.source.text
	}
	if editor != nil && editor.CompositionActive() {
		return gfx.EventResult{}
	}
	changed := false
	switch chord {
	case keymap.Chord{Modifiers: ui.Ctrl, Char: 'z'}:
		if editor != nil && editor.Undo() {
			state.notice, changed = "已撤销", true
		}
	case keymap.Chord{Modifiers: ui.Ctrl, Char: 'y'}, keymap.Chord{Modifiers: ui.Ctrl | ui.Shift, Char: 'z'}:
		if editor != nil && editor.Redo() {
			state.notice, changed = "已重做", true
		}
	case state.owner.keyBindings.chord(shortcutSave):
		if tab != nil && state.owner.saveTab(tab, false) {
			state.notice = "已保存当前版本"
		} else if state.owner.showError && state.owner.nativeWindow != nil {
			state.owner.nativeWindow.Show()
		}
		state.owner.invalidator.Invalidate()
		return gfx.EventResult{Handled: true, Repaint: true}
	default:
		return gfx.EventResult{}
	}
	if changed {
		state.syncCurrent()
		state.problem, state.revealCaret, state.mergeNeedsReview = "", true, false
	}
	return gfx.EventResult{Handled: true, Repaint: changed}
}
