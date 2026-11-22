package main

import (
	"fmt"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

func (m *editorApp) aiSettingsDialog(c ui.C) {
	a, e := &m.ai, &m.ai.settings
	if !a.settingsOpen {
		return
	}
	e.updateProbe(c)
	busy := e.probe != nil
	title, width, height := "AI 设置", float32(850), min(float32(600), c.Bounds().Height-34)
	if a.settingsEditing {
		title, width = "编辑模型配置", 780
		height = min(660, c.Bounds().Height-34)
		if e.original.ID == 0 {
			title = "新增模型配置"
		}
	}
	if e.removePending {
		title, width, height = "删除模型配置？", 460, 232
	}
	submitted := false
	dismissed := ui.ModalWith(c, &a.settingsOpen, title, ui.ModalSpec{
		Width: min(width, c.Bounds().Width-42), Height: height,
		NoClose: busy || a.settingsEditing && e.dirty() || e.removePending,
	}, func() {
		if e.removePending {
			ui.Column(c).FillWidth().Gap(10).Children(func() {
				ui.Wrap(c, "移除「"+e.original.Name+"」及其在本机保存的凭据。").FontSize(14).FillWidth()
				if e.problem != "" {
					ui.Wrap(c, e.problem).Color(m.theme.Danger).FillWidth().FontSize(12)
				}
			})
			return
		}
		content := ui.Scroll(c.Part("settings-content")).FillWidth().Height(height - 110).MinHeight(0).Scrollbars()
		if e.scrollTop {
			content.ScrollState().ScrollTo(0)
			e.scrollTop = false
		}
		content.Children(func() {
			if a.settingsEditing {
				m.aiProfileForm(c.Part("profile-form"), busy, &submitted)
			} else {
				m.aiSettingsOverview(c.Part("profile-overview"))
			}
		})
	}, func() {
		if e.removePending {
			if ui.Button(c, "取消删除").Outline().Clicked() {
				c.After(func() { e.removePending, e.problem = false, "" })
			}
			if ui.Button(c, "确认删除配置").Destructive().Clicked() {
				c.After(m.removeAISettings)
			}
			return
		}
		if !a.settingsEditing {
			if ui.Button(c, "完成").Outline().Clicked() {
				c.After(m.closeAISettings)
			}
			return
		}
		if busy {
			if ui.Button(c, "停止检测").Outline().Clicked() {
				c.After(func() {
					e.probe = nil
					e.message = "已停止检测"
				})
			}
			return
		}
		dirty := e.dirty()
		back := "返回配置列表"
		if dirty {
			back = "放弃修改并返回"
		}
		if ui.Button(c, back).Outline().Clicked() {
			c.After(func() {
				a.settingsEditing = false
				e.load(e.original)
			})
		}
		canSave := dirty || e.original.ID == 0
		save := ui.Button(c, "保存配置").Disabled(!canSave).Tooltip("Enter 保存")
		if canSave && (save.Clicked() || submitted) {
			c.After(func() {
				m.saveAISettings()
				if e.problem == "" {
					a.settingsEditing, e.scrollTop = false, true
				}
			})
		}
	})
	if dismissed {
		m.closeAISettings()
	}
}

func (m *editorApp) aiSettingsOverview(c ui.C) {
	a, e := &m.ai, &m.ai.settings
	ui.Column(c).FillWidth().PadRight(6).Gap(12).Children(func() {
		ui.Row(c).FillWidth().Height(36).Items(ui.Center).Gap(10).Children(func() {
			ui.Text(c, "模型配置").Bold().FontSize(18).Grow(1)
			if ui.Button(c, "新增配置").Icon(plusIcon).Height(32).Clicked() {
				c.After(func() {
					m.editAISettings(aiSettings{Name: a.profiles.availableName("新配置"), Provider: "custom", Protocol: aiChat})
				})
			}
		})
		if len(a.profiles.Profiles) == 0 {
			ui.Wrap(c, "添加服务商配置后，即可问答、起草文档和生成修改。").
				FillWidth().Pad(18).Radius(13).Border(1, m.theme.Border).Color(m.theme.MutedForeground).FontSize(13)
		}
		for _, profile := range a.profiles.Profiles {
			m.aiProfileCard(c.KeyInt(profile.ID), profile)
		}
		if e.problem != "" {
			ui.Wrap(c, e.problem).FillWidth().Color(m.theme.Danger).FontSize(12)
		} else if e.message != "" {
			ui.Wrap(c, e.message).FillWidth().Color(m.theme.Success).FontSize(12)
		}
	})
}

func (m *editorApp) aiProfileCard(c ui.C, profile aiSettings) {
	a, e := &m.ai, &m.ai.settings
	isDefault := profile.ID == a.profiles.active(0).ID
	border, background := m.theme.Border, m.theme.Background
	tint := m.theme.Ring
	tint.Alpha = .14
	if isDefault {
		border, background = m.theme.Ring, tint.Over(background)
	}
	ui.Row(c).FillWidth().Height(66).PadX(12).Gap(10).Items(ui.Center).Radius(13).
		Border(1, border).Background(background).Children(func() {
		ui.Row(c).Size(36, 36).Shrink(0).Pad(9).Radius(10).Items(ui.Center).Background(tint.Over(background)).Children(func() {
			m.aiProviderMark(c, profile.Provider, 18)
		})
		ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
			ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
				ui.Text(c, profile.Name).FontSize(14).Bold().MinWidth(0).Ellipsis()
				if isDefault {
					ui.Text(c, "默认").FontSize(11).Color(m.theme.Ring).PadX(7).PadY(2).Radius(7).
						Background(tint.Over(background)).Shrink(0)
				}
			})
			ui.Text(c, aiProviderLabel(profile.Provider)+" · "+profile.modelLabel(profile.Model)).FontSize(12).Muted().FillWidth().Ellipsis()
		})
		if !isDefault && m.aiProfileAction(c, "设为默认", "设为默认 "+profile.Name, m.theme.MutedForeground) {
			c.After(func() {
				e.load(profile)
				m.setDefaultAISettings()
			})
		}
		if m.aiProfileAction(c, "编辑", "编辑配置 "+profile.Name, m.theme.Foreground) {
			c.After(func() { m.editAISettings(profile) })
		}
		if m.aiProfileAction(c, "删除", "删除配置 "+profile.Name, m.theme.Danger) {
			c.After(func() {
				e.load(profile)
				e.removePending = true
			})
		}
	})
}

