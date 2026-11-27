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
)

func TestGitLogFiltersPaginationAndUI(t *testing.T) {
	root, git := gitReviewFixture(t)
	ctx := context.Background()
	empty, err := readGitLogPage(ctx, root, gitLogQuery{})
	if err != nil || len(empty.commits) != 0 {
		t.Fatalf("empty history: %v, %v", empty, err)
	}
	for _, directory := range []string{"backend/config", "frontend/config"} {
		if err := os.MkdirAll(filepath.Join(root, directory), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, directory, "settings.txt"), []byte("original\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	git("add", ".")
	git("commit", "-m", "Initial commit")
	first := strings.TrimSpace(git("rev-parse", "HEAD"))
	tree := strings.TrimSpace(git("rev-parse", "HEAD^{tree}"))
	var history strings.Builder
	timestamp := time.Now().Unix()
	for i := 0; i < 104; i++ {
		message := fmt.Sprintf("History message %03d", i)
		parent := first
		if i > 0 {
			parent = fmt.Sprintf(":%d", i)
		}
		fmt.Fprintf(&history, "commit refs/heads/main\nmark :%d\ncommitter EditorX Fixture <fixture@example.invalid> %d +0000\ndata %d\n%s\nfrom %s\n\n",
			i+1, timestamp, len(message), message, parent)
	}
	history.WriteString("done\n")
	executable, err := exec.LookPath("git.exe")
	if err != nil {
		t.Fatal(err)
	}
	importContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, err := runGitCommandInput(importContext, executable, root, history.String(), 4<<20, "fast-import", "--quiet"); err != nil {
		t.Fatal(err)
	}
	git("tag", "v1.0", first)
	feature := strings.TrimSpace(git("commit-tree", tree, "-p", first, "-m", "Feature branch commit"))
	git("update-ref", "refs/heads/feature/history", feature)
	git("config", "user.name", "Author with a long display name to check clipping")
	for _, directory := range []string{"backend/config", "frontend/config"} {
		if err := os.WriteFile(filepath.Join(root, directory, "settings.txt"), []byte("updated\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	git("add", ".")
	git("commit", "-m", "优化 Git 历史提交记录：长说明与作者分列显示，完整路径区分同名文件\n\nFull commit message with details.\nKeep each file path visible.")
	latest := strings.TrimSpace(git("rev-parse", "HEAD"))
	page, err := readGitLogPage(ctx, root, gitLogQuery{})
	if err != nil || len(page.commits) != 100 || !page.hasMore || page.commits[0].hash != latest {
		t.Fatalf("first page: %d %v %v", len(page.commits), page.hasMore, err)
	}
	next, err := readGitLogPage(ctx, root, gitLogQuery{offset: 100})
	if err != nil || len(next.commits) != 6 || next.hasMore || next.commits[len(next.commits)-1].hash != first {
		t.Fatalf("second page: %d %v %v", len(next.commits), next.hasMore, err)
	}
	for _, query := range []gitLogQuery{{text: "History message 003"}, {text: latest[:8]}, {author: "long display name"}, {reference: "refs/tags/v1.0"}} {
		result, err := readGitLogPage(ctx, root, query)
		if err != nil || len(result.commits) != 1 {
			t.Fatalf("query %+v returned %d commits: %v", query, len(result.commits), err)
		}
	}
	branch, err := readGitLogPage(ctx, root, gitLogQuery{reference: "refs/heads/feature/history"})
	if err != nil || len(branch.commits) != 2 || branch.commits[0].hash != feature {
		t.Fatalf("branch query: %+v %v", branch, err)
	}
	all, err := readGitLogPage(ctx, root, gitLogQuery{reference: "all", text: "Feature branch"})
	if err != nil || len(all.commits) != 1 || all.commits[0].hash != feature {
		t.Fatalf("all branches query: %+v %v", all, err)
	}
	future, err := readGitLogPage(ctx, root, gitLogQuery{since: time.Now().Add(24 * time.Hour).Format(time.RFC3339)})
	if err != nil || len(future.commits) != 0 {
		t.Fatalf("date query returned %d commits: %v", len(future.commits), err)
	}
	m := newEditorApp()
	m.openProjectGit(root)
	session := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(session.Resize(1400, 900))
	check(session.WaitFor(func(app.Snapshot) bool { return m.tools.git.request == nil }, 10*time.Second))
	m.openGitLog()
	check(session.Frame())
	wait := func() {
		t.Helper()
		check(session.WaitFor(func(app.Snapshot) bool {
			s := m.tools.git.review
			return s.log == nil && s.files == nil && s.read == nil
		}, 10*time.Second))
		if m.tools.git.review.problem != "" {
			t.Fatal(m.tools.git.review.problem)
		}
	}
	wait()
	state := m.tools.git.review
	if len(state.changes) != 2 || state.data != nil {
		t.Fatal("history must show file details before computing a diff")
	}
	for _, filename := range []string{"backend/config/settings.txt", "frontend/config/settings.txt"} {
		_, err := session.Locate("button:查看提交文件 " + filename)
		check(err)
	}
	if preview := os.Getenv("VIVA_PREVIEW_DIR"); preview != "" {
		check(session.Screenshot(filepath.Join(preview, "git-log-dark.png")))
		m.setLightMode(true)
		check(session.Frame())
		check(session.Screenshot(filepath.Join(preview, "git-log-light.png")))
		m.setLightMode(false)
	}
	check(session.Resize(900, 560))
	_, err = session.Locate("button:搜索提交")
	check(err)
	_, err = session.Locate("button:查看提交文件 frontend/config/settings.txt")
	check(err)
	if preview := os.Getenv("VIVA_PREVIEW_DIR"); preview != "" {
		check(session.Screenshot(filepath.Join(preview, "git-log-small.png")))
	}
	check(session.Resize(1400, 900))
	state.logQuery.reference = "refs/heads/missing-pagination-fixture"
	check(session.ClickLabel("button:加载更多提交"))
	check(session.WaitFor(func(app.Snapshot) bool { return state.log == nil }, 10*time.Second))
	if state.problem == "" || state.logQuery.offset != 0 || len(state.commits) != 100 || !state.logHasMore {
		t.Fatal("failed page must preserve loaded commits and return to the previous offset")
	}
	state.logQuery.reference = ""
	check(session.Frame())
	check(session.ClickLabel("button:加载更多提交"))
	wait()
	if len(state.commits) != 106 || state.logHasMore {
		t.Fatal("load more did not append the final page")
	}
	check(session.ClickLabel("button:复制提交哈希"))
	if session.Driver().Clipboard() != latest {
		t.Fatal("copy hash differs from selected commit")
	}
	check(session.ClickLabel("button:查看提交文件 frontend/config/settings.txt"))
	wait()
	if state.data == nil || state.data.path != "frontend/config/settings.txt" {
		t.Fatal("opened wrong same-name file")
	}
	if preview := os.Getenv("VIVA_PREVIEW_DIR"); preview != "" {
		check(session.Screenshot(filepath.Join(preview, "git-log-diff.png")))
	}
	check(session.ClickLabel("button:返回提交记录"))
	if !state.open || state.logDiff {
		t.Fatal("back button closed the commit log")
	}
	state.logSearch.SetText("History message 003")
	check(session.Frame())
	check(session.ClickLabel("button:搜索提交"))
	wait()
	if len(state.commits) != 1 || state.commits[0].subject != "History message 003" {
		t.Fatal("search only filtered loaded rows")
	}
	if git("status", "--porcelain") != "" || strings.TrimSpace(git("rev-parse", "HEAD")) != latest {
		t.Fatal("browsing history changed repository state")
	}
}

func TestGitWorkingReviewIncludesStagedAndUnstaged(t *testing.T) {
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
	git("commit", "-m", "original")
	write("staged\n")
	git("add", ".")
	write("working\n")
	result, err := readGitWorkingReview(context.Background(), root, gitChange{path: "sample.txt", status: "MM"})
	if err != nil || result.comparison.old.saved != "original\n" || result.comparison.current.saved != "working\n" || len(result.hunks) != 0 {
		t.Fatalf("full working comparison: %+v %v", result, err)
	}
	if strings.TrimSpace(git("show", ":sample.txt")) != "staged" {
		t.Fatal("review changed index")
	}
}
