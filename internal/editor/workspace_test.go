package editor

import (
	"encoding/json"
	"errors"
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

func TestProjectCreateOnCompactFolder(t *testing.T) {
	root := filepath.Join(t.TempDir(), "项目")
	top := filepath.Join(root, "src")
	middle := filepath.Join(top, "main")
	deep := filepath.Join(middle, "java")
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(os.MkdirAll(deep, 0700))
	check(os.WriteFile(filepath.Join(deep, "已有文件.txt"), []byte("keep"), 0600))
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	m.addProjects(root)
	s := openTestSession(t, m)
	check(s.Resize(1080, 675))
	click := func(label string) { t.Helper(); check(s.ClickLabel(label)) }
	if len(m.workspace.rows) != 2 || m.workspace.rows[1].node.path != deep || m.workspace.rows[1].label != "src.main.java" {
		t.Fatal("expected the whole single-directory chain to appear as one row under the project")
	}

	check(s.RightClickLabel("src.main.java"))
	click("menuitem:新建文件")
	if m.templateDialog == nil || m.templateDialog.directory != deep || m.projectFolder != nil {
		t.Fatal("new file did not target the last directory")
	}
	if _, err := s.Locate("button:模板"); err == nil || len(m.templateDialog.choices) != len(newTextFormats)+1 {
		t.Fatal("project file creation offered templates while the template library was disabled")
	}
	if preview := os.Getenv("VIVA_PREVIEW_DIR"); preview != "" {
		check(s.Screenshot(filepath.Join(preview, "project-new-file.png")))
	}
	check(s.Type("app"))
	click("button:YAML · .yaml")
	click("textfield:文件名")
	check(s.Keys("enter"))
	config := filepath.Join(deep, "app.yaml")
	data, err := os.ReadFile(config)
	check(err)
	if len(data) != 0 || m.active == nil || m.active.doc.path != config {
		t.Fatal("creation did not add the requested format extension or open its file")
	}
	_, err = s.Locate("app.yaml")
	check(err)

	for _, filename := range []string{"app.custom", "Makefile", ".env", "placeholder.png"} {
		check(s.RightClickLabel("src.main.java"))
		click("menuitem:新建文件")
		if choice := m.templateDialog.choices[m.templateDialog.selected]; choice.label() != "自定义" {
			t.Fatal("new file should default to the custom filename")
		}
		previous := m.active
		m.templateDialog.filename.SetText(filename)
		check(s.Frame())
		click("button:创建文件")
		path := filepath.Join(deep, filename)
		data, err := os.ReadFile(path)
		check(err)
		if len(data) != 0 || m.templateDialog != nil || m.showError {
			t.Fatalf("did not create %s as an empty file", filename)
		}
		if filename == "placeholder.png" {
			if m.active != previous {
				t.Fatal("an empty image placeholder should only be created on disk")
			}
		} else if m.active == nil || m.active.doc.path != path {
			t.Fatalf("did not open %s", filename)
		}
		_, err = s.Locate(filename)
		check(err)
	}

	click("button:设置")
	click("button:侧栏")
	click("switch:模板库")
	click("button:关闭设置页")
	check(s.RightClickLabel("src.main.java"))
	click("menuitem:新建文件")
	click("button:模板")
	click("button:接口响应示例 · 内置")
	check(s.Frame())
	click("button:创建文件")
	data, err = os.ReadFile(filepath.Join(deep, "response.json"))
	check(err)
	if !json.Valid(data) || !strings.Contains(string(data), "success") {
		t.Fatal("template did not create the rendered file in the selected directory")
	}

	check(s.RightClickLabel("src.main.java"))
	click("menuitem:新建文件夹")
	if m.projectFolder == nil || m.projectFolder.directory != deep || m.templateDialog != nil {
		t.Fatal("new folder did not use the independent name dialog")
	}
	check(s.Type("assets.png"))
	check(s.Keys("enter"))
	if m.projectFolder != nil {
		t.Fatal("Enter did not create the folder")
	}
	_, err = s.Locate("assets.png")
	check(err)
	info, err := os.Stat(filepath.Join(deep, "assets.png"))
	check(err)
	if !info.IsDir() {
		t.Fatal("creation did not produce a directory")
	}

	check(s.RightClickLabel("src.main.java"))
	click("menuitem:新建文件夹")
	check(s.Type("app.yaml"))
	click("button:创建文件夹")
	if m.projectFolder == nil || !strings.Contains(m.projectFolder.problem, "同名") {
		t.Fatal("the directory dialog did not report the existing file")
	}
	data, err = os.ReadFile(config)
	check(err)
	if len(data) != 0 {
		t.Fatal("creating a conflicting folder changed an existing file")
	}
	check(s.Keys("escape"))

	check(s.RightClickLabel("src.main.java"))
	click("menuitem:上级目录")
	click("menuitem:main")
	click("menuitem:新建文件夹")
	if m.projectFolder == nil || m.projectFolder.directory != middle {
		t.Fatal("the ancestor action did not preserve access to a merged directory")
	}
	check(s.Type("test"))
	click("button:创建文件夹")
	_, err = s.Locate("src.main")
	check(err)
	_, err = s.Locate("java")
	check(err)
	_, err = s.Locate("test")
	check(err)
	if _, err := s.Locate("src.main.java"); err == nil {
		t.Fatal("the tree kept merging through a directory with a sibling")
	}

	check(s.RightClickLabel("项目"))
	click("menuitem:新建文件夹")
	check(s.Type("docs"))
	click("button:创建文件夹")
	_, err = s.Locate("docs")
	check(err)
}

func TestProjectDeleteConfirmation(t *testing.T) {
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	root := filepath.Join(t.TempDir(), "删除测试")
	folder := filepath.Join(root, "src", "main", "java")
	projectFile := filepath.Join(root, "main.txt")
	nestedFile := filepath.Join(folder, "nested.txt")
	check(os.MkdirAll(folder, 0700))
	check(os.WriteFile(projectFile, []byte("original"), 0600))
	check(os.WriteFile(nestedFile, []byte("nested"), 0600))

	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	m.addProjects(root)
	s := openTestSession(t, m)
	check(s.Resize(1080, 675))
	openDelete := func(label, path, title string) {
		t.Helper()
		check(s.RightClickLabel(label))
		check(s.ClickLabel("menuitem:删除"))
		if m.projectDelete == nil || m.projectDelete.path != path || !s.HasText(title) {
			t.Fatal("did not show a confirmation for the selected path")
		}
		_, err := os.Lstat(path)
		check(err)
	}
	deleteEntry := func(label, path, title string) {
		t.Helper()
		openDelete(label, path, title)
		check(s.ClickLabel("button:确认删除"))
		if m.showError || !strings.HasPrefix(m.status, "已删除 ") {
			t.Fatalf("delete did not finish: %s / %s", m.status, m.errorText)
		}
	}
	notOnDisk := func(path string) {
		t.Helper()
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s still exists: %v", path, err)
		}
	}
	contentIs := func(path, expected string) {
		t.Helper()
		data, err := os.ReadFile(path)
		check(err)
		if string(data) != expected {
			t.Fatalf("unexpected content at %s: %q", path, data)
		}
	}

	openDelete("main.txt", projectFile, "删除文件？")
	check(s.ClickLabel("button:取消"))
	if m.projectDelete != nil {
		t.Fatal("cancelling removed the file or recorded a deletion")
	}
	contentIs(projectFile, "original")

	openDelete("src.main.java", folder, "删除文件夹？")
	check(s.Keys("escape"))
	if m.projectDelete != nil {
		t.Fatal("dismissing the dialog removed the folder or recorded a deletion")
	}
	contentIs(nestedFile, "nested")

	deleteEntry("main.txt", projectFile, "删除文件？")
	notOnDisk(projectFile)
	deleteEntry("src.main.java", folder, "删除文件夹？")
	notOnDisk(folder)
	deleteEntry("删除测试", root, "删除文件夹？")
	notOnDisk(root)
	if m.workspace != nil {
		t.Fatal("deleted project remained in the tree")
	}
	stash, err := filepath.Glob(filepath.Join(filepath.Dir(root), projectTrashPrefix+"*"))
	check(err)
	if len(stash) != 0 {
		t.Fatal("project deletion created EditorX recovery folders")
	}
}
