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
	settingsStorage    = "存储管理"
	settingsShortcuts  = "快捷键"
	settingsAI         = "AI"
	settingsUpdate     = "更新"
	settingsAbout      = "关于"
)

var settingSections = []struct {
	label, topics string
	icon          gfx.Icon
}{
	{settingsAppearance, "界面语言、简体中文、English、颜色主题、跟随系统、深浅配色、托盘、关闭窗口、退出程序、询问保存、不再询问", themeIcon},
	{settingsEditor, "字体、字号、Tab 缩进、自动折行、JSON、输入时自动补全", codeIcon},
	{settingsSidebar, "左侧、右侧、显示按钮、设置按钮、模板库、本地词库、AI 翻译、密码本", leftPanelIcon},
	{settingsStorage, "应用数据、数据目录、存储位置、文件夹、迁移、本地历史、历史版本、保存天数、版本数、占用空间、缓存、清理", folderIcon},
	{settingsShortcuts, "文件、切换标签、查找替换、Ctrl、F3、自动折行、IntelliJ IDEA、VS Code、补全", keyboardIcon},
	{settingsAI, "服务商、模型配置、对话字号、AI 助手", aiIcon},
	{settingsUpdate, "检查更新、自动检查更新、更新日志、版本历史、下载源、GitHub、CNB", updateAvailableIcon},
	{settingsAbout, "EditorX、版本、构建、运行平台、运行时、导入配置、导出配置、恢复默认配置、JSON、调试日志、Windows 打开方式、GitHub", infoIcon},
}

