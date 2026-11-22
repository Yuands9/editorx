package main

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

type aiModelPickerState struct {
	profile     aiSettings
	model       string
	lists       map[int]*aiModelList
	collapsed   map[string]bool
	manualID    int
	manualModel ui.Field
	focusInput  bool
	reasoning   bool
	resetScroll bool
	lastQuery   string
}

type aiModelList struct {
	profile aiSettings
	models  []string
	request *ui.EffectState[[]string, int]
	problem string
	loaded  bool
}

func (p *aiModelPickerState) list(profile aiSettings) *aiModelList {
	if p.lists == nil {
		p.lists = make(map[int]*aiModelList)
	}
	list := p.lists[profile.ID]
	if list == nil || !reflect.DeepEqual(list.profile, profile) {
		list = &aiModelList{profile: profile}
		p.lists[profile.ID] = list
	}
	return list
}

func (a *aiAssistant) selectedModel(profile aiSettings) string {
	if a.modelPicker.model != "" && reflect.DeepEqual(a.modelPicker.profile, profile) {
		return a.modelPicker.model
	}
	return profile.Model
}

func (a *aiAssistant) selectModel(profile aiSettings, model string) {
	picker := &a.modelPicker
	picker.profile, picker.model = profile, ""
	if model != profile.Model {
		picker.model = model
	}
	a.profileID, a.profileMenu, a.focusPrompt, a.message = profile.ID, false, true, ""
}

func (m *editorApp) aiProfilePicker(c ui.C, blocked bool) {
	a := &m.ai
	profile := a.profiles.active(a.profileID)
	picker := &a.modelPicker
	if !reflect.DeepEqual(picker.profile, profile) {
		picker.profile, picker.model = profile, ""
	}
	if blocked {
		a.profileMenu = false
	}
	m.updateAIModelLists(c.Part("model-requests"))

	model := profile.modelLabel(a.selectedModel(profile))
	if profile.ID != 0 {
		a.effort = profile.effectiveReasoning(a.effort)
	}
	label, tooltip := "切换模型配置", "切换模型 · "+model+" · "+profile.Name+" · 思考 "+aiReasoningLabel(a.effort)
	if profile.ID == 0 {
		label, tooltip, model = "选择模型", "添加模型配置", "选择模型"
	}
	button := m.aiPickerPill(c, label, tooltip, blocked).PadX(5).MinWidth(0).MaxWidth(218).Clip()
	button.Children(func() {
		if profile.ID != 0 {
			m.aiProviderMark(c, profile.Provider, 16)
		}
		ui.Text(c, model).FontSize(12).Grow(1).MinWidth(0).Ellipsis()
		ui.Icon(c, chevronDownIcon).Size(10, 10).Shrink(0).Color(m.theme.MutedForeground)
	})
	if button.Clicked() {
		if profile.ID == 0 {
			c.After(m.openAISettings)
		} else {
			a.profileMenu = !a.profileMenu
			a.profileSearch.SetText("")
			picker.manualID, picker.reasoning, picker.resetScroll = 0, false, true
			if a.profileMenu {
				c.After(m.fetchVisibleCLIModels)
			}
		}
	}
	ui.PopoverWith(c, button, ui.PopoverSpec{
		Side: ui.PopoverTop, Align: ui.End, Width: min(382, c.Bounds().Width-20),
		Offset: 8, Padding: 9, Radius: 14, Instant: true,
	}, &a.profileMenu, func() {
		if picker.manualID != 0 {
			if index := a.profiles.index(picker.manualID); index >= 0 {
				m.aiManualModel(c.Part("manual"), a.profiles.Profiles[index])
				return
			}
		}
		if picker.reasoning {
			m.aiReasoningChoices(c.Part("reasoning"), profile)
			return
		}
		m.aiModelInput(c.Part("model-search"), &a.profileSearch, "搜索模型", "搜索模型、配置或服务商").Icon(searchIcon)
		m.aiModelOptions(c.Part("model-options"), profile)
		ui.Slot(c).FillWidth().Height(1).Background(m.theme.Border)
		ui.Row(c.Part("footer")).FillWidth().Gap(5).Items(ui.Center).Children(func() {
			row := ui.Row(c.Part("reasoning-link")).Grow(1).MinWidth(0).Height(33).PadX(8).Gap(7).Radius(8).Items(ui.Center)
			row.HitWith(ui.ID(c.Part("reasoning-link")), "思考水平", gfx.HitOptions{
				Role: gfx.RoleButton, Value: aiReasoningLabel(a.effort),
			}).Hover(ui.Look{}.Background(m.theme.Hover)).Focus(ui.Look{}.Ring(1, m.theme.Ring))
			row.Children(func() {
				ui.Icon(c, chevronRightIcon).Size(11, 11).Color(m.theme.MutedForeground).Shrink(0)
				ui.Text(c, "思考水平").FontSize(12).Grow(1).MinWidth(0).Ellipsis()
				ui.Text(c, aiReasoningLabel(a.effort)).FontSize(11).Muted()
			})
			if row.Clicked() {
				c.After(func() { picker.reasoning = true })
			}
			if ui.Button(c.Part("settings"), "模型设置").Ghost().Height(32).FontSize(12).Clicked() {
				c.After(m.openAISettings)
			}
		})
	})
}

