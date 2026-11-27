package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
)

func TestGitPanelUsesOpenedProjects(t *testing.T) {
	project, _ := gitReviewFixture(t)
	loose, _ := gitReviewFixture(t)
	file := filepath.Join(loose, "loose.txt")
	if err := os.WriteFile(file, []byte("loose"), 0600); err != nil {
		t.Fatal(err)
	}
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	m.load(file)
	s := openTestSession(t, m)
	m.useTool("Git")
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if m.tools.git.directory != "" || m.tools.git.request != nil || !s.HasText("添加项目后可查看 Git 状态") {
		t.Fatal("a loose file opened its enclosing Git repository")
	}
	m.addProjects(project)
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	waitProjectLoads(t, m, s)
	if err := s.WaitFor(func(app.Snapshot) bool {
		return strings.EqualFold(m.tools.git.directory, project) && m.tools.git.request == nil
	}, 10*time.Second); err != nil {
		t.Fatalf("%v: Git open=%t, project=%q repository=%q problem=%q", err, m.tools.gitOpen, m.tools.git.directory, m.tools.git.snapshot.root, m.tools.git.issue)
	}
	if !strings.EqualFold(m.tools.git.snapshot.root, project) || len(m.gitProjects()) != 1 {
		t.Fatalf("wrong project or choices: %q, %v", m.tools.git.snapshot.root, m.gitProjects())
	}
	if m.toolDirectory() != loose {
		t.Fatal("the terminal lost its loose-file working directory")
	}
	m.removeProject(m.workspace.roots[0])
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if m.tools.git.directory != "" || m.tools.git.snapshot.root != "" || !s.HasText("添加项目后可查看 Git 状态") {
		t.Fatal("removed project is still shown in Git")
	}
}

func TestGitCommitChecksDiskAndDraft(t *testing.T) {
	root, git := gitReviewFixture(t)
	path := filepath.Join(root, "example.txt")
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(os.WriteFile(path, []byte("base"), 0600))
	git("add", ".")
	git("commit", "-m", "base")
	head := git("rev-parse", "HEAD")
	check(os.WriteFile(path, []byte("saved"), 0600))
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	m.load(path)
	m.openProjectGit(root)
	s := openTestSession(t, m)
	wait := func() {
		t.Helper()
		check(s.WaitFor(func(app.Snapshot) bool { return m.tools.git.request == nil }, 12*time.Second))
	}
	wait()
	tab := m.active
	check(s.ClickLabel("checkbox:选择提交 · example.txt"))
	m.tools.git.commit.SetText("disk check")
	tab.source.replace("draft")
	check(s.Frame())
	check(s.ClickLabel("button:提交"))
	if !s.HasText("有未保存的内容") || m.tools.git.request != nil || git("rev-parse", "HEAD") != head {
		t.Fatal("the commit did not stop on an unsaved draft")
	}
	tab.source.replace("saved")
	m.watchFiles = false
	check(os.WriteFile(path, []byte("external"), 0600))
	check(s.Frame())
	check(s.ClickLabel("button:提交"))
	wait()
	if !s.HasText("磁盘内容已变化") || git("rev-parse", "HEAD") != head {
		t.Fatalf("the commit did not stop on stale disk content: %q", m.tools.git.commitProblem)
	}
	m.watchFiles, m.checkFiles = true, true
	check(s.Frame())
	check(s.WaitFor(func(app.Snapshot) bool {
		return tab.diskChange != nil && tab.diskChange.changed
	}, 10*time.Second))
	if !s.HasText("磁盘文件已变化，编辑内容已保留") {
		t.Fatal("the disk change warning is missing")
	}
	check(s.ClickLabel("button:重新读取"))
	check(s.WaitFor(func(app.Snapshot) bool { return tab.doc.saved == "external" }, 10*time.Second))
	check(s.ClickLabel("button:提交"))
	// A real commit starts several Git processes. The composer clearing marks
	// completion even if a file watcher has already queued a status refresh.
	check(s.WaitFor(func(app.Snapshot) bool {
		return m.tools.git.commit.Text() == "" || m.tools.git.request == nil
	}, 30*time.Second))
	if git("show", "HEAD:example.txt") != "external" || m.tools.git.commit.Text() != "" {
		t.Fatalf("reloaded file was not committed: %q %q", m.tools.git.issue, m.tools.git.commitProblem)
	}
}

func TestGitCommitChecksLiveUpstream(t *testing.T) {
	root, git := gitReviewFixture(t)
	remote, remoteGit := gitReviewFixture(t)
	path := filepath.Join(root, "example.txt")
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	remoteGit("config", "receive.denyCurrentBranch", "ignore")
	check(os.WriteFile(path, []byte("base"), 0600))
	git("add", ".")
	git("commit", "-m", "base")
	base := strings.TrimSpace(git("rev-parse", "HEAD"))
	git("remote", "add", "origin", remote)
	git("push", "-u", "origin", "main")
	tree := strings.TrimSpace(git("rev-parse", "HEAD^{tree}"))
	newHead := strings.TrimSpace(git("commit-tree", tree, "-p", base, "-m", "remote-only commit"))
	git("push", "origin", newHead+":refs/heads/main")
	git("update-ref", "refs/remotes/origin/main", base)
	if strings.TrimSpace(git("rev-parse", "origin/main")) != base {
		t.Fatal("stale tracking ref was not established")
	}
	check(os.WriteFile(path, []byte("local"), 0600))
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	m.openProjectGit(root)
	s := openTestSession(t, m)
	check(s.WaitFor(func(app.Snapshot) bool { return m.tools.git.request == nil }, 12*time.Second))
	check(s.ClickLabel("checkbox:选择提交 · example.txt"))
	m.tools.git.commit.SetText("local after warning")
	check(s.Frame())
	check(s.ClickLabel("button:提交并推送"))
	check(s.WaitFor(func(app.Snapshot) bool {
		return m.tools.git.commitAttempt != nil && m.tools.git.commitAttempt.open
	}, 15*time.Second))
	if !s.HasText("本地分支尚未包含远程提交") || strings.TrimSpace(git("rev-parse", "HEAD")) != base {
		t.Fatal("the remote advanced without a warning")
	}
	check(s.ClickLabel("button:仅提交到本地"))
	check(s.WaitFor(func(app.Snapshot) bool { return m.tools.git.request == nil }, 12*time.Second))
	if git("show", "HEAD:example.txt") != "local" || strings.TrimSpace(remoteGit("rev-parse", "HEAD")) != newHead {
		t.Fatalf("local-only approval changed the remote or failed: %q", m.tools.git.issue)
	}
}
