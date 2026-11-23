package editor

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

const (
	settingsGeneral    = "常规"
	settingsAppearance = "外观"
	settingsEditor     = "编辑器"
	settingsAI         = "AI 助手"
)

type settingsPageState struct {
	open, returnToAI bool
	section, problem string
	original, draft  applicationPreferences
	fonts            []string
	fontRequest      *ui.EffectState[[]string, int]
}

func (m *editorApp) openApplicationSettings() {
	m.showSettings(settingsGeneral)
}

func (m *editorApp) toggleApplicationSettings() {
	if m.appSettings.open {
		m.closeSettings()
		return
	}
	m.openApplicationSettings()
}

func (m *editorApp) showSettings(section string) {
	settings := &m.appSettings
	if settings.open && settings.section == section {
		return
	}
	if !settings.open {
		if m.dataDirectory == "" {
			m.dataDirectory, _ = editorDataDirectory()
		}
		preferences, err := m.applicationPreferences()
		*settings = settingsPageState{open: true, original: preferences, draft: preferences}
		if err != nil {
			settings.problem = "无法读取设置：" + err.Error()
		}
		m.fileMenu, m.languageMenu = false, false
	}
	settings.section = section
	if section == settingsAI && !m.ai.settingsEditing {
		m.reloadAISettingsPage()
	}
	if section == settingsEditor && settings.fonts == nil && settings.fontRequest == nil {
		settings.loadEditorFonts()
	}
	m.invalidator.Invalidate()
}

func (m *editorApp) closeSettings() {
	returnToAI := m.appSettings.returnToAI
	if m.applyAIProfileChanges() {
		m.ai.settings, m.ai.settingsEditing = aiSettingsEditor{}, false
	} else {
		editor := &m.ai.settings
		if editor.viewingSavedKey {
			editor.toggleKeyVisibility()
		}
		editor.probe = nil
		editor.providersOpen, editor.modelsOpen = false, false
		m.status = "模型配置待修正，已保留本次输入"
	}
	m.appSettings = settingsPageState{}
	if returnToAI && m.ai.open {
		m.ai.focusPrompt = true
	} else if m.active != nil && m.active.search.open {
		m.active.search.focus = true
	} else {
		m.focusActiveSource()
	}
	m.invalidator.Invalidate()
}

