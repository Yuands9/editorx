package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func TestSearchResultsLineGroupingAndScroll(t *testing.T) {
	m := newEditorApp()
	m.addTab(document{saved: strings.Repeat("<fee_type>USD</fee_type> "+strings.Repeat(" ", 180)+"\n", 48)}, "sample.log")
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(s.Resize(1100, 750))
	openSearchForTest(t, s, "fee_type")
	check(s.ClickLabel("查找当前文档全部"))
	r := &m.results
	if r.total != 96 || len(r.rows) != 50 || len(r.groups[0].lines[0].marks) != 2 {
		t.Fatal("expected two highlights per source row and the original total")
	}
	if _, err := s.Locate("横向滚动搜索结果"); err != nil {
		t.Fatal("the result list needs a horizontal scrollbar for long lines", err)
	}
	if directory := os.Getenv("VIVA_PREVIEW_DIR"); directory != "" {
		check(s.Screenshot(filepath.Join(directory, "search-results-grouped.png")))
		m.setLightMode(true)
		check(s.Frame())
		check(s.Screenshot(filepath.Join(directory, "search-results-grouped-light.png")))
	}
	check(s.ClickLabel("结果搜索词 fee_type"))
	if !r.collapsed || len(r.rows) != 1 {
		t.Fatal("search heading did not hide its children")
	}
	check(s.ClickLabel("结果搜索词 fee_type"))
	check(s.ClickLabel("结果分组 sample.log"))
	if r.collapsed || !r.groups[0].collapsed || len(r.rows) != 2 {
		t.Fatal("file heading did not fold its rows")
	}
	check(s.ClickLabel("结果分组 sample.log"))
	check(s.ClickLabel("结果 sample.log 行 1 列 2"))
	if got := m.active.source.text.Selection(); got.Start != 1 || got.End != len("<fee_type") {
		t.Fatalf("row click did not select the first hit: %+v", got)
	}
	check(s.ClickLabel("复制搜索结果"))
	if report := s.Driver().Clipboard(); strings.Count(report, "行 1:") != 1 || !strings.Contains(report, "<fee_type>USD</fee_type>") {
		t.Fatal("the clipboard should include one source line with both matches")
	}
}

func TestLargeEditGroupedSearchResultsReveal(t *testing.T) {
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	path := os.Getenv("VIVA_SEARCH_RESULTS_LOG")
	expectedHits, expectedLines := 4, 2
	if path == "" {
		path = filepath.Join(t.TempDir(), "sample.out")
		line := "INFO " + strings.Repeat("日志", 50) + "\r\n"
		source := "start\r\n<fee_type>CNY</fee_type>\r\n" + strings.Repeat(line, 2400) + "<fee_type>USD</fee_type>\r\n"
		check(os.WriteFile(path, []byte(source), 0600))
	} else {
		expectedHits, expectedLines = 9410, 4705
	}
	page, err := readLargeTextPage(path, 0, -1, "", "")
	check(err)
	m := newEditorApp()
	tab := m.openLoadedFile(openedEditorFile{doc: document{path: path}, largeText: &page})
	defer tab.largeText.close()
	s := openTestSession(t, m)
	check(s.ClickLabel("button:打开完整编辑"))
	check(s.WaitFor(func(app.Snapshot) bool { return tab.largeText.editing != nil }, 5*time.Second))
	check(s.Driver().Key(gfx.KeyEnd, gfx.ModifierControl))
	edit := tab.largeText.editing
	if edit.position.caret != edit.revision.size {
		t.Fatal("fixture caret should start at the end of the log")
	}
	check(s.ClickLabel("button:查找"))
	check(s.Type("fee_type"))
	check(s.ClickLabel("button:查找当前文档全部"))
	check(s.WaitFor(func(app.Snapshot) bool { return m.results.open }, 5*time.Second))
	g := &m.results.groups[0]
	if m.results.total != expectedHits || len(g.lines) != expectedLines {
		t.Fatalf("expected %d hits across %d source lines, got %d hits across %d lines", expectedHits, expectedLines, m.results.total, len(g.lines))
	}
	first := g.lines[0]
	if len(first.marks) != 2 {
		t.Fatal("both hits on the first result row should be marked")
	}
	check(s.ClickLabel(fmt.Sprintf("结果 %s 行 %d 列 %d", tab.title(), first.number, first.column)))
	check(s.Frame())
	begin, end := edit.position.rangeOffsets()
	hit := g.stream.hits[0]
	if begin != int64(hit.span.Start) || end != int64(hit.span.End) {
		t.Fatal("document selection did not move to the first result")
	}
	top, bottom := tab.source.view.Code.VisibleLines()
	if number := first.number - edit.viewport.line - 1; number < top || number > bottom {
		t.Fatalf("selected result line %d is outside the visible editor rows %d–%d", first.number, edit.viewport.line+top+1, edit.viewport.line+bottom+1)
	}
	check(s.Driver().Key(gfx.KeyEnd, gfx.ModifierControl))
	check(s.ClickLabel(fmt.Sprintf("结果 %s 行 %d 列 %d", tab.title(), first.number, first.column)))
	if index, _ := edit.position.rangeOffsets(); index != int64(hit.span.Start) {
		t.Fatal("clicking the already selected result should reveal it again")
	}
	check(s.Driver().Key(gfx.KeyF3, 0))
	if tab.largeText.selectedResult != 1 || m.results.list.Selected() != 2 ||
		tab.source.view.Snapshot.CurrentMark == (text.Span{}) {
		t.Fatal("F3 must advance to the second hit without choosing a different row")
	}
	if os.Getenv("VIVA_SEARCH_RESULTS_LOG") != "" {
		t.Logf("%d matches, %d displayed lines", m.results.total, len(g.lines))
	}
}
