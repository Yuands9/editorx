package main

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
)

func TestImageDecodingAndBounds(t *testing.T) {
	sample := image.NewNRGBA(image.Rect(0, 0, 16, 12))
	sample.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 128})
	lastID := uint64(0)
	for _, format := range []struct {
		extension string
		encode    func(io.Writer, image.Image) error
	}{
		{".PNG", png.Encode}, {".jpg", func(w io.Writer, m image.Image) error { return jpeg.Encode(w, m, nil) }},
		{".gif", func(w io.Writer, m image.Image) error { return gif.Encode(w, m, nil) }}, {".bmp", bmp.Encode},
		{".tiff", func(w io.Writer, m image.Image) error { return tiff.Encode(w, m, nil) }},
	} {
		t.Run(format.extension, func(t *testing.T) {
			var encoded bytes.Buffer
			if err := format.encode(&encoded, sample); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "中文图片"+format.extension)
			if err := os.WriteFile(path, encoded.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			picture, err := readImage(path)
			if err != nil {
				t.Fatal(err)
			}
			if !isImageFile(path) || picture.bitmap.Width != 16 || picture.bitmap.Height != 12 || picture.bitmap.ID == lastID {
				t.Fatal("invalid picture geometry/identity")
			}
			lastID = picture.bitmap.ID
			if format.extension == ".PNG" && !bytes.Equal(picture.bitmap.Pix[:4], []byte{0, 0, 128, 128}) {
				t.Fatalf("premultiplied BGRA: %v", picture.bitmap.Pix[:4])
			}
		})
	}
	picture, err := readImage("testdata/sample.webp")
	if err != nil || picture.kind != "WEBP" || picture.bitmap.Width <= 0 {
		t.Fatalf("WebP: %v", err)
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, sample); err != nil {
		t.Fatal(err)
	}
	huge := encoded.Bytes()
	binary.BigEndian.PutUint32(huge[16:20], 20000)
	binary.BigEndian.PutUint32(huge[29:33], crc32.ChecksumIEEE(huge[12:29]))
	path := filepath.Join(t.TempDir(), "too-wide.png")
	if err := os.WriteFile(path, huge, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readImage(path); err == nil || !strings.Contains(err.Error(), "超出查看范围") {
		t.Fatalf("oversized image: %v", err)
	}
}

func TestImageViewerReadonlyZoomAndMixedSearch(t *testing.T) {
	m := newTestEditorWithDocument()
	textTab := m.active
	textTab.source.replace("needle 中文")
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	openSearchForTest(t, s, "needle")
	m.findAll(textTab, true)
	sample := image.NewRGBA(image.Rect(0, 0, 2048, 1024))
	var encoded bytes.Buffer
	check(png.Encode(&encoded, sample))
	path := filepath.Join(t.TempDir(), "中文图片.png")
	check(os.WriteFile(path, encoded.Bytes(), 0600))
	check(s.Driver().QueueOpenFiles([]string{path}, nil))
	check(s.ClickLabel("打开文件"))
	tab := m.active
	if tab.picture == nil || tab.format() != "图片" || tab.modified() {
		t.Fatal("image not loaded as readonly tab")
	}
	if len(m.results.groups) != 1 || m.results.groups[0].tab != textTab {
		t.Fatal("image included in text search")
	}
	if _, err := s.Locate("代码编辑区"); err == nil {
		t.Fatal("image exposed text editor")
	}
	if m.saveTab(tab, false) || m.saveTab(tab, true) {
		t.Fatal("image entered text save flow")
	}
	if _, err := s.Driver().ShortcutChord(gfx.ModifierControl, 'f'); err != nil {
		t.Fatal(err)
	}
	if tab.search.open {
		t.Fatal("image search opened")
	}
	_, _ = s.Driver().ShortcutChord(gfx.ModifierControl, 's')
	_ = s.Type("不可写入图片")
	if textTab.source.text.String() != "needle 中文" {
		t.Fatal("typing into image altered hidden text tab")
	}
	check(s.ClickLabel("原始尺寸"))
	if tab.picture.zoom != 1 {
		t.Fatal("actual size")
	}
	check(s.ClickLabel("放大图片"))
	if tab.picture.zoom <= 1 {
		t.Fatal("zoom in")
	}
	area, err := s.Locate("图片查看区")
	check(err)
	x, y := area.Bounds.X+100, area.Bounds.Y+100
	check(s.Driver().Press(x, y, gfx.PointerButtonPrimary))
	check(s.Driver().Move(x+45, y+35))
	check(s.Driver().Release(x+45, y+35, gfx.PointerButtonPrimary))
	if tab.picture.pan.X < 40 || tab.picture.pan.Y < 30 {
		t.Fatalf("pan did not follow pointer: %v", tab.picture.pan)
	}
	check(s.ClickLabel("适应窗口"))
	if tab.picture.zoom != 0 || tab.picture.pan != (gfx.Point{}) {
		t.Fatal("fit did not reset view")
	}
	check(s.ClickLabel("切换浅色模式"))
	check(s.Resize(900, 560))
	before := len(m.tabs)
	m.load(path)
	check(s.Frame())
	if len(m.tabs) != before || m.active != tab {
		t.Fatal("image reopened duplicate tab")
	}
	saved, err := os.ReadFile(path)
	check(err)
	if !bytes.Equal(saved, encoded.Bytes()) {
		t.Fatal("image bytes changed")
	}
	check(s.ClickLabel("关闭 " + tab.title()))
	if m.confirm || m.active != textTab {
		t.Fatal("readonly image requested save on close")
	}
	bad := filepath.Join(t.TempDir(), "bad.png")
	check(os.WriteFile(bad, []byte("broken"), 0600))
	m.load(bad)
	if !m.showError || m.active != textTab || textTab.source.text.String() != "needle 中文" {
		t.Fatal("bad image lost current document")
	}
}

