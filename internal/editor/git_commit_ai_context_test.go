package editor

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestGitCommitAIContextSelectedWorkingChanges(t *testing.T) {
	root, git := gitReviewFixture(t)
	write := func(name, value string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("edit.txt", "original line\n")
	write("deleted.txt", "deleted source\n")
	write("旧 名[1].txt", strings.Repeat("rename original\n", 20))
	write("private.txt", "unchanged secret\n")
	git("add", ".")
	git("commit", "-m", "original")
	write("edit.txt", "staged intermediate\n")
	write("private.txt", "UNSELECTED_SECRET_5837\n")
	git("add", "edit.txt", "private.txt")
	write("edit.txt", "saved working result\n")
	git("mv", "旧 名[1].txt", "新 名[2].txt")
	write("新 名[2].txt", strings.Repeat("rename original\n", 20)+"rename working addition\n")
	if err := os.Remove(filepath.Join(root, "deleted.txt")); err != nil {
		t.Fatal(err)
	}
	write("new.txt", "new selected content\n")
	write("image.bin", "\x00\x01binary secret bytes\x00")
	executable, _ := exec.LookPath("git.exe")
	status, err := readGitStatus(context.Background(), executable, root)
	if err != nil {
		t.Fatal(err)
	}
	var selected []gitChange
	for _, change := range status.changes {
		if change.path != "private.txt" {
			selected = append(selected, change)
		}
	}
	indexPath := filepath.Join(root, ".git", "index")
	before, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	value, err := readGitCommitAIContext(context.Background(), root, selected)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"-original line", "+saved working result", "-deleted source", "旧 名[1].txt", "新 名[2].txt", "rename working addition", "new selected content", "Binary or unsupported"} {
		if !strings.Contains(value, want) {
			t.Errorf("missing selected context %q", want)
		}
	}
	for _, secret := range []string{"UNSELECTED_SECRET", "private.txt", "staged intermediate", "binary secret bytes", root} {
		if strings.Contains(value, secret) {
			t.Errorf("context unexpectedly includes %q", secret)
		}
	}
	after, err := os.ReadFile(indexPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("context generation changed the Git index")
	}
	data, err := os.ReadFile(filepath.Join(root, "edit.txt"))
	if err != nil || string(data) != "saved working result\n" {
		t.Fatal("context generation changed a working file")
	}
	if len(value) > gitCommitAIContextLimit {
		t.Fatal("context exceeded its size limit")
	}
}

func TestGitCommitAIContextInitialAndLimits(t *testing.T) {
	root, git := gitReviewFixture(t)
	name := "first.txt"
	if err := os.WriteFile(filepath.Join(root, name), []byte("first commit\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", name)
	value, err := readGitCommitAIContext(context.Background(), root, []gitChange{{status: "A ", path: name}})
	if err != nil || !strings.Contains(value, "first commit") {
		t.Fatalf("initial repository context: %s, %v", value, err)
	}
	for _, names := range [][]gitChange{nil, {{status: "??", path: "../outside.txt"}}, {{status: "??", path: ".git/config"}}, {{status: "UU", path: name}}, make([]gitChange, gitCommitAIFileCount+1)} {
		if _, err := readGitCommitAIContext(context.Background(), root, names); err == nil {
			t.Fatalf("accepted invalid selection: %+v", names)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "large.txt"), bytes.Repeat([]byte("a"), gitCommitAIContextLimit+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readGitCommitAIContext(context.Background(), root, []gitChange{{status: "??", path: "large.txt"}}); err == nil {
		t.Fatal("oversized added content was silently truncated")
	}
	git("commit", "-m", "first")
	if _, err := readGitCommitAIContext(context.Background(), root, []gitChange{{status: " M", path: name}}); err == nil {
		t.Fatal("unchanged saved file should not produce a message context")
	}
	if err := os.WriteFile(filepath.Join(root, name), bytes.Repeat([]byte("a"), gitCommitAIContextLimit+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readGitCommitAIContext(context.Background(), root, []gitChange{{status: " M", path: name}}); err == nil || !strings.Contains(err.Error(), "96 KiB") {
		t.Fatalf("oversized tracked diff should explain the context limit: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := readGitCommitAIContext(ctx, root, []gitChange{{status: "??", path: name}}); err == nil {
		t.Fatal("cancellation was ignored")
	}
	t.Run("link does not expose target", func(t *testing.T) {
		outside := filepath.Join(t.TempDir(), "outside.txt")
		if err := os.WriteFile(outside, []byte("EXTERNAL_SECRET"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(root, "link.txt")); err != nil {
			t.Skipf("Windows symlinks unavailable: %v", err)
		}
		if value, err := readGitCommitAIContext(context.Background(), root, []gitChange{{status: "??", path: "link.txt"}}); err == nil || strings.Contains(value, "EXTERNAL_SECRET") {
			t.Fatal("linked file exposed target content")
		}
	})
}

func TestGitCommitAIContextBoundsDeletedBlob(t *testing.T) {
	root, git := gitReviewFixture(t)
	name := filepath.Join(root, "old-large.txt")
	if err := os.WriteFile(name, bytes.Repeat([]byte("a"), gitCommitAIFileLimit+1), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-m", "large baseline")
	if err := os.Remove(name); err != nil {
		t.Fatal(err)
	}
	if _, err := readGitCommitAIContext(context.Background(), root, []gitChange{{status: " D", path: "old-large.txt"}}); err == nil || !strings.Contains(err.Error(), "8 MiB") {
		t.Fatalf("large deleted blob was not rejected before diff: %v", err)
	}
}

func TestGitCommitAIContextRejectsDirectoryPathspec(t *testing.T) {
	for _, directoryBefore := range []bool{false, true} {
		t.Run(strconv.FormatBool(directoryBefore), func(t *testing.T) {
			root, git := gitReviewFixture(t)
			path := filepath.Join(root, "replaced")
			secretPath := filepath.Join(path, "private.txt")
			if directoryBefore {
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(secretPath, []byte("UNSELECTED_DIRECTORY_SECRET"), 0600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, []byte("old file"), 0600); err != nil {
				t.Fatal(err)
			}
			git("add", ".")
			git("commit", "-m", "before replacement")
			status := " D"
			if directoryBefore {
				if err := os.Remove(secretPath); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("selected new file"), 0600); err != nil {
					t.Fatal(err)
				}
				status = "A "
			} else {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(secretPath, []byte("UNSELECTED_DIRECTORY_SECRET"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			git("add", "replaced")
			value, err := readGitCommitAIContext(context.Background(), root, []gitChange{{status: status, path: "replaced"}})
			if err == nil || strings.Contains(value, "UNSELECTED_DIRECTORY_SECRET") {
				t.Fatal("directory pathspec included unselected children")
			}
		})
	}
}

func TestGitCommitAIContextRejectsForcedBinaryDiff(t *testing.T) {
	root, git := gitReviewFixture(t)
	if err := os.WriteFile(filepath.Join(root, ".gitattributes"), []byte("image.bin diff\n"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "image.bin")
	if err := os.WriteFile(path, []byte("\x00old binary\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-m", "binary baseline")
	if err := os.WriteFile(path, []byte("\x00new binary\n"), 0600); err != nil {
		t.Fatal(err)
	}
	value, err := readGitCommitAIContext(context.Background(), root, []gitChange{{status: " M", path: "image.bin"}})
	if err == nil || strings.Contains(value, "new binary") {
		t.Fatal("forced binary diff should not send raw contents")
	}
}
