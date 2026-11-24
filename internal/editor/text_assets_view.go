package editor

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

var templateIcon = gfx.Icon{ViewBox: 24, Stroke: 1.6, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(3, 3), gfx.LineTo(10, 3), gfx.LineTo(10, 10), gfx.LineTo(3, 10), gfx.LineTo(3, 3),
	gfx.MoveTo(14, 3), gfx.LineTo(21, 3), gfx.LineTo(21, 10), gfx.LineTo(14, 10), gfx.LineTo(14, 3),
	gfx.MoveTo(3, 14), gfx.LineTo(10, 14), gfx.LineTo(10, 21), gfx.LineTo(3, 21), gfx.LineTo(3, 14),
	gfx.MoveTo(14, 14), gfx.LineTo(21, 14), gfx.MoveTo(14, 18), gfx.LineTo(21, 18),
}}

var dictionaryIcon = gfx.Icon{ViewBox: 24, Stroke: 1.6, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(12, 5), gfx.LineTo(8, 3), gfx.LineTo(3, 3), gfx.LineTo(3, 19), gfx.LineTo(8, 19), gfx.LineTo(12, 21),
	gfx.LineTo(16, 19), gfx.LineTo(21, 19), gfx.LineTo(21, 3), gfx.LineTo(16, 3), gfx.LineTo(12, 5), gfx.LineTo(12, 21),
	gfx.MoveTo(6, 8), gfx.LineTo(9, 8), gfx.MoveTo(15, 8), gfx.LineTo(18, 8),
}}

var downloadIcon = gfx.Icon{ViewBox: 24, Stroke: 1.6, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(12, 3), gfx.LineTo(12, 15), gfx.MoveTo(7, 10), gfx.LineTo(12, 15), gfx.LineTo(17, 10),
	gfx.MoveTo(4, 16), gfx.LineTo(4, 21), gfx.LineTo(20, 21), gfx.LineTo(20, 16),
}}

var swapLanguagesIcon = gfx.Icon{ViewBox: 24, Stroke: 1.6, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(4, 8), gfx.LineTo(20, 8), gfx.MoveTo(16, 4), gfx.LineTo(20, 8), gfx.LineTo(16, 12),
	gfx.MoveTo(20, 16), gfx.LineTo(4, 16), gfx.MoveTo(8, 12), gfx.LineTo(4, 16), gfx.LineTo(8, 20),
}}

// Keep feature surfaces in the native UI: icons and tinted cards need no image bundle or WebView.
func (m *editorApp) assetAccent() (ink, fill gfx.Color) {
	if m.light {
		return gfx.RGB(45, 102, 182), gfx.RGB(233, 241, 253)
	}
	return gfx.RGB(144, 188, 250), gfx.RGB(35, 47, 65)
}

func (m *editorApp) assetCard(c ui.C) ui.Box {
	return ui.Column(c).FillWidth().Pad(12).Gap(8).Radius(12).Background(m.theme.Muted)
}

func (m *editorApp) assetHeading(c ui.C, icon gfx.Icon, title, subtitle string) {
	ink, fill := m.assetAccent()
	ui.Row(c).FillWidth().Gap(10).Items(ui.Center).Children(func() {
		ui.Row(c).Size(38, 38).Shrink(0).Radius(11).Background(fill).Items(ui.Center).Justify(ui.Center).Children(func() {
			ui.Icon(c, icon).Size(22, 22).Color(ink)
		})
		ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
			ui.Text(c, title).Bold().FontSize(16)
			if subtitle != "" {
				ui.Wrap(c, subtitle).FillWidth().Muted().FontSize(12)
			}
		})
	})
}

func (m *editorApp) assetBadge(c ui.C, label string) {
	ink, fill := m.assetAccent()
	ui.Row(c).Children(func() {
		ui.Text(c, label).FontSize(11).Color(ink).Background(fill).Radius(5).PadX(6).PadY(3)
	})
}