func (m *editorApp) settingsPage(c ui.C) {
	settings, ai, editor := &m.appSettings, &m.ai, &m.ai.settings
	title := settings.section
	aiDetail := settings.section == settingsAI && (ai.settingsEditing || editor.removePending)
	if aiDetail {
		editor.updateProbe(c.Part("model-probe"))
		title = "编辑模型配置"
		if editor.original.ID == 0 {
			title = "新增模型配置"
		}
		if editor.removePending {
			title = "删除模型配置？"
		}
	}
	if ui.Shortcut(c, 0, "escape") {
		c.After(m.closeSettings)
	}
	ui.Column(c).Grow(1).MinWidth(0).FillHeight().Pad(10).Gap(8).Children(func() {
		ui.Row(c.Part("heading")).FillWidth().Height(48).PadX(12).Gap(10).Items(ui.Center).Children(func() {
			ui.Icon(c, settingsIcon).Size(19, 19).Color(m.theme.MutedForeground)
			ui.Text(c, "设置").FontSize(20).Bold()
		})
		ui.Row(c.Part("layout")).Grow(1).Basis(0).FillWidth().MinHeight(0).Gap(8).Children(func() {
			m.workspacePanel(c.Part("navigation")).Width(170).Shrink(0).Pad(8).Gap(4).Children(func() {
				for _, section := range []struct {
					label string
					icon  gfx.Icon
				}{
					{settingsGeneral, settingsIcon},
					{settingsAppearance, themeIcon},
					{settingsEditor, codeIcon},
					{settingsAI, aiIcon},
				} {
					button := ui.Button(c.Part("category").Key(section.label), section.label).Icon(section.icon).
						Ghost().FillWidth().Height(40).Radius(10).Disabled(aiDetail)
					if section.label == settings.section {
						button.Secondary()
					}
					if button.Clicked() {
						c.After(func() { m.showSettings(section.label) })
					}
				}
			})
			m.workspacePanel(c.Part("content")).Grow(1).Pad(16).Gap(8).Children(func() {
				ui.Text(c, title).FontSize(21).Bold().Height(30)
				body := ui.Scroll(c.Part("options").Key(title)).FillWidth().Grow(1).Basis(0).MinHeight(0).Scrollbars()
				if settings.section == settingsAI && editor.scrollTop {
					body.ScrollState().ScrollTo(0)
					editor.scrollTop = false
				}
				submitted := false
				body.Children(func() {
					ui.Column(c.Part("settings-options")).FillWidth().MaxWidth(850).PadRight(8).Gap(12).Children(func() {
						switch settings.section {
						case settingsGeneral:
							m.generalSettings(c)
						case settingsAppearance:
							m.appearanceSettings(c)
						case settingsEditor:
							m.editorSettings(c)
						case settingsAI:
							m.aiSettingsContent(c, &submitted)
						}
					})
				})
				if settings.problem != "" {
					ui.Wrap(c, settings.problem).FillWidth().FontSize(12).Color(m.theme.Danger)
				}
				if aiDetail {
					ui.Row(c.Part("settings-actions")).FillWidth().MaxWidth(850).Height(40).Gap(8).Items(ui.Center).Justify(ui.End).Children(func() {
						m.aiSettingsActions(c, submitted)
					})
				}
			})
		})
	})
}

func (m *editorApp) generalSettings(c ui.C) {
	draft := &m.appSettings.draft
	m.applicationSetting(c.Part("show-tray"), "显示系统托盘图标",
		"点击托盘图标恢复窗口，右键可打开设置或退出。", func() {
			if ui.Switch(c, &draft.ShowTrayIcon, "").Label("显示系统托盘图标").Changed() {
				c.After(m.saveApplicationSettings)
			}
		})
	m.applicationSetting(c.Part("close-action"), "关闭窗口时退出程序",
		"关闭此项后，窗口会隐藏到后台。再次打开 EditorX 可以唤回窗口。", func() {
			if ui.Switch(c, &draft.ExitOnClose, "").Label("关闭窗口时退出程序").Changed() {
				c.After(m.saveApplicationSettings)
			}
		})
}

func (m *editorApp) appearanceSettings(c ui.C) {
	theme := m.appSettings.draft.Theme
	if theme == "" {
		theme = themeDark
		if m.light {
			theme = themeLight
		}
	}
	m.applicationSetting(c.Part("color-theme"), "颜色主题",
		"编辑器和支持主题切换的预览会使用此配色。", func() {
			ui.Row(c.Part("theme-options")).Gap(5).Items(ui.Center).Children(func() {
				for _, option := range []struct{ id, label string }{
					{themeDark, "深色"}, {themeLight, "浅色"}, {themeSystem, "跟随系统"},
				} {
					button := ui.Button(c.Key(option.id), option.label).Ghost().MinWidth(58).Height(34)
					if option.id == theme {
						button.Secondary()
					}
					if button.Clicked() {
						c.After(func() {
							m.appSettings.draft.Theme = option.id
							m.saveApplicationSettings()
						})
					}
				}
			})
		})
}

func (m *editorApp) applicationSetting(c ui.C, title, description string, control func()) {
	ui.Row(c).FillWidth().PadX(14).PadY(8).Gap(16).Items(ui.Center).
		Radius(12).Border(1, m.theme.Border).Background(m.theme.Background).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
			ui.Text(c, title).FontSize(14).Bold()
			ui.Wrap(c, description).FillWidth().FontSize(12).Color(m.theme.MutedForeground)
		})
		control()
	})
}
