package editor

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"github.com/vivaui/viva/ui"
)

const (
	ideaShortcutPreset    = "IntelliJ IDEA"
	pycharmShortcutPreset = "PyCharm"
	vsCodeShortcutPreset  = "VS Code"
)

var shortcutPresetNames = [...]string{"EditorX", "Notepad++", "Sublime Text", "EditPlus", ideaShortcutPreset, pycharmShortcutPreset, vsCodeShortcutPreset}

// Presets adapt supported commands; they do not emulate another editor's engine.
func shortcutPreset(name string) shortcutOverrides {
	bindings := shortcutOverrides{}
	set := func(action editorShortcut, mods gfx.Modifiers, char byte) {
		bindings[editorShortcutSpecs[action].id] = keymap.Chord{Modifiers: mods, Char: char}
	}
	switch name {
	case "Notepad++":
		set(shortcutReplace, ui.Ctrl, 'h')
		set(shortcutSaveAll, ui.Ctrl|ui.Shift, 's')
		set(shortcutSaveAs, ui.Ctrl|ui.Alt, 's')
	case "Sublime Text":
		set(shortcutSelectNext, ui.Ctrl, 'd')
		set(shortcutSplitSelection, ui.Ctrl|ui.Shift, 'l')
		set(shortcutOpenLink, ui.Ctrl|ui.Alt, 'l')
		set(shortcutReplace, ui.Ctrl, 'h')
		set(shortcutDuplicateLine, ui.Ctrl|ui.Shift, 'd')
		set(shortcutDeleteLine, ui.Ctrl|ui.Shift, 'k')
	case "EditPlus":
		set(shortcutReplace, ui.Ctrl, 'h')
		set(shortcutDuplicateLine, ui.Ctrl, 'j')
		set(shortcutSaveAll, ui.Ctrl|ui.Shift, 's')
		set(shortcutSaveAs, ui.Ctrl|ui.Alt, 's')
	case vsCodeShortcutPreset:
		set(shortcutReplace, ui.Ctrl, 'h')
		set(shortcutDirectoryReplace, ui.Ctrl|ui.Shift, 'h')
		set(shortcutSelectNext, ui.Ctrl, 'd')
		set(shortcutSelectAllMatches, ui.Ctrl|ui.Shift, 'l')
		set(shortcutOpenLink, ui.Ctrl|ui.Alt, 'l')
		set(shortcutDeleteLine, ui.Ctrl|ui.Shift, 'k')
		set(shortcutTerminal, ui.Ctrl, '`')
		bindings[editorShortcutSpecs[shortcutDuplicateLine].id] = keymap.Chord{Modifiers: ui.Alt | ui.Shift, Key: gfx.KeyDown}
		bindings[editorShortcutSpecs[shortcutDuplicateLineAbove].id] = keymap.Chord{Modifiers: ui.Alt | ui.Shift, Key: gfx.KeyUp}
	case ideaShortcutPreset, pycharmShortcutPreset:
		setKey := func(action editorShortcut, mods gfx.Modifiers, key gfx.Key) {
			bindings[editorShortcutSpecs[action].id] = keymap.Chord{Modifiers: mods, Key: key}
		}
		set(shortcutQuickOpen, ui.Ctrl|ui.Shift, 'n')
		set(shortcutCommandPalette, ui.Ctrl|ui.Shift, 'a')
		set(shortcutRecentFiles, ui.Ctrl, 'e')
		set(shortcutSaveAll, ui.Ctrl, 's')
		setKey(shortcutCloseTab, ui.Ctrl, gfx.KeyF4)
		setKey(shortcutNextTab, ui.Alt, gfx.KeyRight)
		setKey(shortcutPreviousTab, ui.Alt, gfx.KeyLeft)
		setKey(shortcutNavigateBack, ui.Ctrl|ui.Alt, gfx.KeyLeft)
		setKey(shortcutNavigateForward, ui.Ctrl|ui.Alt, gfx.KeyRight)
		set(shortcutComplete, ui.Ctrl|ui.Alt, ' ')
		set(shortcutDirectoryReplace, ui.Ctrl|ui.Shift, 'r')
		bindings[editorShortcutSpecs[shortcutDeleteLine].id] = controlY
		set(shortcutSelectNext, ui.Alt, 'j')
		set(shortcutSelectAllMatches, ui.Ctrl|ui.Alt|ui.Shift, 'j')
		set(shortcutSplitSelection, ui.Alt|ui.Shift, 'g')
		setKey(shortcutToggleBookmark, 0, gfx.KeyF11)
		set(shortcutFiles, ui.Alt, '1')
		setKey(shortcutRevealProjectFile, ui.Alt, gfx.KeyF1)
		set(shortcutGit, ui.Alt, '9')
		set(shortcutGitCommit, ui.Ctrl, 'k')
		setKey(shortcutTerminal, ui.Alt, gfx.KeyF12)
		set(shortcutOpenSettings, ui.Ctrl|ui.Alt, 's')
	}
	return bindings
}

func shortcutPresetName(bindings shortcutOverrides, preferred string) string {
	matches := func(name string) bool {
		preset := shortcutPreset(name)
		for action := shortcutNewFile; action < editorShortcutCount; action++ {
			if preset.chord(action) != bindings.chord(action) {
				return false
			}
		}
		return true
	}
	for _, name := range shortcutPresetNames {
		if preferred == name && matches(name) {
			return name
		}
	}
	for _, name := range shortcutPresetNames {
		if matches(name) {
			return name
		}
	}
	return "自定义"
}

func (m *editorApp) applyShortcutPreset(name string) {
	preferences, err := m.applicationPreferences()
	if err != nil {
		m.appSettings.problem = uiText("无法读取快捷键：") + err.Error()
		return
	}
	preferences.Shortcuts = shortcutPreset(name)
	preferences.ShortcutPreset = name
	if m.persistApplicationSettings(preferences) {
		m.appSettings.shortcutEdit = shortcutEditState{}
	}
}
