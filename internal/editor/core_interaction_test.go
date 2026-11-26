package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func TestCoreColumnPointerAndClipboard(t *testing.T) {
	m := newEditorApp()
	tab := m.addTab(document{saved: "abcde\n12345\nABCDE", eol: "LF"}, "columns.txt")
	s := openTestSession(t, m)
	p := tab.source
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	point := func(caret int) gfx.Point {
		p.text.MoveTo(caret, false)
		check(s.Frame())
		rect, ok := p.view.Code.CaretBounds()
		if !ok {
			t.Fatal("caret geometry unavailable")
		}
		return gfx.Point{X: rect.X, Y: rect.Y + rect.Height/2}
	}
	from, to := point(1), point(16)
	p.text.MoveTo(0, false)
	check(s.Frame())
	check(s.KeyDown("alt"))
	check(s.Driver().Press(from.X, from.Y, gfx.PointerButtonPrimary))
	check(s.Driver().Move(to.X, to.Y))
	check(s.Driver().Release(to.X, to.Y, gfx.PointerButtonPrimary))
	check(s.KeyUp("alt"))
	if p.text.SelectionCount() != 3 {
		t.Fatal("Alt drag failed", p.text.Selections())
	}
	_, err := s.Driver().ShortcutChord(gfx.ModifierControl, 'c')
	check(err)
	if got := s.Driver().Clipboard(); got != "bcd\n234\nBCD" {
		t.Fatalf("clipboard=%q", got)
	}
	if dir := os.Getenv("VIVA_PREVIEW_DIR"); dir != "" {
		check(s.Screenshot(filepath.Join(dir, "column-selection.png")))
	}
	check(s.Type("X"))
	if p.text.String() != "aXe\n1X5\nAXE" {
		t.Fatal(p.text.String())
	}
	_, err = s.Driver().ShortcutChord(gfx.ModifierControl, 'z')
	check(err)
	if p.text.String() != "abcde\n12345\nABCDE" {
		t.Fatal("column undo failed")
	}
	p.text.MoveTo(1, false)
	check(s.Frame())
	check(s.Driver().Key(gfx.KeyDown, gfx.ModifierControl|gfx.ModifierAlt))
	check(s.Driver().Key(gfx.KeyDown, gfx.ModifierControl|gfx.ModifierAlt))
	if p.text.SelectionCount() != 3 {
		t.Fatal("keyboard carets failed", p.text.Selections())
	}
	check(s.Driver().Key(gfx.KeyEscape, 0))
	if p.text.SelectionCount() != 1 {
		t.Fatal("Escape did not clear secondary carets")
	}
}

func TestCoreFormatDraftSurvivesRestoreAndClose(t *testing.T) {
	store, err := openSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "sample.txt")
	if err := os.WriteFile(path, []byte("sample\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	m := newEditorApp()
	store.attach(m)
	m.load(path)
	tab := m.active
	m.convertTextEOL(tab, "LF")
	m.convertTextEncoding(tab, utf8Encoding, true)
	if err := store.save(); err != nil {
		t.Fatal(err)
	}
	restored, err := openSessionStore(store.directory)
	if err != nil {
		t.Fatal(err)
	}
	tab = restored.windows[0].active
	if !tab.modified() || tab.doc.eol != "LF" || !tab.doc.bom {
		t.Fatal("format draft lost")
	}
	m.removeTab(m.active)
	m.reopenClosedTab()
	if !m.active.modified() || m.active.doc.eol != "LF" || !m.active.doc.bom {
		t.Fatal("closed format draft lost")
	}
}

func TestCoreSaveAllWaitsForBackgroundWrites(t *testing.T) {
	m := newEditorApp()
	dir := t.TempDir()
	for _, name := range []string{"one.txt", "two.txt"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("initial"), 0600); err != nil {
			t.Fatal(err)
		}
		m.load(path)
		m.active.source.replace(strings.Repeat(name, backgroundTextBytes/4))
	}
	s := openTestSession(t, m)
	m.saveAllTabs()
	if !m.savingAll {
		t.Fatal("save-all finished before background write")
	}
	if err := s.WaitFor(func(app.Snapshot) bool { return !m.savingAll }, 8*time.Second); err != nil {
		t.Fatal(err)
	}
	for _, tab := range m.tabs {
		data, err := os.ReadFile(tab.doc.path)
		if err != nil || string(data) != tab.source.text.String() || tab.modified() {
			t.Fatal("save-all did not persist", err)
		}
	}
}

func TestCoreSelectionAll(t *testing.T) {
	p := newEditorPane("foo foo foo", false)
	p.selectNextOccurrence(true)
	if p.text.SelectionCount() != 3 {
		t.Fatal(p.text.Selections())
	}
	p.text.Insert("词")
	if p.text.String() != "词 词 词" {
		t.Fatal(p.text.String())
	}
	p.text.Undo()
	p.text.SetSelection(text.Range{Start: 0, End: 11})
	p.splitSelectionLines()
	if p.text.SelectionCount() != 1 {
		t.Fatal("single line unexpectedly split")
	}
}
