package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

func TestTablessHomeThroughUI(t *testing.T) {
	t.Cleanup(func() { setUILanguage(menuChinese) })
	directory := t.TempDir()
	store, err := openSessionStore(filepath.Join(directory, "data"))
	if err != nil {
		t.Fatal(err)
	}
	m := newEditorApp()
	store.attach(m)
	var latest string
	for _, name := range []string{"早期.json", "部署", "使用说明.md", "config.xml"} {
		latest = filepath.Join(directory, name)
		if err := os.WriteFile(latest, []byte("测试内容"), 0600); err != nil {
			t.Fatal(err)
		}
		m.rememberPath(latest, false)
	}
	m.rememberPath(directory, true)

	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	expectHome := func() {
		t.Helper()
		if len(m.tabs) != 0 || m.active != nil {
			t.Fatal("home created a document")
		}
		for _, label := range []string{"代码编辑区", "保存文件", "查找内容", "关闭 未命名-1"} {
			if _, err := s.Locate(label); err == nil {
				t.Fatalf("document control visible without a tab: %s", label)
			}
		}
		for _, label := range []string{"打开文件", "打开项目", "最近打开文件", "最近文件 " + latest} {
			_, err := s.Locate(label)
			check(err)
		}
		count := 0
		for _, hit := range s.Snapshot().Hits {
			if strings.HasPrefix(hit.Label, "最近文件 ") {
				count++
			}
		}
		if count != 3 {
			t.Fatalf("want three recent files, got %d", count)
		}
	}
	check(s.Resize(900, 560))
	expectHome()
	projectButton, err := s.Locate("button:打开项目")
	check(err)
	fileButton, err := s.Locate("button:打开文件")
	check(err)
	recentButton, err := s.Locate("button:最近打开文件")
	check(err)
	if projectButton.Bounds.X >= fileButton.Bounds.X || fileButton.Bounds.X >= recentButton.Bounds.X {
		t.Fatal("home actions must be ordered project, file, recent files")
	}
	if preview := os.Getenv("VIVA_PREVIEW_DIR"); preview != "" {
		check(s.Screenshot(filepath.Join(preview, "home-no-tabs-dark.png")))
		m.setLightMode(true)
		check(s.Frame())
		check(s.Screenshot(filepath.Join(preview, "home-no-tabs-light.png")))
	}
	check(s.Driver().QueueOpenFiles(nil, app.ErrCanceled))
	check(s.ClickLabel("打开文件"))
	expectHome()
	check(s.Driver().QueueOpenFiles([]string{filepath.Join(directory, "缺失文件")}, nil))
	check(s.Keys("ctrl+o"))
	check(s.ClickLabel("确定"))
	expectHome()
	check(s.Keys("ctrl+n"))
	if len(m.tabs) != 1 || m.active.title() != "未命名-1" {
		t.Fatal("Ctrl+N did not create the first tab")
	}
	if _, found := s.AccessByRole(gfx.RoleTextField, "代码编辑区"); !found {
		t.Fatal("blank welcome page has no editor")
	}
	for _, label := range []string{"打开文件", "最近打开文件", "打开项目"} {
		_, err := s.Locate("button:" + label)
		check(err)
	}
	if !s.HasText("开始编辑") || !s.HasText("直接输入内容，或打开已有文件") {
		t.Fatal("blank document did not retain the centered welcome page")
	}
	if _, found := s.AccessByRole(gfx.RoleButton, "打开链接文件"); found {
		t.Fatal("home still shows the link button")
	}
	if preview := os.Getenv("VIVA_PREVIEW_DIR"); preview != "" {
		check(s.Screenshot(filepath.Join(preview, "home-blank-light.png")))
		m.setLightMode(false)
		check(s.Frame())
		check(s.Screenshot(filepath.Join(preview, "home-blank-dark.png")))
	}
	setUILanguage(menuEnglish)
	m.menuLanguage = menuEnglish
	check(s.Frame())
	for _, label := range []string{"打开文件", "最近打开文件", "打开项目"} {
		_, err := s.Locate("button:" + uiText(label))
		check(err)
	}
	if preview := os.Getenv("VIVA_PREVIEW_DIR"); preview != "" {
		check(s.Screenshot(filepath.Join(preview, "home-blank-english.png")))
	}
	setUILanguage(menuChinese)
	m.menuLanguage = menuChinese
	check(s.Frame())
	check(s.ClickLabel("最近文件 " + latest))
	if len(m.tabs) != 2 || m.active.doc.path != latest {
		t.Fatal("opening a recent file replaced an explicitly created untitled tab")
	}
	check(s.RightClickLabel(filepath.Base(latest)))
	check(s.ClickLabel("关闭全部标签页"))
	expectHome()

	check(s.ClickLabel("最近打开文件"))
	check(s.ClickLabel("打开历史 " + latest))
	if len(m.tabs) != 1 || m.active.doc.path != latest {
		t.Fatal("recent dialog did not open a file from home")
	}
	check(s.Keys("ctrl+w"))
	expectHome()
	check(s.Driver().QueueOpenFiles([]string{directory}, nil))
	check(s.ClickLabel("打开项目"))
	if m.workspace == nil || !m.showFiles || m.active != nil {
		t.Fatal("opening a folder should leave the editor tabless")
	}
	check(store.save())
	reopened, err := openSessionStore(store.directory)
	check(err)
	m = reopened.windows[0]
	s = openTestSession(t, m)
	expectHome()
}

func TestCollapseOnlyGroupKeepsStartPageAndRestores(t *testing.T) {
	m := newTestEditorWithDocument()
	m.newTabGroup(m.active)
	group := m.active.group
	s := openTestSession(t, m)
	if err := s.ClickLabel(group.label()); err != nil {
		t.Fatal(err)
	}
	if !group.Collapsed || m.active != nil || len(m.tabs) != 1 {
		t.Fatal("collapsing the only group created a blank tab or selected a hidden member")
	}
	if _, err := s.Locate("打开文件"); err != nil {
		t.Fatal(err)
	}
	again := newEditorApp()
	again.restoreSession(m.snapshotSession())
	if again.active != nil || len(again.tabs) != 1 || !again.tabs[0].group.Collapsed {
		t.Fatal("collapsed session restored with a visible editor")
	}
	restored := openTestSession(t, again)
	if err := restored.ClickLabel(group.label()); err != nil {
		t.Fatal(err)
	}
	if again.active != again.tabs[0] || again.active.group.Collapsed {
		t.Fatal("expanding the group did not return to the original tab")
	}
}
