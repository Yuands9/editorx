// Modified for EditorX. See patches/ in the project root.
package windows

import (
	"unsafe"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/internal/platform"
	"github.com/vivaui/viva/internal/render"
	"github.com/vivaui/viva/internal/scene"
)

var createPolygonRgn = webGDI.NewProc("CreatePolygonRgn")

func visibleTooltipCuts(view platform.NativeView, shapes []render.Rectangle, into []render.Rectangle) []render.Rectangle {
	for _, shape := range shapes {
		if shape.Layer != gfx.TooltipLayer || shape.Layer <= view.Layer || shape.Color.Alpha <= 0 ||
			(shape.Kind != scene.KindRectangle && shape.Kind != scene.KindTriangle) {
			continue
		}
		box := shape.Bounds
		if box.X < view.Bounds.X+view.Bounds.Width && box.X+box.Width > view.Bounds.X &&
			box.Y < view.Bounds.Y+view.Bounds.Height && box.Y+box.Height > view.Bounds.Y {
			into = append(into, shape)
		}
	}
	return into
}

// Leave the WebView visible through the rounded corners of the tooltip and
// around its triangular arrow, where a rectangular cut would show the canvas.
func tooltipCutRegion(shape render.Rectangle, view gfx.Rect, scale float32) uintptr {
	x := func(value float32) uintptr { return uintptr(int32(round((value - view.X) * scale))) }
	y := func(value float32) uintptr { return uintptr(int32(round((value - view.Y) * scale))) }
	var region uintptr
	if shape.Kind == scene.KindTriangle {
		points := [3]point{}
		for i, corner := range shape.Points {
			points[i] = point{X: int32(x(corner.X)), Y: int32(y(corner.Y))}
		}
		region, _, _ = createPolygonRgn.Call(uintptr(unsafe.Pointer(&points[0])), 3, 1 /* ALTERNATE */)
	} else {
		box := shape.Bounds
		radius := int32(round(min(shape.Radius, box.Width/2, box.Height/2) * scale))
		if radius > 0 {
			region, _, _ = createRoundRectRgn.Call(x(box.X), y(box.Y), x(box.X+box.Width), y(box.Y+box.Height), uintptr(2*radius), uintptr(2*radius))
		} else {
			region, _, _ = createRectRgn.Call(x(box.X), y(box.Y), x(box.X+box.Width), y(box.Y+box.Height))
		}
	}
	if shape.Clipped && region != 0 {
		box := shape.Clip
		clip, _, _ := createRectRgn.Call(x(box.X), y(box.Y), x(box.X+box.Width), y(box.Y+box.Height))
		if clip != 0 {
			combineRgn.Call(region, region, clip, 1 /* RGN_AND */)
			deleteObject.Call(clip)
		}
	}
	return region
}
