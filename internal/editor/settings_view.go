package editor

import (
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

const (
	settingsAppearance = "外观"
	settingsEditor     = "编辑器"
	settingsSidebar    = "侧栏"
	settingsBackup     = "备份"
	settingsShortcuts  = "快捷键"
	settingsAI         = "AI"
	settingsAbout      = "关于"
)

var settingSections = []struct {
	label, topics string
	icon          gfx.Icon
}{
	{settingsAppearance, "颜色主题、跟随系统、深浅配色、托盘、关闭窗口、询问保存、不再询问", themeIcon},
	{settingsEditor, "字体、字号、Tab 缩进、自动折行、JSON", codeIcon},
	{settingsSidebar, "左侧、右侧、显示按钮、模板库、本地词库、AI 翻译、密码本", leftPanelIcon},
	{settingsBackup, "导出、恢复、会话草稿、压缩备份", folderIcon},
	{settingsShortcuts, "文件、切换标签、查找替换、Ctrl、F3、自动折行", keyboardIcon},
	{settingsAI, "服务商、模型配置、对话字号、AI 助手", aiIcon},
	{settingsAbout, "EditorX、版本、自动检查更新、GitHub", infoIcon},
}

type settingsPageState struct {
	open, returnToAI bool
	section, problem string
	original, draft  applicationPreferences
	query            ui.Field
	editorPreview    *editorPane
	fonts            []string
	fontRequest      *ui.EffectState[[]string, int]
	wrapTypesOpen    bool
	shortcutEdit     shortcutEditState
}

func (m *editorApp) openApplicationSettings() {
	m.showSettings(settingsAppearance)
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
		m.leavePasswordVault()
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
	if settings.section == settingsAI {
		m.pauseAISettings()
	}
	settings.shortcutEdit = shortcutEditState{}
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
	m.update.open = false
	m.backup.selecting, m.backup.choices = false, nil
	m.pauseAISettings()
	if !m.ai.settingsEditing || !m.ai.settings.dirty() {
		m.ai.settings, m.ai.settingsEditing = aiSettingsEditor{}, false
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
	query := strings.ToLower(strings.TrimSpace(settings.query.Text()))
	if query != "" && !aiDetail {
		title = "搜索结果"
	}
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
	if !m.update.open && ui.Shortcut(c, 0, "escape") {
		if settings.shortcutEdit.action != shortcutNone {
			settings.shortcutEdit = shortcutEditState{}
		} else {
			c.After(m.closeSettings)
		}
	}
	ui.Column(c).Grow(1).MinWidth(0).FillHeight().Pad(10).Gap(8).Children(func() {
		ui.Row(c.Part("heading")).FillWidth().Height(48).PadX(12).Gap(10).Items(ui.Center).Children(func() {
			ui.Icon(c, settingsIcon).Size(19, 19).Color(m.theme.MutedForeground)
			ui.Text(c, "设置").FontSize(20).Bold().Grow(1)
			if ui.Button(c.Part("close-settings-page"), "关闭设置页").Icon(closeIcon).IconOnly().
				Ghost().Size(34, 34).Tooltip("关闭设置 · Esc").Clicked() {
				c.After(m.closeSettings)
			}
		})
		ui.Row(c.Part("layout")).Grow(1).Basis(0).FillWidth().MinHeight(0).Gap(8).Children(func() {
			m.workspacePanel(c.Part("navigation")).Width(164).Shrink(0).Pad(8).Gap(4).Children(func() {
				for _, section := range settingSections {
					row := ui.Row(c.Part("category").Key(section.label)).FillWidth().Height(38).
						PadX(12).Gap(9).Items(ui.Center).Radius(10)
					selected := section.label == settings.section
					if selected {
						row.Background(m.theme.Secondary)
					}
					row.HitWith(ui.ID(c), section.label, gfx.HitOptions{Role: gfx.RoleButton, Selected: selected}).
						Hover(ui.Look{}.Background(m.theme.Hover)).Focus(ui.Look{}.Background(m.theme.Hover))
					row.Children(func() {
						ui.Icon(c, section.icon).Size(16, 16).Color(m.theme.MutedForeground)
						ui.Text(c, section.label).FontSize(14).Grow(1)
					})
					if row.Clicked() {
						c.After(func() {
							settings.query.SetText("")
							m.showSettings(section.label)
						})
					}
				}
			})
			m.workspacePanel(c.Part("content")).Grow(1).Pad(16).Gap(10).Children(func() {
				if !aiDetail {
					ui.Row(c.Part("search-bar")).FillWidth().MaxWidth(1020).Height(36).PadX(10).Gap(5).
						Items(ui.Center).Radius(10).Background(m.theme.Card).Children(func() {
						ui.Icon(c, searchIcon).Size(16, 16).Color(m.theme.MutedForeground)
						ui.TextFieldBase(c.Part("search"), &settings.query, "搜索设置", "搜索设置").Grow(1).Height(32)
					})
				}
				if aiDetail {
					ui.Row(c.Part("ai-detail-heading")).FillWidth().MaxWidth(1012).Height(38).Gap(8).Items(ui.Center).Children(func() {
						if ui.Button(c.Part("profiles"), "模型配置").Icon(chevronLeftIcon).IconOnly().Ghost().Size(32, 32).Tooltip("返回模型配置").Clicked() {
							c.After(m.backToAIProfiles)
						}
						ui.Text(c, title).FontSize(21).Bold().Grow(1).MinWidth(0).Ellipsis()
						m.aiSettingsActions(c.Part("profile-actions"))
					})
				} else {
					ui.Text(c, title).FontSize(21).Bold().Height(30)
				}
				body := ui.Scroll(c.Part("options").Key(title)).FillWidth().Grow(1).Basis(0).MinHeight(0).Scrollbars()
				if settings.section == settingsAI && editor.scrollTop {
					body.ScrollState().ScrollTo(0)
					editor.scrollTop = false
				}
				submitted := false
				body.Children(func() {
					ui.Column(c.Part("settings-options")).FillWidth().MaxWidth(1020).PadRight(8).Gap(12).Children(func() {
						if query != "" && !aiDetail {
							m.settingsSearchResults(c, query)
							return
						}
						switch settings.section {
						case settingsAppearance:
							m.appearanceSettings(c)
							m.windowSettings(c)
						case settingsEditor:
							m.editorSettings(c)
						case settingsSidebar:
							m.sidebarSettings(c)
						case settingsBackup:
							m.backupSettings(c)
						case settingsShortcuts:
							m.shortcutSettings(c)
						case settingsAI:
							m.aiSettingsContent(c, &submitted)
						case settingsAbout:
							m.aboutSettings(c)
						}
					})
				})
				if settings.problem != "" {
					ui.Wrap(c, settings.problem).FillWidth().FontSize(12).Color(m.theme.Danger)
				}
				if submitted {
					c.After(m.finishAIProfile)
				}
			})
		})
	})
}

func (m *editorApp) settingsSearchResults(c ui.C, query string) {
	found := false
	for _, section := range settingSections {
		if !strings.Contains(strings.ToLower(section.label+" "+section.topics), query) {
			continue
		}
		found = true
		m.applicationSetting(c.Key(section.label), section.label, section.topics, func() {
			if ui.Button(c, "打开 "+section.label).Ghost().Clicked() {
				c.After(func() {
					m.appSettings.query.SetText("")
					m.showSettings(section.label)
				})
			}
		})
	}
	if !found {
		ui.Text(c, "没有匹配的设置").Muted().FontSize(13)
	}
}

func (m *editorApp) windowSettings(c ui.C) {
	draft := &m.appSettings.draft
	if m.applicationSettingSwitch(c.Part("show-tray"), "显示系统托盘图标",
		"点击托盘图标恢复窗口，右键可打开设置或退出。", &draft.ShowTrayIcon) {
		c.After(m.saveApplicationSettings)
	}
	if m.applicationSettingSwitch(c.Part("close-action"), "关闭最后一个窗口时退出程序",
		"关闭此项后，关闭最后一个窗口会隐藏到后台。再次打开 EditorX 可以唤回窗口。", &draft.ExitOnClose) {
		c.After(m.saveApplicationSettings)
	}
	ask := !draft.SkipSaveConfirmation
	if m.applicationSettingSwitch(c.Part("save-confirmation"), "关闭标签时询问保存",
		"关闭询问后，关闭有修改的标签会自动保存。未命名文件会先选择保存位置。", &ask) {
		draft.SkipSaveConfirmation = !ask
		c.After(m.saveApplicationSettings)
	}
}

func (m *editorApp) appearanceSettings(c ui.C) {
	theme := m.appSettings.draft.Theme
	m.applicationSetting(c.Part("color-theme"), "颜色主题",
		"编辑器和支持主题切换的预览会使用此配色。跟随系统时会随 Windows 自动切换。", func() {
			ui.Row(c.Part("theme-options")).Gap(5).Items(ui.Center).Children(func() {
				for _, option := range themeOptions {
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

func (m *editorApp) applicationSettingSwitch(c ui.C, title, description string, enabled *bool) bool {
	var changed bool
	m.applicationSetting(c, title, description, func() {
		changed = ui.Switch(c, enabled, "").Label(title).Changed()
	})
	return changed
}

func settingsSwitchRow(c ui.C, title string, enabled *bool) bool {
	var changed bool
	ui.Row(c).FillWidth().Height(30).PadX(4).Gap(8).Items(ui.Center).Children(func() {
		label := ui.Text(c, title).Grow(1).MinWidth(0).Ellipsis().FontSize(13)
		control := ui.Switch(c, enabled, "").Label(title).Shrink(0)
		label.LabelFor(control.N().ID)
		changed = control.Changed()
	})
	return changed
}