func (m *editorApp) aiProfileAction(c ui.C, label, accessibleLabel string, color gfx.Color) bool {
	action := ui.Row(c).Height(30).PadX(9).Radius(8).Items(ui.Center)
	action.HitWith(ui.ID(c), accessibleLabel, gfx.HitOptions{Role: gfx.RoleButton}).
		Hover(ui.Look{}.Background(m.theme.Hover)).Focus(ui.Look{}.Border(m.theme.Ring))
	action.Children(func() { ui.Text(c, label).FontSize(12).Color(color) })
	return action.Clicked()
}

func (m *editorApp) aiChatPreferences(c ui.C, disabled bool) {
	a := &m.ai
	ui.Column(c).FillWidth().Pad(12).Gap(8).Radius(12).Background(m.theme.Background).Children(func() {
		ui.Text(c, "对话偏好").FontSize(14).Bold()
		mode, size := a.profiles.DefaultMode, a.profiles.chatFontSize()
		ui.Row(c).FillWidth().Gap(12).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
				ui.Text(c, "新对话的默认模式").Muted().FontSize(12)
				if ui.Select(c, &mode, aiModes, aiModeLabel).Label("新对话的默认模式").FillWidth().Height(34).Disabled(disabled).Changed() {
					c.After(func() { m.saveAIChatPreferences(mode, size) })
				}
			})
			ui.Column(c).Width(150).Gap(4).Children(func() {
				ui.Text(c, "对话字号").Muted().FontSize(12)
				if ui.Select(c, &size, aiFontSizes, func(value int) string { return fmt.Sprintf("%d px", value) }).
					Label("对话字号").FillWidth().Height(34).Disabled(disabled).Changed() {
					c.After(func() { m.saveAIChatPreferences(mode, size) })
				}
			})
		})
	})
}

