package editor

import (
	"io"
	"strings"

	"github.com/vivaui/viva/components"
)

// Line commands reuse the code editor for bounded indentation. Copying and
// deleting lines can operate directly on any range, including the whole file.
func (t *editorTab) largeEditCodeCommand(command components.CodeEditorCommand) {
	state, pane := t.largeText, t.source
	if _, composing := pane.text.Composition(); composing || state.busyEditing() {
		return
	}
	edit := state.editing
	switch command {
	case components.CodeEditorDuplicateLinesUp, components.CodeEditorDuplicateLinesDown, components.CodeEditorDeleteLines,
		components.CodeEditorMoveLinesUp, components.CodeEditorMoveLinesDown:
		t.changeLargeEditLines(command)
	case components.CodeEditorInsertTab, components.CodeEditorIndent, components.CodeEditorOutdent, components.CodeEditorToggleComment:
		start, end := edit.position.rangeOffsets()
		w := edit.viewport
		// A clipped first/last line has only part of its indentation and
		// neighbours. Never run a line command on an incomplete range.
		left, right := w.local(start), w.local(end)
		if start < w.start || end > w.end || w.start > 0 && !strings.ContainsRune(w.value[:left], '\n') ||
			w.end < edit.revision.size && !strings.ContainsRune(w.value[right:], '\n') {
			state.problem = uiText("请缩小选区后再执行此行操作")
			return
		}
		command.Apply(pane.text)
		t.commitLargeEditLocal(w.value, left, right, false)
	}
	pane.focus = true
}

func (t *editorTab) changeLargeEditLines(command components.CodeEditorCommand) {
	state, edit := t.largeText, t.largeText.editing
	r := edit.revision
	position := edit.position
	start, end := position.rangeOffsets()
	first, err := r.lineAt(start)
	if err != nil {
		state.problem = err.Error()
		return
	}
	// The end of a nonempty selection is exclusive, including when it
	// happens to be at the start of the next line.
	last, err := r.lineAt(max(start, end-1))
	if err != nil {
		state.problem = err.Error()
		return
	}
	left, err := r.lineStart(first)
	if err != nil {
		state.problem = err.Error()
		return
	}
	right, err := r.lineStart(last + 1)
	if err != nil {
		state.problem = err.Error()
		return
	}
	if command == components.CodeEditorDeleteLines {
		if last == r.breaks && left > 0 {
			ending, _ := r.read(max(0, left-2), left)
			left--
			if ending == "\r\n" {
				left--
			}
		}
		t.replaceLargeEdit(left, right, "", false)
		return
	}

	if command == components.CodeEditorMoveLinesUp || command == components.CodeEditorMoveLinesDown {
		t.moveLargeEditLines(command == components.CodeEditorMoveLinesUp, first, last, left, right)
		return
	}

	insertion := right
	up := command == components.CodeEditorDuplicateLinesUp
	if up {
		insertion = left
	}
	newline := t.largeEditInputValue("\n")
	next := &largeEditRevision{store: r.store}
	if err = r.copyPieces(next, 0, insertion); err != nil {
		state.problem = err.Error()
		return
	}
	writer := largeEditAppendWriter{next}
	finalLine := last == r.breaks
	if finalLine && !up {
		_, err = io.WriteString(writer, newline)
	}
	if err == nil {
		_, err = io.Copy(writer, io.NewSectionReader(r, left, right-left))
	}
	if err == nil && finalLine && up {
		_, err = io.WriteString(writer, newline)
	}
	if err == nil {
		err = r.copyPieces(next, insertion, r.size)
	}
	if err != nil {
		state.problem = err.Error()
		return
	}
	if !up {
		delta := next.size - r.size
		position.anchor += delta
		position.caret += delta
	}
	t.noteLargeEdit(next, position, false)
}

func (t *editorTab) moveLargeEditLines(up bool, first, last int, left, right int64) {
	state, edit := t.largeText, t.largeText.editing
	r := edit.revision
	if up && first == 0 || !up && last == r.breaks {
		return
	}
	begin, middle, end := left, right, right
	secondEndsDocument := last == r.breaks
	var err error
	if up {
		begin, err = r.lineStart(first - 1)
		middle = left
	} else {
		end, err = r.lineStart(last + 2)
		secondEndsDocument = last+1 == r.breaks
	}
	if err != nil {
		state.problem = err.Error()
		return
	}
	// For the final unterminated line, keep the existing line break between
	// the swapped blocks and leave the new final line unterminated.
	firstEnd := middle
	newline := ""
	if secondEndsDocument {
		newline, err = r.read(max(begin, middle-2), middle)
		if err != nil {
			state.problem = err.Error()
			return
		}
		if newline != "\r\n" {
			newline = "\n"
		}
		firstEnd -= int64(len(newline))
	}
	next := &largeEditRevision{store: r.store}
	err = r.copyPieces(next, 0, begin)
	writer := largeEditAppendWriter{next}
	if err == nil {
		_, err = io.Copy(writer, io.NewSectionReader(r, middle, end-middle))
	}
	if err == nil && newline != "" {
		_, err = io.WriteString(writer, newline)
	}
	if err == nil {
		_, err = io.Copy(writer, io.NewSectionReader(r, begin, firstEnd-begin))
	}
	if err == nil {
		err = r.copyPieces(next, end, r.size)
	}
	if err != nil {
		state.problem = err.Error()
		return
	}
	delta := end - middle + int64(len(newline))
	if up {
		delta = begin - middle
	}
	position := edit.position
	position.anchor = min(r.size, position.anchor+delta)
	position.caret = min(r.size, position.caret+delta)
	t.noteLargeEdit(next, position, false)
}
