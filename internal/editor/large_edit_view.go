package editor

import (
	"context"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"github.com/vivaui/viva/window"
)

const (
	largeEditWindowBytes = 64 << 10
	largeEditWindowLines = 128
	largeEditUndoLimit   = 100
)

type largeEditWindow struct {
	start, end int64
	line       int
	value      string
	// Display byte offsets of the LF in a CRLF. At that offset we map to
	// the start of the whole CRLF, so backspace and selections never split it.
	crlf []int
}

func newLargeEditWindow(start int64, line int, value string) largeEditWindow {
	viewport := largeEditWindow{start: start, end: start + int64(len(value)), line: line, value: value}
	for i := 0; i+1 < len(value); i++ {
		if value[i] == '\r' && value[i+1] == '\n' {
			viewport.crlf = append(viewport.crlf, i-len(viewport.crlf))
		}
	}
	if len(viewport.crlf) > 0 {
		viewport.value = strings.ReplaceAll(value, "\r\n", "\n")
	}
	return viewport
}

func (w largeEditWindow) absolute(offset int) int64 {
	return w.start + int64(offset+sort.SearchInts(w.crlf, offset))
}

func (w largeEditWindow) local(offset int64) int {
	raw := int(max(0, min(w.end-w.start, offset-w.start)))
	return raw - sort.Search(len(w.crlf), func(i int) bool { return w.crlf[i]+i >= raw })
}

type largeEditPosition struct {
	anchor, caret int64
}

func (p largeEditPosition) rangeOffsets() (int64, int64) {
	return min(p.anchor, p.caret), max(p.anchor, p.caret)
}

type largeEditHistory struct {
	revision *largeEditRevision
	position largeEditPosition
}

type largeEditView struct {
	revision, saved   *largeEditRevision
	basePath          string
	position          largeEditPosition
	viewport          largeEditWindow
	top               int64
	pendingTop        bool
	lastType          time.Time
	undo, redo        []largeEditHistory
	bounds, barBounds gfx.Rect
	bar               ui.Handlers
	barDrag           float32
	selecting         bool
	selectionClicks   int
	selectionUnit     largeEditPosition
	searchQuery       largeEditQuery
	searchJob         *ui.EffectState[largeEditSearchOutcome, int]
	searchMatch       text.Span
	searchSelected    bool

	draftRevision *largeEditRevision
	draft         largeEditSession
}

func (t *editorTab) enterLargeEdit(loaded largeEditLoad, saved *largeEditRevision, position largeEditPosition, top int64) error {
	pane, state := t.source, t.largeText
	t.doc = loaded.doc
	edit := &largeEditView{revision: loaded.revision, saved: saved, basePath: loaded.doc.path, position: position}
	state.editing = edit
	state.search, state.allMatches, state.lines, state.indexing = nil, nil, largeLineIndex{}, nil
	state.selectedResult, state.found = -1, -1
	if !t.search.ready && t.search.query.CommittedString() == "" && state.query.Text() != "" {
		t.search.field.SetText(state.query.Text())
		t.search.options.CaseSensitive = true
	}
	if state.searchOpen {
		t.search.open, t.search.focus, t.search.centerOnOpen = true, true, true
	}
	t.search.ready = false
	state.searchOpen, state.focus = false, false
	pane.readOnly, pane.view.Snapshot.ReadOnly = false, false
	pane.view.Snapshot.LineNumbers, pane.view.Snapshot.ContinuousCaretBlink = true, true
	pane.view.Snapshot.ScrollbarID = gfx.ID{}
	pane.view.Snapshot.Placeholder = "文件为空，输入开始编辑"
	pane.view.Scroll, pane.view.Pointer = t.scrollLargeEdit, t.pointLargeEdit
	pane.view.Key = func(event gfx.KeyEvent) gfx.EventResult {
		if pane.searchKey != nil {
			return pane.searchKey(event)
		}
		return t.largeEditKey(event)
	}
	pane.view.Text = func(window.TextInputContext) *window.TextInput {
		pane.input.State = app.TextStateOf(pane.text)
		return &pane.input
	}
	pane.input.Editor = nil
	pane.input.CopyOverride = true
	pane.input.CopyText = func() string {
		start, end := edit.position.rangeOffsets()
		selected, err := edit.revision.read(start, end)
		if err != nil {
			state.problem = err.Error()
			return ""
		}
		return selected
	}
	pane.input.OnEvent = func(event window.TextEvent) (window.TextState, error) {
		t.handleLargeEditText(event)
		state := app.TextStateOf(pane.text)
		state.Redraw = true
		pane.input.State = state
		return state, nil
	}
	edit.bar.Pointer = t.pointLargeEditScrollbar
	return t.loadLargeEditWindow(top)
}

