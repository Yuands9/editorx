package editor

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

func TestFileRoundTrip(t *testing.T) {
	for _, name := range []string{"配置.xml", "配置.json", "说明.md", "Dockerfile"} {
		for _, value := range []string{"", "中文😀", "\ufeff中文\r\n第二行\r\n", "第一行\n第二行", "甲\r\n乙\n丙"} {
			t.Run(name+value, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), name)
				if err := os.WriteFile(path, []byte(value), 0600); err != nil {
					t.Fatal(err)
				}
				doc, err := readDocument(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := doc.save(path, doc.saved, nil); err != nil {
					t.Fatal(err)
				}
				actual, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(actual, []byte(value)) {
					t.Fatalf("round trip = %q, %v", actual, err)
				}
				if err := doc.save(path, doc.saved+"新增中文", nil); err != nil {
					t.Fatal(err)
				}
				reopened, err := readDocument(path)
				if err != nil || reopened.saved != doc.saved {
					t.Fatalf("reopen = %+v, %v", reopened, err)
				}
			})
		}
	}
}

func TestSaveConflictAndFailureKeepContents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	doc, err := readDocument(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("external"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := doc.save(path, "my changes", nil); err == nil {
		t.Fatal("external change overwritten")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "external" || doc.saved != "original" {
		t.Fatal("conflict lost data")
	}
	if err := doc.save(filepath.Join(path, "missing"), "my changes", nil); err == nil {
		t.Fatal("expected save failure")
	}
	if doc.saved != "original" {
		t.Fatal("failure marked document saved")
	}
}

func TestEditorCompositionSaveAndReopen(t *testing.T) {
	m := newTestEditorWithDocument()
	page := ui.NewPage(ui.PageConfig{View: m.page, Theme: &m.theme})
	driver, err := app.NewDriver(ui.Configure(page, app.Config{Width: 1100, Height: 760}), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	if err := driver.Click(350, 150); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("name = "); err != nil {
		t.Fatal(err)
	}
	if err := driver.Compose("zhongwen"); err != nil {
		t.Fatal(err)
	}
	if _, composing := m.active.source.text.Composition(); !composing {
		t.Fatal("missing composition")
	}
	if err := driver.Type("中文"); err != nil {
		t.Fatal(err)
	}
	if m.active.source.text.String() != "name = 中文" {
		t.Fatalf("committed text = %q", m.active.source.text.String())
	}
	if input := driver.Snapshot().Editor; input == nil || !input.Multiline || !input.HideNativeOverlays {
		t.Fatalf("editor lost multiline or placement configuration: %+v", input)
	}
	// 在已有中文选区内取消组合，原文与选区应恢复。
	if err := driver.SelectText(7, 9); err != nil {
		t.Fatal(err)
	}
	if err := driver.Compose("nihao"); err != nil {
		t.Fatal(err)
	}
	if !m.active.source.text.CancelComposition() || m.active.source.text.String() != "name = 中文" {
		t.Fatalf("canceled composition lost text: %q", m.active.source.text.String())
	}
	if err := driver.Paint(); err != nil {
		t.Fatal(err)
	}
	if err := driver.Compose("nihao"); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("你好😀"); err != nil {
		t.Fatal(err)
	}
	if m.active.source.text.String() != "name = 你好😀" {
		t.Fatalf("selection commit = %q", m.active.source.text.String())
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("value = 42"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "无后缀")
	if err := driver.QueueSaveFile(path, nil); err != nil {
		t.Fatal(err)
	}
	if handled, err := driver.ShortcutChord(gfx.ModifierControl, 's'); err != nil || !handled {
		t.Fatalf("save shortcut = %v, %v", handled, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "name = 你好😀\r\nvalue = 42" {
		t.Fatalf("saved = %q, %v; UI error %s", data, err, m.errorText)
	}
	if handled, err := driver.ShortcutChord(gfx.ModifierControl, 'n'); err != nil || !handled {
		t.Fatalf("new shortcut = %v, %v", handled, err)
	}
	if err := driver.QueueOpenFiles([]string{path}, nil); err != nil {
		t.Fatal(err)
	}
	if handled, err := driver.ShortcutChord(gfx.ModifierControl, 'o'); err != nil || !handled {
		t.Fatalf("open shortcut = %v, %v", handled, err)
	}
	if m.active.source.text.String() != "name = 你好😀\nvalue = 42" || m.active.doc.path != path || m.showError {
		t.Fatalf("reopened = %q; UI error %s", m.active.source.text.String(), m.errorText)
	}
	if err := driver.Type(" modified"); err != nil {
		t.Fatal(err)
	}
	m.requestTab(m.active, func() { t.Error("discarded unsaved changes") })
	if !m.confirm {
		t.Fatal("missing unsaved confirmation")
	}
}
