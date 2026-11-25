package editor

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func TestFindDialogResultsAndNavigation(t *testing.T) {
	m := newTestEditorWithDocument()
	first := m.active
	first.source.replace("first 中文命中\n第二行 中文命中\n")
	second := m.addTab(document{saved: "# 中文命中\n\n这是内容", eol: "LF"}, "readme.md")
	m.activate(first)
	s := openTestSession(t, m)
	openSearchForTest(t, s, "中文命中")
	if _, err := s.Locate("查找当前文档全部"); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("计数"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first.search.message, "2 个匹配") {
		t.Fatal("count failed")
	}
	header, err := s.Locate("拖动查找窗口")
	if err != nil {
		t.Fatal(err)
	}
	x, y := header.Bounds.X+20, header.Bounds.Y+10
	before := first.search.bounds
	if err := s.Driver().Press(x, y, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := s.Driver().Move(x-170, y+80); err != nil {
		t.Fatal(err)
	}
	if err := s.Driver().Release(x-170, y+80, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if first.search.bounds.X == before.X || first.search.bounds.Y == before.Y {
		t.Fatal("find dialog did not drag")
	}
	if err := s.ClickLabel("查找全部已打开文档"); err != nil {
		t.Fatal(err)
	}
	if !m.results.open || first.search.open || m.results.total != 3 || len(m.results.groups) != 2 {
		t.Fatalf("results failed: total=%d groups=%d", m.results.total, len(m.results.groups))
	}
	if err := s.ClickLabel("结果 readme.md 行 1 列 3"); err != nil {
		t.Fatal(err)
	}
	if m.active != second {
		t.Fatal("result did not activate matching tab")
	}
	selected := second.source.text.Selection()
	if string([]rune(second.source.text.String())[selected.Start:selected.End]) != "中文命中" {
		t.Fatal("result selection incorrect")
	}
	if err := s.ClickLabel("复制搜索结果"); err != nil {
		t.Fatal(err)
	}
	if report := s.Driver().Clipboard(); !strings.Contains(report, "3 个匹配") || !strings.Contains(report, "readme.md") || !strings.Contains(report, "第二行 中文命中") {
		t.Fatal(report)
	}
	if err := s.ClickLabel("结果分组 readme.md"); err != nil {
		t.Fatal(err)
	}
	if !m.results.groups[1].collapsed {
		t.Fatal("result group did not collapse")
	}
	second.source.replace("# 中文命中\n\n新增 中文命中")
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if m.results.total != 4 {
		t.Fatal("result counts did not refresh")
	}
	if err := s.ClickLabel("关闭搜索结果"); err != nil {
		t.Fatal(err)
	}
	if m.results.open {
		t.Fatal("result panel did not close")
	}
	// 关掉查找窗口后仍可用 F3。
	second.source.focus = true
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if err := s.Driver().Key(gfx.KeyF3, 0); err != nil {
		t.Fatal(err)
	}
	if second.search.current < 0 || second.search.open {
		t.Fatal("F3 requires dialog")
	}
}

func TestFindDialogIMEWrapAndNarrowWindow(t *testing.T) {
	m := newTestEditorWithDocument()
	tab := m.active
	tab.source.replace("中文 中文")
	s := openTestSession(t, m)
	openSearchForTest(t, s, "中文")
	if err := s.Resize(900, 560); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"搜索内容", "查找全部已打开文档", "关闭搜索"} {
		hit, err := s.Locate(name)
		if err != nil || hit.Bounds.X < 0 || hit.Bounds.X+hit.Bounds.Width > 900 || hit.Bounds.Y+hit.Bounds.Height > 560 {
			t.Fatalf("control clipped: %s %+v %v", name, hit, err)
		}
	}
	if err := s.ClickLabel("循环查找"); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("上一个匹配"); err != nil {
		t.Fatal(err)
	}
	if tab.search.current != 0 || tab.search.message != "已到达文档边界" {
		t.Fatal("nonwrapping search wrapped")
	}
	if err := s.Driver().Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if tab.search.open {
		t.Fatal("Esc from dialog button failed")
	}
	if _, err := s.Driver().ShortcutChord(gfx.ModifierControl, 'f'); err != nil {
		t.Fatal(err)
	}
	if err := s.Compose("nihao"); err != nil {
		t.Fatal(err)
	}
	if _, ok := tab.search.query.Composition(); !ok {
		t.Fatal("dialog query IME unavailable")
	}
	if err := s.Type("你好"); err != nil {
		t.Fatal(err)
	}
	if tab.search.query.String() != "你好" || tab.source.text.String() != "中文 中文" {
		t.Fatal("IME changed source or query")
	}
	if err := s.ClickLabel("查找当前文档全部"); err != nil {
		t.Fatal(err)
	}
	if !m.results.open || m.results.total != 0 {
		t.Fatal("no-results panel missing")
	}
}

func TestSearchResultsVirtualizationAndClosedTabs(t *testing.T) {
	m := newTestEditorWithDocument()
	tab := m.active
	tab.source.replace(strings.Repeat("hit 中文\n", 12000))
	second := m.addTab(document{saved: "hit", eol: "LF"}, "second.txt")
	m.activate(tab)
	s := openTestSession(t, m)
	openSearchForTest(t, s, "hit")
	if err := s.ClickLabel("查找全部已打开文档"); err != nil {
		t.Fatal(err)
	}
	if m.results.total != 10000 || !m.results.truncated {
		t.Fatal("results not capped")
	}
	first, last := m.results.list.Visible()
	if last-first > 50 || len(m.results.rows) != 10002 {
		t.Fatal("result list not virtualized")
	}
	m.closeTab(second)
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if len(m.results.groups) != 1 {
		t.Fatal("closed tab retained in results")
	}
	tab.source.replace("nothing")
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if m.results.total != 0 || m.results.truncated {
		t.Fatal("stale result report")
	}
}

func TestJSONScrollSyncBothDirectionsAndToggle(t *testing.T) {
	m := newEditorApp()
	var source strings.Builder
	source.WriteString("{\n\"items\": [\n")
	for i := 0; i < 160; i++ {
		if i > 0 {
			source.WriteString(",\n")
		}
		fmt.Fprintf(&source, `{"id":%d,"name":"中文条目%d"}`, i, i)
	}
	source.WriteString("\n]}")
	tab := m.addTab(document{saved: source.String(), eol: "LF"}, "many.json")
	s := openTestSession(t, m)
	hit, err := s.Locate("代码编辑区")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Scroll(gfx.ScrollEvent{X: hit.Bounds.X + 80, Y: hit.Bounds.Y + 90, DeltaY: 460, Precise: true}); err != nil {
		t.Fatal(err)
	}
	left, right := &tab.source.view.Code, &tab.result.view.Code
	if left.ScrollY() == 0 || right.ScrollY() == 0 {
		t.Fatalf("wheel not synced left=%f right=%f", left.ScrollY(), right.ScrollY())
	}
	ratio := left.ScrollY() / left.MaxScrollY()
	if math.Abs(float64(right.ScrollY()/right.MaxScrollY()-ratio)) > 0.01 {
		t.Fatal("JSON ratios differ")
	}
	// 拖动真实滚动条也走同一联动，不只响应滚轮。
	var scrollbar gfx.Rect
	for _, h := range s.Driver().Snapshot().Hits {
		if h.ID == tab.source.view.Parts().At(0) {
			scrollbar = h.Bounds
			break
		}
	}
	if scrollbar.Height == 0 {
		t.Fatal("missing source scrollbar")
	}
	if err := s.Driver().Press(scrollbar.X+scrollbar.Width/2, scrollbar.Y+scrollbar.Height*0.35, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := s.Driver().Move(scrollbar.X+scrollbar.Width/2, scrollbar.Y+scrollbar.Height*0.55); err != nil {
		t.Fatal(err)
	}
	if err := s.Driver().Release(scrollbar.X+scrollbar.Width/2, scrollbar.Y+scrollbar.Height*0.55, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if math.Abs(float64(left.ScrollY()/left.MaxScrollY()-right.ScrollY()/right.MaxScrollY())) > 0.01 {
		t.Fatal("scrollbar drag not synced")
	}
	originalCaret := tab.source.text.Caret()
	right.SetScrollY(right.MaxScrollY() * 0.7)
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if math.Abs(float64(left.ScrollY()/left.MaxScrollY()-0.7)) > 0.01 || originalCaret != tab.source.text.Caret() {
		t.Fatal("reverse sync moved caret or failed")
	}
	if err := s.ClickLabel("同步滚动"); err != nil {
		t.Fatal(err)
	}
	held := right.ScrollY()
	left.SetScrollY(left.MaxScrollY() * 0.1)
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if right.ScrollY() != held {
		t.Fatal("disabled sync still scrolls")
	}
	if err := s.ClickLabel("同步滚动"); err != nil {
		t.Fatal(err)
	}
	left.SetScrollY(left.MaxScrollY())
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if math.Abs(float64(right.ScrollY()-right.MaxScrollY())) > 1 {
		t.Fatal("JSON bottoms not aligned")
	}
	// 编辑内容使结果重建时不能把左侧拉回顶部。
	tab.source.text.SetSelection(text.Range{Start: tab.source.text.RuneCount(), End: tab.source.text.RuneCount()})
	tab.source.text.Insert(" ")
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if left.ScrollY() == 0 {
		t.Fatal("result refresh reset source scroll")
	}
	if err := s.ClickLabel("压缩"); err != nil {
		t.Fatal(err)
	}
	if right.MaxScrollY() == 0 || left.ScrollY() == 0 || strings.Contains(tab.result.text.String(), "\n") {
		t.Fatal("the single logical line should wrap without resetting source scroll")
	}
}

func TestMarkdownScrollSyncUsesSourceAnchors(t *testing.T) {
	m := newEditorApp()
	var source strings.Builder
	for i := 0; i < 75; i++ {
		fmt.Fprintf(&source, "## 标题 %d\n\n段落 **中文** 描述。\n\n- 条目 A\n- 条目 B\n\n", i)
	}
	tab := m.addTab(document{saved: source.String(), eol: "LF"}, "long.md")
	s := openTestSession(t, m)
	left := &tab.source.view.Code
	left.SetScrollY(left.MaxScrollY() * 0.4)
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	offset, ok := left.OffsetAtY(left.ScrollY())
	expected, found := tab.markdown.View.YAtSource(source.String(), offset)
	if !ok || !found || math.Abs(float64(expected-tab.markdown.Offset)) > 2 {
		t.Fatalf("anchor mismatch %f %f", expected, tab.markdown.Offset)
	}
	tab.markdown.Offset = (tab.markdown.Height() - tab.previewHeight) * 0.7
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	offset, ok = tab.markdown.View.SourceAtY(source.String(), tab.markdown.Offset)
	expected, found = left.YAtOffset(offset)
	if !ok || !found || math.Abs(float64(expected-left.ScrollY())) > 2 {
		t.Fatal("reverse Markdown mapping failed")
	}
	tab.markdown.Offset = tab.markdown.Height() - tab.previewHeight
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if math.Abs(float64(left.ScrollY()-left.MaxScrollY())) > 1 {
		t.Fatal("Markdown bottoms not aligned")
	}
	before := tab.source.text.String()
	if err := s.Resize(900, 560); err != nil {
		t.Fatal(err)
	}
	if tab.source.text.String() != before {
		t.Fatal("sync modified document")
	}
}

func TestApplicationIcon(t *testing.T) {
	for _, light := range []bool{false, true} {
		icon := themedApplicationIcon(light)
		if icon.Size != 512 || len(icon.RGBA) != 512*512*4 {
			t.Fatalf("missing application icon for light=%t", light)
		}
		center, mark := (256*512+256)*4, (180*512+130)*4
		if icon.RGBA[3] != 0 || icon.RGBA[center+3] != 255 || icon.RGBA[mark+3] != 255 {
			t.Fatalf("icon transparency incorrect for light=%t", light)
		}
		if (icon.RGBA[center] > 200) != light || (icon.RGBA[mark] > 200) == light {
			t.Fatalf("tile and mark have the wrong colors for light=%t", light)
		}
	}
}
