package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func TestSessionRestartKeepsDraftsAndLayout(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "配置.json")
	content := "{\n\"list\":[\n" + strings.Repeat("{\"名称\":\"中文\"},\n", 79) + "{\"名称\":\"结束\"}\n]}"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := openSessionStore(filepath.Join(directory, "data"))
	if err != nil {
		t.Fatal(err)
	}
	m := newEditorApp()
	store.attach(m)
	m.load(path)
	fileTab := m.active
	fileTab.source.replace(strings.Replace(content, "结束", "未保存", 1))
	m.togglePin(fileTab)
	m.setLightMode(true)
	m.showFiles = true
	m.showOutline = false
	m.filesSplit.Pos = 290
	m.windowFrame = gfx.Rect{X: 72, Y: 55, Width: 1200, Height: 780}
	m.maximized = true
	m.newDocument()
	draft := m.active
	draft.source.replace("未保存草稿 中文😀\n第二行")
	draft.source.text.SetSelection(text.Range{Start: 8, End: 2})
	draft.source.view.Snapshot.Wrap = true
	m.newDocument()
	htmlTab := m.active
	htmlTab.name = "页面.html"
	htmlTab.source.replace("<html><body>草稿页面</body></html>")
	htmlTab.htmlMode = "预览"
	htmlTab.html.scroll = gfx.Point{Y: 422}
	m.activate(fileTab)
	s := openTestSession(t, m)
	fileTab.source.text.MoveTo(600, false)
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	fileTab.source.view.Code.SetScrollY(260)
	expectedScroll := fileTab.source.view.Code.ScrollY()
	fileTab.source.collapsed[3] = true
	allowed := false
	m.requestClose(func() { allowed = true })
	if !allowed || m.confirm {
		t.Fatal("exit did not save a draft snapshot")
	}
	disk, _ := os.ReadFile(path)
	if string(disk) != content {
		t.Fatal("snapshot wrote unsaved changes into original")
	}
	restored, err := openSessionStore(store.directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(restored.windows) != 1 {
		t.Fatal("window not restored")
	}
	again := restored.windows[0]
	if len(again.tabs) != 3 || again.active != again.tabs[0] || !again.tabs[0].pinned || !again.light || !again.showFiles || again.showOutline || again.filesSplit.Pos != 290 || again.windowFrame != m.windowFrame || !again.maximized {
		t.Fatal("window or tab layout not restored")
	}
	if again.tabs[0].source.text.String() != fileTab.source.text.String() || !again.tabs[0].modified() || again.tabs[1].source.text.String() != draft.source.text.String() || again.tabs[1].source.text.Caret() != 2 || !again.tabs[1].source.view.Snapshot.Wrap {
		t.Fatal("draft or selection missing")
	}
	if again.tabs[2].htmlMode != "预览" || again.tabs[2].html.scroll.Y != 422 {
		t.Fatal("HTML view state missing")
	}
	recoveredSession := openTestSession(t, again)
	if err := recoveredSession.Frame(); err != nil {
		t.Fatal(err)
	}
	if again.active.source.view.Code.ScrollY() != expectedScroll || !again.active.source.collapsed[3] {
		t.Fatalf("scroll/fold missing: want=%g got=%g", expectedScroll, again.active.source.view.Code.ScrollY())
	}
	if len(restored.history) != 1 || restored.history[0].Path != path {
		t.Fatal("recent history missing")
	}
}

