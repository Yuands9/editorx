package windows

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/internal/render"
	"github.com/vivaui/viva/internal/scene"
)

func TestWebTooltipCutsFollowVisibleShape(t *testing.T) {
	view := gfx.Rect{X: 10, Y: 20, Width: 160, Height: 120}
	ptInRegion := webGDI.NewProc("PtInRegion")
	check := func(shape render.Rectangle, inside, outside point) {
		t.Helper()
		region := tooltipCutRegion(shape, view, 2)
		if region == 0 {
			t.Fatal("failed to create the tooltip region")
		}
		defer deleteObject.Call(region)
		hasInside, _, _ := ptInRegion.Call(region, uintptr(inside.X), uintptr(inside.Y))
		hasOutside, _, _ := ptInRegion.Call(region, uintptr(outside.X), uintptr(outside.Y))
		if hasInside == 0 || hasOutside != 0 {
			t.Fatalf("incorrect cut for kind %v", shape.Kind)
		}
	}

	check(render.Rectangle{
		Kind: scene.KindRectangle, Bounds: scene.Rect{X: 30, Y: 40, Width: 60, Height: 24}, Radius: 12,
	}, point{60, 54}, point{40, 40})
	check(render.Rectangle{
		Kind: scene.KindTriangle, Points: [3]scene.Point{{X: 90, Y: 46}, {X: 96, Y: 52}, {X: 90, Y: 58}},
	}, point{164, 64}, point{171, 55})
}
