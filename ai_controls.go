package main

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

var aiAskIcon = gfx.Icon{ViewBox: 24, Stroke: 1.7, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(6, 5.5), gfx.LineTo(18, 5.5), gfx.QuadTo(21, 5.5, 21, 8.5), gfx.LineTo(21, 14),
	gfx.QuadTo(21, 17, 18, 17), gfx.LineTo(10, 17), gfx.LineTo(5, 21), gfx.LineTo(5, 17),
	gfx.QuadTo(3, 16, 3, 14), gfx.LineTo(3, 8.5), gfx.QuadTo(3, 5.5, 6, 5.5),
	gfx.MoveTo(8, 10), gfx.LineTo(16, 10), gfx.MoveTo(8, 13), gfx.LineTo(13, 13),
}}

var aiSelectedIcon = gfx.Icon{ViewBox: 24, Stroke: 1.9, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(5, 12), gfx.LineTo(9.5, 16.5), gfx.LineTo(19, 7),
}}

func (m *editorApp) aiPickerPill(c ui.C, label, tooltip string, blocked bool) ui.Box {
	pill := ui.Row(c).Height(34).PadX(8).Gap(4).Items(ui.Center).Radius(17).
		Border(1, m.theme.Border).Background(m.theme.Background)
	pill.HitWith(ui.ID(c), label, gfx.HitOptions{Role: gfx.RoleButton, Disabled: blocked}).
		Hover(ui.Look{}.Background(m.theme.Hover)).Focus(ui.Look{}.Border(m.theme.Ring)).Tooltip(tooltip)
	return pill
}

func (m *editorApp) aiModePicker(c ui.C, blocked bool) {
	a := &m.ai
	button := m.aiPickerPill(c, "选择 AI 模式", "当前模式 · "+aiModeLabel(a.mode), blocked).Shrink(0)
	button.Children(func() {
		ui.Icon(c, aiModeIcon(a.mode)).Size(13, 13).Color(m.theme.Ring)
		ui.Text(c, aiModeLabel(a.mode)).FontSize(12)
		ui.Icon(c, chevronDownIcon).Size(10, 10).Color(m.theme.MutedForeground)
	})
	if button.Clicked() {
		a.modeMenu = !a.modeMenu
	}
	ui.Menu(c, button, &a.modeMenu, func(menu *ui.MenuItems) {
		menu.WidthRange(188, 235)
		for _, mode := range aiModes {
			description := "解答问题，解释选中的内容"
			switch mode {
			case aiEdit:
				description = "生成差异，审阅后应用"
			case aiDraft:
				description = "起草内容，审阅后创建标签"
			}
			if menu.Item(aiModeLabel(mode)).Icon(aiModeIcon(mode)).Description(description).Checked(a.mode == mode).Clicked() {
				c.After(func() { a.mode = mode })
			}
		}
	})
}

func aiModeIcon(mode int) gfx.Icon {
	switch mode {
	case aiEdit:
		return codeIcon
	case aiDraft:
		return fileIcon
	default:
		return aiAskIcon
	}
}

func (m *editorApp) aiSendRecipe(theme gfx.Theme, variant components.ButtonVariant, state gfx.State) components.ButtonLook {
	theme.Primary, theme.PrimaryForeground = gfx.RGB(134, 181, 255), gfx.RGB(19, 34, 60)
	if m.light {
		theme.Primary, theme.PrimaryForeground = gfx.RGB(48, 108, 210), gfx.RGB(255, 255, 255)
	}
	look := components.ButtonRecipe(theme, variant, state)
	if state.Disabled {
		look.Background, look.Foreground, look.Border = theme.Muted, theme.MutedForeground, theme.Border
		look.Bordered = true
	}
	return look
}
