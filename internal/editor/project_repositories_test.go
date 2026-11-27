package editor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
)

func TestWorkspaceGitRepositoriesAndFileScope(t *testing.T) {
	project := t.TempDir()
	first := filepath.Join(project, "first")
	second := filepath.Join(project, "group", "second")
	git := makeGitProject(t, first)
	makeGitProject(t, second)
	firstFile, secondFile := filepath.Join(first, "change.txt"), filepath.Join(second, "change.txt")
	const source = "unstaged\n"
	for _, path := range []string{firstFile, secondFile} {
		if err := os.WriteFile(path, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	linked := filepath.Join(project, "group", "linked")
	if err := os.MkdirAll(linked, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(linked, ".git"), []byte("gitdir: elsewhere\n"), 0600); err != nil {
		t.Fatal(err)
	}
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	m.addProjects(project)
	m.load(secondFile)
	s := openTestSession(t, m)
	m.useTool("Git")
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(s.Frame())
	check(s.WaitFor(func(app.Snapshot) bool {
		return m.tools.repositories.scan == nil && m.tools.git.request == nil && m.tools.git.snapshot.root == second
	}, 12*time.Second))
	if paths := m.gitProjects(); !slices.Equal(paths, []string{first, linked, second}) {
		t.Fatalf("Git choices = %q", paths)
	}
	if m.toolDirectory() != project {
		t.Fatalf("terminal directory changed: %s", m.toolDirectory())
	}
	check(s.ClickLabel("combobox:Git 项目"))
	check(s.ClickLabel("option:" + m.gitRepositoryLabel(first)))
	check(s.WaitFor(func(app.Snapshot) bool {
		return m.tools.git.request == nil && m.tools.git.snapshot.root == first
	}, 12*time.Second))
	check(s.ClickLabel("combobox:Git 项目"))
	check(s.ClickLabel("option:" + m.gitRepositoryLabel(second)))
	check(s.WaitFor(func(app.Snapshot) bool {
		return m.tools.git.request == nil && m.tools.git.snapshot.root == second
	}, 12*time.Second))
	m.startProjectGitScope(first, projectGitScope{path: firstFile, action: gitStage})
	check(s.Frame())
	if err := s.WaitFor(func(app.Snapshot) bool {
		return m.tools.git.request == nil && m.tools.git.snapshot.root == first
	}, 12*time.Second); err != nil {
		t.Fatalf("%v: selected=%q repo=%q action=%q issue=%q", err, m.tools.git.directory, m.tools.git.snapshot.root, m.tools.git.requestAction, m.tools.git.issue)
	}
	for _, test := range []struct{ root, want string }{{first, "change.txt\n"}, {second, ""}} {
		staged, err := runGitCommand(context.Background(), git, test.root, "diff", "--cached", "--name-only")
		check(err)
		if staged != test.want {
			t.Fatalf("staged in %s = %q, want %q", test.root, staged, test.want)
		}
	}
	if m.gitRepositoryAt(secondFile) != second || m.tools.git.directory != first {
		t.Fatal("file routing or the selected repository changed unexpectedly")
	}
}

func makeGitProject(t *testing.T, root string) string {
	t.Helper()
	git, err := exec.LookPath("git.exe")
	if err != nil {
		t.Skip("Git for Windows is unavailable")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := runGitCommand(context.Background(), git, root, "init", "-q"); err != nil {
		t.Fatal(err)
	}
	return git
}
