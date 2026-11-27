package editor

import (
	"maps"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"github.com/vivaui/viva/ui"
)

type shortcutEditState struct {
	action  editorShortcut
	id      gfx.ID
	input   ui.Handlers
	focus   bool
	problem string
}

func (m *editorApp) shortcutSettings(c ui.C) {
	edit := &m.appSettings.shortcutEdit
	current := shortcutPresetName(m.keyBindings)
	ui.Row(c.Part("presets")).FillWidth().Gap(12).Items(ui.Center).Children(func() {
		ui.Text(c, "快捷键预设").Bold().Grow(1)
		choices := shortcutPresetNames[:]
		if current == "自定义" {
			choices = append(choices, current)
		}
		selected := current
		ui.Select(c, &selected, choices, func(value string) string { return value }).Label("快捷键预设").Width(170).Height(32)
		if selected != current && selected != "自定义" {
			c.After(func() { m.applyShortcutPreset(selected) })
		}
	})
	ui.Wrap(c, "切换预设会替换当前快捷键，之后仍可逐项修改。补全使用独立按键，避开输入法的 Ctrl+Space。").FillWidth().FontSize(12).Muted()
	ui.Row(c.Part("help")).FillWidth().Gap(12).Items(ui.Center).Children(func() {
		ui.Wrap(c, "点击快捷键后，按下新的组合键。").Grow(1).FontSize(12).Muted()
		if ui.Button(c.Part("reset-all"), "恢复默认快捷键").Ghost().Height(30).
			Disabled(len(m.keyBindings) == 0).Clicked() {
			c.After(m.resetEditorShortcuts)
		}
	})
	for _, group := range []struct {
		title       string
		first, last editorShortcut
	}{
		{"文件与标签", shortcutNewFile, shortcutRecentFiles},
		{"查找与编辑", shortcutDirectorySearch, shortcutDirectoryReplace},
		{"工作区", shortcutFiles, shortcutOpenSettings},
		{"分屏、补全与导航", shortcutComplete, shortcutOrdinarySplit},
	} {
		ui.Text(c, group.title).Bold().FontSize(14).Height(22)
		ui.Column(c.Key(group.title)).FillWidth().Radius(11).Border(1, m.theme.Border).Clip().Children(func() {
			for action := group.first; action <= group.last; action++ {
				if action != group.first {
					ui.Slot(c).FillWidth().Height(1).Background(m.theme.Border)
				}
				spec := editorShortcutSpecs[action]
				ui.Row(c.Key(spec.id)).FillWidth().Height(38).PadX(12).Gap(8).Items(ui.Center).Children(func() {
					ui.Text(c, spec.label).Grow(1).MinWidth(0).Ellipsis()
					if edit.action == action {
						m.shortcutRecordingRow(c, action)
						return
					}
					label := m.keyBindings.chord(action).Label()
					if label == "" {
						label = "未绑定"
					}
					if ui.Button(c.Part("change"), label).Ghost().Height(28).MinWidth(88).FontSize(12).
						Radius(7).Background(m.theme.Card).Border(1, m.theme.Border).
						Hover(ui.Look{}.Background(m.theme.Hover)).Tooltip("修改快捷键 · " + spec.label).Clicked() {
						*edit = shortcutEditState{
							action: action, focus: true,
							input: ui.Handlers{Role: gfx.RoleButton, Key: m.recordShortcut},
						}
					}
				})
				if edit.action == action && edit.problem != "" {
					ui.Wrap(c, edit.problem).Color(m.theme.Danger).FillWidth().PadX(12).PadBottom(8).FontSize(12)
				}
			}
		})
	}
}

func (m *editorApp) shortcutRecordingRow(c ui.C, action editorShortcut) {
	edit := &m.appSettings.shortcutEdit
	input := ui.Control(c.Part("capture"), "按下新快捷键", &edit.input).Width(122).Height(28).Radius(7).
		Background(m.theme.Secondary).Focus(ui.Look{}.Border(m.theme.Ring)).Draw(func(d *ui.DrawContext) {
		d.Canvas.Label("按下新快捷键", d.Rect.Inset(gfx.Insets{Left: 8, Right: 8}), gfx.TextStyle{Size: 12, Color: m.theme.Foreground})
	})
	edit.id = input.N().ID
	if edit.focus {
		input.AutoFocus()
		edit.focus = false
	}
	if ui.Button(c.Part("clear"), "清除").Ghost().Height(28).FontSize(12).Clicked() {
		c.After(func() { m.changeEditorShortcut(action, keymap.Chord{}) })
	}
	if ui.Button(c.Part("reset"), "恢复默认").Ghost().Height(28).FontSize(12).Clicked() {
		c.After(func() { m.changeEditorShortcut(action, editorShortcutSpecs[action].chord) })
	}
	if ui.Button(c.Part("cancel"), "取消录入快捷键").Icon(closeIcon).IconOnly().Ghost().Size(26, 28).Clicked() {
		*edit = shortcutEditState{}
	}
}

func (m *editorApp) recordShortcut(event gfx.KeyEvent) gfx.EventResult {
	edit := &m.appSettings.shortcutEdit
	if !m.appSettings.open || edit.action == shortcutNone || event.Target != edit.id || event.Release {
		return gfx.EventResult{}
	}
	if event.Key == gfx.KeyTab && event.Modifiers&^ui.Shift == 0 {
		return gfx.EventResult{}
	}
	if event.Repeat {
		return gfx.EventResult{Handled: true}
	}
	chord, ok := keymap.FromEvent(event)
	if !ok || !assignableEditorShortcut(edit.action, chord) {
		edit.problem = "请使用 Ctrl 或 Alt 组合键，或使用 F1–F12。系统和常用编辑键保留原用途。"
	} else {
		m.changeEditorShortcut(edit.action, chord)
	}
	return gfx.EventResult{Handled: true, Repaint: true}
}

func (m *editorApp) changeEditorShortcut(action editorShortcut, chord keymap.Chord) {
	edit := &m.appSettings.shortcutEdit
	preferences, err := m.applicationPreferences()
	if err != nil {
		edit.problem = "无法读取快捷键：" + err.Error()
		return
	}
	if current := preferences.Shortcuts.actionFor(chord); current != shortcutNone && current != action {
		edit.problem = chord.Label() + " 已用于「" + editorShortcutSpecs[current].label + "」。"
		edit.focus = true
		return
	}
	spec := editorShortcutSpecs[action]
	preferences.Shortcuts = maps.Clone(preferences.Shortcuts)
	if chord == spec.chord {
		delete(preferences.Shortcuts, spec.id)
	} else {
		if preferences.Shortcuts == nil {
			preferences.Shortcuts = make(shortcutOverrides)
		}
		preferences.Shortcuts[spec.id] = chord
	}
	if m.persistApplicationSettings(preferences) {
		*edit = shortcutEditState{}
	}
}

func (m *editorApp) resetEditorShortcuts() {
	preferences, err := m.applicationPreferences()
	if err != nil {
		m.appSettings.problem = "无法读取快捷键：" + err.Error()
		return
	}
	preferences.Shortcuts = nil
	if m.persistApplicationSettings(preferences) {
		m.appSettings.shortcutEdit = shortcutEditState{}
	}
}
