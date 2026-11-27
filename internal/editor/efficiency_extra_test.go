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

func TestIdeaShortcutPreset(t *testing.T) {
	store, err := openSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	prefs, err := readApplicationPreferences(store.directory)
	if err != nil {
		t.Fatal(err)
	}
	desktop := &editorDesktop{store: store, preferences: prefs}
	m := newEditorApp()
	m.desktop = desktop
	store.attach(m)
	left := m.addTab(document{saved: "longerName\nlonger"}, "left.txt")
	right := m.addTab(document{saved: "other file"}, "right.txt")
	m.activate(left)
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	chord := func(mods gfx.Modifiers, key rune) {
		t.Helper()
		_, err := s.Driver().ShortcutChord(mods, key)
		check(err)
	}
	check(s.ClickLabel("button:设置"))
	check(s.ClickLabel("button:快捷键"))
	check(s.ClickLabel("快捷键预设"))
	check(s.ClickLabel("option:" + ideaShortcutPreset))
	saved, err := readApplicationPreferences(store.directory)
	check(err)
	if shortcutPresetName(saved.Shortcuts) != ideaShortcutPreset {
		t.Fatal("IDEA preset did not persist")
	}
	if preview := os.Getenv("VIVA_PREVIEW_DIR"); preview != "" {
		check(s.Screenshot(filepath.Join(preview, "idea-shortcuts.png")))
	}
	check(s.ClickLabel("button:关闭设置"))
	check(s.ClickLabel("代码编辑区"))

	chord(gfx.ModifierControl|gfx.ModifierShift, 'n')
	if m.palette == nil || m.palette.commands {
		t.Fatal("Ctrl+Shift+N did not open quick file search")
	}
	check(s.Driver().Key(gfx.KeyEscape, 0))
	chord(gfx.ModifierControl|gfx.ModifierShift, 'a')
	if m.palette == nil || !m.palette.commands {
		t.Fatal("Ctrl+Shift+A did not open the command palette")
	}
	check(s.Driver().Key(gfx.KeyEscape, 0))
	chord(gfx.ModifierControl|gfx.ModifierShift, 'r')
	if m.directorySearch == nil || !m.directorySearch.open || !m.directorySearch.replaceMode {
		t.Fatal("Ctrl+Shift+R did not open directory replacement")
	}
	check(s.Driver().Key(gfx.KeyEscape, 0))

	check(s.Driver().Key(gfx.KeyRight, gfx.ModifierAlt))
	if m.active != right {
		t.Fatal("Alt+Right did not switch tabs")
	}
	check(s.Driver().Key(gfx.KeyLeft, gfx.ModifierControl|gfx.ModifierAlt))
	if m.active != left {
		t.Fatal("Ctrl+Alt+Left did not return to the previous location")
	}
	left.source.text.MoveTo(left.source.text.RuneCount(), false)
	check(s.Frame())
	check(s.Driver().Key(gfx.KeySpace, gfx.ModifierControl|gfx.ModifierAlt))
	if left.source.completion == nil || len(left.source.completion.items) == 0 {
		t.Fatal("Ctrl+Alt+Space did not open completion")
	}
	check(s.Driver().Key(gfx.KeyEnter, 0))
	if left.source.text.String() != "longerName\nlongerName" {
		t.Fatal("IDEA completion did not change the editor")
	}
	chord(gfx.ModifierControl, 'y')
	if left.source.text.String() != "longerName" {
		t.Fatalf("Ctrl+Y did not delete exactly one line: %q", left.source.text.String())
	}
	chord(gfx.ModifierControl, 'z')
	chord(gfx.ModifierControl|gfx.ModifierShift, 'z')
	if left.source.text.String() != "longerName" {
		t.Fatal("Ctrl+Shift+Z did not redo the line deletion")
	}
	chord(gfx.ModifierControl, 'f')
	check(s.Type("query"))
	chord(gfx.ModifierControl, 'z')
	chord(gfx.ModifierControl, 'y')
	if left.search.query.String() != "query" || left.source.text.String() != "longerName" {
		t.Fatal("Ctrl+Y did not remain redo in the search field")
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
