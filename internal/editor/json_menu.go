package editor

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

func styleJSONMenu(menu *ui.MenuItems) {
	menu.Unpainted().DrawPanel(func(canvas *gfx.Canvas, bounds gfx.Rect, theme gfx.Theme) {
		canvas.RoundedBorder(bounds, theme.Metrics.RadiusControl, 1, theme.Border, theme.Popover)
	}).DrawItem(paintJSONMenuItem)
}

func paintJSONMenuItem(item ui.MenuItemParts) {
	canvas, theme, bounds := item.Canvas, item.Theme, item.Rect
	if item.Separator {
		canvas.Fill(gfx.Rect{X: bounds.X + 6, Y: bounds.Y + (bounds.Height-1)/2, Width: max(0, bounds.Width-12), Height: 1}, theme.Border)
		return
	}

	color, size := item.Foreground, theme.Metrics.FontSizeM
	selected := item.Selected || item.Checked
	switch {
	case item.GroupLabel:
		color, size = theme.MutedForeground, theme.Metrics.FontSizeS
	case selected && !item.Disabled:
		canvas.Rounded(bounds, theme.Metrics.RadiusItem(), theme.Selection)
		color = theme.SelectionForeground
	case item.Active:
		canvas.Rounded(bounds, theme.Metrics.RadiusItem(), theme.Hover)
	}

	textBounds := gfx.Rect{X: bounds.X + 9, Y: bounds.Y, Width: max(0, bounds.Width-39), Height: bounds.Height}
	canvas.Label(canvas.FitLine(item.Label, size, gfx.WeightRegular, textBounds.Width), textBounds,
		gfx.TextStyle{Size: size, Color: color})
	if selected {
		checkColor := theme.Ring
		if item.Disabled {
			checkColor = theme.MutedForeground
		}
		canvas.DrawIcon(aiSelectedIcon, gfx.Rect{X: bounds.X + bounds.Width - 24, Y: bounds.Y + (bounds.Height-15)/2, Width: 15, Height: 15}, checkColor)
	}
}
