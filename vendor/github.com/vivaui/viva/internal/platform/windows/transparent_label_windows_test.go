// Added for EditorX to verify its local Viva changes.
package windows

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/internal/platform"
)

func TestTransparentNativeLongLineDoesNotAllocateTexture(t *testing.T) {
	value := strings.Repeat("<p>中文\t😀</p>", 1000)
	label := labelTexture{label: platform.TextLabel{Value: value, Width: 400000, Height: 1500, FontSize: 16, Clipped: true, Clip: gfx.Rect{Width: 680, Height: 1500}}}
	renderer := renderer{noLabelPictures: true}
	for _, scale := range []float32{1, 1.25, 2} {
		if err := renderer.rasterize(&label, scale); err != nil {
			t.Fatal(err)
		}
		if renderer.direct != nil || label.texture != 0 || label.width != 0 || label.height != 0 {
			t.Fatal("invisible native input allocated graphics resources")
		}
	}
	if label.label.Value != value {
		t.Fatal("rasterization changed native editing text")
	}
}

func TestTransparentNativeLabelDoesNotSuppressColoredRunsOrImages(t *testing.T) {
	cases := []platform.TextLabel{
		{Value: "可见", Width: 80, Height: 20, Color: platform.Color{Alpha: 1}},
		{Value: "中文", Width: 80, Height: 20, Mono: true, CellWidth: 8, Runs: []gfx.TextRun{{Bytes: 6, Cells: 4, Color: gfx.RGB(80, 180, 250)}}},
		{Width: 1, Height: 1, Bitmap: []byte{30, 20, 10, 255}, BitmapWidth: 1, BitmapHeight: 1, BitmapStride: 4},
	}
	for i, value := range cases {
		label := labelTexture{label: value}
		if _, width, visible := label.window(1); !visible || width <= 0 {
			t.Fatalf("visible case %d was discarded", i)
		}
	}
}
