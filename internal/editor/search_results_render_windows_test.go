//go:build windows

package editor

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
)

func TestSearchResultsRenderWideLineAtHighDPI(t *testing.T) {
	const term = "fee_type"
	const tail = "中文😀尾部"
	line := term + strings.Repeat("x\t", 460) + tail + term
	m := newEditorApp()
	tab := m.addTab(document{saved: line + "\n"}, "wide.log")
	page := ui.NewPage(ui.PageConfig{View: m.page, Theme: &m.theme, Styles: &editorControlStyles})
	s, err := headless.Open(ui.Configure(page, app.Config{Width: 1000, Height: 660, OnShortcut: m.windowShortcut, OnKey: m.functionShortcut}), nil, headless.Options{Scale: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	openSearchForTest(t, s, term)
	if err := s.ClickLabel("查找当前文档全部"); err != nil {
		t.Fatal(err)
	}
	if len(m.results.groups) != 1 || m.results.total != 2 || len(m.results.groups[0].lines[0].marks) != 2 {
		t.Fatal("both occurrences should be on the same result line")
	}
	if _, err := s.Image(); err != nil {
		t.Fatalf("render long result at 200%%: %v", err)
	}
	const rowName = "结果 wide.log 行 1 列 1"
	row, err := s.Locate(rowName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Locate("横向滚动搜索结果"); err != nil {
		t.Fatal(err)
	}
	if err := s.Scroll(gfx.ScrollEvent{X: 500, Y: row.Bounds.Y + row.Bounds.Height/2, DeltaX: 50000, Precise: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Image(); err != nil {
		t.Fatalf("render after scrolling to the line end: %v", err)
	}
	var visible, highlighted bool
	for _, label := range s.Snapshot().Labels {
		if label.Bounds.Y < row.Bounds.Y || label.Bounds.Y >= row.Bounds.Bottom() {
			continue
		}
		visible = visible || strings.ContainsRune(label.Value, '尾')
		highlighted = highlighted || label.Value == term && label.Color == m.resultColors().markText
	}
	if !visible || !highlighted {
		t.Fatal("the Unicode text and highlighted match at the far end should be visible after scrolling")
	}
	if err := s.ClickLabel(rowName); err != nil {
		t.Fatal(err)
	}
	if selection := tab.source.text.Selection(); selection.Start != 0 || selection.End != len(term) {
		t.Fatalf("result click did not locate the original match: %+v", selection)
	}
}
