package editor

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGitSelectedCommitPreservesUnselectedIndex(t *testing.T) {
	root, git := gitReviewFixture(t)
	write := func(name, value string) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("chosen.txt", "original\n")
	write("other.txt", "original\n")
	write("old name.txt", "rename\n")
	write("removed.txt", "delete\n")
	git("add", ".")
	git("commit", "-m", "base")
	write("other.txt", "staged only\n")
	git("add", "other.txt")
	write("other.txt", "working tree only\n")
	originalIndexEntry := git("ls-files", "--stage", "--", "other.txt")
	write("chosen.txt", "chosen change\n")
	write("目录/新 文件 [1].txt", "untracked\n")
	git("mv", "old name.txt", "renamed name.txt")
	if err := os.Remove(filepath.Join(root, "removed.txt")); err != nil {
		t.Fatal(err)
	}
	result := runProjectGit(context.Background(), root, gitCommitSelected, "selected", []string{"chosen.txt", "目录/新 文件 [1].txt", "old name.txt", "renamed name.txt", "removed.txt"}, nil)
	if result.fail != "" || !result.committed {
		t.Fatalf("selected commit failed: %+v", result)
	}
	if git("show", "HEAD:other.txt") != "original\n" || git("show", ":other.txt") != "staged only\n" {
		t.Fatal("unselected staged content leaked into commit or was lost")
	}
	if current := git("ls-files", "--stage", "--", "other.txt"); current != originalIndexEntry {
		t.Fatalf("unselected index entry changed: %q", current)
	}
	if value, _ := os.ReadFile(filepath.Join(root, "other.txt")); string(value) != "working tree only\n" {
		t.Fatal("unselected working file changed")
	}
	if git("show", "HEAD:chosen.txt") != "chosen change\n" || git("show", "HEAD:目录/新 文件 [1].txt") != "untracked\n" {
		t.Fatal("selected contents missing")
	}
	files := git("ls-tree", "-r", "--name-only", "HEAD")
	if strings.Contains(files, "old name.txt") || strings.Contains(files, "removed.txt") || !strings.Contains(files, "renamed name.txt") {
		t.Fatal("rename/deletion selection failed: ", files)
	}
	if len(result.status.changes) != 1 || result.status.changes[0].path != "other.txt" || result.status.changes[0].status != "MM" {
		t.Fatalf("status after selected commit: %+v", result.status.changes)
	}
	assertGitSelectionTemporaryFilesRemoved(t, root)
}

func TestGitSelectedCommitInitialAndAmend(t *testing.T) {
	root, git := gitReviewFixture(t)
	for _, name := range []string{"chosen.txt", "unselected.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	git("add", "unselected.txt")
	result := runProjectGit(context.Background(), root, gitCommitSelected, "initial selection", []string{"chosen.txt"}, nil)
	if result.fail != "" || !result.committed {
		t.Fatalf("initial selected commit failed: %+v", result)
	}
	if files := strings.TrimSpace(git("ls-tree", "-r", "--name-only", "HEAD")); files != "chosen.txt" {
		t.Fatalf("initial commit included unselected files: %q", files)
	}
	if index := strings.TrimSpace(git("diff", "--cached", "--name-only")); index != "unselected.txt" {
		t.Fatalf("unselected initial staging lost: %q", index)
	}
	tree := git("rev-parse", "HEAD^{tree}")
	result = runProjectGit(context.Background(), root, gitAmendSelected, "amended message", nil, nil)
	if result.fail != "" || !result.committed {
		t.Fatalf("message-only amend failed: %+v", result)
	}
	if git("rev-parse", "HEAD^{tree}") != tree || strings.TrimSpace(git("log", "-1", "--format=%s")) != "amended message" {
		t.Fatal("message-only amend included staged files")
	}
	if index := strings.TrimSpace(git("diff", "--cached", "--name-only")); index != "unselected.txt" {
		t.Fatal("amend lost unselected index entry")
	}
	assertGitSelectionTemporaryFilesRemoved(t, root)
}

func TestGitSelectedCommitFailurePreservesIndex(t *testing.T) {
	root, git := gitReviewFixture(t)
	path := filepath.Join(root, "chosen.txt")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-m", "base")
	if err := os.WriteFile(path, []byte("staged"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	if err := os.WriteFile(path, []byte("unstaged"), 0600); err != nil {
		t.Fatal(err)
	}
	beforeHead := git("rev-parse", "HEAD")
	beforeIndex, err := os.ReadFile(filepath.Join(root, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	hook := filepath.Join(root, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\necho 'fixture refuses commit' >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	result := runProjectGit(context.Background(), root, gitCommitSelected, "rejected", []string{"chosen.txt"}, nil)
	if result.fail == "" || result.committed {
		t.Fatalf("hook rejection did not fail: %+v", result)
	}
	afterIndex, err := os.ReadFile(filepath.Join(root, ".git", "index"))
	if err != nil || !bytes.Equal(beforeIndex, afterIndex) || git("rev-parse", "HEAD") != beforeHead {
		t.Fatal("failed commit changed real index or HEAD")
	}
	if value, _ := os.ReadFile(path); string(value) != "unstaged" {
		t.Fatal("failed commit changed worktree")
	}
	assertGitSelectionTemporaryFilesRemoved(t, root)
	for _, invalid := range []string{"", ".", "../outside", "bad\x00name", filepath.Join(root, "chosen.txt")} {
		if result := runProjectGit(context.Background(), root, gitCommitSelected, "invalid", []string{invalid}, nil); result.fail == "" || result.committed {
			t.Fatalf("invalid selection accepted: %q", invalid)
		}
	}
}

func assertGitSelectionTemporaryFilesRemoved(t *testing.T, root string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(root, ".git", "editorx-commit-index-*"))
	if err != nil || len(files) > 0 {
		t.Fatalf("temporary index leaked: %v, %v", files, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".git", "index.lock")); !os.IsNotExist(err) {
		t.Fatalf("index lock leaked: %v", err)
	}
}

func TestGitSelectedCommitPostCommitInterruption(t *testing.T) {
	root, git := gitReviewFixture(t)
	path := filepath.Join(root, "chosen.txt")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-m", "base")
	if err := os.WriteFile(path, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "hooks", "post-commit"), []byte("#!/bin/sh\n: > .git/editorx-post-commit-started\nsleep 5\n"), 0700); err != nil {
		t.Fatal(err)
	}
	executable, err := exec.LookPath("git.exe")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	started := filepath.Join(root, ".git", "editorx-post-commit-started")
	go func() {
		ticker := time.NewTicker(15 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := os.Stat(started); err == nil {
					cancel()
					return
				}
			}
		}
	}()
	committed, err := commitSelectedGitFiles(ctx, executable, root, "committed before interruption", []string{"chosen.txt"}, false, true)
	if !committed || !errors.Is(err, context.Canceled) {
		t.Fatalf("completed commit not recognized after hook interruption: committed=%v, err=%v", committed, err)
	}
	if !strings.Contains(err.Error(), "提交已完成") {
		t.Fatalf("post-commit error incorrectly suggests the commit failed: %v", err)
	}
	if git("show", "HEAD:chosen.txt") != "changed" || git("show", ":chosen.txt") != "changed" || strings.TrimSpace(git("log", "-1", "--format=%s")) != "committed before interruption" {
		t.Fatal("completed commit did not reconcile index")
	}
	assertGitSelectionTemporaryFilesRemoved(t, root)
}