func (t *editorTab) loadLargeEditWindow(top int64) error {
	edit, pane := t.largeText.editing, t.source
	r := edit.revision
	top = max(0, min(top, r.size))
	line, err := r.lineAt(top)
	if err != nil {
		return err
	}
	start, err := r.lineStart(max(0, line-32))
	if err != nil {
		return err
	}
	end, err := r.lineStart(line + largeEditWindowLines)
	if err != nil {
		return err
	}
	if top-start > largeEditWindowBytes/3 {
		start = top - largeEditWindowBytes/3
	}
	if end-start > largeEditWindowBytes {
		end = start + largeEditWindowBytes
	}
	for !r.boundary(start) {
		start++
	}
	for !r.boundary(end) {
		end--
	}
	// Do not clip between the CR and LF of a line break.
	if start > 0 {
		edge, err := r.read(start-1, min(r.size, start+1))
		if err != nil {
			return err
		}
		if edge == "\r\n" {
			start--
		}
	}
	if end < r.size {
		edge, err := r.read(end-1, end+1)
		if err != nil {
			return err
		}
		if edge == "\r\n" {
			end--
		}
	}
	value, err := r.read(start, end)
	if err != nil {
		return err
	}
	first, err := r.lineAt(start)
	if err != nil {
		return err
	}
	viewport := newLargeEditWindow(start, first, value)
	options, _ := pane.text.Code()
	pane.text = text.NewEditor(viewport.value)
	pane.text.SetCode(options)
	pane.text.EnableTentativeComposition()
	pane.view.Document = pane.text
	pane.view.Snapshot.LineNumberOffset = first
	edit.viewport, edit.top, edit.pendingTop = viewport, top, true
	t.syncLargeEditSelection(false)
	t.markLargeTextMatches()
	return nil
}

func (t *editorTab) syncLargeEditSelection(reveal bool) {
	edit, pane := t.largeText.editing, t.source
	w := edit.viewport
	pane.text.SetSelection(text.Range{
		Start: pane.text.RuneIndex(w.local(edit.position.anchor)),
		End:   pane.text.RuneIndex(w.local(edit.position.caret)),
	})
	if reveal {
		pane.view.Code.Reveal(w.local(edit.position.caret))
	}
}

func (t *editorTab) moveLargeEdit(position largeEditPosition, reveal bool) {
	state := t.largeText
	edit := state.editing
	edit.lastType = time.Time{}
	edit.position = position
	if reveal && (position.caret < edit.viewport.start || position.caret >= edit.viewport.end && edit.viewport.end != edit.revision.size) {
		if err := t.loadLargeEditWindow(position.caret); err != nil {
			state.problem = err.Error()
		}
		return
	}
	t.syncLargeEditSelection(reveal)
}

func (t *editorTab) noteLargeEdit(next *largeEditRevision, position largeEditPosition, typed bool) {
	state := t.largeText
	edit := state.editing
	if slices.Equal(next.pieces, edit.saved.pieces) {
		next = edit.saved
	}
	if next == edit.revision {
		return
	}
	now := time.Now()
	if !typed || edit.position.anchor != edit.position.caret || now.Sub(edit.lastType) > 500*time.Millisecond {
		if len(edit.undo) == largeEditUndoLimit {
			copy(edit.undo, edit.undo[1:])
			edit.undo = edit.undo[:len(edit.undo)-1]
		}
		edit.undo = append(edit.undo, largeEditHistory{edit.revision, edit.position})
	}
	edit.lastType = time.Time{}
	if typed {
		edit.lastType = now
	}
	clear(edit.redo)
	edit.redo = edit.redo[:0]
	t.useLargeEditRevision(next, position)
}