func TestSessionExternalChangesAndRecovery(t *testing.T) {
	directory := t.TempDir()
	store, _ := openSessionStore(filepath.Join(directory, "data"))
	m := newEditorApp()
	store.attach(m)
	paths := []string{filepath.Join(directory, "clean.txt"), filepath.Join(directory, "dirty.txt"), filepath.Join(directory, "missing.txt")}
	for _, path := range paths {
		if err := os.WriteFile(path, []byte("原文"), 0600); err != nil {
			t.Fatal(err)
		}
		m.load(path)
	}
	m.tabs[1].source.replace("我的未保存修改")
	if err := store.save(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths[0], []byte("磁盘新版本"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths[1], []byte("其他人的修改"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(paths[2]); err != nil {
		t.Fatal(err)
	}
	restored, err := openSessionStore(store.directory)
	if err != nil {
		t.Fatal(err)
	}
	again := restored.windows[0]
	if again.tabs[0].source.text.String() != "磁盘新版本" || again.tabs[1].source.text.String() != "我的未保存修改" || again.tabs[2].source.text.String() != "原文" {
		t.Fatal("external/missing files lost contents")
	}
	if again.saveTab(again.tabs[1], false) {
		t.Fatal("external changes overwritten")
	}
	data, _ := os.ReadFile(paths[1])
	if string(data) != "其他人的修改" {
		t.Fatal("conflict changed disk")
	}
	again.showError = false
	if err := restored.save(); err != nil {
		t.Fatal(err)
	}
	sessionPath := filepath.Join(store.directory, "session.json")
	if err := os.WriteFile(sessionPath, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	backup, err := openSessionStore(store.directory)
	if err != nil || len(backup.windows[0].tabs) != 3 {
		t.Fatalf("backup recovery: %v", err)
	}
	if err := os.WriteFile(sessionPath+".bak", []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := openSessionStore(store.directory); err == nil {
		t.Fatal("invalid sessions silently reset")
	}
}

func TestSessionWriteFailureAndGeneration(t *testing.T) {
	directory := t.TempDir()
	store, _ := openSessionStore(directory)
	m := newTestEditorWithDocument()
	store.attach(m)
	m.active.source.replace("旧快照")
	old, older := store.capture()
	m.active.source.replace("新快照")
	current, newer := store.capture()
	if err := store.write(current, newer); err != nil {
		t.Fatal(err)
	}
	if err := store.write(old, older); err != nil {
		t.Fatal(err)
	}
	recovered, err := openSessionStore(directory)
	if err != nil || recovered.windows[0].active.source.text.String() != "新快照" {
		t.Fatal("stale background snapshot replaced final snapshot")
	}
	blocked := filepath.Join(directory, "not-a-directory")
	if err := os.WriteFile(blocked, []byte("保留"), 0600); err != nil {
		t.Fatal(err)
	}
	store.directory = blocked
	m.active.source.replace("保存失败也要保留")
	allowed := false
	m.requestClose(func() { allowed = true })
	if allowed || !m.showError || m.active.source.text.String() != "保存失败也要保留" {
		t.Fatal("failed snapshot closed window or lost draft")
	}
	data, _ := os.ReadFile(blocked)
	if string(data) != "保留" {
		t.Fatal("failed snapshot damaged existing file")
	}
}

func TestSessionIMEAndExplicitClose(t *testing.T) {
	store, _ := openSessionStore(t.TempDir())
	m := newTestEditorWithDocument()
	store.attach(m)
	m.active.source.replace("已提交中文")
	s := openTestSession(t, m)
	if err := s.Compose("pinyin"); err != nil {
		t.Fatal(err)
	}
	if err := store.save(); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.active.source.text.Composition(); !ok {
		t.Fatal("snapshot canceled active IME")
	}
	restored, err := openSessionStore(store.directory)
	if err != nil || restored.windows[0].active.source.text.String() != "已提交中文" {
		t.Fatal("uncommitted pinyin saved")
	}
	m.active.source.text.CancelComposition()
	m.closeTab(m.active)
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("放弃修改"); err != nil {
		t.Fatal(err)
	}
	if err := store.save(); err != nil {
		t.Fatal(err)
	}
	restored, err = openSessionStore(store.directory)
	if err != nil || len(restored.windows[0].tabs) != 0 || restored.windows[0].active != nil {
		t.Fatal("explicitly closed draft or implicit blank tab restored")
	}
}

func TestRecentHistoryOpenRemoveAndPersist(t *testing.T) {
	directory := t.TempDir()
	store, _ := openSessionStore(filepath.Join(directory, "data"))
	m := newEditorApp()
	store.attach(m)
	older := []string{filepath.Join(directory, "较早.json"), filepath.Join(directory, "配置.xml")}
	for _, path := range older {
		if err := os.WriteFile(path, []byte("历史内容"), 0600); err != nil {
			t.Fatal(err)
		}
		m.rememberPath(path, false)
	}
	a, b := filepath.Join(directory, "甲.txt"), filepath.Join(directory, "乙.txt")
	for _, path := range []string{a, b} {
		if err := os.WriteFile(path, []byte("历史内容"), 0600); err != nil {
			t.Fatal(err)
		}
		m.load(path)
	}
	m.load(a)
	if len(store.history) != 4 || store.history[0].Path != a {
		t.Fatal("history not deduplicated or ordered")
	}
	m.addProjects(directory)
	if !store.history[0].Directory {
		t.Fatal("folder not remembered")
	}
	s := openTestSession(t, m)
	if err := s.DoubleClickLabel("双击新建"); err != nil {
		t.Fatal(err)
	}
	quickFiles := []string{}
	for _, hit := range s.Snapshot().Hits {
		if strings.HasPrefix(hit.Label, "最近文件 ") {
			quickFiles = append(quickFiles, strings.TrimPrefix(hit.Label, "最近文件 "))
		}
	}
	if len(quickFiles) != 3 || quickFiles[0] != a || quickFiles[1] != b || quickFiles[2] != older[1] {
		t.Fatalf("empty tab recent files: %v", quickFiles)
	}
	for _, label := range []string{"最近打开文件", "关闭历史", "最近文件 " + b} {
		if err := s.ClickLabel(label); err != nil {
			t.Fatal(err)
		}
	}
	if m.active.doc.path != b || len(m.tabs) != 3 {
		t.Fatal("quick recent file did not reuse open tab")
	}
	if err := s.DoubleClickLabel("双击新建"); err != nil {
		t.Fatal(err)
	}
	if err := s.Compose("zhongwen"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Locate("最近打开文件"); err == nil {
		t.Fatal("recent card covers IME composition")
	}
	if err := s.Type("中文草稿"); err != nil {
		t.Fatal(err)
	}
	if m.active.source.text.String() != "中文草稿" {
		t.Fatal("recent card lost first Chinese input")
	}
	if _, err := s.Locate("最近打开文件"); err == nil {
		t.Fatal("recent card covers nonempty document")
	}
	for _, label := range []string{"主菜单", "最近打开…", "打开历史 " + b} {
		if err := s.ClickLabel(label); err != nil {
			t.Fatal(err)
		}
	}
	if m.active.doc.path != b || m.showHistory {
		t.Fatal("history did not open file")
	}
	for _, label := range []string{"主菜单", "最近打开…", "移除历史 " + a, "关闭历史"} {
		if err := s.ClickLabel(label); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.save(); err != nil {
		t.Fatal(err)
	}
	restored, err := openSessionStore(store.directory)
	if err != nil || len(restored.history) != 4 {
		t.Fatal("history removal not persisted")
	}
	m.showHistory = true
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("清空打开历史"); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("关闭历史"); err != nil {
		t.Fatal(err)
	}
	m.newDocument()
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Locate("最近打开文件"); err != nil {
		t.Fatal(err)
	}
	for _, hit := range s.Snapshot().Hits {
		if strings.HasPrefix(hit.Label, "最近文件 ") {
			t.Fatal("cleared history still shown in empty tab")
		}
	}
	if err := store.save(); err != nil {
		t.Fatal(err)
	}
	restored, err = openSessionStore(store.directory)
	if err != nil || len(restored.history) != 0 {
		t.Fatal("clear history not persisted")
	}
}

func TestSessionDirectoryLock(t *testing.T) {
	path := t.TempDir()
	release, err := lockSessionDirectory(path)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := lockSessionDirectory(path); err == nil {
		second()
		t.Fatal("two processes could write same session")
	}
	release()
	release, err = lockSessionDirectory(path)
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestPreviewHTMLKeepsRelativeResources(t *testing.T) {
	original := `D:\项目 中文\page.html`
	source := `<!doctype html><!-- <head>fake --><html><head><script>const example="<base href='fake/'>"</script></head><body><img src="图.png"></body></html>`
	output := previewHTML(source, original)
	if !strings.Contains(output, `<head><meta charset="utf-8"><base href="file:///D:/`) || !strings.Contains(output, `<img src="图.png">`) || !strings.Contains(output, `const example="<base href='fake/'>"`) {
		t.Fatal(output)
	}
	withBase := previewHTML(`<head><base href="../assets/"></head>`, original)
	if !strings.Contains(withBase, `<base href="file:///D:/assets/">`) || strings.Count(withBase, `<base href=`) != 1 {
		t.Fatal(withBase)
	}
	if !strings.Contains(previewHTML("<h1>中文</h1>", ""), `<meta charset="utf-8">`) {
		t.Fatal("new HTML charset missing")
	}
	if bytes.Equal([]byte(source), []byte(output)) {
		t.Fatal("preview fixture did not exercise preparation")
	}
}

func TestSessionInactiveTabsAcrossRepeatedRestarts(t *testing.T) {
	directory := t.TempDir()
	project := filepath.Join(directory, "project")
	child := filepath.Join(project, "child")
	if err := os.MkdirAll(child, 0700); err != nil {
		t.Fatal(err)
	}
	imagePath := filepath.Join(project, "image.png")
	if err := os.WriteFile(imagePath, applicationIconPNG, 0600); err != nil {
		t.Fatal(err)
	}
	store, _ := openSessionStore(filepath.Join(directory, "data"))
	m := newTestEditorWithDocument()
	store.attach(m)
	m.addProjects(project)
	if err := m.workspace.toggle(m.workspace.roots[0].children[0]); err != nil {
		t.Fatal(err)
	}
	m.active.name = "草稿.md"
	m.active.source.replace("# 标题\n\n" + strings.Repeat("中文段落\n\n", 80))
	m.active.markdownMode = "分屏"
	m.active.markdown.Offset = 270
	m.active.outlineCollapsed[0] = true
	scroll := gfx.Point{Y: 210}
	m.active.source.restoreScroll = &scroll
	m.active.openSearch()
	m.active.search.query.Insert("中文")
	m.active.search.bounds = gfx.Rect{X: 72, Y: 64, Width: 600, Height: 260}
	m.load(imagePath)
	m.active.picture.zoom = 2
	m.active.picture.pan = gfx.Point{X: 31, Y: 21}
	second := newTestEditorWithDocument()
	store.attach(second)
	second.active.source.replace("第二个窗口的草稿")
	for i := 0; i < 3; i++ {
		if err := store.save(); err != nil {
			t.Fatal(err)
		}
		var err error
		store, err = openSessionStore(store.directory)
		if err != nil {
			t.Fatal(err)
		}
	}
	again := store.windows[0]
	tab := again.tabs[0]
	if len(store.windows) != 2 || store.windows[1].active.source.text.String() != "第二个窗口的草稿" {
		t.Fatal("multiple windows not restored")
	}
	if tab.source.restoreScroll == nil || tab.source.restoreScroll.Y != 210 || tab.markdown.Offset != 270 || !tab.outlineCollapsed[0] || !tab.search.open || tab.search.query.String() != "中文" || tab.search.bounds.X != 72 {
		t.Fatal("inactive view state lost across restarts")
	}
	if again.workspace == nil || !again.workspace.roots[0].children[0].expanded || again.active.picture.zoom != 2 || again.active.picture.pan.X != 31 {
		t.Fatal("folder or image view state not restored")
	}
	if err := os.Remove(imagePath); err != nil {
		t.Fatal(err)
	}
	missing, err := openSessionStore(store.directory)
	if err != nil || missing.windows[0].active.picture.errorText == "" {
		t.Fatalf("missing image recovery: %v", err)
	}
	session := openTestSession(t, missing.windows[0])
	if err := session.Frame(); err != nil {
		t.Fatal(err)
	}
}
