package editor

import (
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"github.com/vivaui/viva/ui"
)

type editorShortcut uint8

const (
	shortcutNone editorShortcut = iota
	shortcutNewFile
	shortcutNewWindow
	shortcutOpenFile
	shortcutOpenLink
	shortcutOpenProject
	shortcutSave
	shortcutSaveAs
	shortcutCloseTab
	shortcutNextTab
	shortcutPreviousTab
	shortcutFind
	shortcutReplace
	shortcutFindNext
	shortcutFindPrevious
	shortcutSearchNewline
	shortcutWrap
	shortcutGotoLine
	shortcutToggleBookmark
	shortcutNextBookmark
	shortcutPreviousBookmark
	shortcutDuplicateLine
	shortcutDeleteLine
	shortcutFiles
	shortcutOutline
	shortcutGit
	shortcutTerminal
	shortcutOpenSettings
	editorShortcutCount
)

var editorShortcutSpecs = [editorShortcutCount]struct {
	id, label string
	chord     keymap.Chord
}{
	shortcutNewFile:          {"newFile", "新建文件", keymap.Chord{Modifiers: ui.Ctrl, Char: 'n'}},
	shortcutNewWindow:        {"newWindow", "新建窗口", keymap.Chord{Modifiers: ui.Ctrl | ui.Shift, Char: 'n'}},
	shortcutOpenFile:         {"openFile", "打开文件", keymap.Chord{Modifiers: ui.Ctrl, Char: 'o'}},
	shortcutOpenLink:         {"openLink", "打开链接文件", keymap.Chord{Modifiers: ui.Ctrl | ui.Shift, Char: 'l'}},
	shortcutOpenProject:      {"openProject", "添加项目", keymap.Chord{Modifiers: ui.Ctrl | ui.Shift, Char: 'o'}},
	shortcutSave:             {"save", "保存", keymap.Chord{Modifiers: ui.Ctrl, Char: 's'}},
	shortcutSaveAs:           {"saveAs", "另存为", keymap.Chord{Modifiers: ui.Ctrl | ui.Shift, Char: 's'}},
	shortcutCloseTab:         {"closeTab", "关闭当前标签", keymap.Chord{Modifiers: ui.Ctrl, Char: 'w'}},
	shortcutNextTab:          {"nextTab", "下一个标签", keymap.Chord{Modifiers: ui.Ctrl, Key: gfx.KeyTab}},
	shortcutPreviousTab:      {"previousTab", "上一个标签", keymap.Chord{Modifiers: ui.Ctrl | ui.Shift, Key: gfx.KeyTab}},
	shortcutFind:             {"find", "查找", keymap.Chord{Modifiers: ui.Ctrl, Char: 'f'}},
	shortcutReplace:          {"replace", "替换", keymap.Chord{Modifiers: ui.Ctrl, Char: 'r'}},
	shortcutFindNext:         {"findNext", "下一个匹配", keymap.Chord{Key: gfx.KeyF3}},
	shortcutFindPrevious:     {"findPrevious", "上一个匹配", keymap.Chord{Modifiers: ui.Shift, Key: gfx.KeyF3}},
	shortcutSearchNewline:    {"searchNewline", "查找框内输入换行", keymap.Chord{Modifiers: ui.Ctrl, Key: gfx.KeyEnter}},
	shortcutWrap:             {"wrap", "自动折行", keymap.Chord{Modifiers: ui.Alt, Char: 'z'}},
	shortcutGotoLine:         {"gotoLine", "转到行", keymap.Chord{Modifiers: ui.Ctrl, Char: 'g'}},
	shortcutToggleBookmark:   {"toggleBookmark", "切换行书签", keymap.Chord{Modifiers: ui.Ctrl, Key: gfx.KeyF2}},
	shortcutNextBookmark:     {"nextBookmark", "下一个书签", keymap.Chord{Key: gfx.KeyF2}},
	shortcutPreviousBookmark: {"previousBookmark", "上一个书签", keymap.Chord{Modifiers: ui.Shift, Key: gfx.KeyF2}},
	shortcutDuplicateLine:    {"duplicateLine", "复制当前行", keymap.Chord{Modifiers: ui.Ctrl, Char: 'd'}},
	shortcutDeleteLine:       {"deleteLine", "删除选中行", keymap.Chord{Modifiers: ui.Ctrl, Char: 'l'}},
	shortcutFiles:            {"files", "文件栏", keymap.Chord{Modifiers: ui.Ctrl, Char: 'b'}},
	shortcutOutline:          {"outline", "Markdown 大纲", keymap.Chord{Modifiers: ui.Ctrl | ui.Alt, Char: 'b'}},
	shortcutGit:              {"git", "Git", keymap.Chord{Modifiers: ui.Ctrl | ui.Shift, Char: 'g'}},
	shortcutTerminal:         {"terminal", "终端", keymap.Chord{Modifiers: ui.Ctrl | ui.Alt, Char: 't'}},
	shortcutOpenSettings:     {"settings", "设置", keymap.Chord{Modifiers: ui.Ctrl, Char: ','}},
}

