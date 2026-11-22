package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/text"
)

func openSearchForTest(t *testing.T, s *headless.Session, query string) {
	t.Helper()
	if handled, err := s.Driver().ShortcutChord(gfx.ModifierControl, 'f'); err != nil || !handled {
		t.Fatalf("Ctrl+F: %v %v", handled, err)
	}
	if err := s.Type(query); err != nil {
		t.Fatal(err)
	}
}

func TestSearchEveryFileAndUnsavedTab(t *testing.T) {
	for _, name := range []string{"config.json", "config.xml", "readme.md", "app.go", "data.txt", "无后缀", ""} {
		t.Run(name, func(t *testing.T) {
			m := newTestEditorWithDocument()
			source := "前缀😀 中文配置\n第二行 中文配置\n第三行 中文配置"
			tab := m.active
			if name != "" {
				tab = m.addTab(document{path: filepath.Join(t.TempDir(), name), saved: source, eol: "LF"}, name)
			} else {
				tab.source.replace(source)
			}
			s := openTestSession(t, m)
			openSearchForTest(t, s, "中文配置")
			search := &tab.search
			if !search.open || len(search.matches) != 3 || !s.HasText("1 / 3") {
				t.Fatalf("missing search results: %+v", search.matches)
			}
			for _, step := range []struct {
				key   gfx.Key
				mods  gfx.Modifiers
				index int
			}{{gfx.KeyEnter, 0, 1}, {gfx.KeyF3, 0, 2}, {gfx.KeyF3, 0, 0}, {gfx.KeyEnter, gfx.ModifierShift, 2}} {
				if err := s.Driver().Key(step.key, step.mods); err != nil {
					t.Fatal(err)
				}
				match := search.matches[step.index]
				selection := tab.source.text.Selection()
				if search.current != step.index || tab.source.text.ByteOffset(selection.Start) != match.Start || tab.source.text.ByteOffset(selection.End) != match.End {
					t.Fatalf("wrong Unicode selection: %v %v current=%d", selection, match, search.current)
				}
			}
			if tab.source.text.String() != source {
				t.Fatal("search modified document")
			}
			if err := s.Driver().Key(gfx.KeyEscape, 0); err != nil {
				t.Fatal(err)
			}
			if search.open || len(tab.source.view.Snapshot.Marks) != 0 {
				t.Fatal("search did not close")
			}
			if err := s.Compose("zhongwen"); err != nil {
				t.Fatal(err)
			}
			if err := s.Type("替换"); err != nil {
				t.Fatal(err)
			}
			if !strings.HasSuffix(tab.source.text.String(), "第三行 替换") {
				t.Fatal("close did not return editing focus")
			}
		})
	}
}

