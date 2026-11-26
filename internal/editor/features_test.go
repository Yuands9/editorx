package editor

import (
	"bytes"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

var examplesDirectory = filepath.Join("..", "..", "examples")

func newTestEditorWithDocument() *editorApp {
	m := newEditorApp()
	m.newDocument()
	return m
}

func openTestSession(t *testing.T, m *editorApp) *headless.Session {
	t.Helper()
	page := ui.NewPage(ui.PageConfig{View: m.page, Theme: &m.theme, Styles: &editorControlStyles})
	session, err := headless.Open(ui.Configure(page, app.Config{
		Width: 1440, Height: 900, Invalidator: &m.invalidator, OnShortcut: m.windowShortcut, OnKey: m.functionShortcut,
		OnFileDrop: m.dropFiles, OnWake: m.openDroppedFiles,
	}), nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Close)
	return session
}

func TestJSONTransformsPreserveValues(t *testing.T) {
	source := "{\"z\":900719925474099312345,\"a\":{\"y\":1e+30,\"b\":\"中文<&>\"},\"list\":[3,1,2],\"a\":false}"
	formatted, _, err := transformJSON(source, "格式化", 4)
	if err != nil || !strings.Contains(formatted, "\n") || strings.Index(formatted, "\"z\"") > strings.Index(formatted, "\"a\"") {
		t.Fatalf("format: %s, %v", formatted, err)
	}
	compressed, _, err := transformJSON(formatted, "压缩", 4)
	if err != nil || compressed != source {
		t.Fatalf("compact: %s, %v", compressed, err)
	}
	sorted, _, err := transformJSON(source, "键排序", 4)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(sorted, "\"a\"") > strings.Index(sorted, "\"z\"") || !strings.Contains(sorted, "900719925474099312345") || !strings.Contains(sorted, "1e+30") || strings.Count(sorted, "\"a\"") != 2 {
		t.Fatal(sorted)
	}
	if strings.Index(sorted, "\"b\"") > strings.Index(sorted, "\"y\"") {
		t.Fatal("nested object not sorted")
	}
	var compact bytes.Buffer
	value, err := parseJSON(sorted)
	if err != nil {
		t.Fatal(err)
	}
	value.writeJSON(&compact)
	if !strings.Contains(compact.String(), "\"list\":[3,1,2]") {
		t.Fatal("array reordered")
	}
	output, kind, err := transformJSON(source, "转 XML", 4)
	if err != nil || kind != "XML" {
		t.Fatalf("xml: %s, %v", output, err)
	}
	decoder := xml.NewDecoder(strings.NewReader(output))
	for {
		_, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if !strings.Contains(output, "中文&lt;&amp;&gt;") || !strings.Contains(output, "900719925474099312345") || strings.Count(output, "name=\"a\"") != 2 {
		t.Fatal(output)
	}
	for _, source := range []string{"\"\u0000\"", "{\"\u0001\":true}"} {
		if _, _, err := transformJSON(source, "转 XML", 4); err == nil {
			t.Fatal("unrepresentable XML character silently replaced")
		}
	}
	for _, invalid := range []string{"", "{", "{\"a\":1,}", "true false"} {
		for _, action := range []string{"格式化", "压缩", "键排序", "转 XML"} {
			if _, _, err := transformJSON(invalid, action, 4); err == nil {
				t.Fatalf("accepted %q for %s", invalid, action)
			}
		}
	}
}

func TestMultipleTabsAndClosePrompts(t *testing.T) {
	directory := t.TempDir()
	first, second := filepath.Join(directory, "一.json"), filepath.Join(directory, "二.md")
	if err := os.WriteFile(first, []byte("{\"b\":2,\"a\":1}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("# 第二个文件\n"), 0600); err != nil {
		t.Fatal(err)
	}
	m := newEditorApp()
	s := openTestSession(t, m)
	if err := s.Driver().QueueOpenFiles([]string{first, second}, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("主菜单"); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("打开…"); err != nil {
		t.Fatal(err)
	}
	if len(m.tabs) != 2 || m.active.doc.path != second {
		t.Fatalf("tabs = %d", len(m.tabs))
	}
	a, b := m.tabs[0], m.tabs[1]
	if err := s.ClickLabel("一.json"); err != nil {
		t.Fatal(err)
	}
	if m.active != a {
		t.Fatal("tab click failed")
	}
	a.source.text.Insert("中文")
	a.source.text.BreakUndoGroup()
	aCaret := a.source.text.Caret()
	if err := s.ClickLabel("二.md"); err != nil {
		t.Fatal(err)
	}
	b.source.text.Insert("标题")
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("● 一.json"); err != nil {
		t.Fatal(err)
	}
	if a.source.text.Caret() != aCaret || !a.source.text.Undo() || a.source.text.String() != a.doc.saved || !strings.Contains(b.source.text.String(), "标题") {
		t.Fatal("tab state leaked")
	}
	m.togglePin(b)
	if m.tabs[0] != b || !b.pinned {
		t.Fatal("pin did not move tab to front")
	}
	m.togglePin(b)
	if b.pinned {
		t.Fatal("unpin failed")
	}
	m.load(first)
	if len(m.tabs) != 2 || m.active != a {
		t.Fatal("opened duplicate path")
	}
	m.closeTab(b)
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if !m.confirm || m.confirmTab != b {
		t.Fatal("missing close prompt")
	}
	if err := s.ClickLabel("取消"); err != nil {
		t.Fatal(err)
	}
	if len(m.tabs) != 2 || !b.modified() || m.confirm {
		t.Fatal("cancel lost file")
	}
	m.closeTab(b)
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if err := s.Driver().QueueSaveFile("", app.ErrCanceled); err != nil {
		t.Fatal(err)
	}
	// 已命名文件直接保存；另存为取消不改变当前文件或脏状态。
	if m.saveTab(b, true) || !b.modified() {
		t.Fatal("save-as cancel changed file")
	}
	if err := s.ClickLabel("保存并继续"); err != nil {
		t.Fatal(err)
	}
	if len(m.tabs) != 1 || m.confirm {
		t.Fatal("save-and-close failed")
	}
	data, err := os.ReadFile(second)
	if err != nil || !strings.Contains(string(data), "标题") {
		t.Fatal("save-and-close lost change")
	}
	if err := s.DoubleClickLabel("双击新建"); err != nil {
		t.Fatal(err)
	}
	if len(m.tabs) != 2 || m.active.doc.path != "" {
		t.Fatal("blank strip double click did not create tab")
	}
	m.active.source.text.Insert("discard")
	m.closeTab(m.active)
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("放弃修改"); err != nil {
		t.Fatal(err)
	}
	if len(m.tabs) != 1 {
		t.Fatal("discard close failed")
	}
}

func TestJSONPanelButtonsAndEditableResult(t *testing.T) {
	m := newEditorApp()
	tab := m.addTab(document{saved: "{\"b\":2,\"a\":{\"c\":1}}", eol: "LF"}, "test.json")
	s := openTestSession(t, m)
	if err := s.ClickLabel("键排序"); err != nil {
		t.Fatal(err)
	}
	if tab.resultError != "" || strings.Index(tab.result.text.String(), "\"a\"") > strings.Index(tab.result.text.String(), "\"b\"") {
		t.Fatal(tab.resultError)
	}
	if err := s.ClickLabel("复制"); err != nil {
		t.Fatal(err)
	}
	if clipboard := s.Driver().Clipboard(); clipboard != tab.result.text.String() {
		t.Fatalf("clipboard = %q", clipboard)
	}
	original := tab.source.text.String()
	if err := s.ClickLabel("button:应用到原文"); err != nil {
		t.Fatal(err)
	}
	if tab.source.text.String() == original || !tab.source.text.Undo() || tab.source.text.String() != original {
		t.Fatal("apply is not one undo operation")
	}
	if err := s.ClickLabel("压缩"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(tab.result.text.String(), "\n") {
		t.Fatal("not compressed")
	}
	if err := s.ClickLabel("格式化"); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("折叠"); err != nil {
		t.Fatal(err)
	}
	if len(tab.result.view.Snapshot.FoldedLines) == 0 {
		t.Fatal("not folded")
	}
	if err := s.ClickLabel("复制"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s.Driver().Clipboard(), "\"c\"") {
		t.Fatal("copy used only visible lines")
	}
	if err := s.ClickLabel("展开"); err != nil {
		t.Fatal(err)
	}
	if len(tab.result.view.Snapshot.FoldedLines) != 0 {
		t.Fatal("not expanded")
	}
	result := tab.result.text.String()
	if err := s.ClickLabel("JSON 处理结果"); err != nil {
		t.Fatal(err)
	}
	value := tab.result.text.RuneIndex(strings.LastIndex(result, "1"))
	tab.result.text.SetSelection(text.Range{Start: value, End: value + 1})
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if err := s.Type("4"); err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(result, "\"c\": 1", "\"c\": 4", 1)
	if tab.result.text.String() != updated || tab.source.text.String() != updated {
		t.Fatal("the right edit did not reach the source")
	}
	if err := s.ClickLabel("转 XML"); err != nil {
		t.Fatal(err)
	}
	if tab.resultKind != "XML" || !strings.HasPrefix(tab.result.text.String(), "<?xml") {
		t.Fatal("XML UI failed")
	}
	if err := s.ClickLabel("button:在新标签打开结果"); err != nil {
		t.Fatal(err)
	}
	if m.active == tab || m.active.format() != "XML" || tab.source.text.String() != updated {
		t.Fatal("XML overwrote JSON")
	}
	m.activate(tab)
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "导出.xml")
	if err := s.Driver().QueueSaveFile(path, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("另存"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "<root") {
		t.Fatalf("export %s %v", data, err)
	}
}

func TestFolderNavigationAndMarkdownOutline(t *testing.T) {
	directory := t.TempDir()
	subdirectory := filepath.Join(directory, "子目录")
	if err := os.Mkdir(subdirectory, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(subdirectory, "说明.md")
	content := "# 项目说明\n\n正文 **粗体**。\n\n## 安装\n\n" + strings.Repeat("内容段落。\n\n", 50) + "### Windows\n\n使用步骤。\n\n## 配置\n\n结束。\n\n" + "~~~md\n# 代码块中的标题\n~~~\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	m := newEditorApp()
	s := openTestSession(t, m)
	if err := s.Driver().QueueOpenFiles([]string{directory}, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("主菜单"); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("添加项目…"); err != nil {
		t.Fatal(err)
	}
	if m.workspace == nil || len(m.workspace.rows) != 2 || m.workspace.rows[1].node.loaded {
		t.Fatal("tree should load children lazily")
	}
	if err := s.ClickLabel("子目录"); err != nil {
		t.Fatal(err)
	}
	if len(m.workspace.rows) != 3 {
		t.Fatal("folder did not expand")
	}
	if err := s.ClickLabel("说明.md"); err != nil {
		t.Fatal(err)
	}
	tab := m.active
	if tab.doc.path != path || len(tab.headings) != 4 || tab.markdown.Height() == 0 {
		t.Fatalf("markdown: %s, %d headings", tab.doc.path, len(tab.headings))
	}
	if err := s.ClickLabel("Windows"); err != nil {
		t.Fatal(err)
	}
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if tab.source.text.ByteOffset(tab.source.text.Caret()) != tab.headings[2].offset || tab.markdown.Offset <= 0 {
		t.Fatalf("outline jump: caret=%d want=%d offset=%f height=%f viewport=%f pending=%d", tab.source.text.ByteOffset(tab.source.text.Caret()), tab.headings[2].offset, tab.markdown.Offset, tab.markdown.Height(), tab.previewHeight, tab.previewJump)
	}
	if err := s.ClickLabel("折叠章节 项目说明"); err != nil {
		t.Fatal(err)
	}
	if !tab.outlineCollapsed[tab.headings[0].offset] {
		t.Fatal("outline disclosure did not collapse")
	}
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Locate("Windows"); err == nil {
		t.Fatal("collapsed child still visible")
	}
	if err := s.ClickLabel("全部展开"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Locate("Windows"); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("全部折叠"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Locate("项目说明"); err != nil {
		t.Fatal("folding all should keep the top-level heading visible")
	}
	if _, err := s.Locate("安装"); err == nil {
		t.Fatal("folding all should hide child headings")
	}
	if err := s.ClickLabel("展开章节 项目说明"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Locate("安装"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Locate("Windows"); err == nil {
		t.Fatal("the nested section should stay folded when its parent opens")
	}
	if err := s.ClickLabel("全部展开"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Locate("Windows"); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("编辑"); err != nil {
		t.Fatal(err)
	}
	if tab.markdownMode != "编辑" {
		t.Fatal("edit mode")
	}
	tab.source.text.Insert("新增内容")
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("预览"); err != nil {
		t.Fatal(err)
	}
	if tab.markdown.Source != tab.source.text.String() {
		t.Fatal("preview stale")
	}
	if err := s.ClickLabel("分屏"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Locate("代码编辑区"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Locate("Markdown 预览"); err != nil {
		t.Fatal(err)
	}
}

func TestCloseWindowChecksEveryDirtyTab(t *testing.T) {
	m := newTestEditorWithDocument()
	m.active.source.text.Insert("one")
	first := m.active
	m.newDocument()
	m.active.source.text.Insert("two")
	second := m.active
	s := openTestSession(t, m)
	closed := false
	m.requestClose(func() { closed = true })
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if closed || m.confirmTab != first {
		t.Fatal("first dirty tab skipped")
	}
	if err := s.ClickLabel("放弃修改"); err != nil {
		t.Fatal(err)
	}
	if closed || m.confirmTab != second {
		t.Fatal("second dirty tab skipped")
	}
	if err := s.ClickLabel("取消"); err != nil {
		t.Fatal(err)
	}
	if closed || len(m.tabs) != 1 || m.active != second || !second.modified() {
		t.Fatal("cancel window close failed")
	}
	m.requestClose(func() { closed = true })
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("放弃修改"); err != nil {
		t.Fatal(err)
	}
	if !closed {
		t.Fatal("window did not close")
	}
}

func TestSwitchTabCancelsUncommittedComposition(t *testing.T) {
	m := newTestEditorWithDocument()
	s := openTestSession(t, m)
	if err := s.Type("中文"); err != nil {
		t.Fatal(err)
	}
	if err := s.Compose("pinyin"); err != nil {
		t.Fatal(err)
	}
	old := m.active
	m.newDocument()
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if old.source.text.String() != "中文" {
		t.Fatalf("uncommitted syllable leaked: %q", old.source.text.String())
	}
	if err := s.Type("另一个标签"); err != nil {
		t.Fatal(err)
	}
	if old.source.text.String() != "中文" || m.active.source.text.String() != "另一个标签" {
		t.Fatal("IME edited another tab")
	}

}

func TestTabOverflowAndKeyboardNavigation(t *testing.T) {
	m := newTestEditorWithDocument()
	for i := 0; i < 11; i++ {
		m.newDocument()
	}
	s := openTestSession(t, m)
	last := m.active
	hit, err := s.Locate(last.title())
	if err != nil || hit.Bounds.X >= 1440 || hit.Bounds.X+hit.Bounds.Width <= 220 {
		t.Fatalf("last tab offscreen: %+v, %v", hit, err)
	}
	if err := s.Driver().Key(gfx.KeyTab, gfx.ModifierControl); err != nil {
		t.Fatal(err)
	}
	if m.active != m.tabs[0] {
		t.Fatal("Ctrl+Tab did not wrap")
	}
	if err := s.Driver().Key(gfx.KeyTab, gfx.ModifierControl|gfx.ModifierShift); err != nil {
		t.Fatal(err)
	}
	if m.active != last {
		t.Fatal("Ctrl+Shift+Tab did not go back")
	}
	if err := s.ClickLabel("固定 " + last.title()); err != nil {
		t.Fatal(err)
	}
	if m.tabs[0] != last || !last.pinned {
		t.Fatal("pin click failed")
	}
	if err := s.ClickLabel("取消固定 " + last.title()); err != nil {
		t.Fatal(err)
	}
	if last.pinned {
		t.Fatal("unpin click failed")
	}
	if err := s.ClickLabel("关闭 " + last.title()); err != nil {
		t.Fatal(err)
	}
	if len(m.tabs) != 11 || m.active == last {
		t.Fatal("close icon failed")
	}
	opened := 0
	m.newWindow = func() { opened++ }
	if _, err := s.Driver().ShortcutChord(gfx.ModifierControl|gfx.ModifierShift, 'n'); err != nil {
		t.Fatal(err)
	}
	if opened != 1 {
		t.Fatal("new window shortcut failed")
	}
}

func TestJSONIndividualFoldMarker(t *testing.T) {
	m := newEditorApp()
	tab := m.addTab(document{saved: "{\n    \"a\": {\n        \"b\": 1\n    }\n}", eol: "LF"}, "fold.json")
	s := openTestSession(t, m)
	marker := tab.result.view.Snapshot.FoldMarkers[1]
	found := false
	for _, hit := range s.Snapshot().Hits {
		if hit.ID == marker.ID {
			found = true
			if err := s.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if !found || !tab.result.collapsed[marker.Line] || len(tab.result.view.Snapshot.FoldedLines) != 1 {
		t.Fatal("individual fold marker did not collapse")
	}
}

func TestSaveAsDoesNotOverwriteAnotherOpenTab(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "original.json")
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	m := newEditorApp()
	m.load(path)
	m.newDocument()
	m.active.source.text.Insert("replacement")
	s := openTestSession(t, m)
	if err := s.Driver().QueueSaveFile(path, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Driver().ShortcutChord(gfx.ModifierControl|gfx.ModifierShift, 's'); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "{}" || !m.showError || !m.active.modified() {
		t.Fatal("save-as overwrote another open tab")
	}
}

func TestEdgePanelsAndCompositionAfterResize(t *testing.T) {
	m := newEditorApp()
	m.addProjects(examplesDirectory)
	m.load(filepath.Join(examplesDirectory, "使用指南.md"))
	tab := m.active
	if tab == nil {
		t.Fatal(m.errorText)
	}
	source := tab.source.text.String()
	s := openTestSession(t, m)
	if err := s.Resize(900, 560); err != nil {
		t.Fatal(err)
	}
	for _, hit := range s.Snapshot().Hits {
		switch hit.Label {
		case "新建", "打开…", "添加项目…", "保存", "另存为…", "文件栏", "大纲":
			t.Fatalf("old toolbar still present: %s", hit.Label)
		}
	}
	menu, err := s.Locate("主菜单")
	if err != nil || menu.Bounds.X > workspaceInset+10 {
		t.Fatalf("main menu not on left edge: %+v %v", menu, err)
	}
	left, err := s.Locate("收起文件栏")
	if err != nil || left.Bounds.X <= menu.Bounds.X+menu.Bounds.Width {
		t.Fatalf("sidebar toggle not to the right of the title: %+v %v", left, err)
	}
	outlineToggle, err := s.Locate("收起大纲")
	if err != nil {
		t.Fatal(err)
	}
	syncScroll, err := s.Locate("同步滚动")
	if err != nil || outlineToggle.Bounds.Y != syncScroll.Bounds.Y || outlineToggle.Bounds.X+outlineToggle.Bounds.Width >= syncScroll.Bounds.X {
		t.Fatalf("outline toggle should start the Markdown toolbar: outline=%+v sync=%+v %v", outlineToggle, syncScroll, err)
	}
	before, err := s.Locate("textfield:代码编辑区")
	if err != nil {
		t.Fatal(err)
	}
	foldHeadings, err := s.Locate("全部折叠")
	if err != nil || foldHeadings.Bounds.X+foldHeadings.Bounds.Width >= before.Bounds.X {
		t.Fatalf("outline panel should sit to the left of the source editor: %+v %v", foldHeadings, err)
	}
	if err := s.ClickLabel("收起大纲"); err != nil {
		t.Fatal(err)
	}
	reopen, err := s.Locate("展开大纲")
	if err != nil || reopen.Bounds != outlineToggle.Bounds {
		t.Fatalf("closing the outline moved the toggle from %v to %v: %v", outlineToggle.Bounds, reopen.Bounds, err)
	}
	if err := s.ClickLabel("收起文件栏"); err != nil {
		t.Fatal(err)
	}
	after, err := s.Locate("textfield:代码编辑区")
	if err != nil || after.Bounds.Width <= before.Bounds.Width+100 || m.showFiles || m.showOutline {
		t.Fatalf("collapsed panels did not release editor space: before=%+v after=%+v %v", before, after, err)
	}
	if err := s.Compose("zhongwen"); err != nil {
		t.Fatal(err)
	}
	if err := s.Type("中文"); err != nil {
		t.Fatal(err)
	}
	if tab.source.text.String() != "中文"+source {
		t.Fatal("panel toggle lost editor focus or composition")
	}
	for _, label := range []string{"展开文件栏", "展开大纲"} {
		if err := s.ClickLabel(label); err != nil {
			t.Fatal(err)
		}
	}
	if m.active != tab || !m.showFiles || !m.showOutline || !tab.source.text.Undo() || tab.source.text.String() != source {
		t.Fatal("panel toggle changed tab state or undo history")
	}
	if _, err := s.Driver().ShortcutChord(gfx.ModifierControl, 'b'); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Driver().ShortcutChord(gfx.ModifierControl|gfx.ModifierAlt, 'b'); err != nil {
		t.Fatal(err)
	}
	if m.showFiles || m.showOutline {
		t.Fatal("sidebar shortcuts failed")
	}
	if err := s.DoubleClickLabel("双击新建"); err != nil {
		t.Fatal(err)
	}
	if len(m.tabs) != 2 || m.active == tab || m.active.source.text.String() != "" {
		t.Fatal("double click did not create exactly one blank tab")
	}
	if _, err := s.Locate("展开大纲"); err == nil {
		t.Fatal("outline toggle should be absent on a plain text tab")
	}
	if err := s.Type("新标签"); err != nil {
		t.Fatal(err)
	}
	if m.active.source.text.String() != "新标签" || tab.source.text.String() != source {
		t.Fatal("new tab focus or isolation failed")
	}
}

func TestMainMenuSaveAndTabContextMenu(t *testing.T) {
	m := newTestEditorWithDocument()
	s := openTestSession(t, m)
	if err := s.Type("中文配置"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "无后缀")
	if err := s.Driver().QueueSaveFile(path, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("主菜单"); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("保存"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "中文配置" || m.active.modified() || m.fileMenu {
		t.Fatalf("menu save failed: %q %v", data, err)
	}
	copyPath := filepath.Join(filepath.Dir(path), "另存.xml")
	if err := s.Driver().QueueSaveFile(copyPath, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("主菜单"); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("另存为…"); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(copyPath)
	if err != nil || string(data) != "中文配置" || m.active.doc.path != copyPath {
		t.Fatalf("menu save-as failed: %q %v", data, err)
	}
	if err := s.RightClickLabel(m.active.title()); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("固定标签"); err != nil {
		t.Fatal(err)
	}
	if !m.active.pinned {
		t.Fatal("context menu pin failed")
	}
	if err := s.RightClickLabel(m.active.title()); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("取消固定标签"); err != nil {
		t.Fatal(err)
	}
	if m.active.pinned {
		t.Fatal("context menu unpin failed")
	}
	if err := s.ClickLabel("自动折行"); err != nil {
		t.Fatal(err)
	}
	if !m.active.source.view.Snapshot.Wrap {
		t.Fatal("wrap icon failed")
	}
	if _, err := s.Driver().ShortcutChord(gfx.ModifierAlt, 'z'); err != nil {
		t.Fatal(err)
	}
	if m.active.source.view.Snapshot.Wrap {
		t.Fatal("wrap shortcut failed")
	}
	if err := s.ClickLabel("主菜单"); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("新建"); err != nil {
		t.Fatal(err)
	}
	if len(m.tabs) != 2 || m.active.doc.path != "" {
		t.Fatal("menu new tab failed")
	}
	if err := s.ClickLabel("主菜单"); err != nil {
		t.Fatal(err)
	}
	if err := s.Driver().Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if m.fileMenu {
		t.Fatal("Escape did not dismiss menu")
	}
}

func TestDirectOpenActionsExpandedAndCollapsed(t *testing.T) {
	for _, expanded := range []bool{true, false} {
		name := "collapsed"
		if expanded {
			name = "expanded"
		}
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			var paths []string
			for _, name := range []string{"配置.json", "配置.xml", "说明.md", "无后缀"} {
				path := filepath.Join(directory, name)
				if err := os.WriteFile(path, []byte("中文内容"), 0600); err != nil {
					t.Fatal(err)
				}
				paths = append(paths, path)
			}
			m := newTestEditorWithDocument()
			m.showFiles = expanded
			draft := m.active
			s := openTestSession(t, m)
			if err := s.Resize(900, 560); err != nil {
				t.Fatal(err)
			}
			if err := s.Type("未保存草稿"); err != nil {
				t.Fatal(err)
			}
			for _, label := range []string{"打开文件", "添加项目"} {
				hit, err := s.Locate(label)
				if err != nil || hit.Disabled || hit.Bounds.Y > 160 || hit.Bounds.X < 0 || hit.Bounds.X+hit.Bounds.Width > 253 {
					t.Fatalf("one-click action unavailable: %s %+v %v", label, hit, err)
				}
			}
			if err := s.Driver().QueueOpenFiles(nil, app.ErrCanceled); err != nil {
				t.Fatal(err)
			}
			if err := s.ClickLabel("打开文件"); err != nil {
				t.Fatal(err)
			}
			if m.active != draft || len(m.tabs) != 1 || m.fileMenu || m.showError {
				t.Fatal("canceled open changed document or required menu")
			}
			if err := s.Compose("zhongwen"); err != nil {
				t.Fatal(err)
			}
			if err := s.Type("中文"); err != nil {
				t.Fatal(err)
			}
			if draft.source.text.String() != "未保存草稿中文" {
				t.Fatal("cancel did not restore editor focus for IME")
			}
			if err := s.Driver().QueueOpenFiles(nil, app.ErrCanceled); err != nil {
				t.Fatal(err)
			}
			if err := s.ClickLabel("添加项目"); err != nil {
				t.Fatal(err)
			}
			if err := s.Type("继续"); err != nil {
				t.Fatal(err)
			}
			if m.workspace != nil || m.showFiles != expanded || m.active != draft || draft.source.text.String() != "未保存草稿中文继续" {
				t.Fatal("folder cancel changed workspace or lost editing focus")
			}
			if err := s.Driver().QueueOpenFiles(paths, nil); err != nil {
				t.Fatal(err)
			}
			if err := s.ClickLabel("打开文件"); err != nil {
				t.Fatal(err)
			}
			if len(m.tabs) != 5 || m.active.doc.path != paths[3] || m.fileMenu || m.showFiles != expanded {
				t.Fatal("single click did not open all selected formats")
			}
			for i, path := range paths {
				if m.tabs[i+1].doc.path != path || m.tabs[i+1].source.text.String() != "中文内容" {
					t.Fatalf("file not loaded: %s", path)
				}
			}
			if err := s.Driver().QueueOpenFiles([]string{paths[1]}, nil); err != nil {
				t.Fatal(err)
			}
			if err := s.ClickLabel("打开文件"); err != nil {
				t.Fatal(err)
			}
			if len(m.tabs) != 5 || m.active.doc.path != paths[1] {
				t.Fatal("direct open duplicated existing tab")
			}
			current := m.active
			if err := s.Driver().QueueOpenFiles([]string{directory}, nil); err != nil {
				t.Fatal(err)
			}
			if err := s.ClickLabel("添加项目"); err != nil {
				t.Fatal(err)
			}
			if m.workspace == nil || m.workspace.roots[0].path != directory || !m.showFiles || m.active != current || m.fileMenu {
				t.Fatal("direct folder open did not reveal directory or changed current tab")
			}
			if len(m.workspace.rows) != 5 {
				t.Fatal("folder contents missing")
			}
			if draft.source.text.String() != "未保存草稿中文继续" || !draft.modified() {
				t.Fatal("opening paths lost unsaved draft")
			}
		})
	}
}

func TestJSONCompactDoesNotBecomeFoldedFormatting(t *testing.T) {
	m := newEditorApp()
	source := "{\n  \"name\": \"中文配置\",\n  \"nested\": {\"enabled\": true},\n  \"items\": [1,2,3]\n}"
	tab := m.addTab(document{saved: source, eol: "LF"}, "压缩验证.json")
	s := openTestSession(t, m)
	want := "{\"name\":\"中文配置\",\"nested\":{\"enabled\":true},\"items\":[1,2,3]}"
	if err := s.ClickLabel("折叠"); err != nil {
		t.Fatal(err)
	}
	if len(tab.result.view.Snapshot.FoldedLines) == 0 {
		t.Fatal("setup: formatted JSON was not folded")
	}
	if err := s.ClickLabel("压缩"); err != nil {
		t.Fatal(err)
	}
	if tab.jsonAction != "压缩" || tab.result.text.String() != want || len(tab.result.view.Snapshot.FoldedLines) != 0 || len(tab.result.view.Snapshot.FoldMarkers) != 0 {
		t.Fatal("compact result retained folding or lost content")
	}
	for _, label := range []string{"折叠", "展开"} {
		var hit gfx.Hit
		for _, candidate := range s.Snapshot().Hits {
			if candidate.Label == label {
				hit = candidate
				break
			}
		}
		if hit.ID.IsZero() || !hit.Disabled {
			t.Fatalf("single-line fold control not disabled: %s", label)
		}
		if err := s.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
			t.Fatal(err)
		}
		if tab.jsonAction != "压缩" || tab.result.text.String() != want || len(tab.result.view.Snapshot.FoldedLines) != 0 {
			t.Fatalf("%s changed compact JSON into folded formatting", label)
		}
	}
	if err := s.ClickLabel("复制"); err != nil {
		t.Fatal(err)
	}
	if s.Driver().Clipboard() != want || tab.source.text.String() != source {
		t.Fatal("compact copy changed content")
	}
	path := filepath.Join(t.TempDir(), "压缩.json")
	if err := s.Driver().QueueSaveFile(path, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("另存"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != want {
		t.Fatalf("compact export: %q %v", data, err)
	}
}
