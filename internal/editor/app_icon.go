package editor

import (
	"bytes"
	_ "embed"
	"image"
	"image/draw"
	"image/png"

	"github.com/vivaui/viva/app"
)

//go:embed assets/icon.png
var applicationIconPNG []byte

var applicationIcon = loadApplicationIcon()

func loadApplicationIcon() app.WindowIcon {
	decoded, err := png.Decode(bytes.NewReader(applicationIconPNG))
	if err != nil {
		panic(err)
	}
	pixels := image.NewNRGBA(decoded.Bounds())
	draw.Draw(pixels, pixels.Bounds(), decoded, decoded.Bounds().Min, draw.Src)
	return app.WindowIcon{Size: pixels.Bounds().Dx(), RGBA: pixels.Pix}
}