func (m *editorApp) aiModelInput(c ui.C, field *ui.Field, label, placeholder string) ui.Box {
	return ui.TextFieldBase(c, field, label, placeholder).
		FillWidth().Height(34).PadX(9).Radius(9).Background(m.theme.Background).Border(1, m.theme.Border)
}

func (m *editorApp) aiReasoningChoices(c ui.C, profile aiSettings) {
	if ui.Button(c.Part("back"), "返回模型列表").Icon(chevronLeftIcon).Ghost().Height(32).FontSize(12).Clicked() {
		c.After(func() { m.ai.modelPicker.reasoning = false })
	}
	ui.Slot(c).FillWidth().Height(1).Background(m.theme.Border)
	for index, level := range profile.reasoningLevels() {
		label := aiReasoningLabel(level)
		selected := m.ai.effort == level
		place := c.KeyInt(index)
		row := ui.Row(place).FillWidth().Height(32).PadX(9).Gap(8).Radius(8).Items(ui.Center)
		row.HitWith(ui.ID(place), label, gfx.HitOptions{Role: gfx.RoleRadio, Checked: selected, Description: "思考水平"}).
			Hover(ui.Look{}.Background(m.theme.Hover)).Focus(ui.Look{}.Ring(1, m.theme.Ring))
		if selected {
			row.Background(m.theme.Selection)
		}
		row.Children(func() {
			ui.Text(place, label).FontSize(13).Grow(1)
			if selected {
				ui.Icon(place, aiSelectedIcon).Size(14, 14).Color(m.theme.Ring)
			}
		})
		if row.Clicked() {
			c.After(func() {
				m.ai.effort = level
				m.ai.modelPicker.reasoning = false
			})
		}
	}
}

func (m *editorApp) updateAIModelLists(c ui.C) {
	a := &m.ai
	for id, list := range a.modelPicker.lists {
		index := a.profiles.index(id)
		if index < 0 || !reflect.DeepEqual(a.profiles.Profiles[index], list.profile) {
			delete(a.modelPicker.lists, id)
			continue
		}
		if !a.profileMenu {
			list.request = nil
		}
		if list.request == nil {
			continue
		}
		ui.Effect(c.KeyInt(id), list.request, 0)
		models, err, done := list.request.Take()
		if !done {
			continue
		}
		list.request, list.loaded = nil, true
		if err != nil {
			list.problem = err.Error()
		} else {
			list.models = models
			if len(models) == 0 {
				list.problem = "服务未返回可选模型"
			}
		}
	}
}

func (m *editorApp) fetchVisibleCLIModels() {
	picker := &m.ai.modelPicker
	for _, profile := range m.ai.profiles.Profiles {
		if !profile.usesCLI() || !profile.canListModels() || picker.collapsed[profile.Provider] {
			continue
		}
		list := picker.list(profile)
		if !list.loaded && list.request == nil {
			m.fetchAIModels(profile)
		}
	}
}

func (m *editorApp) fetchAIModels(profile aiSettings) {
	a := &m.ai
	list := a.modelPicker.list(profile)
	if err := m.reloadAIProfiles(); err != nil {
		list.problem = err.Error()
		return
	}
	index := a.profiles.index(profile.ID)
	if index < 0 || !reflect.DeepEqual(a.profiles.Profiles[index], profile) {
		return
	}
	list.problem = ""
	profile.cliDirectory = m.dataDirectory
	list.request = ui.NewEffect[[]string, int](func(ctx context.Context) ([]string, error) {
		ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
		defer cancel()
		return profile.models(ctx)
	})
}