func (t *editorTab) useLargeEditRevision(next *largeEditRevision, position largeEditPosition) {
	state, pane := t.largeText, t.source
	edit := state.editing
	t.updateLargeEditBookmarks(edit.revision, next)
	edit.revision, edit.position = next, position
	state.found, state.selectedResult, state.search, state.allMatches, state.problem = -1, -1, nil, nil, ""
	edit.searchJob, edit.searchSelected = nil, false
	t.search.message = ""
	pane.view.Snapshot.CurrentMark = text.Span{}
	// Retain the visible top while typing in the viewport. For a paste or an
	// undo that jumps elsewhere, place the window at the new caret.
	top := edit.top
	if position.caret < edit.viewport.start || position.caret >= edit.viewport.end || top > position.caret {
		top = position.caret
	}
	if err := t.loadLargeEditWindow(top); err != nil {
		state.problem = err.Error()
	}
	if position.caret >= edit.viewport.end && edit.viewport.end != next.size {
		if err := t.loadLargeEditWindow(position.caret); err != nil {
			state.problem = err.Error()
		}
	}
	t.syncLargeEditSelection(true)
}

func (t *editorTab) undoLargeEdit(redo bool) {
	edit := t.largeText.editing
	history, opposite := &edit.undo, &edit.redo
	if redo {
		history, opposite = opposite, history
	}
	n := len(*history)
	if n == 0 {
		return
	}
	old := (*history)[n-1]
	(*history)[n-1] = largeEditHistory{}
	*history = (*history)[:n-1]
	*opposite = append(*opposite, largeEditHistory{edit.revision, edit.position})
	edit.lastType = time.Time{}
	t.useLargeEditRevision(old.revision, old.position)
}

func (t *editorTab) largeEditInputValue(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	if t.doc.eol == "CRLF" {
		value = strings.ReplaceAll(value, "\n", "\r\n")
	}
	return value
}

func (t *editorTab) replaceLargeEdit(start, end int64, value string, typed bool) {
	t.replaceLargeEditRaw(start, end, t.largeEditInputValue(value), typed)
}

func (t *editorTab) replaceLargeEditRaw(start, end int64, value string, typed bool) {
	state := t.largeText
	edit := state.editing
	if start == end && value == "" {
		return
	}
	if end-start == int64(len(value)) && len(value) <= largeEditWindowBytes {
		if old, err := edit.revision.read(start, end); err == nil && old == value {
			t.moveLargeEditCaret(end, false)
			return
		}
	}
	next, err := edit.revision.apply(context.Background(), []largeEditChange{{Start: start, End: end, Text: value}})
	if err != nil {
		state.problem = err.Error()
		t.syncLargeEditSelection(false)
		return
	}
	caret := start + int64(len(value))
	t.noteLargeEdit(next, largeEditPosition{caret, caret}, typed)
}

func (t *editorTab) handleLargeEditText(event window.TextEvent) {
	state, pane := t.largeText, t.source
	edit := state.editing
	_, composing := pane.text.Composition()
	if state.busyEditing() {
		return
	}
	start, end := edit.position.rangeOffsets()
	if composing || event.Kind == window.TextSetMarked || event.Kind == window.TextUnmark ||
		event.Kind == window.TextCancelComposition || event.CompositionPhase != text.CompositionNone {
		before := edit.viewport.value
		left, right := edit.viewport.local(start), edit.viewport.local(end)
		if !composing && event.HasReplacement {
			left = pane.text.ByteOffset(event.Replacement.Start)
			right = pane.text.ByteOffset(event.Replacement.End)
		}
		if !composing && event.HasReplacementUTF16 {
			left = pane.text.ByteOffset(pane.text.UTF16ToRuneIndex(event.ReplacementUTF16.Start))
			right = pane.text.ByteOffset(pane.text.UTF16ToRuneIndex(event.ReplacementUTF16.Start + event.ReplacementUTF16.Length))
		}
		if !event.Apply(pane.text) {
			return
		}
		if _, stillComposing := pane.text.Composition(); stillComposing {
			edit.lastType = time.Time{}
			return
		}
		t.commitLargeEditLocal(before, left, right, event.Kind == window.TextCancelComposition || event.CompositionPhase == text.CompositionCancel)
		return
	}
	switch event.Kind {
	case window.TextInsert, window.TextPaste:
		if event.HasReplacement {
			start = edit.viewport.absolute(pane.text.ByteOffset(event.Replacement.Start))
			end = edit.viewport.absolute(pane.text.ByteOffset(event.Replacement.End))
		}
		t.replaceLargeEdit(start, end, event.Text, event.Kind == window.TextInsert && !strings.ContainsAny(event.Text, "\r\n"))
	case window.TextCommandEvent:
		t.commandLargeEdit(event)
	case window.TextSetSelection:
		start := edit.viewport.absolute(pane.text.ByteOffset(event.Selection.Start))
		end := edit.viewport.absolute(pane.text.ByteOffset(event.Selection.End))
		t.moveLargeEdit(largeEditPosition{start, end}, false)
	}
}

