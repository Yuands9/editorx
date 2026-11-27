package editor

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
)

func TestProjectPreviewPreservesEditsAndReveal(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "folder")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	a, b := filepath.Join(root, "a.txt"), filepath.Join(nested, "b.txt")
	for _, path := range []string{a, b} {
		if err := os.WriteFile(path, []byte("value"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	m := newEditorApp()
	m.addProjects(root)
	if m.workspace.roots[0].loaded {
		t.Fatal("directory read blocked addProjects")
	}
	s := openTestSession(t, m)
	m.previewProjectFile(a)
	m.previewProjectFile(b)
	if len(m.tabs) != 1 || m.active.doc.path != b || !m.active.projectPreview || len(m.closedTabs) != 0 {
		t.Fatal("preview did not reuse its slot")
	}
	draft := m.active
	draft.source.text.Insert("draft")
	m.previewProjectFile(a)
	if len(m.tabs) != 2 || draft.projectPreview || !draft.modified() {
		t.Fatal("preview replaced an edited file")
	}
	m.previewProjectFile(a)
	if m.active.projectPreview {
		t.Fatal("second click did not retain preview")
	}
	m.revealProjectFile(b)
	waitProjectLoads(t, m, s)
	if node := m.workspace.findNode(nested); node == nil || !node.expanded {
		t.Fatal("reveal did not expand ancestors")
	}
	found := false
	for _, row := range m.workspace.rows {
		found = found || row.node.path == b
	}
	if !found {
		t.Fatal("revealed file is not visible")
	}
}

func TestProjectWatcherRefreshAndRelease(t *testing.T) {
	root := t.TempDir()
	m := newEditorApp()
	m.addProjects(root)
	s := openTestSession(t, m)
	m.watchFiles = true
	defer m.closeProjectWatches()
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "external.txt")
	if err := os.WriteFile(path, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.WaitFor(func(app.Snapshot) bool { return m.workspace.findNode(path) != nil }, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	id := m.workspace.findNode(path).id
	if err := os.WriteFile(path, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.WaitFor(func(app.Snapshot) bool { state := m.projectWatches[root]; return state != nil && state.dirty }, 3*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := s.WaitFor(func(app.Snapshot) bool { return len(m.workspace.loads) == 0 && !m.projectWatches[root].dirty }, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if m.workspace.findNode(path).id != id {
		t.Fatal("refresh changed a stable row ID")
	}
	m.removeProject(m.workspace.roots[0])
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if len(m.projectWatches) != 0 {
		t.Fatal("removed project retained a watcher")
	}
}