func (m *editorApp) aiProviderPicker(c ui.C, disabled bool) {
	a, e := &m.ai, &m.ai.settings
	m.aiSettingsRow(c, "服务商", func() {
		trigger := ui.Row(c).Width(222).Height(36).PadX(10).Gap(7).Items(ui.Center).
			Radius(9).Border(1, m.theme.Border).Background(m.theme.Card)
		trigger.HitWith(ui.ID(c), "服务商预设", gfx.HitOptions{
			Role: gfx.RoleCombobox, Value: aiProviderLabel(e.provider), Expandable: true, Expanded: e.providersOpen, Disabled: disabled,
		}).Hover(ui.Look{}.Background(m.theme.Hover)).Focus(ui.Look{}.Border(m.theme.Ring))
		trigger.Children(func() {
			m.aiProviderMark(c, e.provider, 17)
			ui.Text(c, aiProviderLabel(e.provider)).FontSize(13).Grow(1).MinWidth(0).Ellipsis()
			ui.Icon(c, chevronDownIcon).Size(12, 12).Shrink(0).Color(m.theme.MutedForeground)
		})
		if trigger.Clicked() {
			e.providersOpen = !e.providersOpen
			e.modelsOpen = false
		}
		scope := ui.ID(c.Part("provider-popup"))
		ui.PopoverWith(c, trigger, ui.PopoverSpec{
			Side: ui.PopoverBottom, Align: ui.Start, Width: 270, Offset: 5, Padding: 6, Radius: 11,
			Instant: true, Scope: scope, ConsumeOutsidePress: true,
		}, &e.providersOpen, func() {
			height := min(float32(len(aiPresetIDs)*31+49), max(150, c.Bounds().Height-120))
			list := ui.Scroll(c).FillWidth().Height(height).Scrollbars()
			list.Draw(func(d *ui.DrawContext) { d.Canvas.TrapFocus(scope) })
			list.Children(func() {
				ui.Column(c).FillWidth().PadRight(4).Gap(1).Children(func() {
					ui.Text(c, "API 服务").Muted().FontSize(11).PadX(9).Height(22)
					for _, id := range aiPresetIDs {
						if id == aiClaudeCLI {
							ui.Text(c, "命令行").Muted().FontSize(11).PadX(9).Height(22)
						} else if id == "custom" {
							ui.Slot(c).FillWidth().MarginY(2).Height(1).Background(m.theme.Border)
						}
						label := aiProviderLabel(id)
						selected := label == aiProviderLabel(e.provider)
						option := ui.Row(c.Key(id)).FillWidth().Height(30).PadX(9).Gap(8).Items(ui.Center).Radius(7)
						option.HitWith(ui.ID(c), label, gfx.HitOptions{Role: gfx.RoleOption, Selected: selected}).
							Hover(ui.Look{}.Background(m.theme.Hover)).Focus(ui.Look{}.Ring(1, m.theme.Ring))
						if selected {
							option.Background(m.theme.Selection)
						}
						option.Children(func() {
							m.aiProviderMark(c, id, 17)
							ui.Text(c, label).FontSize(13).Grow(1).MinWidth(0).Ellipsis()
							if selected {
								ui.Icon(c, aiSelectedIcon).Size(14, 14).Color(m.theme.Ring)
							}
						})
						if option.Clicked() {
							c.After(func() {
								e.providersOpen = false
								if !selected {
									e.provider = id
									e.applyPreset(a.profiles)
								}
							})
						}
					}
				})
			})
		})
	})
}

