package editor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
)

func gitReviewFixture(t *testing.T) (string, func(...string) string) {
	t.Helper()
	exe, err := exec.LookPath("git.exe")
	if err != nil {
		t.Skip("Git not installed")
	}
	root := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		value, err := runGitCommand(ctx, exe, root, args...)
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		return value
	}
	git("init", "--initial-branch=main")
	git("config", "user.name", "EditorX Fixture")
	git("config", "user.email", "fixture@example.invalid")
	git("config", "core.autocrlf", "false")
	return root, git
}

func TestGitConflictKeepsUndoBudget(t *testing.T) {
	m := newEditorApp()
	value := "<<<<<<< HEAD\n" + strings.Repeat("a", historyMergeUndoRunes/2) + "\n=======\n" + strings.Repeat("b", historyMergeUndoRunes/2) + "\n>>>>>>> incoming\n"
	tab := m.addTab(document{path: filepath.Join(t.TempDir(), "conflict.txt"), saved: value}, "conflict.txt")
	m.tools.git.conflict = tab
	page := ui.NewPage(ui.PageConfig{View: func(c ui.C) { m.gitConflictToolbar(c, false) }, Theme: &m.theme})
	s, err := headless.Open(ui.Configure(page, app.Config{Width: 1200, Height: 100}), nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.ClickLabel("button:采用当前分支"); err != nil {
		t.Fatal(err)
	}
	if tab.source.text.String() != value || !strings.Contains(m.tools.git.issue, "可撤销上限") {
		t.Fatal("oversized conflict merge lost undo safety")
	}
}

func TestGitReviewPartialStageHistory(t *testing.T) {
	root, git := gitReviewFixture(t)
	path := filepath.Join(root, "有 空格.txt")
	var original strings.Builder
	for i := 0; i < 60; i++ {
		fmt.Fprintf(&original, "line %d\n", i)
	}
	write := func(value string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(original.String())
	git("add", ".")
	git("commit", "-m", "first")
	changed := strings.Replace(original.String(), "line 3\n", "changed first\n", 1)
	changed = strings.Replace(changed, "line 49\n", "changed second\n", 1)
	write(changed)
	ctx := context.Background()
	data, err := readGitReview(ctx, root, gitChange{status: " M", path: "有 空格.txt"}, false, "", "")
	if err != nil || len(data.hunks) != 2 {
		t.Fatalf("review hunks=%d error=%v", len(data.hunks), err)
	}
	if err := applyGitHunk(ctx, data, 0); err != nil {
		t.Fatal(err)
	}
	index := git("show", ":有 空格.txt")
	if !strings.Contains(index, "changed first") || strings.Contains(index, "changed second") {
		t.Fatalf("wrong index: %s", index)
	}
	if err := applyGitHunk(ctx, data, 1); err == nil {
		t.Fatal("stale diff was accepted")
	}
	staged, err := readGitReview(ctx, root, gitChange{status: "MM", path: "有 空格.txt"}, true, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := applyGitHunk(ctx, staged, 0); err != nil {
		t.Fatal(err)
	}
	if git("show", ":有 空格.txt") != original.String() {
		t.Fatal("unstage changed wrong lines")
	}
	onDisk, _ := os.ReadFile(path)
	if string(onDisk) != changed {
		t.Fatal("staging modified working file")
	}
	git("add", ".")
	git("commit", "-m", "second\n\nbody")
	commits, err := readGitLog(ctx, root)
	if err != nil || len(commits) != 2 {
		t.Fatalf("log %v %v", commits, err)
	}
	for _, commit := range commits {
		files, err := readGitCommitFiles(ctx, root, commit.hash)
		if err != nil || len(files.changes) != 1 {
			t.Fatalf("files %v %v", files, err)
		}
		review, err := readGitReview(ctx, root, files.changes[0], false, commit.hash, files.parent)
		if err != nil || len(review.comparison.diff.blocks) == 0 {
			t.Fatalf("commit diff %v", err)
		}
	}
	git("mv", "有 空格.txt", "renamed.txt")
	rename, err := readGitReview(ctx, root, gitChange{status: "R ", path: "renamed.txt", original: "有 空格.txt"}, true, "", "")
	if err != nil || rename.comparison.old.saved != rename.comparison.current.saved {
		t.Fatalf("rename %v", err)
	}
}

func TestGitReviewUIAndConflictUndo(t *testing.T) {
	root, git := gitReviewFixture(t)
	path := filepath.Join(root, "sample.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-m", "first")
	if err := os.WriteFile(path, []byte("one\nchanged\n"), 0600); err != nil {
		t.Fatal(err)
	}
	m := newEditorApp()
	m.load(path)
	m.openProjectGit(root)
	session := openTestSession(t, m)
	if err := session.WaitFor(func(app.Snapshot) bool { return m.tools.git.request == nil }, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	m.openGitReview(gitChange{status: " M", path: "sample.txt"}, false)
	session.Frame()
	if err := session.WaitFor(func(app.Snapshot) bool { return m.tools.git.review.read == nil }, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if m.tools.git.review.problem != "" {
		t.Fatal(m.tools.git.review.problem)
	}
	if directory := os.Getenv("VIVA_PREVIEW_DIR"); directory != "" {
		session.Screenshot(filepath.Join(directory, "git-diff.png"))
	}
	if err := session.ClickLabel("button:暂存此块"); err != nil {
		t.Fatal(err)
	}
	if err := session.WaitFor(func(app.Snapshot) bool {
		return m.tools.git.review.apply == nil && m.tools.git.review.read == nil && m.tools.git.request == nil
	}, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(git("show", ":sample.txt"), "changed") {
		t.Fatal("UI partial staging failed")
	}
	m.tools.git.review = nil
	m.openGitLog()
	session.Frame()
	if err := session.WaitFor(func(app.Snapshot) bool {
		s := m.tools.git.review
		return s.log == nil && s.files == nil && s.read == nil
	}, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if len(m.tools.git.review.changes) == 0 || m.tools.git.review.data != nil {
		t.Fatal("history should load commit details without eagerly computing a diff")
	}
	if err := session.ClickLabel("button:查看提交文件 sample.txt"); err != nil {
		t.Fatal(err)
	}
	if err := session.WaitFor(func(app.Snapshot) bool { return m.tools.git.review.read == nil }, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if m.tools.git.review.data == nil {
		t.Fatal("selected history file did not load its diff")
	}
	if err := session.ClickLabel("button:返回提交记录"); err != nil {
		t.Fatal(err)
	}
	m.tools.git.review = nil
	conflict := "before\n<<<<<<< HEAD\nours\n||||||| base\nbase\n=======\ntheirs\n>>>>>>> other\nafter\n"
	tab := m.active
	tab.source.replace(conflict)
	m.tools.git.conflict = tab
	session.Frame()
	if err := session.ClickLabel("button:保留双方"); err != nil {
		t.Fatal(err)
	}
	if tab.source.text.String() != "before\nours\ntheirs\nafter\n" {
		t.Fatal(tab.source.text.String())
	}
	if !tab.source.text.Undo() || tab.source.text.String() != conflict {
		t.Fatal("conflict choice not undoable")
	}
	if len(gitConflictBlocks(conflict)) != 1 {
		t.Fatal("diff3 conflict parsing")
	}
}