func (m *editorApp) aiManualModel(c ui.C, profile aiSettings) {
	a := &m.ai
	picker := &a.modelPicker
	ui.Text(c, "模型 ID · "+profile.Name).FillWidth().Ellipsis().FontSize(12)
	input := m.aiModelInput(c.Part("manual-model"), &picker.manualModel, "手动输入模型 ID", "输入模型 ID")
	if picker.focusInput {
		input.AutoFocus()
		picker.focusInput = false
	}
	model := strings.TrimSpace(picker.manualModel.Text())
	_, composing := picker.manualModel.GetEditor().Composition()
	canSelect := validAIModelID(model) && !composing
	submitted := input.Submitted()
	ui.Row(c).FillWidth().Items(ui.Center).Justify(ui.Between).Children(func() {
		if ui.Button(c, "返回列表").Ghost().Height(30).FontSize(12).Clicked() {
			c.After(func() { picker.manualID = 0 })
		}
		use := ui.Button(c, "使用此模型").Height(30).FontSize(12).Disabled(!canSelect)
		if canSelect && (submitted || use.Clicked()) {
			c.After(func() { a.selectModel(profile, model) })
		}
	})
}

type aiModelChoices struct {
	profile aiSettings
	list    *aiModelList
	models  []string
}

func (m *editorApp) aiModelOptions(c ui.C, active aiSettings) {
	a := &m.ai
	picker := &a.modelPicker
	needle := strings.ToLower(strings.TrimSpace(a.profileSearch.Text()))
	groups := make(map[string][]aiModelChoices)
	var providers []string
	height := float32(0)
	for _, profile := range a.profiles.Profiles {
		list := picker.list(profile)
		models := []string{profile.Model}
		for _, model := range list.models {
			if model != profile.Model {
				models = append(models, model)
			}
		}
		if profile.ID == active.ID && picker.model != "" && !slices.Contains(models, picker.model) {
			models = append(models, picker.model)
		}
		models = slices.DeleteFunc(models, func(model string) bool {
			return !matchesAIModel(needle, profile, model)
		})
		if len(models) == 0 {
			continue
		}
		if len(groups[profile.Provider]) == 0 {
			providers = append(providers, profile.Provider)
			height += 40
		} else {
			height += 21
		}
		groups[profile.Provider] = append(groups[profile.Provider], aiModelChoices{profile: profile, models: models, list: list})
		if needle != "" || !picker.collapsed[profile.Provider] {
			height += 32 * float32(len(models))
			if needle == "" {
				height += 34
			}
			if list.problem != "" {
				height += 30
			}
		}
	}
	if len(providers) == 0 {
		ui.Text(c, "没有匹配的模型").Pad(10).Muted().FontSize(12)
		return
	}
	maxHeight := max(132, min(330, c.Bounds().Height-195))
	scroll := ui.Scroll(c).FillWidth().Height(min(maxHeight, max(50, height))).Scrollbars()
	if picker.resetScroll || needle != picker.lastQuery {
		scroll.ScrollState().ScrollTo(0)
		picker.resetScroll, picker.lastQuery = false, needle
	}
	selected := a.selectedModel(active)
	scroll.Children(func() {
		ui.Column(c).FillWidth().PadRight(4).Gap(3).Children(func() {
			for index, provider := range providers {
				if index > 0 {
					ui.Slot(c).FillWidth().Height(1).Background(m.theme.Border)
				}
				ui.Column(c.Key(provider)).FillWidth().Gap(2).Children(func() {
					choices := groups[provider]
					expanded := needle != "" || !picker.collapsed[provider]
					m.aiModelGroupHeading(c.Part("heading"), provider, choices, expanded, needle != "")
					if !expanded {
						return
					}
					ui.Row(c.Part("group")).FillWidth().PadLeft(20).PadBottom(3).Gap(5).Children(func() {
						ui.Slot(c).Width(1).FillHeight().Background(m.theme.Border)
						ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
							for _, choice := range choices {
								ui.Column(c.KeyInt(choice.profile.ID)).FillWidth().Gap(1).Children(func() {
									if len(choices) > 1 {
										ui.Text(c, choice.profile.Name).PadX(8).Height(21).Muted().FontSize(11).Ellipsis()
									}
									for _, model := range choice.models {
										m.aiModelOption(c.Key(model), choice.profile, model, choice.profile.ID == active.ID && model == selected)
									}
									if needle == "" {
										m.aiModelGroupActions(c.Part("actions"), choice)
									}
								})
							}
						})
					})
				})
			}
		})
	})
}