func TestSearchOptionsEditsTabsAndIME(t *testing.T) {
	m := newTestEditorWithDocument()
	tab := m.active
	tab.source.replace("Cat cat cats 中文 中文 😀")
	s := openTestSession(t, m)
	openSearchForTest(t, s, "cat")
	if len(tab.search.matches) != 3 {
		t.Fatal("case insensitive search")
	}
	if err := s.ClickLabel("区分大小写"); err != nil {
		t.Fatal(err)
	}
	if len(tab.search.matches) != 2 {
		t.Fatal("case sensitive search")
	}
	if err := s.ClickLabel("全词匹配"); err != nil {
		t.Fatal(err)
	}
	if len(tab.search.matches) != 1 {
		t.Fatal("whole word search")
	}
	tab.search.options.WholeWord = false
	tab.search.query.SetSelection(text.Range{Start: 0, End: tab.search.query.RuneCount()})
	tab.search.query.Insert("[")
	if err := s.ClickLabel("正则表达式"); err != nil {
		t.Fatal(err)
	}
	if tab.search.errorText == "" || len(tab.search.matches) != 0 {
		t.Fatal("invalid regex not shown")
	}
	tab.search.query.SetSelection(text.Range{Start: 0, End: tab.search.query.RuneCount()})
	tab.search.query.Insert("cat(s)?")
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if tab.search.errorText != "" || len(tab.search.matches) != 2 {
		t.Fatal("regex search failed")
	}
	tab.search.options.Regexp = false
	tab.search.query.SetSelection(text.Range{Start: 0, End: tab.search.query.RuneCount()})
	tab.search.focus = true
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if err := s.Compose("zhongwen"); err != nil {
		t.Fatal(err)
	}
	if _, ok := tab.search.query.Composition(); !ok {
		t.Fatal("query missing IME")
	}
	if err := s.Type("中文"); err != nil {
		t.Fatal(err)
	}
	if tab.search.query.String() != "中文" || len(tab.search.matches) != 2 {
		t.Fatal("Chinese query failed")
	}
	tab.source.replace("只有 中文")
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if len(tab.search.matches) != 1 {
		t.Fatal("search not refreshed after edit")
	}
	// 居中的查找框会盖住编辑器中心，点击仍露出的正文顶部。
	if err := s.Driver().Click(180, 110); err != nil {
		t.Fatal(err)
	}
	if !tab.source.focused {
		t.Fatal("visible editor did not receive focus")
	}
	if err := s.Driver().Key(gfx.KeyF3, 0); err != nil {
		t.Fatal(err)
	}
	sel := tab.source.text.Selection()
	if string([]rune(tab.source.text.String())[sel.Start:sel.End]) != "中文" {
		t.Fatal("F3 from editor failed")
	}
	m.newDocument()
	second := m.active
	second.source.replace("另一个 中文 中文 中文")
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if second.search.open {
		t.Fatal("search leaked across tabs")
	}
	openSearchForTest(t, s, "另一个")
	m.activate(tab)
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if tab.search.query.String() != "中文" || len(tab.search.matches) != 1 {
		t.Fatal("tab query lost")
	}
	if tab.source.text.String() != "只有 中文" {
		t.Fatal("search changed source")
	}
}

func TestSearchFoldedJSONAndOtherPanes(t *testing.T) {
	m := newEditorApp()
	source := "{\n  \"nested\": {\n    \"name\": \"中文\"\n  },\n  \"other\": {\n    \"v\": 1\n  }\n}"
	tab := m.addTab(document{saved: source, eol: "LF"}, "data.json")
	s := openTestSession(t, m)
	tab.source.foldAll(true)
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	openSearchForTest(t, s, "中文")
	if tab.source.collapsed[0] || tab.source.collapsed[1] || !tab.source.collapsed[4] {
		t.Fatal("search did not expand only matching ancestors")
	}
	if err := s.ClickLabel("关闭搜索"); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("处理结果"); err != nil {
		t.Fatal(err)
	}
	openSearchForTest(t, s, "中文")
	if tab.search.target != tab.result || len(tab.search.matches) != 1 {
		t.Fatal("result search failed")
	}
	if err := s.ClickLabel("对比"); err != nil {
		t.Fatal(err)
	}
	tab.comparison.replace(`{"name":"对比中文"}`)
	tab.comparison.focus = true
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	openSearchForTest(t, s, "对比中文")
	if tab.search.target != tab.comparison || len(tab.search.matches) != 1 {
		t.Fatal("comparison search failed")
	}
	if tab.source.text.String() != source {
		t.Fatal("search corrupted JSON")
	}
}