func (t *editorTab) commitLargeEditLocal(before string, left, right int, cancelled bool) {
	pane, edit := t.source, t.largeText.editing
	after := pane.text.String()
	if cancelled || before == after {
		t.syncLargeEditSelection(false)
		return
	}
	prefix := 0
	for prefix < min(left, len(before), len(after)) && before[prefix] == after[prefix] {
		prefix++
	}
	for prefix > 0 && prefix < len(before) && prefix < len(after) && (!utf8.RuneStart(before[prefix]) || !utf8.RuneStart(after[prefix])) {
		prefix--
	}
	oldEnd, newEnd := len(before), len(after)
	for oldEnd > max(prefix, right) && newEnd > prefix && before[oldEnd-1] == after[newEnd-1] {
		oldEnd--
		newEnd--
	}
	for oldEnd < len(before) && newEnd < len(after) && (!utf8.RuneStart(before[oldEnd]) || !utf8.RuneStart(after[newEnd])) {
		oldEnd++
		newEnd++
	}
	start, end := edit.viewport.absolute(prefix), edit.viewport.absolute(oldEnd)
	selectedStart, selectedEnd := edit.position.rangeOffsets()
	if prefix == left && selectedStart < start && left == 0 {
		start = selectedStart
	}
	if oldEnd == right && selectedEnd > end && right == len(before) {
		end = selectedEnd
	}
	insert := after[prefix:newEnd]
	if t.doc.eol == "混合" && strings.Count(before[prefix:oldEnd], "\n") == strings.Count(insert, "\n") {
		var originalEndings strings.Builder
		oldAt := prefix
		for {
			line, tail, more := strings.Cut(insert, "\n")
			originalEndings.WriteString(line)
			if !more {
				break
			}
			oldAt += strings.IndexByte(before[oldAt:oldEnd], '\n')
			i := sort.SearchInts(edit.viewport.crlf, oldAt)
			if i < len(edit.viewport.crlf) && edit.viewport.crlf[i] == oldAt {
				originalEndings.WriteByte('\r')
			}
			originalEndings.WriteByte('\n')
			insert, oldAt = tail, oldAt+1
		}
		t.replaceLargeEditRaw(start, end, originalEndings.String(), false)
		return
	}
	t.replaceLargeEdit(start, end, insert, false)
}

