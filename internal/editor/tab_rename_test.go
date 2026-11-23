package editor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTabRenameByEnterKeepsFileAndRestoresDisplayName(t *testing.T) {
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "settings.toml")
	const source = "port = 8080\n"
	check(os.WriteFile(path, []byte(source), 0600))
	store, err := openSessionStore(filepath.Join(directory, "data"))
	check(err)
	m := newEditorApp()
	store.attach(m)
	m.load(path)
	saved := m.active
	m.newDocument()
	untitled := m.active
	session := openTestSession(t, m)

	check(session.RightClickLabel(saved.tabTitle()))
	check(session.ClickLabel("重命名标签…"))
	check(session.Type("本地启动参数"))
	check(session.Keys("enter"))
	if saved.tabTitle() != "本地启动参数" || saved.format() != "TOML" || saved.doc.path != path || saved.modified() || m.active != untitled || m.rename.open {
		t.Fatal("renaming a document tab did not preserve the file, format, and active tab")
	}
	if content, err := os.ReadFile(path); err != nil || string(content) != source {
		t.Fatalf("renaming a tab changed the file: %q, %v", content, err)
	}

	check(session.RightClickLabel(untitled.tabTitle()))
	check(session.ClickLabel("重命名标签…"))
	check(session.Compose("huanjing"))
	check(session.Keys("enter"))
	if !m.rename.open || untitled.tabTitle() != "未命名-1" {
		t.Fatal("confirming an IME candidate renamed the tab")
	}
	check(session.Keys("cmd+a"))
	check(session.Keys("backspace"))
	check(session.Keys("enter"))
	if !m.rename.open {
		t.Fatal("Enter accepted an empty tab name")
	}
	check(session.Type(".env.local"))
	check(session.Keys("enter"))
	if m.rename.open || untitled.title() != ".env.local" || untitled.format() != "ENV" {
		t.Fatal("Enter did not rename the draft or update its syntax")
	}
	check(store.save())
	restored, err := openSessionStore(store.directory)
	check(err)
	tabs := restored.windows[0].tabs
	if len(tabs) != 2 || tabs[0].tabTitle() != "本地启动参数" ||
		tabs[0].doc.path != path || tabs[1].tabTitle() != ".env.local" {
		t.Fatal("tab names did not survive session restoration")
	}
}

func TestDoubleClickRenamesTabsAndGroups(t *testing.T) {
	m := newTestEditorWithDocument()
	member := m.active
	m.newTabGroup(member)
	group := member.group
	m.newDocument()
	outside := m.active
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	check(s.DoubleClickLabel(member.tabTitle()))
	if m.rename.tab != member || !m.rename.open {
		t.Fatal("double-clicking a tab did not open its rename field")
	}
	check(s.Type("编译参数"))
	check(s.Keys("enter"))
	if member.tabTitle() != "编译参数" || m.active != member {
		t.Fatal("enter did not update the selected tab")
	}

	check(s.DoubleClickLabel(group.label()))
	if m.rename.group != group || !m.rename.open || group.Collapsed || m.active != member {
		t.Fatal("double-clicking an expanded group changed its visibility or active tab")
	}
	check(s.Type("项目配置"))
	check(s.Keys("enter"))
	check(s.ClickLabel(group.label()))
	if !group.Collapsed || m.active != outside {
		t.Fatal("single-clicking the renamed group no longer collapses it")
	}
	check(s.DoubleClickLabel(group.label()))
	if m.rename.group != group || !m.rename.open || !group.Collapsed || m.active != outside {
		t.Fatal("double-clicking a collapsed group changed its visibility or active tab")
	}
	check(s.Type("运行环境"))
	check(s.Keys("enter"))
	if group.Name != "运行环境" || !group.Collapsed {
		t.Fatal("renaming the collapsed group expanded it")
	}
}
