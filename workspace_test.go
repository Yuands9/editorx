package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMultipleProjectsThroughUIAndSession(t *testing.T) {
	directory := t.TempDir()
	server := filepath.Join(directory, "服务端")
	web := filepath.Join(directory, "站点资源")
	serverChild := filepath.Join(server, "环境配置")
	webChild := filepath.Join(web, "模板")
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(os.MkdirAll(serverChild, 0700))
	check(os.MkdirAll(webChild, 0700))
	file := filepath.Join(serverChild, "应用.env")
	const source = "APP_ENV=development\nHTTP_PORT=8080\n"
	check(os.WriteFile(file, []byte(source), 0600))
	check(os.WriteFile(filepath.Join(webChild, "站点.yaml"), []byte("title: 本地站点\n"), 0600))
	check(os.WriteFile(filepath.Join(web, "构建.txt"), []byte("构建站点"), 0600))
	store, err := openSessionStore(filepath.Join(directory, "data"))
	check(err)
	m := newEditorApp()
	store.attach(m)
	s := openTestSession(t, m)
	check(s.Resize(1120, 690))
	check(s.Driver().QueueOpenFiles([]string{server, web}, nil))
	check(s.ClickLabel("添加项目"))
	tree := m.workspace
	if tree == nil || len(tree.roots) != 2 || tree.roots[0].path != server || tree.roots[1].path != web || m.active != nil {
		t.Fatal("multi-select did not create two project roots in selection order")
	}
	first, second := tree.roots[0], tree.roots[1]
	check(s.ClickLabel("环境配置"))
	check(s.ClickLabel("模板"))
	check(s.ClickLabel("应用.env"))
	textX := make(map[string]float32)
	for _, line := range s.Snapshot().Texts {
		textX[line.Value] = line.Bounds.X
	}
	if textX["模板"] == 0 || textX["模板"] != textX["构建.txt"] || textX["站点.yaml"] <= textX["模板"] {
		t.Fatal("the project tree does not align sibling names or indent their children")
	}
	tab := m.active
	if tab == nil || tab.doc.path != file || tab.source.text.String() != source || !first.children[0].expanded || !second.children[0].expanded {
		t.Fatal("files and subfolders were not reachable across independent projects")
	}
	check(s.ClickLabel("服务端"))
	if first.expanded || !second.expanded {
		t.Fatal("collapsing one project affected the other")
	}
	check(store.save())
	restored, err := openSessionStore(store.directory)
	check(err)
	again := restored.windows[0]
	if len(again.workspace.roots) != 2 || again.workspace.roots[0].expanded || !again.workspace.roots[0].children[0].expanded ||
		!again.workspace.roots[1].expanded || !again.workspace.roots[1].children[0].expanded || again.active.doc.path != file {
		t.Fatal("project order, hidden branch state or the active file failed to restore")
	}

	check(s.Driver().QueueOpenFiles([]string{strings.ToUpper(server), web}, nil))
	check(s.ClickLabel("添加项目"))
	if len(tree.roots) != 2 || tree.roots[0] != first || first.expanded {
		t.Fatal("re-adding existing project paths duplicated or reset the tree")
	}
	check(s.ClickLabel("服务端"))
	check(os.WriteFile(filepath.Join(serverChild, "新增配置.txt"), []byte("示例"), 0600))
	webDirectory := second.children[0]
	check(s.ClickLabel("项目操作 " + server))
	check(s.ClickLabel("刷新项目"))
	_, err = s.Locate("新增配置.txt")
	check(err)
	if second.children[0] != webDirectory || !second.children[0].expanded || !first.children[0].expanded || m.active != tab {
		t.Fatal("refresh affected a different project, expansion or the active file")
	}
	if preview := os.Getenv("VIVA_PREVIEW_DIR"); preview != "" {
		check(s.Move(885, 400))
		check(s.Screenshot(filepath.Join(preview, "projects-dark.png")))
		m.setLightMode(true)
		check(s.Frame())
		check(s.Screenshot(filepath.Join(preview, "projects-light.png")))
		m.setLightMode(false)
		check(s.Frame())
		check(s.ClickLabel("服务端"))
		check(s.ClickLabel("站点资源"))
		check(s.Move(885, 400))
		check(s.Screenshot(filepath.Join(preview, "projects-collapsed-dark.png")))
		check(s.ClickLabel("服务端"))
		check(s.ClickLabel("站点资源"))
		check(s.Move(885, 400))
	}
	const draft = "APP_ENV=测试环境\nHTTP_PORT=8080\n"
	tab.source.replace(draft)
	check(s.Frame())
	check(s.RightClickLabel("服务端"))
	check(s.ClickLabel("移除项目"))
	if len(tree.roots) != 1 || tree.roots[0] != second || m.active != tab || len(m.tabs) != 1 ||
		!tab.modified() || tab.source.text.String() != draft || m.confirm {
		t.Fatal("removing a project did not preserve the open draft")
	}
	original, err := os.ReadFile(file)
	check(err)
	if string(original) != source {
		t.Fatal("removing a project wrote to disk")
	}
	check(store.save())
	restored, err = openSessionStore(store.directory)
	check(err)
	again = restored.windows[0]
	if len(again.workspace.roots) != 1 || again.workspace.roots[0].path != web || again.active.source.text.String() != draft {
		t.Fatal("removed project resurrected or draft was lost")
	}
	check(s.ClickLabel("项目操作 " + web))
	check(s.ClickLabel("移除项目"))
	if m.workspace != nil || m.active != tab {
		t.Fatal("removing the last project affected the editor")
	}
	_, err = os.Stat(webChild)
	check(err)
}

func TestLegacyFolderSessionMigrationAndPartialProjectAdd(t *testing.T) {
	directory := t.TempDir()
	legacyRoot := filepath.Join(directory, "旧项目")
	legacyChild := filepath.Join(legacyRoot, "配置")
	newRoot := filepath.Join(directory, "新项目")
	data := filepath.Join(directory, "data")
	for _, folder := range []string{legacyChild, newRoot, data} {
		if err := os.MkdirAll(folder, 0700); err != nil {
			t.Fatal(err)
		}
	}
	legacy := struct {
		Version int
		Windows []struct {
			Active          int
			ShowFiles       bool
			Workspace       string
			ExpandedFolders []string
		}
	}{Version: 1}
	legacy.Windows = append(legacy.Windows, struct {
		Active          int
		ShowFiles       bool
		Workspace       string
		ExpandedFolders []string
	}{Active: -1, ShowFiles: true, Workspace: legacyRoot, ExpandedFolders: []string{legacyRoot, legacyChild}})
	saved, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(data, "session.json")
	if err := os.WriteFile(path, saved, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := openSessionStore(data)
	if err != nil {
		t.Fatal(err)
	}
	m := store.windows[0]
	if m.workspace == nil || len(m.workspace.roots) != 1 || m.workspace.roots[0].path != legacyRoot ||
		!m.workspace.roots[0].children[0].expanded || !m.showFiles {
		t.Fatal("legacy workspace did not migrate with its expanded branches")
	}
	m.addProjects(legacyRoot, filepath.Join(directory, "不存在"), newRoot)
	if !m.showError || len(m.workspace.roots) != 2 || m.workspace.roots[1].path != newRoot || len(m.tabs) != 0 {
		t.Fatal("an invalid choice blocked the other folders or created a tab")
	}
	if err := store.save(); err != nil {
		t.Fatal(err)
	}
	saved, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(saved), `"Workspace"`) || !strings.Contains(string(saved), `"Projects"`) {
		t.Fatal("new session still wrote the legacy workspace field")
	}
}