func TestHTMLSaveOpenAndCancel(t *testing.T) {
	m := newTestEditorWithDocument()
	tab := m.active
	source := `<!doctype html><html lang="zh"><body><img src="图.png"><script>document.title="中文";</script>你好</body></html>`
	tab.source.replace(source)
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	if tab.format() != "HTML" {
		t.Fatal("unsaved HTML not detected")
	}
	check(s.Driver().QueueSaveFile("", app.ErrCanceled))
	check(s.ClickLabel("保存并在浏览器打开"))
	if m.browser != nil || tab.doc.path != "" || !tab.modified() {
		t.Fatal("cancel still launched browser or changed document")
	}
	if _, ok := s.Driver().LastOpenedInBrowser(); ok {
		t.Fatal("browser opened after canceled save")
	}
	path := filepath.Join(t.TempDir(), "中文 空格#%.html")
	check(s.Driver().QueueSaveFile(path, nil))
	check(s.ClickLabel("保存并在浏览器打开"))
	check(s.WaitFor(func(app.Snapshot) bool { return m.browser == nil }, time.Second))
	opened, ok := s.Driver().LastOpenedInBrowser()
	if !ok || opened != path || m.showError {
		t.Fatalf("browser path: %q %v %s", opened, ok, m.errorText)
	}
	data, err := os.ReadFile(path)
	check(err)
	if string(data) != source || tab.modified() {
		t.Fatal("browser did not receive saved source")
	}
	tab.source.replace(source + "\n<!-- 新版 -->")
	check(s.Frame())
	check(s.ClickLabel("保存并在浏览器打开"))
	check(s.WaitFor(func(app.Snapshot) bool { return m.browser == nil }, time.Second))
	data, err = os.ReadFile(path)
	check(err)
	if !strings.Contains(string(data), "新版") {
		t.Fatal("browser received stale file")
	}
	m.load(path)
	if m.active != tab {
		t.Fatal("HTML duplicate tab")
	}
	check(s.ClickLabel("代码编辑区"))
	check(s.Driver().SelectText(0, 1))
	check(s.Compose("<"))
	if tab.modified() {
		t.Fatal("same-text composition fixture changed bytes")
	}
	m.openHTML(tab)
	if m.browser != nil || !m.showError {
		t.Fatal("unchanged IME composition opened browser")
	}
	check(s.Frame())
	check(s.ClickLabel("确定"))
	tab.source.text.CancelComposition()
	check(s.Frame())
	check(s.Compose("zhongwen"))
	check(s.Type("中文输入"))
	if !strings.Contains(tab.source.text.String(), "中文输入") {
		t.Fatal("HTML IME failed")
	}
}

func TestHTMLConflictAndMissingFile(t *testing.T) {
	for _, mode := range []string{"external-change", "removed"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "page.HTM")
			if err := os.WriteFile(path, []byte("<html>旧版</html>"), 0600); err != nil {
				t.Fatal(err)
			}
			m := newEditorApp()
			m.load(path)
			tab := m.active
			s := openTestSession(t, m)
			if mode == "external-change" {
				tab.source.replace("<html>本地编辑</html>")
				if err := os.WriteFile(path, []byte("<html>外部修改</html>"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			m.openHTML(tab)
			if err := s.Frame(); err != nil {
				t.Fatal(err)
			}
			if err := s.WaitFor(func(app.Snapshot) bool { return m.browser == nil }, time.Second); err != nil {
				t.Fatal(err)
			}
			if !m.showError {
				t.Fatal("open failure not shown")
			}
			if _, ok := s.Driver().LastOpenedInBrowser(); ok {
				t.Fatal("browser opened despite failure")
			}
			if mode == "external-change" {
				data, err := os.ReadFile(path)
				if err != nil || string(data) != "<html>外部修改</html>" {
					t.Fatal("external modification overwritten")
				}
			}
		})
	}
}
