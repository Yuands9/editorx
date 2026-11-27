package editor

import (
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

var themeOptions = []struct{ id, label string }{
	{themeDark, "深色"},
	{themeLight, "浅色"},
	{themeSystem, "跟随系统"},
}

var editorControlStyles = ui.Styles{
	TextEntry: func(theme gfx.Theme, state gfx.State) components.TextEntryLook {
		fill := theme.Muted
		if state.Focused && !state.Disabled {
			fill = theme.Hover
		}
		return components.TextEntryLook{Fill: fill, Radius: theme.Metrics.RadiusControl}
	},
	Checkbox: func(theme gfx.Theme, state gfx.State) components.CheckboxLook {
		theme.Input = theme.MutedForeground
		theme.Input.Alpha = .7
		return components.CheckboxRecipe(theme, state)
	},
	Switch: func(theme gfx.Theme, state gfx.State, progress float32) components.SwitchLook {
		theme.Input = theme.MutedForeground
		return components.SwitchRecipe(theme, state, progress)
	},
}

func settingsExternalLink(c ui.C, label string) ui.Box {
	return ui.Button(c, label).Icon(externalLinkIcon).Link().Height(32).Shrink(0).
		ButtonRecipe(func(theme gfx.Theme, variant components.ButtonVariant, state gfx.State) components.ButtonLook {
			look := components.ButtonRecipe(theme, variant, state)
			look.Foreground, look.Underlined = theme.Ring, true
			return look
		})
}

func (m *editorApp) themeButton(c ui.C, blocked bool) {
	if ui.Button(c, m.menuText("切换颜色主题")).Icon(themeIcon).IconOnly().Ghost().Size(32, 34).
		Tooltip(m.menuText("切换主题 · 深色 / 浅色")).TooltipOn(ui.TooltipLeft).Disabled(blocked).Clicked() {
		c.After(m.cycleTheme)
	}
}

func editorTheme() gfx.Theme {
	theme := gfx.DarkTheme()
	theme.Background = gfx.RGB(24, 24, 24)
	theme.Foreground = gfx.RGB(229, 229, 229)
	theme.Card = gfx.RGB(29, 30, 32)
	theme.CardForeground = theme.Foreground
	theme.CardElevated = gfx.RGB(47, 49, 54)
	theme.Popover = gfx.RGB(38, 39, 41)
	theme.PopoverForeground = theme.Foreground
	theme.Muted = gfx.RGB(35, 36, 38)
	theme.MutedForeground = gfx.RGB(157, 162, 172)
	theme.Border = gfx.RGB(49, 52, 57)
	theme.Hover = gfx.RGB(47, 50, 53)
	theme.Input = gfx.RGBA(24, 24, 24, 0)
	theme.Primary = gfx.RGB(229, 233, 240)
	theme.PrimaryForeground = gfx.RGB(30, 33, 39)
	theme.Secondary = gfx.RGB(55, 57, 60)
	theme.SecondaryForeground = gfx.RGB(242, 242, 242)
	theme.Selection = gfx.RGB(58, 67, 78)
	theme.SelectionForeground = gfx.RGB(247, 247, 247)
	theme.Accent = theme.Hover
	theme.AccentForeground = theme.Foreground
	theme.Ring = gfx.RGB(151, 182, 230)
	theme.Metrics = theme.Metrics.WithDensity(gfx.DensityCompact)
	theme.Metrics.FontSizeM = 14
	theme.Metrics.FontSizeS = 12
	theme.Metrics.RadiusControl = 12
	theme.Metrics.RadiusSurface = 18
	// 编辑器控件直接响应状态变化，不等待颜色过渡。
	theme.Transition = gfx.Motion{}
	return theme
}

func lightEditorTheme() gfx.Theme {
	theme := gfx.LightTheme()
	theme.Background = gfx.RGB(255, 255, 255)
	theme.Foreground = gfx.RGB(38, 44, 53)
	theme.Card = gfx.RGB(245, 246, 248)
	theme.CardForeground = theme.Foreground
	theme.CardElevated = gfx.RGB(232, 235, 240)
	theme.Popover = gfx.RGB(255, 255, 255)
	theme.PopoverForeground = theme.Foreground
	theme.Muted = gfx.RGB(239, 241, 244)
	theme.MutedForeground = gfx.RGB(96, 105, 118)
	theme.Border = gfx.RGB(218, 223, 230)
	theme.Hover = gfx.RGB(230, 235, 242)
	theme.Input = gfx.RGBA(255, 255, 255, 0)
	theme.Primary = gfx.RGB(44, 100, 178)
	theme.PrimaryForeground = gfx.RGB(255, 255, 255)
	theme.Secondary = gfx.RGB(222, 232, 246)
	theme.SecondaryForeground = gfx.RGB(31, 72, 127)
	theme.Selection = gfx.RGB(190, 216, 252)
	theme.SelectionForeground = theme.Foreground
	theme.Accent = theme.Hover
	theme.AccentForeground = theme.Foreground
	theme.Ring = gfx.RGB(45, 99, 172)
	theme.Success = gfx.RGB(30, 112, 68)
	theme.Danger = gfx.RGB(178, 46, 56)
	theme.Metrics = editorTheme().Metrics
	theme.Transition = gfx.Motion{}
	return theme
}

func themeAppearance(theme string) app.Appearance {
	switch theme {
	case themeLight:
		return app.AppearanceLight
	case themeDark:
		return app.AppearanceDark
	default:
		return app.AppearanceUnknown
	}
}

func editorThemeIsLight(theme string) bool {
	if theme == themeSystem {
		return app.SystemAppearance() == app.AppearanceLight
	}
	return theme == themeLight
}

func (m *editorApp) applyThemePreference(theme string) {
	if light := editorThemeIsLight(theme); light != m.light {
		m.setLightMode(light)
	}
}

func (d *editorDesktop) refreshSystemTheme() {
	if d.preferences.Theme != themeSystem {
		return
	}
	for _, window := range d.store.windows {
		if !window.windowClosed {
			window.applyThemePreference(themeSystem)
			window.invalidator.Invalidate()
		}
	}
	if d.tray != nil {
		_ = d.syncTray()
	}
}

func (m *editorApp) setLightMode(light bool) {
	m.light = light
	m.theme = editorTheme()
	if light {
		m.theme = lightEditorTheme()
	}
	if m.nativeWindow != nil {
		_ = m.nativeWindow.SetIcon(themedApplicationIcon(light))
	}
	if state := m.historyCompare; state != nil {
		if state.window != nil {
			_ = state.window.SetIcon(themedApplicationIcon(light))
		}
		state.invalidator.Invalidate()
	}
	if state := m.gitLogWindow; state != nil {
		_ = state.window.SetIcon(themedApplicationIcon(light))
		state.invalidator.Invalidate()
	}
	for _, tab := range m.tabs {
		tab.source.light, tab.result.light, tab.comparison.light = light, light, light
		tab.office.setLightMode(light)
		if tab.preview != nil && tab.preview.view != nil {
			tab.preview.view.Eval(previewThemeJS(light))
		}
	}
	for _, shell := range m.tools.terminals {
		if shell.view != nil {
			shell.view.Eval(previewThemeJS(light))
		}
	}
}

func (m *editorApp) pinButtonRecipe(theme gfx.Theme, variant components.ButtonVariant, state gfx.State) components.ButtonLook {
	if variant == components.ButtonSecondary {
		theme.Secondary = gfx.RGB(30, 58, 95)
		theme.SecondaryForeground = gfx.RGB(104, 169, 255)
		if m.light {
			theme.Secondary = gfx.RGB(222, 232, 246)
			theme.SecondaryForeground = gfx.RGB(35, 99, 196)
		}
	} else {
		theme.Foreground = theme.MutedForeground
	}
	return components.ButtonRecipe(theme, variant, state)
}

func groupedTabButtonRecipe(theme gfx.Theme, variant components.ButtonVariant, state gfx.State) components.ButtonLook {
	if variant == components.ButtonSecondary {
		theme.Foreground = theme.Ring
	} else if !state.Hovered && !state.FocusVisible {
		theme.Foreground = theme.MutedForeground
	}
	look := components.ButtonRecipe(theme, components.ButtonGhost, state)
	look.Filled = false
	return look
}

func (m *editorApp) diskWarningColors() (ink, surface gfx.Color) {
	if m.light {
		return gfx.RGB(129, 65, 0), gfx.RGB(255, 244, 216)
	}
	return gfx.RGB(255, 205, 112), gfx.RGB(51, 40, 24)
}
