package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
)

func TestTablessHomeThroughUI(t *testing.T) {
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
		for _, label := range []string{"打开现有文件", "最近打开文件", "最近文件 " + latest} {
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
	if preview := os.Getenv("VIVA_PREVIEW_DIR"); preview != "" {
		check(s.Screenshot(filepath.Join(preview, "home-no-tabs-dark.png")))
		m.setLightMode(true)
		check(s.Frame())
		check(s.Screenshot(filepath.Join(preview, "home-no-tabs-light.png")))
	}
	check(s.Driver().QueueOpenFiles(nil, app.ErrCanceled))
	check(s.ClickLabel("打开现有文件"))
	expectHome()
	check(s.Driver().QueueOpenFiles([]string{filepath.Join(directory, "缺失文件")}, nil))
	check(s.Keys("ctrl+o"))
	check(s.ClickLabel("确定"))
	expectHome()
	check(s.Keys("ctrl+n"))
	if len(m.tabs) != 1 || m.active.title() != "未命名-1" {
		t.Fatal("Ctrl+N did not create the first tab")
	}
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
	check(s.Keys("ctrl+shift+o"))
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
	if _, err := s.Locate("打开现有文件"); err != nil {
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