func (m *editorApp) aiProfileForm(c ui.C, disabled bool, submitted *bool) {
	a, e := &m.ai, &m.ai.settings
	profile := aiSettings{Provider: e.provider}
	cli := profile.usesCLI()
	ui.Column(c).FillWidth().PadRight(9).Gap(10).Children(func() {
		m.aiSettingTextField(c.Part("name"), "配置名称", "例如 日常写作", &e.name, disabled, submitted)
		m.aiProviderPicker(c.Part("provider"), disabled)
		modelHint := "手动输入，或从服务商获取"
		if cli {
			modelHint = "留空使用 CLI 默认模型"
			if m.aiSettingTextField(c.Part("cli-path"), "CLI 路径", "留空自动查找已安装的 CLI", &e.cliPath, disabled, submitted) {
				e.clearProbe(true)
			}
		} else {
			m.aiSettingsRow(c.Part("credential"), "API 密钥", func() {
				keyDisabled := disabled || e.auth == aiAuthNone
				placeholder := "输入 API 密钥"
				hasSavedKey := len(e.original.Key) != 0 && e.provider == e.original.Provider
				if hasSavedKey && !e.clearKey && e.key.Secure {
					placeholder = strings.Repeat("•", 24)
				}
				icon, title := showSecretIcon, "显示密钥"
				if !e.key.Secure {
					icon, title = hideSecretIcon, "隐藏密钥"
				}
				key := m.aiSettingsField(c.Part("key-input"), "API 密钥", placeholder, &e.key, keyDisabled, submitted).Action(icon, title)
				if key.Changed() {
					e.clearProbe(true)
				}
				if key.ActionClicked() {
					c.After(e.toggleKeyVisibility)
				}
				if hasSavedKey && ui.Checkbox(c.Part("clear-key"), &e.clearKey, "清除已保存的密钥").FontSize(12).Disabled(disabled).Changed() {
					if e.clearKey && e.viewingSavedKey {
						e.toggleKeyVisibility()
					}
					e.clearProbe(true)
				}
			})
			if m.aiSettingTextField(c.Part("endpoint"), "接口基址", "https://example.com/v1", &e.baseURL, disabled, submitted) {
				e.clearProbe(true)
			}
		}
		m.aiSettingsRow(c.Part("model"), "默认模型", func() {
			ui.Row(c.Part("model-field")).FillWidth().Gap(8).Items(ui.Center).Children(func() {
				if m.aiSettingsField(c.Part("model-id"), "模型 ID", modelHint, &e.model, disabled, submitted).Changed() {
					e.suggestName(a.profiles)
					e.clearProbe(false)
				}
				if profile.canListModels() && ui.Button(c.Part("fetch"), "获取模型").Icon(refreshIcon).Outline().Height(36).FontSize(12).
					Disabled(disabled || !cli && strings.TrimSpace(e.baseURL.Text()) == "").Clicked() {
					c.After(func() { m.startAISettingsProbe(true) })
				}
			})
			ui.Row(c.Part("model-actions")).FillWidth().Items(ui.Center).Gap(8).Children(func() {
				if len(e.models) > 0 {
					picker := ui.Button(c, "选择模型").Icon(chevronDownIcon).Outline().Height(30).FontSize(12).Disabled(disabled)
					if picker.Clicked() {
						e.modelsOpen = !e.modelsOpen
					}
					ui.Menu(c, picker, &e.modelsOpen, func(menu *ui.MenuItems) {
						menu.WidthRange(220, 400)
						for _, model := range e.models {
							if menu.Item(model).Checked(model == e.model.Text()).Clicked() {
								c.After(func() {
									e.model.SetText(model)
									e.suggestName(a.profiles)
									e.clearProbe(false)
								})
							}
						}
					})
				}
				canProbe := cli || strings.TrimSpace(e.baseURL.Text()) != "" && strings.TrimSpace(e.model.Text()) != ""
				if ui.Button(c, "测试连接").Outline().Height(30).FontSize(12).Disabled(disabled || !canProbe).Clicked() {
					c.After(func() { m.startAISettingsProbe(false) })
				}
				if cli {
					ui.Text(c, "使用已登录的 CLI").FontSize(11).Muted().Grow(1).MinWidth(0).Ellipsis()
				}
			})
			message, color := e.message, m.theme.MutedForeground
			if e.passed {
				color = m.theme.Success
			}
			if e.probe != nil {
				message = e.probeLabel + "…"
			}
			if e.problem != "" {
				message, color = e.problem, m.theme.Danger
			}
			if message != "" {
				ui.Wrap(c, message).FontSize(12).Color(color).FillWidth().PadY(3)
			}
		})
		if !cli {
			ui.Slot(c).FillWidth().Height(1).Background(m.theme.Border)
			advancedIcon := chevronRightIcon
			if e.advanced {
				advancedIcon = chevronDownIcon
			}
			if ui.Button(c, "高级设置").Icon(advancedIcon).Ghost().Height(28).FontSize(12).Disabled(disabled).Clicked() {
				e.advanced = !e.advanced
			}
			if e.advanced {
				m.aiAdvancedSettings(c.Part("advanced"), disabled, submitted)
			}
		}
		m.aiChatPreferences(c.Part("chat-preferences"), disabled)
	})
}