func (t *editorTab) commandLargeEdit(event window.TextEvent) {
	edit, pane := t.largeText.editing, t.source
	position := edit.position
	start, end := position.rangeOffsets()
	r := edit.revision
	switch event.Command {
	case window.TextUndo:
		t.undoLargeEdit(false)
	case window.TextRedo:
		t.undoLargeEdit(true)
	case window.TextSelectAll:
		t.moveLargeEdit(largeEditPosition{0, r.size}, false)
	case window.TextMoveDocumentStart, window.TextMoveDocumentStartAndModifySelection:
		t.moveLargeEditCaret(0, event.Command == window.TextMoveDocumentStartAndModifySelection)
	case window.TextMoveDocumentEnd, window.TextMoveDocumentEndAndModifySelection:
		t.moveLargeEditCaret(r.size, event.Command == window.TextMoveDocumentEndAndModifySelection)
	case window.TextInsertTab:
		t.largeEditCodeCommand(components.CodeEditorInsertTab)
	case window.TextInsertNewline, window.TextInsertLineBreak:
		if start < edit.viewport.start || start > edit.viewport.end {
			if err := t.loadLargeEditWindow(start); err != nil {
				t.largeText.problem = err.Error()
				return
			}
		}
		left, right := edit.viewport.local(start), edit.viewport.local(end)
		event.Apply(pane.text)
		t.commitLargeEditLocal(edit.viewport.value, left, right, false)
	case window.TextDeleteBackward, window.TextDeleteForward, window.TextDeleteWordBackward, window.TextDeleteWordForward,
		window.TextDeleteToLineStart, window.TextDeleteToLineEnd:
		if start == end {
			forward := event.Command == window.TextDeleteForward || event.Command == window.TextDeleteWordForward || event.Command == window.TextDeleteToLineEnd
			target := t.largeEditCommandOffset(event, forward)
			start, end = min(start, target), max(end, target)
		}
		t.replaceLargeEdit(start, end, "", true)
	default:
		var forward, extend bool
		switch event.Command {
		case window.TextMoveRight, window.TextMoveWordRight, window.TextMoveLineEnd:
			forward = true
		case window.TextMoveRightAndModifySelection, window.TextMoveWordRightAndModifySelection, window.TextMoveLineEndAndModifySelection:
			forward, extend = true, true
		case window.TextMoveLeftAndModifySelection, window.TextMoveWordLeftAndModifySelection, window.TextMoveLineStartAndModifySelection:
			extend = true
		}
		if start != end && !extend && (event.Command == window.TextMoveLeft || event.Command == window.TextMoveRight) {
			if forward {
				start = end
			}
			t.moveLargeEditCaret(start, false)
			return
		}
		t.moveLargeEditCaret(t.largeEditCommandOffset(event, forward), extend)
	}
}

func (t *editorTab) largeEditCommandOffset(event window.TextEvent, forward bool) int64 {
	edit := t.largeText.editing
	caret := edit.position.caret
	w, r := edit.viewport, edit.revision
	switch event.Command {
	case window.TextMoveLineStart, window.TextMoveLineStartAndModifySelection, window.TextDeleteToLineStart:
		return t.largeEditLineEdge(false)
	case window.TextMoveLineEnd, window.TextMoveLineEndAndModifySelection, window.TextDeleteToLineEnd:
		return t.largeEditLineEdge(true)
	}
	if caret >= w.start && caret <= w.end {
		local := t.source.text.RuneIndex(w.local(caret))
		probe := t.source.text
		probe.MoveTo(local, false)
		switch event.Command {
		case window.TextDeleteWordBackward, window.TextDeleteWordForward, window.TextMoveWordLeft,
			window.TextMoveWordRight, window.TextMoveWordLeftAndModifySelection, window.TextMoveWordRightAndModifySelection:
			probe.MoveWord(forward, false)
		default:
			if forward {
				probe.MoveRight(false)
			} else {
				probe.MoveLeft(false)
			}
		}
		if to := w.absolute(probe.ByteOffset(probe.Caret())); to != caret {
			return to
		}
	}
	if forward {
		value, err := r.read(caret, min(r.size, caret+utf8.UTFMax))
		if err == nil && value != "" {
			if strings.HasPrefix(value, "\r\n") {
				return caret + 2
			}
			_, width := utf8.DecodeRuneInString(value)
			return caret + int64(width)
		}
	} else {
		value, err := r.read(max(0, caret-utf8.UTFMax), caret)
		if err == nil && value != "" {
			if strings.HasSuffix(value, "\r\n") {
				return caret - 2
			}
			_, width := utf8.DecodeLastRuneInString(value)
			return caret - int64(width)
		}
	}
	return caret
}

func (t *editorTab) largeEditLineEdge(end bool) int64 {
	edit := t.largeText.editing
	caret := edit.position.caret
	r := edit.revision
	line, err := r.lineAt(caret)
	if err != nil {
		return caret
	}
	if !end || line == r.breaks {
		if end {
			return r.size
		}
		start, err := r.lineStart(line)
		if err == nil {
			return start
		}
		return caret
	}
	next, err := r.lineStart(line + 1)
	if err != nil {
		return caret
	}
	ending, _ := r.read(max(0, next-2), next)
	if ending == "\r\n" {
		return next - 2
	}
	return next - 1
}

func (t *editorTab) moveLargeEditCaret(caret int64, extend bool) {
	position := t.largeText.editing.position
	position.caret = caret
	if !extend {
		position.anchor = caret
	}
	t.moveLargeEdit(position, true)
}