func TestSearchMarkdownPreviewAndSelectedQuery(t *testing.T) {
	m := newEditorApp()
	tab := m.addTab(document{saved: "# 标题\n\n测试中文\n\n测试中文", eol: "LF"}, "readme.md")
	tab.markdownMode = "预览"
	s := openTestSession(t, m)
	openSearchForTest(t, s, "测试中文")
	if tab.markdownMode != "分屏" || len(tab.search.matches) != 2 {
		t.Fatal("preview search did not reveal source")
	}
	if _, err := s.Locate("代码编辑区"); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("关闭搜索"); err != nil {
		t.Fatal(err)
	}
	tab.source.text.SetSelection(text.Range{Start: 2, End: 4})
	tab.source.focus = true
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Driver().ShortcutChord(gfx.ModifierControl, 'f'); err != nil {
		t.Fatal(err)
	}
	if tab.search.query.String() != "标题" || len(tab.search.matches) != 1 {
		t.Fatal("selected query not reused")
	}
}

func TestSearchLimitsAndNoResults(t *testing.T) {
	m := newTestEditorWithDocument()
	tab := m.active
	tab.source.replace(strings.Repeat("中文 ", 10001))
	tab.openSearch()
	tab.search.query.Insert("中文")
	tab.search.refresh()
	if !tab.search.truncated || len(tab.search.matches) != 10000 {
		t.Fatal("search limit not reported")
	}
	tab.search.query.SetSelection(text.Range{Start: 0, End: tab.search.query.RuneCount()})
	tab.search.query.Insert("不存在")
	tab.search.refresh()
	tab.search.step(1)
	if len(tab.search.matches) != 0 || tab.search.current != -1 || tab.search.truncated || len(tab.source.view.Snapshot.Marks) != 0 {
		t.Fatal("stale matches after no results")
	}
}

func TestUnsavedFormatDetection(t *testing.T) {
	cases := []struct{ value, kind string }{
		{"", "纯文本"}, {"普通文本\nname = value", "纯文本"}, {"[server]\nport=80", "纯文本"},
		{`{"中文":"😀","count":9007199254740993123}`, "JSON"}, {`[{"a":1},2]`, "JSON"}, {"{\n  \"name\":", "JSON"},
		{"# 标题\n\n正文", "Markdown"}, {"- 项目一\n- 项目二", "Markdown"}, {"> 引用", "Markdown"}, {"```go\nfmt.Println(1)\n```", "Markdown"}, {"**加粗**", "Markdown"}, {"[链接](https://example.com)", "Markdown"},
	}
	for _, test := range cases {
		t.Run(test.value, func(t *testing.T) {
			m := newTestEditorWithDocument()
			m.active.source.replace(test.value)
			if got := m.active.format(); got != test.kind {
				t.Fatalf("format=%s want %s", got, test.kind)
			}
		})
	}
	for _, name := range []string{"explicit.txt", "explicit.xml", "explicit.go", "explicit.json", "explicit.md"} {
		m := newEditorApp()
		tab := m.addTab(document{saved: "# 标题", eol: "LF"}, name)
		expected := "纯文本"
		switch filepath.Ext(name) {
		case ".xml":
			expected = "XML"
		case ".go":
			expected = "Go"
		case ".json":
			expected = "JSON"
		case ".md":
			expected = "Markdown"
		}
		if tab.format() != expected {
			t.Fatalf("named format changed: %s", name)
		}
	}
}

