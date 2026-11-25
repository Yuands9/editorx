package editor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

func TestImageOCRIsOptIn(t *testing.T) {
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	m.load("assets/icon.png")
	session := openTestSession(t, m)
	if err := session.ClickLabel("button:识别文字"); err != nil {
		t.Fatal(err)
	}
	state := m.imageOCR
	if state == nil || state.task != nil || state.installed || !session.HasText("下载后可离线识别简体中文和英文。") {
		t.Fatal("opening OCR before installation must only offer the download")
	}
	if _, err := os.Stat(filepath.Join(m.dataDirectory, imageOCRDirectory)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("opening OCR created local storage", err)
	}
	if err := session.Keys("escape"); err != nil || m.imageOCR != nil {
		t.Fatal("the OCR dialog did not dismiss", err)
	}
}

func TestImageOCRPixels(t *testing.T) {
	bitmap := gfx.Bitmap{
		Width: 2, Height: 1, Stride: 8,
		Pix: []byte{0, 0, 0, 0, 0, 0, 0, 255},
	}
	pixels, width, height, err := imageOCRPixels(context.Background(), bitmap)
	if err != nil || width != 2 || height != 1 || len(pixels) != 2 || pixels[0] != 255 || pixels[1] != 0 {
		t.Fatalf("expected transparency against white and opaque black: %v %dx%d %v", pixels, width, height, err)
	}
	bitmap.Height = 2
	if _, _, _, err := imageOCRPixels(context.Background(), bitmap); err == nil {
		t.Fatal("accepted truncated backing pixels")
	}
}

func TestImageOCRNetworkInstallAndCopy(t *testing.T) {
	image := os.Getenv("EDITORX_TEST_OCR_IMAGE")
	if image == "" {
		t.Skip("set EDITORX_TEST_OCR_IMAGE to the bilingual OCR fixture to exercise real downloads")
	}
	if directory := os.Getenv("EDITORX_TEST_OCR_DOWNLOADS"); directory != "" {
		previous := editorUpdateHTTP
		client := *previous
		client.Transport = offlineArchiveFiles(directory)
		editorUpdateHTTP = &client
		t.Cleanup(func() { editorUpdateHTTP = previous })
	}
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	m.load(image)
	if m.showError || m.active == nil || m.active.picture == nil {
		t.Fatal("test image failed to open", m.errorText)
	}

	session := openTestSession(t, m)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(session.ClickLabel("button:识别文字"))
	state := m.imageOCR
	bytes := imageOCRComponent.downloadBytes()
	started := time.Now()
	must(session.ClickLabel(fmt.Sprintf("button:下载并识别 · %.1f MiB", float64(bytes)/(1<<20))))
	progress := state.progress
	if progress == nil {
		t.Fatal("download did not start")
	}
	must(session.WaitFor(func(app.Snapshot) bool { return state.task == nil }, 3*time.Minute))
	if state.problem != "" || !state.recognized || !state.installed || progress.received.Load() != bytes {
		t.Fatalf("OCR did not complete: problem=%q download=%d", state.problem, progress.received.Load())
	}
	text := state.output.Text()
	compact := strings.Join(strings.Fields(text), "")
	for _, expected := range []string{"EDITORXOCR12345", "简体中文", "本地识别"} {
		if !strings.Contains(compact, expected) {
			t.Fatalf("OCR did not find %q in %q", expected, text)
		}
	}
	info, err := readOfflineInstallation(m.dataDirectory, imageOCRComponent)
	must(err)
	if info == nil {
		t.Fatal("OCR installation missing")
	}
	t.Logf("OCR downloaded %d bytes, installed %d bytes, completed first download and recognition in %s", bytes, info.Bytes, time.Since(started))
	t.Logf("recognized synthetic fixture: %q", text)

	must(session.ClickLabel("识别结果"))
	state.output.Select(0, 7)
	must(session.Frame())
	handled, err := session.Driver().ShortcutChord(gfx.ModifierControl, 'c')
	must(err)
	if !handled {
		t.Fatal("Ctrl+C did not reach the selected text field")
	}
	if selection := string([]rune(text)[:7]); session.Driver().Clipboard() != selection {
		t.Fatalf("clipboard did not contain the partial selection: %q", session.Driver().Clipboard())
	}
	must(session.ClickLabel("button:复制全文"))
	if session.Driver().Clipboard() != text {
		t.Fatal("clipboard did not contain full result")
	}

	started = time.Now()
	must(session.ClickLabel("button:重新识别"))
	must(session.WaitFor(func(app.Snapshot) bool { return state.task == nil }, 15*time.Second))
	if state.problem != "" || state.output.Text() != text {
		t.Fatal("recognition could not reuse the installed component", state.problem)
	}
	t.Logf("repeat recognition took %s", time.Since(started))
	if !imageOCRComponentLock.TryLock() {
		t.Fatal("the OCR task retained the component lock")
	}
	imageOCRComponentLock.Unlock()

	must(session.ClickLabel("button:移除 OCR 组件"))
	must(session.WaitFor(func(app.Snapshot) bool { return state.task == nil }, 15*time.Second))
	if _, err := os.Stat(offlineComponentPath(m.dataDirectory, imageOCRComponent)); !errors.Is(err, os.ErrNotExist) || state.installed {
		t.Fatal("OCR removal did not release the component directory", err)
	}
}