func (t *editorTab) largeEditKey(event gfx.KeyEvent) gfx.EventResult {
	edit, pane := t.largeText.editing, t.source
	if t.largeText.busyEditing() {
		return gfx.EventResult{Handled: true}
	}
	if _, composing := pane.text.Composition(); composing {
		return gfx.EventResult{Handled: true}
	}
	if command, ok := components.CodeEditorCommandForKey(event); ok {
		t.largeEditCodeCommand(command)
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	movement, extend := components.TextareaMovementForKey(event.Key, event.Modifiers)
	offset := edit.position.caret
	w := edit.viewport
	switch movement {
	case components.TextareaMoveTextStart:
		offset = 0
	case components.TextareaMoveTextEnd:
		offset = edit.revision.size
	case components.TextareaMoveLineStart:
		offset = t.largeEditLineEdge(false)
	case components.TextareaMoveLineEnd:
		offset = t.largeEditLineEdge(true)
	case components.TextareaMoveUp, components.TextareaMoveDown, components.TextareaMovePageUp, components.TextareaMovePageDown:
		if offset >= w.start && offset <= w.end {
			offset = w.absolute(pane.view.Code.Move(w.value, w.local(offset), movement))
		}
		forward := movement == components.TextareaMoveDown || movement == components.TextareaMovePageDown
		if !forward && offset <= w.start && w.start > 0 || forward && offset >= w.end && w.end < edit.revision.size ||
			edit.position.caret < w.start || edit.position.caret > w.end {
			line, err := edit.revision.lineAt(edit.position.caret)
			if err == nil {
				step := 1
				if movement == components.TextareaMovePageUp || movement == components.TextareaMovePageDown {
					first, last := pane.view.Code.VisibleLines()
					step = max(1, last-first)
				}
				if !forward {
					step = -step
				}
				offset, err = edit.revision.lineStart(max(0, line+step))
			}
			if err != nil {
				offset = edit.position.caret
			}
		}
	default:
		return gfx.EventResult{}
	}
	t.moveLargeEditCaret(offset, extend)
	return gfx.EventResult{Handled: true, Repaint: true}
}

func (t *editorTab) pointLargeEdit(event gfx.PointerEvent) gfx.EventResult {
	edit, pane := t.largeText.editing, t.source
	if _, composing := pane.text.Composition(); composing || t.largeText.busyEditing() {
		return gfx.EventResult{}
	}
	switch event.Kind {
	case gfx.PointerDown:
		if event.Button != gfx.PointerButtonPrimary {
			return gfx.EventResult{}
		}
	case gfx.PointerMove, gfx.PointerUp:
		if !event.Captured || !edit.selecting {
			return gfx.EventResult{}
		}
	case gfx.PointerExit:
		edit.selecting = false
		return gfx.EventResult{}
	default:
		return gfx.EventResult{}
	}
	offset, ok := pane.view.Code.OffsetAt(gfx.Point{X: event.X, Y: event.Y})
	if !ok {
		return gfx.EventResult{}
	}
	position := edit.position
	position.caret = edit.viewport.absolute(offset)
	if event.Kind == gfx.PointerDown {
		edit.selecting = true
		edit.selectionClicks = 0
		t.largeText.selectedResult = -1
		if event.Modifiers&ui.Shift == 0 {
			position.anchor = position.caret
			if event.ClickCount >= 2 {
				edit.selectionClicks = event.ClickCount
				edit.selectionUnit = t.largeEditPointerUnit(offset, event.ClickCount)
				position = edit.selectionUnit
			}
		}
	} else if edit.selectionClicks >= 2 {
		unit := edit.selectionUnit
		switch {
		case position.caret < unit.anchor:
			position = largeEditPosition{unit.caret, t.largeEditPointerUnit(offset, edit.selectionClicks).anchor}
		case position.caret > unit.caret:
			position = largeEditPosition{unit.anchor, t.largeEditPointerUnit(offset, edit.selectionClicks).caret}
		default:
			position = unit
		}
	}
	if event.Kind == gfx.PointerUp {
		edit.selecting = false
	}
	t.moveLargeEdit(position, false)
	if event.Kind == gfx.PointerMove && (event.Y < edit.bounds.Y || event.Y > edit.bounds.Bottom()) {
		direction := -1
		if event.Y > edit.bounds.Bottom() {
			direction = 1
		}
		t.panLargeEdit(direction * 3)
	}
	return gfx.EventResult{Focus: event.Target, Handled: true, Repaint: true}
}

func (t *editorTab) largeEditPointerUnit(offset, clicks int) largeEditPosition {
	edit := t.largeText.editing
	viewport, revision := edit.viewport, edit.revision
	caret := viewport.absolute(offset)
	if clicks >= 3 {
		line, err := revision.lineAt(caret)
		if err == nil {
			start, startErr := revision.lineStart(line)
			end, endErr := revision.lineStart(line + 1)
			if startErr == nil && endErr == nil {
				return largeEditPosition{start, end}
			}
		}
		return largeEditPosition{caret, caret}
	}
	pane := t.source
	word := text.WordRange(viewport.value, pane.text.RuneIndex(offset))
	start, end := pane.text.ByteOffset(word.Start), pane.text.ByteOffset(word.End)
	if strings.TrimSpace(viewport.value[start:end]) == "" {
		return largeEditPosition{caret, caret}
	}
	return largeEditPosition{viewport.absolute(start), viewport.absolute(end)}
}

func (t *editorTab) scrollLargeEdit(event gfx.ScrollEvent) gfx.EventResult {
	if event.DeltaY == 0 || event.DeltaX != 0 || event.Modifiers != 0 {
		return gfx.EventResult{}
	}
	rows := int(event.StepY(20) / 20)
	if rows == 0 {
		rows = 1
		if event.DeltaY < 0 {
			rows = -1
		}
	}
	changed := t.panLargeEdit(rows)
	return gfx.EventResult{Handled: changed, Repaint: changed}
}

func (t *editorTab) panLargeEdit(rows int) bool {
	edit := t.largeText.editing
	if _, composing := t.source.text.Composition(); composing || t.largeText.busyEditing() {
		return false
	}
	if rows < 0 && edit.viewport.start == 0 || rows > 0 && edit.viewport.end == edit.revision.size {
		return false
	}
	line, err := edit.revision.lineAt(edit.top)
	if err != nil {
		return false
	}
	top, err := edit.revision.lineStart(max(0, line+rows))
	if err == nil && top != edit.top {
		err = t.loadLargeEditWindow(top)
	}
	if err != nil {
		t.largeText.problem = err.Error()
		return false
	}
	return true
}

func (t *editorTab) pointLargeEditScrollbar(event gfx.PointerEvent) gfx.EventResult {
	edit := t.largeText.editing
	if _, composing := t.source.text.Composition(); composing || t.largeText.busyEditing() {
		return gfx.EventResult{}
	}
	if event.Kind == gfx.PointerDown && event.Button != gfx.PointerButtonPrimary ||
		event.Kind == gfx.PointerMove && !event.Captured ||
		event.Kind != gfx.PointerDown && event.Kind != gfx.PointerMove {
		return gfx.EventResult{}
	}
	track, thumb := edit.barBounds, t.largeEditScrollbarThumb()
	if thumb.Height >= track.Height {
		return gfx.EventResult{}
	}
	if event.Kind == gfx.PointerDown {
		edit.barDrag = thumb.Height / 2
		if event.Y >= thumb.Y && event.Y < thumb.Bottom() {
			edit.barDrag = event.Y - thumb.Y
			return gfx.EventResult{Handled: true, Repaint: true}
		}
	}
	first, last := t.source.view.Code.VisibleLines()
	maxLine := max(0, edit.revision.breaks-(last-first))
	percent := (event.Y - track.Y - edit.barDrag) / (track.Height - thumb.Height)
	line := int(math.Round(float64(max(0, min(1, percent))) * float64(maxLine)))
	offset, err := edit.revision.lineStart(line)
	if err == nil && offset != edit.top {
		err = t.loadLargeEditWindow(offset)
	}
	if err != nil {
		t.largeText.problem = err.Error()
	}
	return gfx.EventResult{Handled: true, Repaint: true}
}

func (t *editorTab) largeEditScrollbarThumb() gfx.Rect {
	edit := t.largeText.editing
	track := edit.barBounds
	first, last := t.source.view.Code.VisibleLines()
	height := min(track.Height, max(24, track.Height*float32(last-first+1)/float32(edit.revision.breaks+1)))
	line := edit.viewport.line + first
	percent := min(1, float32(line)/float32(max(1, edit.revision.breaks-(last-first))))
	return gfx.Rect{X: track.X + 2, Y: track.Y + percent*(track.Height-height), Width: track.Width - 4, Height: height}
}

func (m *editorApp) largeEditPane(c ui.C, t *editorTab, blocked bool) {
	state, pane := t.largeText, t.source
	edit := state.editing
	ui.Row(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
		w := edit.viewport
		box := pane.declare(c.Part("editable"), "大文件内容", !blocked && !state.busyEditing() &&
			edit.position.caret >= w.start && edit.position.caret <= w.end).FillHeight().Grow(1).MinWidth(0).Clip()
		if pane.focus && !blocked && !state.busyEditing() {
			box.AutoFocus()
			pane.focus = false
		}
		box.DrawOver(func(d *ui.DrawContext) {
			pane.focused = d.Canvas.Focused() == d.Node.ID
			edit.bounds = d.Rect
			if edit.pendingTop {
				if y, ok := pane.view.Code.YAtOffset(edit.viewport.local(edit.top)); ok {
					first, last := pane.view.Code.VisibleLines()
					caretLine := edit.viewport.line + first
					if edit.position.caret >= w.start && edit.position.caret <= w.end {
						caretLine = w.line + strings.Count(w.value[:w.local(edit.position.caret)], "\n")
					}
					topLine := w.line + strings.Count(w.value[:w.local(edit.top)], "\n")
					if edit.top != edit.position.caret || caretLine <= topLine+last-first {
						pane.view.Code.SetScrollY(y)
					}
					edit.pendingTop = false
					d.Canvas.RequestFrame()
				}
			}
			if offset, ok := pane.view.Code.OffsetAtY(pane.view.Code.ScrollY()); ok && !edit.pendingTop {
				edit.top = edit.viewport.absolute(offset)
			}
		})
		ui.Control(c.Part("document-scroll"), "文档滚动条", &edit.bar).Width(12).Shrink(0).FillHeight().Draw(func(d *ui.DrawContext) {
			edit.barBounds = d.Rect
			thumb := t.largeEditScrollbarThumb()
			if thumb.Height < d.Rect.Height {
				d.Canvas.Rounded(thumb, 4, m.theme.Muted)
			}
		})
		if !blocked && !state.busyEditing() {
			m.editorContextMenu(c.Part("large-edit-menu"), box, t, pane)
		}
	})
}