// An absent action uses its default. A zero override explicitly removes its binding.
type shortcutOverrides map[string]keymap.Chord

func (bindings shortcutOverrides) chord(action editorShortcut) keymap.Chord {
	spec := editorShortcutSpecs[action]
	if chord, found := bindings[spec.id]; found && (chord == (keymap.Chord{}) || assignableEditorShortcut(chord)) {
		return chord
	}
	return spec.chord
}

func (bindings shortcutOverrides) actionFor(chord keymap.Chord) editorShortcut {
	if chord.Valid() {
		for action := shortcutNewFile; action < editorShortcutCount; action++ {
			if bindings.chord(action) == chord {
				return action
			}
		}
	}
	return shortcutNone
}

func assignableEditorShortcut(chord keymap.Chord) bool {
	if !chord.Valid() || chord.Modifiers&gfx.ModifierMeta != 0 {
		return false
	}
	if chord.Modifiers&(ui.Ctrl|ui.Alt) == 0 {
		return chord.Key >= gfx.KeyF1 && chord.Key <= gfx.KeyF12
	}
	// Windows owns window switching/closing. Focused text fields own standard editing chords.
	if chord.Modifiers&ui.Alt != 0 {
		switch chord.Key {
		case gfx.KeyTab, gfx.KeyF4, gfx.KeyEscape:
			return false
		}
		return chord.Char != ' ' || chord.Modifiers&ui.Ctrl != 0
	}
	switch chord.Key {
	case gfx.KeyEscape, gfx.KeyBackspace, gfx.KeyDelete, gfx.KeyLeft, gfx.KeyRight, gfx.KeyHome, gfx.KeyEnd:
		return false
	case gfx.KeyInsert:
		return chord.Modifiers != ui.Ctrl
	}
	if chord.Modifiers == ui.Ctrl {
		return !strings.ContainsRune("acvxyz", rune(chord.Char))
	}
	return chord.Char != 'z'
}

func (m *editorApp) shortcutPressed(c ui.C, action editorShortcut) bool {
	chord := m.keyBindings.chord(action)
	return chord.Valid() && ui.Shortcut(c.Part(editorShortcutSpecs[action].id), chord.Modifiers, chord.Name())
}

func (m *editorApp) shortcutHint(label string, action editorShortcut) string {
	if binding := m.keyBindings.chord(action).Label(); binding != "" {
		return label + " · " + binding
	}
	return label
}

func (m *editorApp) shortcutMenu(item ui.MenuItem, action editorShortcut) ui.MenuItem {
	if chord := m.keyBindings.chord(action); chord.Valid() {
		return item.Keys(chord.Modifiers, chord.Name())
	}
	return item
}

func (m *editorApp) editorShortcuts(c ui.C) {
	if m.keyBindings.actionFor(keymap.Chord{Modifiers: ui.Ctrl, Char: 'h'}) == shortcutNone &&
		m.keyBindings.chord(shortcutReplace) == editorShortcutSpecs[shortcutReplace].chord &&
		ui.Shortcut(c.Part("replace-compatible"), ui.Ctrl, "h") {
		c.After(func() { m.runEditorShortcut(shortcutReplace) })
	}
	for action := shortcutNewFile; action < editorShortcutCount; action++ {
		if action == shortcutOpenSettings || action == shortcutSearchNewline {
			continue
		}
		if m.shortcutPressed(c, action) {
			c.After(func() { m.runEditorShortcut(action) })
		}
	}
}

