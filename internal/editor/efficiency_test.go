package editor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
)

func TestEfficiencyPaletteKeyboard(t *testing.T) {
	m := newEditorApp()
	m.addTab(document{saved: "alpha"}, "配置-alpha.txt")
	want := m.addTab(document{saved: "beta"}, "配置-beta.txt")
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err := s.Driver().ShortcutChord(gfx.ModifierControl, 'p')
	check(err)
	if m.palette == nil || m.palette.commands {
		t.Fatal("Ctrl+P did not open quick open")
	}
	check(s.Type("alpha"))
	if len(m.palette.matches) != 1 {
		t.Fatalf("filtered files: %+v", m.palette.matches)
	}
	check(s.Driver().Key(gfx.KeyEnter, 0))
	if m.palette != nil || m.active == want {
		t.Fatal("quick open did not activate the matching tab")
	}
	_, err = s.Driver().ShortcutChord(gfx.ModifierControl|gfx.ModifierShift, 'p')
	check(err)
	check(s.Type("newFile"))
	if m.palette == nil || len(m.palette.matches) != 1 {
		t.Fatal("command search did not match command ID")
	}
	if dir := os.Getenv("VIVA_PREVIEW_DIR"); dir != "" {
		check(s.Screenshot(filepath.Join(dir, "command-palette.png")))
	}
	check(s.Driver().Key(gfx.KeyEnter, 0))
	if len(m.tabs) != 3 || m.palette != nil {
		t.Fatal("command did not create a document")
	}
	_, err = s.Driver().ShortcutChord(gfx.ModifierControl, 'p')
	check(err)
	check(s.Driver().Key(gfx.KeyEscape, 0))
	if m.palette != nil {
		t.Fatal("escape did not close palette")
	}
	check(s.Type("focus returned"))
	if m.active.source.text.String() != "focus returned" {
		t.Fatal("palette did not return editor focus")
	}
}

func TestEfficiencyProjectScanAndRanking(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "node_modules"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"main.go", "中文.txt", "node_modules/hidden.js"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("sample"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	files := scanPaletteFiles(context.Background(), []string{dir})
	if files.partial || len(files.paths) != 2 {
		t.Fatalf("scan: %+v", files)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result := scanPaletteFiles(ctx, []string{dir}); !result.partial || len(result.paths) != 0 {
		t.Fatalf("cancellation: %+v", result)
	}
	state := &navigationPalette{dirty: true}
	state.addPath(filepath.Join(dir, "main.go"), "")
	state.addPath(filepath.Join(dir, "main.go"), "")
	state.addPath(filepath.Join(dir, "domain.go"), "")
	state.query.SetText("main")
	state.filter()
	if len(state.matches) != 2 || state.matches[0].label != "main.go" {
		t.Fatalf("ranking/deduplication: %+v", state.matches)
	}
}

func TestEfficiencyReopenBoundedAndFreshDisk(t *testing.T) {
	m := newEditorApp()
	tab := m.addTab(document{saved: "original"}, "draft.txt")
	tab.source.replace("未保存的草稿")
	m.removeTab(tab)
	m.reopenClosedTab()
	if len(m.tabs) != 1 || m.active.source.text.String() != "未保存的草稿" || !m.active.modified() {
		t.Fatal("unsaved text not restored")
	}
	for i := 0; i < closedTabLimit+5; i++ {
		m.removeTab(m.addTab(document{}, "empty.txt"))
	}
	if len(m.closedTabs) != closedTabLimit {
		t.Fatal("closed stack exceeded its limit")
	}
	big := m.addTab(document{saved: strings.Repeat("x", closedTabTextLimit+1)}, "huge.txt")
	m.removeTab(big)
	if len(m.closedTabs) != closedTabLimit {
		t.Fatal("oversize unnamed text retained")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	m.load(path)
	m.removeTab(m.active)
	if err := os.WriteFile(path, []byte("after"), 0600); err != nil {
		t.Fatal(err)
	}
	m.reopenClosedTab()
	if m.active.source.text.String() != "after" || m.active.modified() {
		t.Fatal("clean file did not reread disk")
	}
	m.removeTab(m.active)
	m.load(path)
	count := len(m.tabs)
	m.reopenClosedTab()
	if len(m.tabs) != count {
		t.Fatal("reopen duplicated an already open path")
	}
}

func TestEfficiencySaveAllStopsOnCancelAndConflict(t *testing.T) {
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	paths := []string{filepath.Join(t.TempDir(), "first.txt"), filepath.Join(t.TempDir(), "last.txt")}
	for _, path := range paths {
		if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	m.load(paths[0])
	first := m.active
	first.source.replace("first changed")
	draft := m.addTab(document{}, "untitled")
	draft.source.replace("draft")
	m.load(paths[1])
	last := m.active
	last.source.replace("last changed")
	s := openTestSession(t, m)
	if err := s.Driver().QueueSaveFile("", app.ErrCanceled); err != nil {
		t.Fatal(err)
	}
	m.saveAllTabs()
	if m.savingAll || first.modified() || !draft.modified() || !last.modified() {
		t.Fatal("save all did not stop at canceled save-as")
	}
	draftPath := filepath.Join(t.TempDir(), "draft.txt")
	if err := s.Driver().QueueSaveFile(draftPath, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths[1], []byte("external"), 0600); err != nil {
		t.Fatal(err)
	}
	m.saveAllTabs()
	if m.savingAll || draft.modified() || !last.modified() || !m.showError {
		t.Fatal("save all did not stop on conflict")
	}
	bytes, err := os.ReadFile(paths[1])
	if err != nil || string(bytes) != "external" {
		t.Fatal("disk conflict was overwritten")
	}
}

func TestEfficiencyShortcutPresets(t *testing.T) {
	for _, name := range shortcutPresetNames {
		bindings := shortcutPreset(name)
		if shortcutPresetName(bindings) != name {
			t.Fatalf("preset recognition: %s", name)
		}
		seen := map[keymap.Chord]bool{}
		for action := shortcutNewFile; action < editorShortcutCount; action++ {
			chord := bindings.chord(action)
			if !chord.Valid() {
				continue
			}
			if seen[chord] || !assignableEditorShortcut(action, chord) {
				t.Fatalf("%s conflicting/invalid %s", name, chord.Label())
			}
			seen[chord] = true
		}
	}
	custom := shortcutOverrides{"find": {Modifiers: gfx.ModifierControl, Char: 'p'}}
	if custom.chord(shortcutQuickOpen).Valid() || custom.actionFor(keymap.Chord{Modifiers: gfx.ModifierControl, Char: 'p'}) != shortcutFind {
		t.Fatal("new default overrode an existing custom binding")
	}
}

func TestEfficiencySettingsPreview(t *testing.T) {
	if os.Getenv("VIVA_PREVIEW_DIR") == "" {
		t.Skip("preview opt-in")
	}
	m := newEditorApp()
	m.newDocument()
	s := openTestSession(t, m)
	if err := s.ClickLabel("button:设置"); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("button:快捷键"); err != nil {
		t.Fatal(err)
	}
	if err := s.Screenshot(filepath.Join(os.Getenv("VIVA_PREVIEW_DIR"), "shortcut-presets.png")); err != nil {
		t.Fatal(err)
	}
}
