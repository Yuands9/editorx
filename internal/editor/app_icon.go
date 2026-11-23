package editor

import (
	"bytes"
	_ "embed"
	"image"
	"image/draw"
	"image/png"
	"sync"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

//go:embed assets/icon.png
var applicationIconPNG []byte

//go:embed assets/icon-light.png
var lightApplicationIconPNG []byte

var applicationIcon = loadApplicationIcon(applicationIconPNG)
var lightApplicationIcon = loadApplicationIcon(lightApplicationIconPNG)

var darkEditorLogo = sync.OnceValue(func() gfx.Bitmap { return applicationIconBitmap(applicationIcon) })
var lightEditorLogo = sync.OnceValue(func() gfx.Bitmap { return applicationIconBitmap(lightApplicationIcon) })

func themedApplicationIcon(light bool) app.WindowIcon {
	if light {
		return lightApplicationIcon
	}
	return applicationIcon
}

func editorLogo(light bool) gfx.Bitmap {
	if light {
		return lightEditorLogo()
	}
	return darkEditorLogo()
}

func applicationIconBitmap(icon app.WindowIcon) gfx.Bitmap {
	return imageToVivaBitmap(&image.NRGBA{
		Pix: icon.RGBA, Stride: icon.Size * 4, Rect: image.Rect(0, 0, icon.Size, icon.Size),
	})
}

func loadApplicationIcon(data []byte) app.WindowIcon {
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		panic(err)
	}
	pixels := image.NewNRGBA(decoded.Bounds())
	draw.Draw(pixels, pixels.Bounds(), decoded, decoded.Bounds().Min, draw.Src)
	return app.WindowIcon{Size: pixels.Bounds().Dx(), RGBA: pixels.Pix}
}
