package editor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func TestLargeFindAllNavigatesAndHighlights(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.out")
	query := "ID-跨页"
	prefix := "start\n" + strings.Repeat("x", largeTextPageBytes-len("start\n")-3)
	source := prefix + query + "\n" + strings.Repeat("line\n", 53000) + "中文 " + query + " and " + query + "\n"
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	page, err := readLargeTextPage(path, 0, -1, "", "")
	if err != nil {
		t.Fatal(err)
	}
	m := newEditorApp()
	tab := m.openLoadedFile(openedEditorFile{doc: document{path: path}, largeText: &page})
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(s.ClickLabel("button:查找"))
	check(s.Type(query))
	check(s.ClickLabel("button:查找全部"))
	check(s.WaitFor(func(app.Snapshot) bool { return m.results.total == 3 && len(tab.largeText.lines.breaks) > 0 }, 3*time.Second))
	group := &m.results.groups[0]
	if len(m.results.rows) != 4 || len(group.lines) != 2 || len(group.lines[1].marks) != 2 {
		t.Fatalf("one source line per result, preserving every hit: %+v", group.lines)
	}
	first, last := group.stream.hits[0], group.stream.hits[1]
	if first.line != 2 || last.line != 53003 || last.column != 4 || group.stream.hits[2].column != 14 {
		t.Fatalf("unexpected source positions: first=%d:%d last=%d:%d", first.line, first.column, last.line, last.column)
	}
	check(s.Frame())
	check(s.ClickLabel(fmt.Sprintf("结果 sample.out 行 %d 列 %d", last.line, last.column)))
	if len(tab.source.view.Snapshot.Marks) != 2 || tab.source.view.Snapshot.LineNumberOffset == 0 {
		t.Fatalf("missing hit highlights or document line offset: %+v, %d, page=%d status=%s selected=%d", tab.source.view.Snapshot.Marks, tab.source.view.Snapshot.LineNumberOffset, tab.largeText.index, m.status, m.results.list.Selected())
	}
	check(s.Driver().Key(gfx.KeyF3, 0))
	if tab.largeText.selectedResult != 2 || m.results.list.Selected() != 3 {
		t.Fatal("F3 did not navigate to the second hit on the selected line")
	}
	check(s.Driver().Key(gfx.KeyF3, gfx.ModifierShift))
	if tab.largeText.selectedResult != 1 {
		t.Fatal("Shift+F3 did not go to the previous matching result")
	}
	check(s.ClickLabel(fmt.Sprintf("结果 sample.out 行 %d 列 %d", first.line, first.column)))
	pane := tab.source
	sel := pane.text.Selection()
	if got := pane.text.String()[pane.text.ByteOffset(sel.Start):pane.text.ByteOffset(sel.End)]; got != query {
		t.Fatalf("cross-page result selected %q", got)
	}
	handled, err := s.Driver().ShortcutChord(gfx.ModifierControl, 'g')
	check(err)
	if !handled {
		t.Fatal("Ctrl+G was not handled")
	}
	check(s.Type("53003"))
	check(s.Keys("enter"))
	if line := pane.view.Snapshot.LineNumberOffset + text.PositionAt(pane.text.String(), pane.text.ByteOffset(pane.text.Caret())).Line + 1; line != 53003 {
		t.Fatalf("Ctrl+G landed on line %d", line)
	}
	check(os.WriteFile(path, []byte("replacement"), 0600))
	m.chooseSearchResult(3)
	if m.status != "文件已变化，请重新查找" {
		t.Fatal("a stale result was not rejected")
	}
}

func TestTextActionsKeepSelectionAndUndo(t *testing.T) {
	m := newEditorApp()
	tab := m.openLoadedFile(openedEditorFile{doc: document{path: "notes.txt", saved: "keep\nz\n   \na\nlast\n"}})
	editor := tab.source.text
	editor.SetSelection(text.Range{Start: 5, End: 13})
	if !rewriteTextSelection(tab, textSortLines) || editor.String() != "keep\n   \na\nz\nlast\n" {
		t.Fatalf("selected lines were not sorted in place: %q", editor.String())
	}
	if !rewriteTextSelection(tab, textRemoveBlankLines) || editor.String() != "keep\na\nz\nlast\n" {
		t.Fatalf("blank lines were not removed: %q", editor.String())
	}
	editor.Undo()
	if editor.String() != "keep\n   \na\nz\nlast\n" {
		t.Fatalf("line action was not undoable: %q", editor.String())
	}
}

func TestEncodedLargeFindAllAndLineIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.log")
	const lines = 24000
	content := strings.Repeat("中文\r\n", lines) + "终点"
	doc := document{encoding: utf16BEEncoding, bom: true}
	encoded, err := doc.fileBytes(content)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	idx, err := indexLargeText(ctx, path, "")
	if err != nil {
		t.Fatal(err)
	}
	page, offset, err := idx.goToLine(path, utf16BEEncoding, lines+1)
	if err != nil || page.value[offset:] != "终点" {
		t.Fatalf("UTF-16 line lookup failed: %v", err)
	}
	results, err := collectLargeSearchResults(ctx, path, "终点", "")
	if err != nil || len(results.hits) != 1 || results.hits[0].line != lines+1 || results.hits[0].column != 1 {
		t.Fatalf("UTF-16 result line is incorrect: %+v, %v", results.hits, err)
	}
}