func (m *editorApp) functionShortcut(event gfx.KeyEvent) gfx.EventResult {
	if event.Release || event.Key < gfx.KeyF1 || event.Key > gfx.KeyF12 {
		return gfx.EventResult{}
	}
	chord, _ := keymap.FromEvent(event)
	handled := m.runEditorShortcut(m.keyBindings.actionFor(chord))
	return gfx.EventResult{Handled: handled, Repaint: handled}
}

func (m *editorApp) runEditorShortcut(action editorShortcut) bool {
	if action == shortcutNone || m.interactionBlocked() {
		return false
	}
	if action == shortcutOpenSettings {
		if m.appSettings.shortcutEdit.action != shortcutNone {
			return false
		}
		m.toggleApplicationSettings()
		return true
	}
	if m.appSettings.open {
		return false
	}
	if m.vaultActive() {
		switch action {
		case shortcutCloseTab:
			m.closePasswordVault()
			return true
		case shortcutFind:
			m.passwordVault.focusSearch = m.passwordVault.store != nil
			return true
		case shortcutNextTab, shortcutPreviousTab:
			if m.active == nil {
				return false
			}
			m.activate(m.active)
			return true
		case shortcutNewFile, shortcutNewWindow, shortcutOpenFile, shortcutOpenLink, shortcutOpenProject, shortcutGit, shortcutTerminal:
		default:
			return false
		}
	}
	switch action {
	case shortcutNewFile:
		m.newDocument()
	case shortcutNewWindow:
		if m.newWindow == nil {
			return false
		}
		m.newWindow()
	case shortcutOpenFile:
		m.open()
	case shortcutOpenLink:
		m.openLinkedFileDialog()
	case shortcutOpenProject:
		m.openFolder()
	case shortcutFiles:
		m.toggleFiles()
	case shortcutGit:
		m.useTool("Git")
	case shortcutTerminal:
		m.useTool("终端")
	default:
		tab := m.active
		if tab == nil {
			return false
		}
		switch action {
		case shortcutFind:
			tab.openSearch()
		case shortcutReplace:
			tab.openReplace()
		case shortcutFindNext, shortcutFindPrevious:
			if !tab.canFind() {
				return false
			}
			direction := 1
			if action == shortcutFindPrevious {
				direction = -1
			}
			m.stepSearch(tab, direction)
		case shortcutSave:
			m.saveTab(tab, false)
		case shortcutSaveAs:
			m.saveTab(tab, true)
		case shortcutCloseTab:
			m.closeTab(tab)
		case shortcutNextTab:
			m.stepTab(1)
		case shortcutPreviousTab:
			m.stepTab(-1)
		case shortcutOutline:
			m.toggleMarkdownOutline()
		case shortcutGotoLine:
			m.openGotoLine()
		case shortcutToggleBookmark:
			m.toggleTextBookmark(tab)
		case shortcutNextBookmark, shortcutPreviousBookmark:
			m.stepTextBookmark(tab, action == shortcutNextBookmark)
		case shortcutDuplicateLine, shortcutDeleteLine:
			command := components.CodeEditorDuplicateLinesDown
			if action == shortcutDeleteLine {
				command = components.CodeEditorDeleteLines
			}
			if tab.largeText != nil && tab.largeText.editing != nil {
				tab.largeEditCodeCommand(command)
				return true
			}
			if !editableTextSource(tab) {
				return false
			}
			if _, composing := tab.source.text.Composition(); composing {
				return false
			}
			command.Apply(tab.source.text)
			tab.source.focus = true
		case shortcutWrap:
			if !tab.canSearchText() && (tab.largeText == nil || tab.largeText.editing == nil) {
				return false
			}
			tab.toggleWrap()
		default:
			return false
		}
	}
	return true
}