func (m *editorApp) aiModelGroupHeading(c ui.C, provider string, choices []aiModelChoices, expanded, filtering bool) {
	label := aiProviderLabel(provider)
	heading := ui.Row(c).FillWidth().Height(34).PadX(7).Gap(8).Items(ui.Center).Radius(8)
	heading.HitWith(ui.ID(c), label+" 模型", gfx.HitOptions{Role: gfx.RoleButton, Expandable: true, Expanded: expanded, Disabled: filtering}).
		Hover(ui.Look{}.Background(m.theme.Hover)).Focus(ui.Look{}.Ring(1, m.theme.Ring))
	heading.Children(func() {
		arrow := chevronRightIcon
		if expanded {
			arrow = chevronDownIcon
		}
		ui.Icon(c, arrow).Size(12, 12).Color(m.theme.MutedForeground).Shrink(0)
		m.aiProviderMark(c, provider, 18)
		ui.Text(c, label).Bold().FontSize(13).Grow(1).MinWidth(0).Ellipsis()
		if slices.ContainsFunc(choices, func(choice aiModelChoices) bool { return choice.profile.ID == m.ai.profiles.DefaultID }) {
			ui.Text(c, "默认").Muted().FontSize(11)
		}
	})
	if heading.Clicked() {
		c.After(func() {
			picker := &m.ai.modelPicker
			if picker.collapsed == nil {
				picker.collapsed = make(map[string]bool)
			}
			picker.collapsed[provider] = expanded
			if !expanded {
				m.fetchVisibleCLIModels()
			}
		})
	}
}

func (m *editorApp) aiModelGroupActions(c ui.C, choice aiModelChoices) {
	picker := &m.ai.modelPicker
	ui.Row(c).FillWidth().Height(31).Gap(3).Items(ui.Center).Children(func() {
		if ui.Button(c.Part("manual"), "手动输入 ID").Icon(modelEditIcon).Ghost().Height(29).FontSize(12).
			Tooltip("手动输入 " + choice.profile.Name + " 的模型 ID").Clicked() {
			c.After(func() {
				picker.manualID, picker.focusInput = choice.profile.ID, true
				picker.manualModel.SetText("")
			})
		}
		ui.Slot(c).Grow(1).MinWidth(0)
		if choice.profile.canListModels() {
			label := "获取模型"
			if choice.list.request != nil {
				label = "获取中…"
			}
			if ui.Button(c.Part("fetch"), label).Icon(refreshIcon).Ghost().Height(29).FontSize(11).
				Tooltip("从 " + choice.profile.Name + " 获取模型").Disabled(choice.list.request != nil).Clicked() {
				c.After(func() { m.fetchAIModels(choice.profile) })
			}
		}
	})
	if choice.list.problem != "" {
		ui.Wrap(c, choice.list.problem).FillWidth().PadX(8).MaxHeight(38).Clip().FontSize(11).Muted()
	}
}

func matchesAIModel(needle string, profile aiSettings, model string) bool {
	return strings.Contains(strings.ToLower(aiPickerModelName(profile, model)+" "+model+" "+profile.Name+" "+aiProviderLabel(profile.Provider)), needle)
}

func aiPickerModelName(profile aiSettings, model string) string {
	if name, ok := strings.CutPrefix(model, "gpt-"); ok {
		return "GPT-" + name
	}
	if profile.Provider == "deepseek" {
		if name, ok := strings.CutPrefix(model, "deepseek-"); ok {
			return "DeepSeek " + strings.ReplaceAll(name, "-", " ")
		}
	}
	return profile.modelLabel(model)
}

func (m *editorApp) aiModelOption(c ui.C, choice aiSettings, model string, selected bool) {
	label := "选择配置 " + choice.Name
	if model != choice.Model {
		label = "选择模型 " + model
	}
	name, detail := aiPickerModelName(choice, model), model
	if model == "" {
		detail = "default"
	}
	id := ui.ID(c)
	row := ui.Row(c).FillWidth().Height(31).PadX(8).Gap(7).Items(ui.Center).Radius(8)
	row.HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton, Selected: selected}).
		Hover(ui.Look{}.Background(m.theme.Hover)).Focus(ui.Look{}.Ring(1, m.theme.Ring)).Tooltip(choice.Name + " · " + detail)
	if selected {
		row.Background(m.theme.Ring.WithAlpha(.20).Over(m.theme.Popover))
	}
	row.Children(func() {
		if detail == name {
			ui.Text(c, name).FontSize(13).Grow(1).MinWidth(0).Ellipsis()
		} else {
			ui.Text(c, name).FontSize(13).MaxWidth(155).MinWidth(0).Ellipsis()
			ui.Text(c, detail).FontSize(11).Muted().Grow(1).MinWidth(0).Ellipsis()
		}
		if selected {
			ui.Icon(c, aiSelectedIcon).Size(15, 15).Color(m.theme.Ring).Shrink(0)
		}
	})
	if row.Clicked() {
		c.After(func() { m.ai.selectModel(choice, model) })
	}
}
