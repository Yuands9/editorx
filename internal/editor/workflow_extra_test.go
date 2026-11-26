package editor

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/window"
)

func TestWorkflowReadAllowsRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rotation.log")
	workflowCheck(t, os.WriteFile(path, []byte("old"), 0600))
	file, err := openSharedTextFile(path)
	workflowCheck(t, err)
	defer file.Close()
	workflowCheck(t, os.Rename(path, path+".1"))
	workflowCheck(t, os.WriteFile(path, []byte("new"), 0600))
	page, err := readLargeTextPage(path, -1, -1, "", utf8Encoding)
	workflowCheck(t, err)
	if page.value != "new" {
		t.Fatal(page)
	}
}

func TestWorkflowBackgroundReadKeepsNewEdit(t *testing.T) {
	m := newEditorApp()
	tab := m.addTab(document{path: filepath.Join(t.TempDir(), "a.txt"), saved: "old"}, "a.txt")
	s := &diskChangeState{path: tab.doc.path, action: "reload", before: "old", beforeFormat: tab.doc}
	tab.diskChange = s
	tab.source.replace("new draft")
	m.acceptDiskRead(tab, s, diskReadResult{file: openedEditorFile{doc: document{path: tab.doc.path, saved: "disk"}}}, nil)
	if tab.source.text.String() != "new draft" || s.problem == "" {
		t.Fatal("reload replaced newer draft")
	}
	editor := text.NewEditor("unchanged")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := replayMacroContext(ctx, editor, editorMacro{Steps: []macroStep{{Kind: window.TextInsert, Text: "bad"}}}, 100)
	if err == nil || editor.String() != "unchanged" {
		t.Fatal("canceled macro changed document")
	}
}
