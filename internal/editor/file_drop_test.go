package editor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestFileDropOpensDocuments(t *testing.T) {
	directory := t.TempDir()
	text := filepath.Join(directory, "无后缀")
	pdf := filepath.Join(directory, "多页 预览.pdf")
	project := filepath.Join(directory, "项目")
	for path, data := range map[string][]byte{text: []byte("中文文本"), pdf: previewFixturePDF()} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(project, 0700); err != nil {
		t.Fatal(err)
	}

	m := newEditorApp()
	m.dataDirectory = filepath.Join(directory, "data")
	session := openTestSession(t, m)
	drop := func(phase gfx.FileDropPhase, paths ...string) bool {
		t.Helper()
		accepted, err := session.Drop(gfx.FileDropEvent{Phase: phase, X: 320, Y: 260, Paths: paths})
		if err != nil {
			t.Fatal(err)
		}
		return accepted
	}
	if !drop(gfx.FileDropOver) || drop(gfx.FileDropLeave) || drop(gfx.FileDropDrop) || len(m.tabs) != 0 {
		t.Fatal("hover or an empty drop changed the workspace")
	}
	if !drop(gfx.FileDropDrop, text, project, pdf) {
		t.Fatal("file drop was rejected")
	}
	if len(m.tabs) != 2 || m.tabs[0].source.text.String() != "中文文本" ||
		m.active == nil || m.active.doc.path != pdf || m.active.preview == nil || m.active.preview.kind != "PDF" ||
		m.workspace == nil || m.workspace.project(project) == nil {
		t.Fatal("drop did not open the text, display the PDF, and add the folder")
	}
	if err := session.ClickLabel("button:设置"); err != nil {
		t.Fatal(err)
	}
	if !drop(gfx.FileDropDrop, text) || m.appSettings.open || len(m.tabs) != 2 || m.active != m.tabs[0] {
		t.Fatal("dropping an already-open file did not return to its existing tab")
	}
}
