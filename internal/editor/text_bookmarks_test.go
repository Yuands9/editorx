package editor

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func TestTextBookmarksTrackEditsAndRestore(t *testing.T) {
	m := newEditorApp()
	store, err := openSessionStore(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	store.attach(m)
	tab := m.openLoadedFile(openedEditorFile{name: "notes.txt", doc: document{saved: "one\ntwo\nthree\n"}})
	s := openTestSession(t, m)
	if err := s.Driver().Key(gfx.KeyF2, gfx.ModifierControl); err != nil {
		t.Fatal(err)
	}
	tab.source.text.MoveTo(8, false)
	if err := s.Driver().Key(gfx.KeyF2, gfx.ModifierControl); err != nil {
		t.Fatal(err)
	}
	tab.source.text.ReplaceRange(text.Range{}, "inserted\n")
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if len(tab.bookmarks.lines) != 2 || tab.bookmarks.lines[0] != 1 || tab.bookmarks.lines[1] != 3 {
		t.Fatalf("bookmarks did not follow inserted lines: %v", tab.bookmarks.lines)
	}
	tab.source.text.MoveTo(0, false)
	if err := s.Driver().Key(gfx.KeyF2, 0); err != nil {
		t.Fatal(err)
	}
	if got := tab.source.text.String()[tab.source.text.ByteOffset(tab.source.text.Caret()):]; !strings.HasPrefix(got, "one\n") {
		t.Fatal("F2 did not go to first bookmark")
	}
	if err := store.save(); err != nil {
		t.Fatal(err)
	}
	restored, err := openSessionStore(store.directory)
	if err != nil {
		t.Fatal(err)
	}
	if lines := restored.windows[0].active.bookmarks.lines; len(lines) != 2 || lines[0] != 1 || lines[1] != 3 {
		t.Fatalf("session bookmarks: %v", lines)
	}
}
