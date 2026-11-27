package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

func waitProjectLoads(t *testing.T, m *editorApp, s *headless.Session) {
	t.Helper()
	if err := s.WaitFor(func(app.Snapshot) bool { return m.workspace == nil || len(m.workspace.loads) == 0 }, 8*time.Second); err != nil {
		t.Fatal(err)
	}
}

// Session fixtures must load asynchronously without painting editor panes,
// which would consume restored scroll positions that those tests inspect.
func loadProjectFixture(t *testing.T, m *editorApp) {
	t.Helper()
	page := ui.NewPage(ui.PageConfig{View: m.updateProjectDirectories, Theme: &m.theme})
	s, err := headless.Open(ui.Configure(page, app.Config{Width: 200, Height: 100}), nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	waitProjectLoads(t, m, s)
}

func TestLanguageCommentShortcut(t *testing.T) {
	for _, sample := range []struct{ language, prefix string }{
		{"Go", "//"}, {"JavaScript", "//"}, {"Python", "#"}, {"SQL", "--"}, {"INI", ";"},
	} {
		t.Run(sample.language, func(t *testing.T) {
			p := newEditorPane("  value\n  next", false)
			p.view.Snapshot.Language = sample.language
			p.text.SetSelection(text.Range{End: p.text.RuneCount()})
			key := gfx.KeyEvent{Text: "/", Modifiers: gfx.ModifierControl}
			if !p.key(key).Handled || p.text.String() != "  "+sample.prefix+" value\n  "+sample.prefix+" next" {
				t.Fatalf("comment failed: %q", p.text.String())
			}
			p.key(key)
			if p.text.String() != "  value\n  next" {
				t.Fatal("uncomment failed")
			}
			p.text.Undo()
			if !strings.Contains(p.text.String(), sample.prefix) {
				t.Fatal("comment undo failed")
			}
		})
	}
}

func TestActiveProjectContext(t *testing.T) {
	m := newEditorApp()
	root := t.TempDir()
	a, b := filepath.Join(root, "a"), filepath.Join(root, "b")
	nested := filepath.Join(b, "nested")
	m.workspace = newFolderTree()
	for _, path := range []string{a, b, nested} {
		m.workspace.roots = append(m.workspace.roots, m.workspace.newProject(path))
	}
	m.addTab(document{path: filepath.Join(nested, "file.go")}, "")
	m.openDirectorySearch("")
	if m.directorySearch.root.Text() != nested || m.currentToolContext().project != nested {
		t.Fatal("active file did not select its innermost project")
	}
	m.addTab(document{path: filepath.Join(root, "outside.txt")}, "")
	m.openDirectorySearch("")
	if m.directorySearch.root.Text() != root || m.currentToolContext().project != "" {
		t.Fatal("outside file inherited an unrelated project")
	}
}

func TestLargeJSONRightEditSaveUndoAndSession(t *testing.T) {
	m := newEditorApp()
	path := filepath.Join(t.TempDir(), "large.json")
	value := "{\"text\":\"" + strings.Repeat("x", backgroundTextBytes) + "\"}"
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
	s := openTestSession(t, m)
	m.load(path)
	wait := func(ready func() bool) {
		t.Helper()
		if err := s.WaitFor(func(app.Snapshot) bool { return ready() }, 8*time.Second); err != nil {
			t.Fatal(err)
		}
	}
	wait(func() bool { return m.active != nil && m.active.resultReady && m.active.jsonPreview == nil })
	tab := m.active
	tab.result.focused = true
	tab.result.text.MoveTo(tab.result.text.RuneCount(), false)
	tab.result.text.Insert(" ")
	if !tab.modified() {
		t.Fatal("right edit was considered saved")
	}
	m.saveTab(tab, false)
	if tab.source.text.String() != value+" " || tab.jsonPreview == nil {
		t.Fatal("save did not capture right edit before deferred formatting")
	}
	wait(func() bool { return tab.savingText == nil && tab.jsonPreview == nil })
	saved, err := os.ReadFile(path)
	if err != nil || string(saved) != value+" " {
		t.Fatal("saved stale JSON", err)
	}
	tab.result.text.Insert("?")
	tab.updateResult()
	if !strings.HasSuffix(tab.source.text.String(), "?") {
		t.Fatal("incomplete JSON was lost")
	}
	tab.result.text.Undo()
	undone := tab.result.text.String()
	tab.updateResult()
	wait(func() bool { return tab.jsonPreview == nil })
	if strings.HasSuffix(tab.source.text.String(), "?") || tab.result.text.String() != undone || !tab.result.text.CanRedo() {
		t.Fatal("background preview broke undo/redo")
	}
	tab.result.text.Redo()
	tab.updateResult()
	if !strings.HasSuffix(tab.source.text.String(), "?") {
		t.Fatal("redo did not sync")
	}
	tab.result.text.Insert("draft")
	m.snapshotSession()
	if !strings.HasSuffix(tab.source.text.String(), "draft?") && !strings.Contains(tab.source.text.String(), "draft") {
		t.Fatal("session snapshot lost the pending right edit")
	}
}

func TestJSONPreviewEditPreservesSourceLayout(t *testing.T) {
	for _, source := range []string{`{"a":"中文 空格\\\"","b":12}`, " { \"a\" : 1, \"b\" : [true, null] }\n", "{\"a\":\"x\",\"b\":true}"} {
		preview, _, err := transformJSON(source, "格式化", 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, pair := range [][2]string{{`"a"`, `"长名字"`}, {`"b"`, `"b c"`}, {":", ":?"}} {
			edited := strings.Replace(preview, pair[0], pair[1], 1)
			value, ok := jsonPreviewEdit(source, preview, edited)
			want := strings.Replace(source, pair[0], pair[1], 1)
			if !ok || value != want {
				t.Fatalf("mapped edit: %q want %q (%v)", value, want, ok)
			}
		}
	}
	if _, ok := jsonPreviewEdit(`{"b":2,"a":1}`, `{"a":1,"b":2}`, `{"a":3,"b":2}`); ok {
		t.Fatal("mapped across reordered tokens")
	}
}
