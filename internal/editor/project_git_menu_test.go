package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

func TestProjectGitContextScopes(t *testing.T) {
	root, git := gitReviewFixture(t)
	directory := filepath.Join(root, "subfolder")
	left := filepath.Join(directory, "改动 [1].txt")
	right := filepath.Join(root, "other.txt")
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(os.Mkdir(directory, 0700))
	check(os.WriteFile(left, []byte("old left\n"), 0600))
	check(os.WriteFile(right, []byte("old right\n"), 0600))
	git("add", ".")
	git("commit", "-m", "base")
	check(os.WriteFile(right, []byte("new right\n"), 0600))
	git("commit", "-am", "outside scope")
	check(os.WriteFile(left, []byte("new left\n"), 0600))
	check(os.WriteFile(right, []byte("third right\n"), 0600))
	git("add", "--", "other.txt")

	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	m.addProjects(root)
	s := openTestSession(t, m)
	check(s.WaitFor(func(app.Snapshot) bool { return m.workspace.roots[0].loaded }, 10*time.Second))
	projectPoint := func(label string) gfx.Point {
		t.Helper()
		for index, row := range m.workspace.rows {
			if row.label != label {
				continue
			}
			for _, hit := range s.Snapshot().Hits {
				if hit.ID == m.workspace.state.RowID(index) {
					return gfx.Point{X: hit.Bounds.X + hit.Bounds.Width/2, Y: hit.Bounds.Y + hit.Bounds.Height/2}
				}
			}
		}
		t.Fatalf("project tree row %q missing", label)
		return gfx.Point{}
	}
	openMenu := func(row, action string) {
		t.Helper()
		point := projectPoint(row)
		check(s.RightClick(point.X, point.Y))
		check(s.ClickLabel("menuitem:Git"))
		check(s.ClickLabel("menuitem:" + action))
	}
	wait := func() {
		t.Helper()
		check(s.WaitFor(func(app.Snapshot) bool { return m.tools.git.request == nil }, 10*time.Second))
		if m.tools.git.issue != "" {
			t.Fatal(m.tools.git.issue)
		}
	}
	openMenu("subfolder", "勾选提交")
	wait()
	if paths := m.tools.git.selectedCommitPaths(); len(paths) != 1 || paths[0] != "subfolder/改动 [1].txt" {
		t.Fatalf("directory selection changed unrelated files: %v", paths)
	}
	if !s.HasText("已选 1/2") || strings.TrimSpace(git("diff", "--cached", "--name-only")) != "other.txt" {
		t.Fatal("selection did not show its count or altered the index")
	}
	openMenu("subfolder", "暂存变更")
	wait()
	if cached := git("diff", "--cached", "--name-only", "-z"); !strings.Contains(cached, "subfolder/改动 [1].txt\x00") || !strings.Contains(cached, "other.txt\x00") {
		t.Fatalf("unexpected staged paths: %q", cached)
	}
	point := projectPoint("subfolder")
	check(s.Click(point.X, point.Y))
	waitProjectLoads(t, m, s)
	openMenu("改动 [1].txt", "取消暂存")
	wait()
	if strings.TrimSpace(git("diff", "--cached", "--name-only")) != "other.txt" {
		t.Fatal("unstaging the clicked file altered another staged file")
	}
	openMenu("改动 [1].txt", "查看 Git 差异")
	check(s.WaitFor(func(app.Snapshot) bool {
		review := m.tools.git.review
		return review != nil && review.data != nil
	}, 10*time.Second))
	if m.tools.git.review.data.path != "subfolder/改动 [1].txt" {
		t.Fatal("diff opened a different file")
	}
	check(s.Driver().Key(gfx.KeyEscape, 0))
	check(s.Frame())
	check(s.WaitFor(func(app.Snapshot) bool { return m.tools.git.review == nil }, 5*time.Second))
	check(s.Frame())
	openMenu("subfolder", "提交记录…")
	check(s.WaitFor(func(app.Snapshot) bool {
		review := m.tools.git.review
		return review != nil && review.history && review.log == nil && review.files == nil
	}, 10*time.Second))
	log := m.tools.git.review
	if log.problem != "" || len(log.commits) != 1 || log.commits[0].subject != "base" || len(log.changes) != 1 || log.changes[0].path != "subfolder/改动 [1].txt" {
		t.Fatalf("history escaped the directory scope: %q, %v, %v", log.problem, log.commits, log.changes)
	}

	check(s.Driver().Key(gfx.KeyEscape, 0))
	check(s.Frame())
	check(s.WaitFor(func(app.Snapshot) bool { return m.tools.git.review == nil }, 5*time.Second))
	plainProject := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(plainProject))
	m.addProjects(plainProject)
	waitProjectLoads(t, m, s)
	openMenu(filepath.Base(plainProject), "打开 Git 面板")
	wait()
	if _, err := s.Locate("button:初始化仓库"); err != nil {
		t.Fatal("Git panel must still offer to initialize the clicked project", err)
	}
}
