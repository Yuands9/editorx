package editor

import "github.com/vivaui/viva/ui"

type sidebarPreferences struct {
	Position     string `json:"position,omitempty"`
	HideSave     bool   `json:"hideSave,omitempty"`
	HideSearch   bool   `json:"hideSearch,omitempty"`
	HideWrap     bool   `json:"hideWrap,omitempty"`
	HideAI       bool   `json:"hideAI,omitempty"`
	HideGit      bool   `json:"hideGit,omitempty"`
	HideTerminal bool   `json:"hideTerminal,omitempty"`
	HideTheme    bool   `json:"hideTheme,omitempty"`
	Templates    bool   `json:"templates,omitempty"`
}

func (preferences sidebarPreferences) normalized() sidebarPreferences {
	if preferences.Position != "left" {
		preferences.Position = "right"
	}
	return preferences
}

func (preferences sidebarPreferences) withChanges(before, after sidebarPreferences) sidebarPreferences {
	if before.Position != after.Position {
		preferences.Position = after.Position
	}
	if before.HideSave != after.HideSave {
		preferences.HideSave = after.HideSave
	}
	if before.HideSearch != after.HideSearch {
		preferences.HideSearch = after.HideSearch
	}
	if before.HideWrap != after.HideWrap {
		preferences.HideWrap = after.HideWrap
	}
	if before.HideAI != after.HideAI {
		preferences.HideAI = after.HideAI
	}
	if before.HideGit != after.HideGit {
		preferences.HideGit = after.HideGit
	}
	if before.HideTerminal != after.HideTerminal {
		preferences.HideTerminal = after.HideTerminal
	}
	if before.HideTheme != after.HideTheme {
		preferences.HideTheme = after.HideTheme
	}
	if before.Templates != after.Templates {
		preferences.Templates = after.Templates
	}
	return preferences.normalized()
}

func (m *editorApp) applySidebarPreferences(preferences sidebarPreferences) {
	preferences = preferences.normalized()
	if (m.sidebarPrefs.Position == "left") != (preferences.Position == "left") {
		for _, split := range []*ui.SplitterState{&m.aiEditorSplit, m.activeAssetSplit()} {
			if split != nil {
				split.Ratio = 1 - split.Ratio
				split.Pos = 0
			}
		}
	}
	if m.sidebarPrefs.Templates && !preferences.Templates {
		m.templatePanel = nil
	}
	m.sidebarPrefs = preferences
}

func (m *editorApp) activeAssetSplit() *ui.SplitterState {
	if m.translation != nil {
		return &m.translation.split
	}
	if m.templatePanel != nil {
		return &m.templatePanel.split
	}
	return nil
}

func (m *editorApp) newAssetSplit() ui.SplitterState {
	ratio := float32(.64)
	if m.sidebarPrefs.Position == "left" {
		ratio = 1 - ratio
	}
	return ui.SplitterState{Ratio: ratio}
}

func (m *editorApp) sidebarSettings(c ui.C) {
	draft := &m.appSettings.draft
	m.applicationSetting(c.Part("position"), "侧栏位置", "工具按钮及其打开的面板显示在这一侧。", func() {
		ui.Row(c).Gap(5).Children(func() {
			for _, option := range []struct{ value, label string }{{"left", "左侧"}, {"right", "右侧"}} {
				button := ui.Button(c.Key(option.value), option.label).Ghost()
				if draft.Sidebar.Position == option.value {
					button.Secondary()
				}
				if button.Clicked() {
					draft.Sidebar.Position = option.value
					c.After(m.saveApplicationSettings)
				}
			}
		})
	})
	ui.Text(c, "显示的按钮").Bold().FontSize(14)
	options := []struct {
		label    string
		value    *bool
		inverted bool
	}{
		{"模板库", &draft.Sidebar.Templates, false}, {"翻译", &draft.TranslationEnabled, false},
		{"保存文件", &draft.Sidebar.HideSave, true}, {"查找内容", &draft.Sidebar.HideSearch, true},
		{"自动折行", &draft.Sidebar.HideWrap, true}, {"AI 助手", &draft.Sidebar.HideAI, true},
		{"Git", &draft.Sidebar.HideGit, true}, {"终端", &draft.Sidebar.HideTerminal, true}, {"颜色主题", &draft.Sidebar.HideTheme, true},
	}
	ui.Column(c).FillWidth().Pad(12).Gap(8).Radius(12).Border(1, m.theme.Border).Children(func() {
		for row := 0; row < len(options); row += 2 {
			ui.Row(c.KeyInt(row)).FillWidth().Gap(16).Children(func() {
				for _, item := range options[row:min(row+2, len(options))] {
					ui.Column(c.Key(item.label)).Grow(1).Basis(0).MinWidth(0).Children(func() {
						visible := *item.value != item.inverted
						if settingsSwitchRow(c, item.label, &visible) {
							*item.value = visible != item.inverted
							c.After(m.saveApplicationSettings)
						}
					})
				}
				if row+1 == len(options) {
					ui.Slot(c).Grow(1).Basis(0)
				}
			})
		}
	})
	ui.Wrap(c, "模板库和翻译默认隐藏。显示翻译按钮不会下载词库或启动模型；设置按钮始终保留。").FillWidth().Muted().FontSize(12)
}
