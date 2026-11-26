package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

func TestEfficiencyPresetUIAndPersistence(t *testing.T) {
	store, err := openSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	prefs, err := readApplicationPreferences(store.directory)
	if err != nil {
		t.Fatal(err)
	}
	desktop := &editorDesktop{store: store, preferences: prefs}
	m, other := newEditorApp(), newEditorApp()
	for _, window := range []*editorApp{m, other} {
		window.desktop = desktop
		store.attach(window)
	}
	tab := m.addTab(document{saved: "one\ntwo\nthree"}, "plain.txt")
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(s.ClickLabel("button:设置"))
	check(s.ClickLabel("button:快捷键"))
	check(s.ClickLabel("快捷键预设"))
	check(s.ClickLabel("Sublime Text"))
	saved, err := readApplicationPreferences(store.directory)
	check(err)
	if shortcutPresetName(saved.Shortcuts) != "Sublime Text" || shortcutPresetName(other.keyBindings) != "Sublime Text" {
		t.Fatal("preset was not saved and broadcast")
	}
	check(s.ClickLabel("button:关闭设置"))
	_, err = s.Driver().ShortcutChord(gfx.ModifierControl, 'd')
	check(err)
	if tab.source.text.String() != "one\ntwo\nthree" {
		t.Fatal("Sublime Ctrl+D duplicated text")
	}
	_, err = s.Driver().ShortcutChord(gfx.ModifierControl|gfx.ModifierShift, 'd')
	check(err)
	if tab.source.text.String() != "one\none\ntwo\nthree" {
		t.Fatal("Sublime duplicate did not execute exactly once")
	}
	_, err = s.Driver().ShortcutChord(gfx.ModifierControl|gfx.ModifierShift, 'k')
	check(err)
	if tab.source.text.String() != "one\ntwo\nthree" {
		t.Fatalf("delete key applied twice: %q", tab.source.text.String())
	}
}

func TestEfficiencySaveAllRestoredLargeDraft(t *testing.T) {
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	path := filepath.Join(m.dataDirectory, "sample.out")
	if err := os.WriteFile(path, []byte("before\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	doc, err := readDocument(path)
	if err != nil {
		t.Fatal(err)
	}
	page, err := readLargeTextPage(path, 0, -1, "", "")
	if err != nil {
		t.Fatal(err)
	}
	tab := m.openLoadedFile(openedEditorFile{doc: document{path: path}, largeText: &page})
	tab.largeText.editSession = &largeEditSession{BaseHash: doc.diskHash, Changes: []largeEditChange{{Start: 0, End: 6, Text: "after"}}}
	tab.largeText.resumeEdit = true
	t.Cleanup(tab.largeText.close)
	m.newDocument()
	s := openTestSession(t, m)
	m.saveAllTabs()
	if err := s.WaitFor(func(app.Snapshot) bool { return !m.savingAll }, 3*time.Second); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "after\r\n" || tab.modified() || m.showError {
		t.Fatalf("save large draft: %q %v %s", data, err, m.errorText)
	}
}

func TestEfficiencyQuickOpenProjectAndNavigation(t *testing.T) {
	m := newEditorApp()
	dir := t.TempDir()
	for _, name := range []string{"alpha.txt", "beta.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	m.addProjects(dir)
	s := openTestSession(t, m)
	_, err := s.Driver().ShortcutChord(gfx.ModifierControl, 'p')
	if err != nil {
		t.Fatal(err)
	}
	if err = s.WaitFor(func(app.Snapshot) bool { return m.palette != nil && m.palette.scan == nil }, 3*time.Second); err != nil {
		t.Fatal(err)
	}
	if err = s.Driver().Key(gfx.KeyUp, 0); err != nil {
		t.Fatal(err)
	}
	if m.palette.list.Selected() != 1 {
		t.Fatal("up did not wrap to last file")
	}
	if dir := os.Getenv("VIVA_PREVIEW_DIR"); dir != "" {
		if err = s.Screenshot(filepath.Join(dir, "quick-open.png")); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.Driver().Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if m.active == nil || m.active.doc.path != filepath.Join(dir, "beta.txt") {
		t.Fatal("project file did not open")
	}
}

func BenchmarkEfficiencyPalette20K(b *testing.B) {
	state := &navigationPalette{}
	for i := 0; i < paletteFileLimit; i++ {
		name := fmt.Sprintf("file-%05d.go", i)
		state.items = append(state.items, navigationItem{label: name, search: "d:/project/src/" + name})
	}
	state.query.SetText("f19999")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		state.dirty = true
		state.filter()
	}
}
