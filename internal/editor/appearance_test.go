package editor

import (
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

func TestThemeSwitchAndDefaultSidebar(t *testing.T) {
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	if m.showFiles {
		t.Fatal("sidebar must start closed")
	}
	tab := m.addTab(document{saved: "{\n  \"名称\":\"中文😀\",\n  \"items\":[1,2]\n}", eol: "LF"}, "theme.json")
	session := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(session.ClickLabel("展开文件栏"))
	if !m.showFiles {
		t.Fatal("sidebar cannot be reopened")
	}
	check(session.ClickLabel("收起文件栏"))
	tab.result.foldAll(true)
	check(session.Frame())
	openSearchForTest(t, session, "中文")
	selection := tab.source.text.Selection()
	source := tab.source.text.String()
	check(session.ClickLabel("button:设置"))
	check(session.ClickLabel("外观"))
	check(session.ClickLabel("浅色"))
	check(session.ClickLabel("关闭设置"))
	if !m.light || m.theme.Background != gfx.RGB(255, 255, 255) || tab.source.text.Selection() != selection || tab.source.text.String() != source || !tab.search.open {
		t.Fatal("theme switch lost document/search state")
	}
	darkPane := newEditorPane("", false)
	dark := darkPane.highlightLine("JSON", `"名称":"中文"`, nil)
	light := tab.source.highlightLine("JSON", `"名称":"中文"`, nil)
	if len(light) != 2 || light[0].Color == dark[0].Color || light[1].Color == dark[1].Color {
		t.Fatal("JSON colors did not follow theme")
	}
	if len(tab.result.view.Snapshot.FoldedLines) != 1 {
		t.Fatal("theme change reset folds")
	}
	check(session.ClickLabel("关闭搜索"))
	check(session.Compose("zhongwen"))
	check(session.Type("浅色中文"))
	if !strings.Contains(tab.source.text.String(), "浅色中文") {
		t.Fatal("IME commit lost after theme switch")
	}
	m.newDocument()
	check(session.Frame())
	if !m.active.source.light || !m.active.result.light || !m.active.comparison.light {
		t.Fatal("new tab lost theme")
	}
	check(session.ClickLabel("主菜单"))
	check(session.ClickLabel("menuitem:设置"))
	check(session.ClickLabel("外观"))
	check(session.ClickLabel("深色"))
	check(session.ClickLabel("关闭设置"))
	if m.light || tab.source.light || m.active.source.light {
		t.Fatal("existing tabs did not switch back")
	}
}

func TestFindDragTracksPointerAndClamps(t *testing.T) {
	m := newEditorApp()
	source := "[" + strings.Repeat("{\"name\":\"中文😀\"},", 1999) + "{\"name\":\"中文😀\"}]"
	formatted, _, err := transformJSON(source, "格式化", 2)
	if err != nil {
		t.Fatal(err)
	}
	tab := m.addTab(document{saved: formatted, eol: "LF"}, "drag.json")
	tab.jsonIndent = 2
	session := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	openSearchForTest(t, session, "中文")
	beforeSelection := tab.source.text.Selection()
	header, err := session.Locate("拖动查找窗口")
	check(err)
	x, y := header.Bounds.X+20, header.Bounds.Y+10
	origin := tab.search.bounds
	check(session.Driver().Press(x, y, gfx.PointerButtonPrimary))
	for i := 1; i <= 20; i++ {
		dx, dy := float32(-i*6), float32(i*4)
		check(session.Driver().Move(x+dx, y+dy))
		if tab.search.bounds.X != origin.X+dx || tab.search.bounds.Y != origin.Y+dy {
			t.Fatal("dialog lagged behind pointer")
		}
		moved, err := session.Locate("拖动查找窗口")
		check(err)
		if moved.Bounds.X != header.Bounds.X+dx || moved.Bounds.Y != header.Bounds.Y+dy {
			t.Fatal("drawn hit area lagged behind dialog")
		}
	}
	check(session.Driver().Move(-1000, -1000))
	if tab.search.bounds.X != 12 || tab.search.bounds.Y != 12 {
		t.Fatal("dialog escaped viewport")
	}
	check(session.Driver().Release(-1000, -1000, gfx.PointerButtonPrimary))
	if tab.search.dragging || tab.source.text.String() != formatted || tab.source.text.Selection() != beforeSelection || len(tab.search.matches) != 2000 {
		t.Fatal("drag altered search or document")
	}
	check(session.Resize(900, 560))
	if tab.search.bounds.X+tab.search.bounds.Width > 888 || tab.search.bounds.Y+tab.search.bounds.Height > 548 {
		t.Fatal("dialog outside small viewport")
	}
	check(session.ClickLabel("关闭搜索"))
	check(session.Compose("zhongwen"))
	check(session.Type("拖动后输入"))
	if !strings.Contains(tab.source.text.String(), "拖动后输入") {
		t.Fatal("drag lost editor input focus")
	}
}

func TestEditorSurfaceAndInsertionCaret(t *testing.T) {
	m := newEditorApp()
	tab := m.addTab(document{saved: "中文输入\n第二行", eol: "LF"}, "编辑.txt")
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(s.Frame())
	check(s.ClickLabel("代码编辑区"))
	assertCaret := func(want bool) {
		t.Helper()
		position, ok := tab.source.view.Code.CaretBounds()
		if !ok {
			t.Fatal("missing caret geometry for native IME")
		}
		visible := false
		for _, shape := range s.Snapshot().Primitives {
			if shape.Bounds.Width == position.Width && shape.Bounds.X == position.X && shape.Bounds.Y >= position.Y &&
				shape.Bounds.Y+shape.Bounds.Height <= position.Y+position.Height && shape.Color == m.theme.Foreground {
				visible = true
			}
		}
		if visible != want {
			t.Fatalf("caret visible = %t, wanted %t at %s", visible, want, s.Now())
		}
	}
	for _, light := range []bool{false, true} {
		m.setLightMode(light)
		check(s.Frame())
		pane, err := s.Locate("代码编辑区")
		check(err)
		for _, shape := range s.Snapshot().Primitives {
			if shape.Bounds == pane.Bounds && shape.Color == m.theme.Primary {
				t.Fatalf("edit pane drew a focus outline (light=%t)", light)
			}
		}
	}
	const half = components.CaretBlinkHalf
	assertCaret(true)
	check(s.Advance(half + 15*time.Millisecond))
	assertCaret(false)
	check(s.Advance(half))
	assertCaret(true)
	check(s.Advance(41 * half))
	assertCaret(false)
	check(s.Type("新"))
	assertCaret(true)
	openSearchForTest(t, s, "不存在")
	assertCaret(false)
}