func (m *editorApp) aiAdvancedSettings(c ui.C, disabled bool, submitted *bool) {
	e := &m.ai.settings
	m.aiSettingsRow(c.Part("protocol"), "API 协议", func() {
		if ui.Select(c, &e.protocol, aiProtocols, aiProtocolLabel).Label("API 协议").Width(240).Height(34).Disabled(disabled).Changed() {
			e.clearProbe(true)
		}
	})
	m.aiSettingsRow(c.Part("auth"), "认证方式", func() {
		if ui.Select(c, &e.auth, aiAuthOptions, aiAuthLabel).Label("认证方式").Width(240).Height(34).Disabled(disabled).Changed() {
			e.clearProbe(true)
		}
	})
	m.aiSettingsRow(c.Part("network"), "网络", func() {
		if ui.Checkbox(c, &e.direct, "直连 · 不使用环境代理").FontSize(12).Disabled(disabled).Changed() {
			e.clearProbe(true)
		}
	})
	m.aiSettingsRow(c.Part("headers-row"), "自定义请求头", func() {
		placeholder := `{"X-Tenant":"example"}`
		if len(e.original.Headers) != 0 {
			placeholder = "已加密保存，留空则沿用"
		}
		if m.aiSettingsField(c.Part("headers"), "自定义请求头 · JSON", placeholder, &e.headers, disabled, submitted).Changed() {
			e.clearProbe(true)
		}
		if len(e.original.Headers) != 0 && ui.Checkbox(c, &e.clearHeaders, "清除已保存的请求头").FontSize(12).Disabled(disabled).Changed() {
			e.clearProbe(true)
		}
	})
}

func (m *editorApp) aiSettingsRow(c ui.C, label string, control func()) {
	ui.Row(c).FillWidth().Gap(12).Items(ui.Start).Children(func() {
		ui.Text(c, label).Width(142).PadTop(9).FontSize(13).Muted()
		ui.Column(c).Grow(1).MinWidth(0).Gap(5).Children(control)
	})
}

func (m *editorApp) aiSettingTextField(c ui.C, label, placeholder string, field *ui.Field, disabled bool, submitted *bool) bool {
	var changed bool
	m.aiSettingsRow(c, label, func() {
		changed = m.aiSettingsField(c, label, placeholder, field, disabled, submitted).Changed()
	})
	return changed
}

func (m *editorApp) aiSettingsField(c ui.C, label, placeholder string, field *ui.Field, disabled bool, submitted *bool) ui.Box {
	input := ui.TextFieldBase(c, field, label, placeholder).FillWidth().MinWidth(0).Height(36).PadX(11).
		Radius(9).Background(m.theme.Background).Border(1, m.theme.Border).Focus(ui.Look{}.Border(m.theme.Ring)).Disabled(disabled)
	if !disabled && input.Submitted() {
		*submitted = true
	}
	return input
}