func (m *editorApp) largeEditToolbar(c ui.C, t *editorTab, disabled bool) {
	ui.Row(c).FillWidth().Height(40).PadX(12).Gap(5).Items(ui.Center).Children(func() {
		ui.Text(c, "完整编辑").Bold().FontSize(13)
		if ui.Button(c, "转到行").Ghost().FontSize(12).Height(28).Disabled(disabled).Clicked() {
			c.After(m.openGotoLine)
		}
		ui.Slot(c).Grow(1)
		if ui.Button(c, "查找").Icon(searchIcon).Ghost().FontSize(12).Height(28).Disabled(disabled).Clicked() {
			c.After(t.openSearch)
		}
		if ui.Button(c, "替换").Ghost().FontSize(12).Height(28).Disabled(disabled).Clicked() {
			c.After(t.openReplace)
		}
	})
}

func (m *editorApp) largeEditStatusBar(c ui.C, t *editorTab, blocked bool) {
	edit := t.largeText.editing
	w := edit.viewport
	line := w.line + strings.Count(w.value[:w.local(edit.position.caret)], "\n")
	var err error
	if edit.position.caret < w.start || edit.position.caret > w.end {
		line, err = edit.revision.lineAt(edit.position.caret)
	}
	status := m.status
	if t.modified() {
		status = "未保存"
	}
	ui.Row(c).FillWidth().Height(28).PadX(14).Gap(18).Items(ui.Center).Background(m.theme.Card).Children(func() {
		ui.Text(c, status).Muted().FontSize(12).Grow(1).MinWidth(0).Ellipsis()
		if err == nil {
			ui.Text(c, fmt.Sprintf("行 %d · 共 %d 行", line+1, edit.revision.breaks+1)).Muted().FontSize(12)
		}
		ui.Text(c, t.doc.eol).Muted().FontSize(12)
		m.encodingPicker(c, t, blocked)
	})
}