func TestUnsavedDetectionKeepsEditingAndUndo(t *testing.T) {
	for _, mode := range []string{"JSON", "Markdown"} {
		t.Run(mode, func(t *testing.T) {
			m := newTestEditorWithDocument()
			tab := m.active
			s := openTestSession(t, m)
			first, second := "{", `"name":"中文"}`
			if mode == "Markdown" {
				first, second = "# ", "中文标题"
			}
			if err := s.Type(first); err != nil {
				t.Fatal(err)
			}
			if err := s.Type(second); err != nil {
				t.Fatal(err)
			}
			if tab.source.text.String() != first+second || tab.format() != mode || tab.doc.path != "" {
				t.Fatalf("auto mode lost typing: %q (%s)", tab.source.text.String(), tab.format())
			}
			if mode == "JSON" {
				if tab.resultError != "" || !strings.Contains(tab.result.text.String(), "中文") {
					t.Fatal("auto JSON result missing")
				}
			} else {
				if len(tab.headings) != 1 || !strings.Contains(tab.markdown.Source, "中文标题") {
					t.Fatal("auto Markdown preview missing")
				}
			}
			if err := s.Compose("ceshi"); err != nil {
				t.Fatal(err)
			}
			if tab.format() != mode {
				t.Fatal("IME changed format")
			}
			if err := s.Type("测试"); err != nil {
				t.Fatal(err)
			}
			if !strings.HasSuffix(tab.source.text.String(), "测试") {
				t.Fatal("IME after detection failed")
			}
			if _, err := s.Driver().ShortcutChord(gfx.ModifierControl, 'z'); err != nil {
				t.Fatal(err)
			}
			if tab.source.text.String() != first+second {
				t.Fatalf("undo lost document: %q", tab.source.text.String())
			}
			tab.source.replace("")
			if err := s.Frame(); err != nil {
				t.Fatal(err)
			}
			if tab.format() != "纯文本" {
				t.Fatal("clearing did not reset mode")
			}
			if err := s.Type("普通文本"); err != nil {
				t.Fatal(err)
			}
			if tab.source.text.String() != "普通文本" {
				t.Fatal("reset lost focus")
			}
			path := filepath.Join(t.TempDir(), "draft.md")
			if err := s.Driver().QueueSaveFile(path, nil); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Driver().ShortcutChord(gfx.ModifierControl, 's'); err != nil {
				t.Fatal(err)
			}
			if tab.doc.path != path || tab.format() != "Markdown" {
				t.Fatal("saved suffix did not take precedence")
			}
		})
	}
}

func TestSearchScrollAndCompositionBoundaries(t *testing.T) {
	m := newTestEditorWithDocument()
	tab := m.active
	original := strings.Repeat("ordinary line\n", 180) + "最后的中文命中"
	tab.source.replace(original)
	s := openTestSession(t, m)
	if err := s.Compose("pinyin"); err != nil {
		t.Fatal(err)
	}
	openSearchForTest(t, s, "中文命中")
	if tab.source.text.String() != original {
		t.Fatal("opening search committed preedit")
	}
	if !s.HasText("最后的中文命中") {
		t.Fatal("offscreen match was not revealed")
	}
	if err := s.Compose("nihao"); err != nil {
		t.Fatal(err)
	}
	m.newDocument()
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if _, ok := tab.search.query.Composition(); ok || tab.search.query.String() != "中文命中" {
		t.Fatal("switching tab committed query preedit")
	}
	m.activate(tab)
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if err := s.Driver().Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if tab.search.open {
		t.Fatal("Esc from source did not close search")
	}
}

func TestUnsavedFormatChangesAcrossKinds(t *testing.T) {
	m := newTestEditorWithDocument()
	tab := m.active
	s := openTestSession(t, m)
	for _, sample := range []struct{ value, kind string }{
		{`[1,`, "JSON"}, {"[链接](https://example.com)", "Markdown"},
		{`{"x":1}`, "JSON"}, {"# 标题", "Markdown"}, {"ordinary text", "纯文本"}, {"# 再次编辑", "Markdown"},
	} {
		tab.source.replace(sample.value)
		if err := s.Frame(); err != nil {
			t.Fatal(err)
		}
		if tab.format() != sample.kind {
			t.Fatalf("%q detected as %s", sample.value, tab.format())
		}
		if sample.kind == "Markdown" {
			tab.markdownMode = "预览"
		}
	}
	// 重新识别 Markdown 时应保留可以继续输入的源文件面板。
	tab.source.replace("ordinary text")
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	tab.source.replace("# 继续")
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if tab.markdownMode != "分屏" {
		t.Fatal("auto detection hid editor")
	}
	if err := s.Type("新文字"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tab.source.text.String(), "新文字") {
		t.Fatal("type switch lost input")
	}
}