type settingsPageState struct {
	open, returnToAI, returnToGit   bool
	aboutCopied, openWithRegistered bool
	section, problem                string
	transferNotice                  string
	importPreferences               *applicationPreferences
	restoreDefaults                 bool
	mirrorField                     ui.Field
	mirrorSaved                     string
	mirrorEdit                      bool
	debugNotice                     string
	releases                        editorReleaseHistory
	original, draft                 applicationPreferences
	query                           ui.Field
	editorPreview                   *editorPane
	fonts                           []string
	fontRequest                     *ui.EffectState[[]string, int]
	wrapTypesOpen                   bool
	shortcutEdit                    shortcutEditState
	history                         localHistoryPage
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
			settings.problem = uiText("无法读取设置：") + err.Error()
		}
		m.fileMenu, m.languageMenu = false, false
	}
	if settings.section == settingsAI {
		m.pauseAISettings()
	}
	settings.shortcutEdit = shortcutEditState{}
	settings.aboutCopied = false
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
	returnToAI, returnToGit := m.appSettings.returnToAI, m.appSettings.returnToGit
	m.update.open = false
	m.pauseAISettings()
	if !m.ai.settingsEditing || !m.ai.settings.dirty() {
		m.ai.settings, m.ai.settingsEditing = aiSettingsEditor{}, false
	}
	m.appSettings = settingsPageState{}
	if returnToGit {
		m.showProjectGitPanel()
		m.tools.git.focusCommit = true
	} else if returnToAI && m.ai.open {
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
	searchSettings := query != "" && !aiDetail && settings.section != settingsShortcuts
	if searchSettings {
		title = uiText("搜索结果")
	}
	if aiDetail {
		editor.updateProbe(c.Part("model-probe"))
		title = uiText("编辑模型配置")
		if editor.original.ID == 0 {
			title = uiText("新增模型配置")
		}
		if editor.removePending {
			title = uiText("删除模型配置？")
		}
	}
	if !m.update.open && !settings.history.confirm && settings.importPreferences == nil && ui.Shortcut(c, 0, "escape") {
		if settings.shortcutEdit.action != shortcutNone {
			settings.shortcutEdit = shortcutEditState{}
		} else {
			c.After(m.closeSettings)
		}
	}
	ui.Column(c).Grow(1).MinWidth(0).FillHeight().Pad(10).Gap(8).Children(func() {
		ui.Row(c.Part("heading")).FillWidth().Height(48).PadX(12).Gap(10).Items(ui.Center).Children(func() {
			if settings.returnToGit && ui.Button(c.Part("back-to-git"), uiText("返回 Git 提交")).
				Icon(chevronLeftIcon).Ghost().Height(34).Clicked() {
				c.After(m.closeSettings)
			}
			ui.Icon(c, settingsIcon).Size(19, 19).Color(m.theme.MutedForeground)
			ui.Text(c, m.menuText("设置")).FontSize(20).Bold().Grow(1)
			if ui.Button(c.Part("close-settings-page"), m.menuText("关闭设置页")).Icon(closeIcon).IconOnly().
				Ghost().Size(34, 34).Tooltip(m.menuText("关闭设置") + " · Esc").Clicked() {
				c.After(m.closeSettings)
			}
		})
		ui.Row(c.Part("layout")).Grow(1).Basis(0).FillWidth().MinHeight(0).Gap(8).Children(func() {
			m.workspacePanel(c.Part("navigation")).Width(164).Shrink(0).Pad(8).Gap(4).Children(func() {
				for _, section := range settingSections {
					label := m.menuText(section.label)
					row := ui.Row(c.Part("category").Key(section.label)).FillWidth().Height(38).
						PadX(12).Gap(9).Items(ui.Center).Radius(10)
					selected := section.label == settings.section
					if selected {
						row.Background(m.theme.Secondary)
					}
					row.HitWith(ui.ID(c), label, gfx.HitOptions{Role: gfx.RoleButton, Selected: selected}).
						Hover(ui.Look{}.Background(m.theme.Hover)).Focus(ui.Look{}.Background(m.theme.Hover))
					row.Children(func() {
						ui.Icon(c, section.icon).Size(16, 16).Color(m.theme.MutedForeground)
						ui.Text(c, label).FontSize(14).Grow(1)
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
						label := m.menuText("搜索设置")
						if settings.section == settingsShortcuts {
							label = uiText("搜索快捷键")
						}
						ui.TextFieldBase(c.Part("search"), &settings.query, label, label).Grow(1).Height(32)
					})
				}
				if aiDetail {
					ui.Row(c.Part("ai-detail-heading")).FillWidth().MaxWidth(1012).Height(38).Gap(8).Items(ui.Center).Children(func() {
						if ui.Button(c.Part("profiles"), uiText("模型配置")).Icon(chevronLeftIcon).IconOnly().Ghost().Size(32, 32).Tooltip(uiText("返回模型配置")).Clicked() {
							c.After(m.backToAIProfiles)
						}
						ui.Text(c, m.menuText(title)).FontSize(21).Bold().Grow(1).MinWidth(0).Ellipsis()
						m.aiSettingsActions(c.Part("profile-actions"))
					})
				} else {
					ui.Text(c, m.menuText(title)).FontSize(21).Bold().Height(30)
				}
				body := ui.Scroll(c.Part("options").Key(title)).FillWidth().Grow(1).Basis(0).MinHeight(0).Scrollbars()
				if settings.section == settingsAI && editor.scrollTop {
					body.ScrollState().ScrollTo(0)
					editor.scrollTop = false
				}
				submitted := false
				body.Children(func() {
					ui.Column(c.Part("settings-options")).FillWidth().MaxWidth(1020).PadRight(8).Gap(12).Children(func() {
						if searchSettings {
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
						case settingsStorage:
							m.localHistorySettings(c)
						case settingsShortcuts:
							m.shortcutSettings(c, query)
						case settingsAI:
							m.aiSettingsContent(c, &submitted)
						case settingsUpdate:
							m.updateSettings(c)
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
	m.configurationImportDialog(c.Part("configuration-import"))
}

func (m *editorApp) settingsSearchResults(c ui.C, query string) {
	found := false
	for _, section := range settingSections {
		label := m.menuText(section.label)
		topics := m.menuText(section.topics)
		shortcutSearch := section.label == settingsShortcuts && m.hasMatchingShortcut(query)
		if !shortcutSearch && !strings.Contains(strings.ToLower(label+" "+topics+" "+section.label+" "+section.topics), query) {
			continue
		}
		found = true
		m.applicationSetting(c.Key(section.label), label, topics, func() {
			if ui.Button(c, m.menuText("打开 ")+label).Ghost().Clicked() {
				c.After(func() {
					if !shortcutSearch {
						m.appSettings.query.SetText("")
					}
					m.showSettings(section.label)
				})
			}
		})
	}
	if !found {
		ui.Text(c, m.menuText("没有匹配的设置")).Muted().FontSize(13)
	}
}

func (m *editorApp) windowSettings(c ui.C) {
	draft := &m.appSettings.draft
	if m.applicationSettingSwitch(c.Part("show-tray"), m.menuText("显示系统托盘图标"),
		m.menuText("点击托盘图标恢复窗口，右键可打开设置或退出。"), &draft.ShowTrayIcon) {
		c.After(m.saveApplicationSettings)
	}
	if m.applicationSettingSwitch(c.Part("close-action"), m.menuText("关闭最后一个窗口时退出程序"),
		m.menuText("关闭此项后，关闭最后一个窗口会隐藏到后台。再次打开 EditorX 可以唤回窗口。"), &draft.ExitOnClose) {
		c.After(m.saveApplicationSettings)
	}
	ask := !draft.SkipSaveConfirmation
	if m.applicationSettingSwitch(c.Part("save-confirmation"), m.menuText("关闭标签时询问保存"),
		m.menuText("关闭询问后，关闭有修改的标签会自动保存。未命名文件会先选择保存位置。"), &ask) {
		draft.SkipSaveConfirmation = !ask
		c.After(m.saveApplicationSettings)
	}
	askOnExit := !draft.SkipExitSaveConfirmation
	if m.applicationSettingSwitch(c.Part("exit-save-confirmation"), m.menuText("退出时询问保存"),
		m.menuText("关闭询问后，退出时会保留未保存的草稿，下次打开时可恢复。"), &askOnExit) {
		draft.SkipExitSaveConfirmation = !askOnExit
		c.After(m.saveApplicationSettings)
	}
}

func (m *editorApp) appearanceSettings(c ui.C) {
	draft := &m.appSettings.draft
	language := draft.MenuLanguage
	label := uiText("界面语言")
	m.applicationSetting(c.Part("interface-language"), label, uiText("即时切换整个应用的显示语言。"), func() {
		ui.Select(c, &language, menuLanguages[:], menuLanguage.name).Label(label).Width(142).Height(32)
	})
	if language != draft.MenuLanguage {
		c.After(func() {
			draft.MenuLanguage = language
			m.saveApplicationSettings()
		})
	}
	theme := m.appSettings.draft.Theme
	m.applicationSetting(c.Part("color-theme"), m.menuText("颜色主题"),
		m.menuText("编辑器和支持主题切换的预览会使用此配色。跟随系统时会随 Windows 自动切换。"), func() {
			ui.Row(c.Part("theme-options")).Gap(5).Items(ui.Center).Children(func() {
				for _, option := range themeOptions {
					button := ui.Button(c.Key(option.id), m.menuText(option.label)).Ghost().MinWidth(58).Height(34)
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
	title = uiText(title)
	var changed bool
	ui.Row(c).FillWidth().Height(30).PadX(4).Gap(8).Items(ui.Center).Children(func() {
		label := ui.Text(c, title).Grow(1).MinWidth(0).Ellipsis().FontSize(13)
		control := ui.Switch(c, enabled, "").Label(title).Shrink(0)
		label.LabelFor(control.N().ID)
		changed = control.Changed()
	})
	return changed
}
