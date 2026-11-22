package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestJSONFoldSummaryLabels(t *testing.T) {
	for _, sample := range []struct{ source, display string }{
		{"{\n  \"x\":1\n}", "Object{...}"},
		{"[\n  {\"a\":1},\n  [1,2,3]\n]", "Array[2]"},
		{"[\n]", "Array[0]"},
		{"{\r\n  \"中文\":true\r\n}\r\n", "Object{...}"},
	} {
		folds := jsonFolds(sample.source)
		if len(folds) == 0 || folds[0].Display != sample.display {
			t.Fatalf("root summary: %+v, want %s", folds, sample.display)
		}
	}
	source := "{\n  \"中文\\\"键\": [\n    \"a,b,[{}]\",\n    [1,2,3],\n    {\"x\":true}\n  ],\n  \"tail\": 1\n}"
	folds := jsonFolds(source)
	if len(folds) != 2 || folds[1].Display != "  \"中文\\\"键\": Array[3]," || folds[1].LastLine != 5 {
		t.Fatalf("nested array counted incorrectly or punctuation lost: %+v", folds)
	}
	items := make([]string, 288)
	for i := range items {
		items[i] = fmt.Sprintf(`{"id":%d}`, i)
	}
	large, _, err := transformJSON(`{"list":[`+strings.Join(items, ",")+`],"timeStamp":123}`, "格式化", 2)
	if err != nil {
		t.Fatal(err)
	}
	folds = jsonFolds(large)
	if folds[1].Display != `  "list": Array[288],` {
		t.Fatal(folds[1].Display)
	}
	if len(jsonFolds(`{"items":[1,2]}`)) != 0 || len(jsonFolds("{\n invalid")) != 0 {
		t.Fatal("single-line or invalid JSON unexpectedly foldable")
	}
}

func TestJSONFoldSummaryRenderingAndPreservation(t *testing.T) {
	m := newEditorApp()
	source, _, err := transformJSON(`{"status":200,"msg":"操作成功","list":[{"name":"示例一"},{"name":"示例二"}],"timeStamp":123}`, "格式化", 2)
	if err != nil {
		t.Fatal(err)
	}
	tab := m.addTab(document{saved: source, eol: "LF"}, "折叠.json")
	tab.jsonIndent = 2
	s := openTestSession(t, m)
	if err := s.ClickLabel("折叠"); err != nil {
		t.Fatal(err)
	}
	if !s.HasText("Object{...}") {
		t.Fatal("root object summary was not painted")
	}
	if err := s.ClickLabel("展开 Object{...}"); err != nil {
		t.Fatal(err)
	}
	if !s.HasText(`"list": Array[2],`) || !s.HasText(`"timeStamp": 123`) {
		t.Fatal("array summary or following sibling missing")
	}
	if err := s.ClickLabel("展开 Array[2],"); err != nil {
		t.Fatal(err)
	}
	if !s.HasText("Object{...},") || !s.HasText("Object{...}") {
		t.Fatal("array objects not summarized individually")
	}
	// 直接点第二个对象的摘要，第一项保持折叠。
	marker := tab.result.view.Snapshot.FoldMarkers[3]
	clicked := false
	for _, hit := range s.Snapshot().Hits {
		if hit.ID == marker.ID && strings.HasPrefix(hit.Label, "展开 ") {
			if err := s.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
				t.Fatal(err)
			}
			clicked = true
			break
		}
	}
	if !clicked || !s.HasText(`"name": "示例二"`) || !tab.result.collapsed[tab.result.folds[2].Header] {
		t.Fatal("individual expansion disturbed neighboring object")
	}
	if tab.result.text.String() != source || tab.source.text.String() != source || tab.modified() {
		t.Fatal("folding changed document data")
	}
	if err := s.ClickLabel("复制"); err != nil {
		t.Fatal(err)
	}
	if s.Driver().Clipboard() != source {
		t.Fatal("copy included display placeholders")
	}
	path := filepath.Join(t.TempDir(), "完整结果.json")
	if err := s.Driver().QueueSaveFile(path, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("另存"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || strings.ReplaceAll(string(data), "\r\n", "\n") != source {
		t.Fatalf("export: %s %v", data, err)
	}
	m.activate(tab)
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("压缩"); err != nil {
		t.Fatal(err)
	}
	if len(tab.result.view.Snapshot.FoldedLines) != 0 || strings.Contains(tab.result.text.String(), "Object{...}") || strings.Contains(tab.result.text.String(), "\n") {
		t.Fatal("compression retained visual summaries")
	}
	// 源文件也用摘要；全选复制依然取完整文本。
	tab.source.foldAll(true)
	tab.source.focus = true
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Driver().ShortcutChord(gfx.ModifierControl, 'a'); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Driver().ShortcutChord(gfx.ModifierControl, 'c'); err != nil {
		t.Fatal(err)
	}
	if s.Driver().Clipboard() != source {
		t.Fatal("source copy lost folded data")
	}
	tab.source.foldAll(false)
	offset := strings.Index(source, "操作成功")
	tab.source.text.MoveTo(tab.source.text.RuneIndex(offset), false)
	tab.source.focus = true
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if err := s.Compose("zhongwen"); err != nil {
		t.Fatal(err)
	}
	if err := s.Type("中文"); err != nil {
		t.Fatal(err)
	}
	if tab.source.text.String() != source[:offset]+"中文"+source[offset:] {
		t.Fatal("fold/unfold broke Chinese insertion position")
	}
}

func TestJSONFoldSummaryRootArrayAndScrolling(t *testing.T) {
	m := newEditorApp()
	source := "[\n {\"x\":1},\n {\"x\":2}\n]"
	tab := m.addTab(document{saved: source, eol: "LF"}, "array.json")
	s := openTestSession(t, m)
	if err := s.ClickLabel("折叠"); err != nil {
		t.Fatal(err)
	}
	if !s.HasText("Array[2]") {
		t.Fatal("root array count not rendered")
	}
	if err := s.ClickLabel("展开 Array[2]"); err != nil {
		t.Fatal(err)
	}
	key := strings.Repeat("中文", 45)
	tab.source.replace("{\n\t\"" + key + "\": [\n1,2\n]\n}")
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	tab.result.collapsed[tab.result.folds[1].Header] = true
	tab.result.view.Snapshot.Wrap = true
	if err := s.Resize(900, 560); err != nil {
		t.Fatal(err)
	}
	if tab.result.view.Code.MaxScrollX() <= 0 {
		t.Fatal("long summary cannot scroll horizontally")
	}
	tab.result.view.Code.SetScrollX(tab.result.view.Code.MaxScrollX())
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if !s.HasText("Array[2]") {
		t.Fatal("summary missing after horizontal scroll")
	}
	if err := s.ClickLabel("展开 Array[2]"); err != nil {
		t.Fatal(err)
	}
	if len(tab.result.view.Snapshot.FoldedLines) != 0 {
		t.Fatal("scrolled summary not clickable")
	}
}
