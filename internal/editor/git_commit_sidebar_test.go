package editor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
)

func TestGitCommitSidebarGroupingAndTerminal(t *testing.T) {
	t.Cleanup(func() { setUILanguage(menuChinese) })
	root, git := gitReviewFixture(t)
	remote := t.TempDir()
	exe, err := exec.LookPath("git.exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runGitCommand(context.Background(), exe, remote, "init", "--bare", "--initial-branch=main"); err != nil {
		t.Fatal(err)
	}
	git("remote", "add", "origin", remote)
	write := func(path, content string) {
		t.Helper()
		name := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(".gitignore", "ignored.tmp\n")
	write("module/module/edited.txt", "original\n")
	write("deleted.txt", "original\n")
	git("add", ".")
	git("commit", "-m", "original")
	write("module/module/edited.txt", "changed\n")
	if err := os.Remove(filepath.Join(root, "deleted.txt")); err != nil {
		t.Fatal(err)
	}
	write("new.txt", "new\n")
	write("ignored.tmp", "ignored\n")
	m := newEditorApp()
	m.addTab(document{saved: "editor\n"}, "sample.txt")
	m.openProjectGit(root)
	m.tools.terminalOpen = true
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(s.Resize(1280, 820))
	check(s.WaitFor(func(app.Snapshot) bool { return m.tools.git.request == nil && len(m.tools.git.snapshot.changes) == 3 }, 10*time.Second))
	for _, label := range []string{"textfield:代码编辑区", "button:关闭 Git 侧栏", "button:收起底部面板"} {
		_, err := s.Locate(label)
		check(err)
	}
	for _, label := range []string{"更改", "已删除", "未加入 Git", "仓库根目录"} {
		if !s.HasText(label) {
			t.Fatalf("missing change group %q", label)
		}
	}
	check(s.ClickLabel("button:收起底部面板"))
	if !m.tools.gitOpen || m.tools.terminalOpen {
		t.Fatal("closing the terminal changed the Git sidebar")
	}
	if m.tools.git.groupRepository || !s.HasText("module/module") {
		t.Fatal("default tree should compact directories and avoid repeated repository levels")
	}
	check(s.Resize(900, 560))
	for _, language := range []menuLanguage{menuChinese, menuEnglish} {
		setUILanguage(language)
		m.menuLanguage = language
		check(s.Frame())
		for _, label := range []string{"提交变更", "搁置", "贮藏", "关闭 Git 侧栏", "切换分支", "新建分支", "获取", "拉取", "推送", "全选变更", "分组与显示"} {
			_, err := s.Locate("button:" + uiText(label))
			check(err)
		}
		if output := os.Getenv("VIVA_PREVIEW_DIR"); output != "" {
			check(s.Screenshot(filepath.Join(output, "git-small-"+string(language)+".png")))
		}
	}
	setUILanguage(menuChinese)
	m.menuLanguage = menuChinese
	check(s.Resize(1280, 820))
	check(s.Frame())
	if output := os.Getenv("VIVA_PREVIEW_DIR"); output != "" {
		check(s.Screenshot(filepath.Join(output, "git-changes-dark.png")))
		m.setLightMode(true)
		check(s.Frame())
		check(s.Screenshot(filepath.Join(output, "git-changes-light.png")))
		m.setLightMode(false)
		check(s.Frame())
	}
	check(s.ClickLabel("checkbox:选择提交 · module/module"))
	if m.tools.git.request != nil || len(m.tools.git.selectedCommitPaths()) != 1 {
		t.Fatal("selection must update immediately without a Git request")
	}
	if staged := strings.TrimSpace(git("diff", "--cached", "--name-only")); staged != "" {
		t.Fatalf("checkbox changed the Git index: %s", staged)
	}
	check(s.ClickLabel("checkbox:选择提交 · module/module"))
	if m.tools.git.request != nil || len(m.tools.git.selectedCommitPaths()) != 0 {
		t.Fatal("deselection launched Git or retained the path")
	}
	check(s.ClickLabel("button:分组与显示"))
	check(s.ClickLabel("menuitem:按模块分组"))
	check(s.ClickLabel("button:分组与显示"))
	check(s.ClickLabel("menuitem:显示忽略文件"))
	check(s.WaitFor(func(app.Snapshot) bool { return m.tools.git.request == nil && s.HasText("已忽略") }, 10*time.Second))
	check(s.ClickLabel("button:折叠全部变更"))
	if s.HasText("edited.txt") {
		t.Fatal("collapsed tree still showed files")
	}
	check(s.ClickLabel("button:展开全部变更"))
	check(s.ClickLabel("button:全选变更"))
	if m.tools.git.request != nil || len(m.tools.git.selectedCommitPaths()) != 3 || git("diff", "--cached", "--name-only") != "" {
		t.Fatal("select all changed the index or selected ignored files")
	}
	if s.HasText("勾选文件以加入提交") || s.HasText("提交所选文件的全部已保存改动") || s.HasText("已选择 3 个文件") {
		t.Fatal("commit composer still shows the removed helper rows")
	}
	check(s.ClickLabel("button:刷新 Git 状态"))
	check(s.WaitFor(func(app.Snapshot) bool { return m.tools.git.request == nil }, 10*time.Second))
	if len(m.tools.git.selectedCommitPaths()) != 3 {
		t.Fatal("refresh discarded the selected files")
	}
	if output := os.Getenv("VIVA_PREVIEW_DIR"); output != "" {
		check(s.Screenshot(filepath.Join(output, "git-commit-sidebar.png")))
	}
	m.tools.git.commit.SetText("updated")
	check(s.Frame())
	check(s.ClickLabel("button:提交并推送"))
	check(s.WaitFor(func(app.Snapshot) bool { return m.tools.git.request == nil && !m.tools.git.snapshot.hasStaged() }, 10*time.Second))
	if strings.TrimSpace(git("log", "-1", "--format=%s")) != "updated" {
		t.Fatal("the commit sidebar did not commit the staged changes")
	}
	message, err := runGitCommand(context.Background(), exe, remote, "log", "-1", "--format=%s")
	if err != nil || strings.TrimSpace(message) != "updated" {
		t.Fatalf("commit was not pushed to the configured local remote: %q, %v", message, err)
	}
}

func TestGitSidebarKeepsPartialStageExplicit(t *testing.T) {
	root, git := gitReviewFixture(t)
	name := filepath.Join(root, "partial.txt")
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(name, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("base\n")
	git("add", ".")
	git("commit", "-m", "base")
	write("staged\n")
	git("add", "partial.txt")
	write("staged\nunstaged\n")
	m := newEditorApp()
	m.openProjectGit(root)
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(s.WaitFor(func(app.Snapshot) bool { return m.tools.git.request == nil && len(m.tools.git.snapshot.changes) == 1 }, 10*time.Second))
	if len(m.tools.git.selectedCommitPaths()) != 0 {
		t.Fatal("a partial stage must not automatically select the entire file")
	}
	m.tools.git.commit.SetText("staged only")
	check(s.Frame())
	check(s.ClickLabel("button:更多提交操作"))
	check(s.ClickLabel("menuitem:仅提交暂存区"))
	check(s.WaitFor(func(app.Snapshot) bool { return m.tools.git.request == nil && m.tools.git.commit.Text() == "" }, 10*time.Second))
	if got := git("show", "HEAD:partial.txt"); got != "staged\n" {
		t.Fatalf("staged-only UI committed additional content: %q", got)
	}
	if data, err := os.ReadFile(name); err != nil || string(data) != "staged\nunstaged\n" {
		t.Fatalf("staged-only UI modified the working file: %q, %v", data, err)
	}
}

func TestProjectGitShelveAndAmend(t *testing.T) {
	root, git := gitReviewFixture(t)
	path := filepath.Join(root, "sample.txt")
	write := func(value string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("original\n")
	git("add", ".")
	git("commit", "-m", "first")
	run := func(action gitAction, input string) gitResult {
		t.Helper()
		result := runProjectGit(context.Background(), root, action, input, nil, nil)
		if result.fail != "" {
			t.Fatalf("%s failed: %s", action, result.fail)
		}
		return result
	}
	write("shelved\n")
	run(gitStashSave, gitShelfPrefix+"fixture")
	entries := run(gitListStashes, "").items
	if len(entries) != 3 || !strings.Contains(entries[2], gitShelfPrefix+"fixture") || git("status", "--porcelain") != "" {
		t.Fatalf("shelving failed: %q", entries)
	}
	input := entries[0] + "\x00" + entries[1]
	write("another change\n")
	run(gitStashSave, "other entry")
	if result := runProjectGit(context.Background(), root, gitStashPop, input, nil, nil); result.fail == "" {
		t.Fatal("stale stash selection was accepted")
	}
	entries = run(gitListStashes, "").items
	if len(entries) != 6 {
		t.Fatalf("missing known shelf entry in %q", entries)
	}
	run(gitStashPop, entries[3]+"\x00"+entries[4])
	if data, err := os.ReadFile(path); err != nil || string(data) != "shelved\n" || len(run(gitListStashes, "").items) != 3 {
		t.Fatalf("popping the shelf entry failed: %q, %v", data, err)
	}
	git("add", ".")
	run(gitAmend, "amended")
	if messages := run(gitListMessages, "").items; len(messages) != 1 || strings.TrimSpace(messages[0]) != "amended" {
		t.Fatalf("amended commit history = %q", messages)
	}
	m := newEditorApp()
	m.openProjectGit(root)
	s := openTestSession(t, m)
	if err := s.WaitFor(func(app.Snapshot) bool { return m.tools.git.request == nil && m.tools.git.snapshot.root != "" }, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("button:贮藏"); err != nil {
		t.Fatal(err)
	}
	if err := s.WaitFor(func(app.Snapshot) bool { return m.tools.git.request == nil && s.HasText("On main: other entry") }, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("button:提交变更"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Locate("textfield:提交说明"); err != nil {
		t.Fatal(err)
	}
}
