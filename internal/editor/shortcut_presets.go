package editor

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"github.com/vivaui/viva/ui"
)

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
	}
	return bindings
}

func shortcutPresetName(bindings shortcutOverrides) string {
	for _, name := range []string{"EditorX", "Notepad++", "Sublime Text", "EditPlus"} {
		preset := shortcutPreset(name)
		matches := true
		for action := shortcutNewFile; action < editorShortcutCount; action++ {
			if preset.chord(action) != bindings.chord(action) {
				matches = false
				break
			}
		}
		if matches {
			return name
		}
	}
	return "自定义"
}

func (m *editorApp) applyShortcutPreset(name string) {
	preferences, err := m.applicationPreferences()
	if err != nil {
		m.appSettings.problem = "无法读取快捷键：" + err.Error()
		return
	}
	preferences.Shortcuts = shortcutPreset(name)
	if m.persistApplicationSettings(preferences) {
		m.appSettings.shortcutEdit = shortcutEditState{}
	}
}
