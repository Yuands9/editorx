package editor

import (
	"slices"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/text"
)

type textBookmarks struct {
	lines    []int
	editor   *text.Editor
	revision uint64
}

func (tab *editorTab) syncBookmarks() {
	bookmarks := &tab.bookmarks
	editor := tab.source.text
	if tab.largeText != nil || len(bookmarks.lines) == 0 {
		bookmarks.editor = nil
		return
	}
	if bookmarks.editor != editor {
		bookmarks.editor, bookmarks.revision = editor, editor.TrackLineEdits()
		return
	}
	changes, revision, ok := editor.LineEditsSince(bookmarks.revision, nil)
	bookmarks.revision = revision
	if !ok {
		return
	}
	for _, change := range changes {
		delta := change.InsertedLines - change.RemovedLines
		if delta == 0 {
			continue
		}
		for i, line := range bookmarks.lines {
			switch {
			case line > change.Line+change.RemovedLines || change.AtLineStart && line >= change.Line+change.RemovedLines:
				bookmarks.lines[i] = line + delta
			case line > change.Line:
				bookmarks.lines[i] = change.Line
			}
		}
	}
	bookmarks.lines = slices.Compact(bookmarks.lines)
}

func (tab *editorTab) bookmarkLine() (int, bool) {
	pane := tab.source
	line := text.PositionAt(pane.text.String(), pane.text.ByteOffset(pane.text.Caret())).Line
	if large := tab.largeText; large != nil {
		if edit := large.editing; edit != nil {
			line, err := edit.revision.lineAt(edit.position.caret)
			return line, err == nil
		}
		if large.index >= int64(len(large.lines.breaks)-1) {
			return 0, false
		}
		line += large.lines.breaks[large.index]
	}
	return line, true
}

func (m *editorApp) toggleTextBookmark(tab *editorTab) {
	if !tab.canFind() {
		return
	}
	tab.syncBookmarks()
	line, ok := tab.bookmarkLine()
	if !ok {
		return
	}
	i, existing := slices.BinarySearch(tab.bookmarks.lines, line)
	if existing {
		tab.bookmarks.lines = slices.Delete(tab.bookmarks.lines, i, i+1)
	} else {
		tab.bookmarks.lines = slices.Insert(tab.bookmarks.lines, i, line)
		tab.syncBookmarks()
	}
	tab.source.focus = true
}

func (m *editorApp) stepTextBookmark(tab *editorTab, forward bool) {
	if !tab.canFind() {
		return
	}
	tab.syncBookmarks()
	lines := tab.bookmarks.lines
	line, ok := tab.bookmarkLine()
	if !ok || len(lines) == 0 {
		return
	}
	next, existing := slices.BinarySearch(lines, line)
	if forward {
		if existing {
			next++
		}
		next %= len(lines)
	} else {
		next = (next + len(lines) - 1) % len(lines)
	}
	if err := m.revealTextLine(tab, lines[next]+1); err != nil {
		m.status = err.Error()
	}
}

func (m *editorApp) updateBookmarkMarkers(tab *editorTab) {
	tab.syncBookmarks()
	snapshot := &tab.source.view.Snapshot
	snapshot.GutterMarkers = snapshot.GutterMarkers[:0]
	if len(tab.bookmarks.lines) == 0 {
		return
	}
	offset := 0
	if large := tab.largeText; large != nil {
		switch {
		case large.editing != nil:
			offset = large.editing.viewport.line
		case large.index < int64(len(large.lines.breaks)-1):
			offset = large.lines.breaks[large.index]
		default:
			return
		}
	}
	for _, line := range tab.bookmarks.lines {
		if line >= offset {
			snapshot.GutterMarkers = append(snapshot.GutterMarkers, components.CodeEditorGutterMarker{Line: line - offset, Color: m.theme.Ring})
		}
	}
}

func (tab *editorTab) updateLargeEditBookmarks(previous, next *largeEditRevision) {
	bookmarks := tab.bookmarks.lines
	if len(bookmarks) == 0 || previous.breaks == next.breaks {
		return
	}
	var delta int
	for _, span := range largeEditSpans(previous, next) {
		from, err := previous.lineAt(span.start)
		if err != nil {
			return
		}
		through, err := previous.lineAt(span.end)
		if err != nil {
			return
		}
		to, err := next.lineAt(span.insertStart)
		if err != nil {
			return
		}
		end, err := next.lineAt(span.insertEnd)
		if err != nil {
			return
		}
		shift := (end - to) - (through - from)
		lineStart, err := previous.lineStart(from)
		if err != nil {
			return
		}
		for i, line := range bookmarks {
			switch {
			case line > through+delta || span.start == lineStart && line >= through+delta:
				bookmarks[i] = line + shift
			case line > from+delta:
				bookmarks[i] = from + delta
			}
		}
		delta += shift
	}
	tab.bookmarks.lines = slices.Compact(bookmarks)
}
