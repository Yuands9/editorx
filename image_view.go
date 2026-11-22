package main

import (
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

var imageSequence atomic.Uint64

type imageDocument struct {
	errorText        string
	bitmap           gfx.Bitmap
	kind             string
	bytes            int64
	zoom, scale      float32
	pan, lastPointer gfx.Point
	dragging         bool
	handlers         ui.Handlers
}

func isImageFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".webp", ".tif", ".tiff":
		return true
	default:
		return false
	}
}

func readImage(path string) (*imageDocument, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	config, kind, err := image.DecodeConfig(file)
	if err != nil {
		return nil, fmt.Errorf("无法识别图片或图片已损坏：%w", err)
	}
	// 先检查尺寸再解码，避免巨大像素分配和超过 D3D 纹理的单边上限。
	if config.Width <= 0 || config.Height <= 0 || config.Width > 16384 || config.Height > 16384 || int64(config.Width)*int64(config.Height) > 40_000_000 {
		return nil, fmt.Errorf("图片尺寸 %d × %d 超出查看范围（最多 4000 万像素，单边不超过 16384）", config.Width, config.Height)
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	decoded, _, err := image.Decode(file)
	if err != nil {
		return nil, fmt.Errorf("图片解码失败：%w", err)
	}
	picture := &imageDocument{
		bitmap: imageToVivaBitmap(decoded),
		kind:   strings.ToUpper(kind), bytes: info.Size(), scale: 1,
	}
	picture.handlers.Pointer = picture.pointer
	picture.handlers.Scroll = func(event gfx.ScrollEvent) gfx.EventResult {
		delta := event.StepY(48)
		if delta == 0 {
			return gfx.EventResult{}
		}
		factor := float32(1.2)
		if delta > 0 {
			factor = 1 / factor
		}
		picture.changeZoom(factor)
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	return picture, nil
}

// imageToVivaBitmap takes ownership of the decoded pixels and switches them to premultiplied BGRA.
func imageToVivaBitmap(decoded image.Image) gfx.Bitmap {
	size := decoded.Bounds().Size()
	pixels, ok := decoded.(*image.RGBA)
	if !ok {
		pixels = image.NewRGBA(image.Rect(0, 0, size.X, size.Y))
		draw.Draw(pixels, pixels.Bounds(), decoded, decoded.Bounds().Min, draw.Src)
	}
	for y := 0; y < size.Y; y++ {
		row := pixels.Pix[y*pixels.Stride : y*pixels.Stride+size.X*4]
		for x := 0; x < len(row); x += 4 {
			row[x], row[x+2] = row[x+2], row[x]
		}
	}
	return gfx.Bitmap{Pix: pixels.Pix, Width: size.X, Height: size.Y, Stride: pixels.Stride, ID: imageSequence.Add(1), Version: 1}
}

func (picture *imageDocument) changeZoom(factor float32) {
	zoom := picture.zoom
	if zoom == 0 {
		zoom = picture.scale
	}
	picture.zoom = max(0.05, min(16, zoom*factor))
}

func (picture *imageDocument) pointer(event gfx.PointerEvent) gfx.EventResult {
	switch event.Kind {
	case gfx.PointerDown:
		if event.Button != gfx.PointerButtonPrimary {
			return gfx.EventResult{}
		}
		picture.dragging = true
		picture.lastPointer = gfx.Point{X: event.X, Y: event.Y}
	case gfx.PointerMove:
		if !picture.dragging {
			return gfx.EventResult{}
		}
		picture.pan.X += event.X - picture.lastPointer.X
		picture.pan.Y += event.Y - picture.lastPointer.Y
		picture.lastPointer = gfx.Point{X: event.X, Y: event.Y}
	case gfx.PointerUp:
		picture.dragging = false
	default:
		return gfx.EventResult{}
	}
	return gfx.EventResult{Handled: true, Repaint: true}
}

func (m *editorApp) imageView(c ui.C, t *editorTab) {
	picture := t.picture
	if picture.errorText != "" {
		ui.Text(c, picture.errorText).FillWidth().Pad(24)
		return
	}
	ui.Row(c).FillWidth().Height(44).PadX(12).Gap(8).Items(ui.Center).Children(func() {
		if ui.Button(c, "适应窗口").Ghost().Height(28).FontSize(12).Clicked() {
			picture.zoom = 0
			picture.pan = gfx.Point{}
		}
		if ui.Button(c, "原始尺寸").Ghost().Height(28).FontSize(12).Tooltip("100% · 一个图片像素对应一个屏幕像素").Clicked() {
			picture.zoom = 1
			picture.pan = gfx.Point{}
		}
		if ui.Button(c, "缩小图片").Icon(minusIcon).IconOnly().Ghost().Width(28).Height(28).Clicked() {
			picture.changeZoom(1 / 1.2)
		}
		label := "适应窗口"
		if picture.zoom > 0 {
			label = fmt.Sprintf("%.0f%%", picture.zoom*100)
		}
		ui.Text(c, label).FontSize(12).Muted().Width(68)
		if ui.Button(c, "放大图片").Icon(plusIcon).IconOnly().Ghost().Width(28).Height(28).Clicked() {
			picture.changeZoom(1.2)
		}
		ui.Slot(c).Grow(1)
		ui.Text(c, "滚轮缩放 · 拖动查看").FontSize(12).Muted()
	})
	control := ui.Control(c, "图片查看区", &picture.handlers).FillWidth().Grow(1).MinHeight(0)
	cursor := gfx.PointerCursorHand
	if picture.dragging {
		cursor = gfx.PointerCursorGrabbing
	}
	control.HitWith(control.N().ID, "图片查看区", gfx.HitOptions{Role: gfx.RoleGroup, Cursor: cursor})
	control.Draw(func(d *ui.DrawContext) {
		zoom := picture.zoom
		if zoom == 0 {
			zoom = min(1, min(d.Rect.Width/float32(picture.bitmap.Width), d.Rect.Height/float32(picture.bitmap.Height))*d.Canvas.PixelScale())
		}
		geometry := components.PaintImageView(d.Canvas, m.theme, d.Rect, components.ImageViewOptions{Bitmap: picture.bitmap, Zoom: zoom, Pan: picture.pan})
		picture.scale = geometry.Scale * d.Canvas.PixelScale()
		picture.pan = gfx.Point{X: geometry.Image.X + (geometry.Image.Width-d.Rect.Width)/2 - d.Rect.X, Y: geometry.Image.Y + (geometry.Image.Height-d.Rect.Height)/2 - d.Rect.Y}
	})
}

func (m *editorApp) imageStatus(c ui.C, t *editorTab) {
	picture := t.picture
	ui.Slot(c).FillWidth().Height(1).Background(m.theme.Border)
	ui.Row(c).FillWidth().Height(28).PadX(14).Gap(18).Items(ui.Center).Background(m.theme.Card).Children(func() {
		ui.Text(c, "图片 · 只读查看").FontSize(12).Muted().Grow(1)
		if picture.kind == "GIF" {
			ui.Text(c, "GIF 首帧").FontSize(12).Muted()
		}
		ui.Text(c, fmt.Sprintf("%d × %d px", picture.bitmap.Width, picture.bitmap.Height)).FontSize(12).Muted()
		ui.Text(c, fmt.Sprintf("%.1f KB", float64(picture.bytes)/1024)).FontSize(12).Muted()
		ui.Text(c, picture.kind).FontSize(12).Muted()
	})
}
