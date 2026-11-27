package editor

import (
	"context"
	"github.com/vivaui/viva/headless"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"weak"

	"github.com/vivaui/viva/app"
)

func TestGitWatchAndConflictResolution(t *testing.T) {
	root, git := gitReviewFixture(t)
	path := filepath.Join(root, "conflict.txt")
	write := func(value string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("base\n")
	git("add", ".")
	git("commit", "-m", "base")
	git("switch", "-c", "incoming")
	write("theirs\n")
	git("commit", "-am", "incoming")
	git("switch", "main")
	write("ours\n")
	git("commit", "-am", "ours")
	exe, _ := exec.LookPath("git.exe")
	if _, err := runGitCommand(context.Background(), exe, root, "merge", "incoming"); err == nil {
		t.Fatal("fixture should conflict")
	}
	m := newEditorApp()
	m.watchFiles = true
	m.openProjectGit(root)
	s := openTestSession(t, m)
	if err := s.WaitFor(func(app.Snapshot) bool {
		return m.tools.git.request == nil && m.tools.git.watch.working != nil && m.tools.git.watch.metadata != nil
	}, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if len(m.tools.git.snapshot.changes) != 1 || !m.tools.git.snapshot.changes[0].conflicted() {
		t.Fatal("missing conflict status")
	}
	m.openGitConflict(m.tools.git.snapshot.changes[0])
	s.Frame()
	if err := s.WaitFor(func(app.Snapshot) bool { return m.tools.git.request == nil && m.tools.git.watch.delay == nil }, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("button:采用传入分支"); err != nil {
		t.Fatal(err)
	}
	if m.active.source.text.String() != "theirs\n" {
		t.Fatal(m.active.source.text.String())
	}
	if err := s.ClickLabel("button:保存并标记已解决"); err != nil {
		t.Fatal(err)
	}
	if err := s.WaitFor(func(app.Snapshot) bool { return m.tools.git.request == nil && m.tools.git.snapshot.hasStaged() }, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if git("ls-files", "-u") != "" || git("show", ":conflict.txt") != "theirs\n" {
		t.Fatal("resolution did not stage")
	}
	git("commit", "-m", "resolved")
	if err := s.WaitFor(func(app.Snapshot) bool { return m.tools.git.request == nil && len(m.tools.git.snapshot.changes) == 0 }, 10*time.Second); err != nil {
		t.Fatal("external commit status not refreshed:", err)
	}
	write("external\n")
	if err := s.WaitFor(func(app.Snapshot) bool { return len(m.tools.git.snapshot.changes) > 0 && m.tools.git.request == nil }, 10*time.Second); err != nil {
		t.Fatal("working change not refreshed:", err)
	}
	m.tools.gitOpen = false
	s.Frame()
	if m.tools.git.watch.working != nil || m.tools.git.watch.metadata != nil {
		t.Fatal("hidden Git panel retained watches")
	}
}

func TestEditingDepthReleasesClosedBuffers(t *testing.T) {
	m := newEditorApp()
	s := openTestSession(t, m)
	closed := openAndCloseDepthFixture(m, s)
	for i := 0; i < 6; i++ {
		s.Frame()
	}
	s.Driver().Advance(time.Second)
	runtime.GC()
	if closed.Value() != nil {
		t.Fatal("closed tab retained by new editing workflow")
	}
	if m.tools.git.conflict != nil || m.editorSplit != nil {
		t.Fatal("workflow kept closed pane")
	}
}

//go:noinline
func openAndCloseDepthFixture(m *editorApp, s *headless.Session) weak.Pointer[editorTab] {
	tab := m.addTab(document{saved: strings.Repeat("long identifier\n", 10000)}, "memory.go")
	m.openEditorSplit(tab)
	s.Frame()
	tab.source.openCompletion()
	s.Frame()
	m.tools.git.conflict = tab
	closed := weak.Make(tab)
	m.removeTab(tab)
	return closed
}
