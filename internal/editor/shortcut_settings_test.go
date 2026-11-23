package editor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
)

func TestEditorShortcutRebinding(t *testing.T) {
	store, err := openSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	preferences, err := readApplicationPreferences(store.directory)
	if err != nil {
		t.Fatal(err)
	}
	desktop := &editorDesktop{store: store, preferences: preferences}
	m, another := newEditorApp(), newEditorApp()
	for _, window := range []*editorApp{m, another} {
		window.desktop = desktop
		store.attach(window)
	}
	tab := m.addTab(document{saved: "同一行\n同一行\n最后一行"}, "search.txt")
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	click := func(label string) {
		t.Helper()
		check(s.ClickLabel(label))
	}
	chord := func(mods gfx.Modifiers, key rune) {
		t.Helper()
		_, err := s.Driver().ShortcutChord(mods, key)
		check(err)
	}
	click("button:设置")
	click("button:快捷键")
	click("button:Ctrl+R")
	if preview := os.Getenv("VIVA_PREVIEW_DIR"); preview != "" {
		check(s.Screenshot(filepath.Join(preview, "shortcut-capture.png")))
	}
	chord(gfx.ModifierControl, 'f')
	if !s.HasText("Ctrl+F 已用于「查找」。") {
		t.Fatal("duplicate shortcut did not show a conflict")
	}
	chord(gfx.ModifierControl, 'h')
	if !s.HasText("Ctrl+H") || m.appSettings.shortcutEdit.action != shortcutNone {
		t.Fatal("new replacement shortcut was not displayed")
	}
	wantReplace := keymap.Chord{Modifiers: gfx.ModifierControl, Char: 'h'}
	saved, err := readApplicationPreferences(store.directory)
	check(err)
	if saved.Shortcuts.chord(shortcutReplace) != wantReplace || another.keyBindings.chord(shortcutReplace) != wantReplace {
		t.Fatal("new binding did not persist or did not reach other windows")
	}
	click("button:F3")
	check(s.Driver().Key(gfx.KeyF8, 0))
	if m.keyBindings.chord(shortcutFindNext).Key != gfx.KeyF8 {
		t.Fatal("unmodified function key was not recorded")
	}
	click("button:Ctrl+Enter")
	check(s.Driver().Key(gfx.KeyF9, 0))
	click("button:关闭设置")
	chord(gfx.ModifierControl, 'r')
	if tab.search.open {
		t.Fatal("old replacement shortcut remained active")
	}
	chord(gfx.ModifierControl, 'h')
	if !tab.search.open || !tab.search.replacing {
		t.Fatal("new replacement shortcut did not open the dialog")
	}
	check(s.Type("同一行"))
	check(s.Driver().Key(gfx.KeyF9, 0))
	if got := tab.search.query.String(); got != "同一行\n" {
		t.Fatalf("new search newline shortcut produced %q", got)
	}
	check(s.Driver().Key(gfx.KeyBackspace, 0))
	click("关闭搜索")
	before := tab.source.text.Selection()
	check(s.Driver().Key(gfx.KeyF3, 0))
	if tab.source.text.Selection() != before {
		t.Fatal("old find-next key moved the selection")
	}
	check(s.Driver().Key(gfx.KeyF8, 0))
	if tab.source.text.Selection() == before {
		t.Fatal("new find-next key did not move the selection in the editor")
	}

	click("button:设置")
	click("button:快捷键")
	click("恢复默认快捷键")
	saved, err = readApplicationPreferences(store.directory)
	check(err)
	if len(saved.Shortcuts) != 0 || another.keyBindings.chord(shortcutReplace).Label() != "Ctrl+R" {
		t.Fatal("restoring defaults did not restore previous bindings")
	}
}
