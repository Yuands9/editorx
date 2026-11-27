package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
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
	if shortcutPresetName(saved.Shortcuts, saved.ShortcutPreset) != "Sublime Text" || shortcutPresetName(other.keyBindings, other.keyPreset) != "Sublime Text" {
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
	if shortcutPresetName(saved.Shortcuts, saved.ShortcutPreset) != ideaShortcutPreset {
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

func TestShortcutSearchAndPyCharmPreset(t *testing.T) {
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(s.ClickLabel("button:设置"))
	check(s.ClickLabel("textfield:搜索设置"))
	check(s.Type("gitCommit"))
	check(s.ClickLabel("button:打开 快捷键"))
	if !s.HasText("打开 Git 提交面板") || s.HasText("新建文件") {
		t.Fatal("global settings search did not open the matching shortcut", s.Texts())
	}
	check(s.ClickLabel("textfield:搜索快捷键"))
	check(s.Keys("cmd+a"))
	check(s.Type("Ctrl + K"))
	if !s.HasText("打开 Git 提交面板") || s.HasText("新建文件") {
		t.Fatal("search by chord did not filter the shortcut list", s.Texts())
	}
	check(s.ClickLabel("combobox:快捷键预设"))
	check(s.ClickLabel("option:PyCharm"))
	saved, err := readApplicationPreferences(m.dataDirectory)
	check(err)
	if saved.ShortcutPreset != pycharmShortcutPreset || m.keyBindings.chord(shortcutComplete).Label() != "Ctrl+Alt+Space" ||
		m.keyBindings.chord(shortcutCommandPalette).Label() != "Ctrl+Shift+A" {
		t.Fatal("PyCharm keymap could not be selected and saved")
	}
}

func TestVSCodeShortcutPreset(t *testing.T) {
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	tab := m.addTab(document{saved: "word one\nword two\nword three"}, "vscode.txt")
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
	check(s.ClickLabel("combobox:快捷键预设"))
	check(s.ClickLabel("option:VS Code"))
	saved, err := readApplicationPreferences(m.dataDirectory)
	check(err)
	if shortcutPresetName(saved.Shortcuts, saved.ShortcutPreset) != vsCodeShortcutPreset || m.keyBindings.chord(shortcutComplete).Label() != "Alt+/" {
		t.Fatal("VS Code preset or input-method-friendly completion key was not applied")
	}
	check(s.ClickLabel("button:关闭设置页"))
	check(s.ClickLabel("textfield:代码编辑区"))
	chord(gfx.ModifierControl, 'h')
	if !tab.search.open || !tab.search.replacing {
		t.Fatal("Ctrl+H did not open Replace")
	}
	check(s.Driver().Key(gfx.KeyEscape, 0))
	chord(gfx.ModifierControl|gfx.ModifierShift, 'h')
	if m.directorySearch == nil || !m.directorySearch.open || !m.directorySearch.replaceMode {
		t.Fatal("Ctrl+Shift+H did not open Replace in Files")
	}
	check(s.Driver().Key(gfx.KeyEscape, 0))
	const original = "word one\nword two\nword three"
	tab.source.text.MoveTo(1, false)
	check(s.Frame())
	chord(gfx.ModifierControl, 'd')
	if tab.source.text.Selection() != (text.Range{Start: 0, End: 4}) || tab.source.text.String() != original {
		t.Fatal("Ctrl+D did not select the word without duplicating the line")
	}
	chord(gfx.ModifierControl|gfx.ModifierShift, 'l')
	if tab.source.text.SelectionCount() != 3 {
		t.Fatal("Ctrl+Shift+L did not select all occurrences")
	}
	check(s.Driver().Key(gfx.KeyEscape, 0))
	for _, key := range []gfx.Key{gfx.KeyDown, gfx.KeyUp} {
		tab.source.text.MoveTo(1, false)
		check(s.Frame())
		check(s.Driver().Key(key, gfx.ModifierAlt|gfx.ModifierShift))
		if tab.source.text.String() != "word one\nword one\nword two\nword three" {
			t.Fatalf("Alt+Shift+%v did not duplicate exactly one line: %q", key, tab.source.text.String())
		}
		chord(gfx.ModifierControl, 'z')
	}
	tab.source.text.MoveTo(1, false)
	check(s.Frame())
	check(s.Driver().Key(gfx.KeyDown, gfx.ModifierControl|gfx.ModifierAlt|gfx.ModifierShift))
	if tab.source.text.SelectionCount() != 2 {
		t.Fatal("VS Code column selection did not add the second line")
	}
	check(s.Driver().Key(gfx.KeyEscape, 0))
	tab.source.text.MoveTo(1, false)
	check(s.Frame())
	chord(gfx.ModifierControl|gfx.ModifierShift, 'k')
	if tab.source.text.String() != "word two\nword three" {
		t.Fatalf("Ctrl+Shift+K did not delete exactly one line: %q", tab.source.text.String())
	}
	m.tools.terminalOpen = true
	chord(gfx.ModifierControl, '`')
	if m.tools.terminalOpen {
		t.Fatal("Ctrl+backtick did not toggle the terminal")
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
