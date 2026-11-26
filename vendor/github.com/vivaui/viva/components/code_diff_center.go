package components

import "github.com/vivaui/viva/gfx"

const codeDiffMergeWidth = 22

func (diff *CodeDiff) usesCenterGutters() bool {
	return diff.snapshot.CenterGutters && diff.snapshot.Layout == CodeDiffSideBySide
}

func (diff *CodeDiff) paintRowSeparator(canvas *gfx.Canvas, row gfx.Rect, label string) {
	canvas.Fill(row, diff.theme.Card)
	style := gfx.TextStyle{Size: diff.theme.Metrics.FontSizeS, Color: diff.theme.MutedForeground}
	lineWidth := max((row.Width-canvas.TextWidth(label, style.Size))/2-10, 0)
	y := row.Y + row.Height/2
	canvas.Fill(gfx.Rect{X: row.X, Y: y, Width: lineWidth, Height: 1}, diff.theme.Border)
	canvas.Fill(gfx.Rect{X: row.X + row.Width - lineWidth, Y: y, Width: lineWidth, Height: 1}, diff.theme.Border)
	canvas.LabelCenter(label, row, style)
}

func (diff *CodeDiff) paintActiveHunkRow(canvas *gfx.Canvas, y float32, row int) {
	accent := diff.theme.Ring
	accent.Alpha *= .55
	left, right := diff.leftGutter, diff.rightGutter
	x := (left.X + left.Width + right.X) / 2
	canvas.Fill(gfx.Rect{X: x, Y: y, Width: 2, Height: diff.rowHeight}, accent)
	line := gfx.Rect{X: diff.Geometry.Viewport.X, Y: y, Width: diff.Geometry.Viewport.Width, Height: 1}
	if row == diff.snapshot.ActiveRows.Start {
		canvas.Fill(line, accent)
	}
	if row+1 == diff.snapshot.ActiveRows.End {
		line.Y += diff.rowHeight - 1
		canvas.Fill(line, accent)
	}
}

var codeDiffMergeArrow = gfx.Icon{ViewBox: 24, Stroke: 1.8, Cap: gfx.CapRound, Join: gfx.JoinRound,
	Commands: []gfx.PathCommand{
		gfx.MoveTo(5, 6), gfx.LineTo(11, 12), gfx.LineTo(5, 18),
		gfx.MoveTo(12, 6), gfx.LineTo(18, 12), gfx.LineTo(12, 18),
	},
}

func (diff *CodeDiff) paintMergeArrow(canvas *gfx.Canvas, action CodeDiffGutterAction, y float32) {
	gutter := diff.leftGutter
	if !action.Left {
		gutter = diff.rightGutter
	}
	button := gfx.Rect{X: gutter.X + 2, Y: y + 1, Width: min(codeDiffMergeWidth-2, gutter.Width-2), Height: max(diff.rowHeight-2, 1)}
	color := diff.theme.MutedForeground
	if canvas.Hovered() == action.ID || canvas.Focused() == action.ID {
		canvas.Rounded(button, 4, diff.theme.Hover)
		color = diff.theme.Foreground
	} else if action.Row == diff.snapshot.ActiveRows.Start && diff.snapshot.ActiveRows.End > action.Row {
		color = diff.theme.Ring
	}
	canvas.DrawIcon(codeDiffMergeArrow, button, color)
	canvas.HitWithOptions(action.ID, button, action.Label, gfx.HitOptions{
		Role: gfx.RoleButton, Disabled: diff.snapshot.Disabled, Cursor: gfx.PointerCursorHand,
	})
}
