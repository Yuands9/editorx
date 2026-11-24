package editor

import (
	"slices"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

func TestJSONComparisonToolsTargetTheirOwnSide(t *testing.T) {
	m := newEditorApp()
	left := `{"z":10,"a":"中文"}`
	right := `{"z":20,"a":"中文","huge":900719925474099312345}`
	tab := m.addTab(document{saved: left, eol: "LF"}, "compare.json")
	tab.comparison.replace(right)
	s := openTestSession(t, m)
	if err := s.Resize(900, 560); err != nil {
		t.Fatal(err)
	}
	click := func(labels ...string) {
		t.Helper()
		for _, label := range labels {
			if err := s.ClickLabel(label); err != nil {
				t.Fatalf("%s: %v", label, err)
			}
		}
	}
	click("对比")
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	click("2 空格")
	if tab.jsonIndent != 2 {
		hit, err := s.Locate("2 空格")
		t.Fatalf("compare indentation control ignored click: indent=%d, hit=%+v, error=%v", tab.jsonIndent, hit, err)
	}
	click("右侧 JSON 菜单", "menuitem:格式化")
	wantRight, _, _ := transformJSON(right, "格式化", 2)
	if tab.source.text.String() != left || tab.comparison.text.String() != wantRight || !tab.jsonCompare {
		t.Fatalf("right format left=%q right=%q compare=%t want=%q status=%q error=%q", tab.source.text.String(), tab.comparison.text.String(), tab.jsonCompare, wantRight, m.status, m.errorText)
	}
	click("格式化左侧")
	wantLeft, _, _ := transformJSON(left, "格式化", 2)
	if tab.source.text.String() != wantLeft || tab.comparison.text.String() != wantRight || !tab.jsonCompare || len(tab.differences) != 2 {
		t.Fatal("left shortcut changed the other input or structural differences")
	}
	click("右侧 JSON 菜单", "menuitem:压缩", "左侧 JSON 菜单", "menuitem:键排序")
	sorted, _, _ := transformJSON(left, "键排序", 2)
	if !tab.jsonCompare || tab.source.text.String() != sorted || tab.comparison.text.String() != right {
		t.Fatal("pane menus targeted the wrong input")
	}
	click("右侧 JSON 菜单", "menuitem:转 XML")
	xml, _, _ := transformJSON(right, "转 XML", 2)
	if m.active == tab || m.active.source.text.String() != xml || tab.source.text.String() != sorted || tab.comparison.text.String() != right || !tab.jsonCompare {
		t.Fatal("right conversion did not produce a separate tab from the correct input")
	}
}

func TestJSONDiffResizeAndMergeAfterTwoHundredRows(t *testing.T) {
	m := newEditorApp()
	left, right := "["+strings.Repeat("0,", 249)+"0]", "["+strings.Repeat("1,", 249)+"1]"
	tab := m.addTab(document{saved: left, eol: "LF"}, "long.json")
	tab.comparison.replace(right)
	tab.jsonCompare = true
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	const dividerLabel = "调整 JSON 比对结果高度"
	divider, err := s.Locate(dividerLabel)
	check(err)
	viewport := tab.diffList.GetList().Scroll.Viewport
	x, y := divider.Bounds.X+divider.Bounds.Width/2, divider.Bounds.Y+divider.Bounds.Height/2
	check(s.Press(x, y))
	check(s.Move(x, y-130))
	check(s.Release(x, y-130))
	check(s.Frame())
	enlarged := tab.diffList.GetList().Scroll.Viewport
	resized, err := s.Locate(dividerLabel)
	check(err)
	if resized.Bounds.Y > divider.Bounds.Y-125 || enlarged < viewport+125 {
		t.Fatalf("divider did not resize the list: divider %v → %v, list %v → %v", divider.Bounds, resized.Bounds, viewport, enlarged)
	}
	savedRatio := tab.diffSplit.Ratio
	check(s.Resize(900, 560))
	compact, err := s.Locate("textfield:代码编辑区")
	check(err)
	compactViewport := tab.diffList.GetList().Scroll.Viewport
	if compact.Role != gfx.RoleTextField || compact.Bounds.Height < 65 || compactViewport < 55 {
		t.Fatalf("resize hid editors or results at minimum window size: %v %v", compact.Bounds, compactViewport)
	}

	tab.diffList.ScrollTo(249, ui.ListEnd)
	for range 6 {
		check(s.Frame())
	}
	first, last := tab.diffList.Visible()
	if last < 249 || last-first > 25 {
		t.Fatalf("diff list did not scroll or virtualize its rows: %d to %d", first, last)
	}
	check(s.ClickLabel("合并到左侧 /249"))
	want := "[" + strings.Repeat("0,", 249) + "1]"
	if tab.source.text.String() != want || tab.comparison.text.String() != right || len(tab.differences) != 249 {
		t.Fatal("a difference beyond the first 200 could not be merged")
	}
	restored := newEditorApp()
	restored.restoreSession(m.snapshotSession())
	if restored.active.diffSplit.Ratio != savedRatio {
		t.Fatal("comparison divider ratio was not restored")
	}
}

func TestJSONMergeDirectionsAndUndoThroughUI(t *testing.T) {
	m := newEditorApp()
	left := `{"keep":1,"old":null,"value":true}`
	right := `{"keep":1,"value":"中文😀","new":900719925474099312345}`
	tab := m.addTab(document{saved: left, eol: "LF"}, "merge.json")
	tab.comparison.replace(right)
	tab.jsonCompare = true
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(s.ClickLabel("合并到右侧 /old"))
	if tab.comparison.text.String() != strings.TrimSuffix(right, "}")+`,"old":null}` || tab.source.text.String() != left {
		t.Fatal("merge to the right did not add the left-side member")
	}
	check(s.Keys("cmd+z"))
	if tab.comparison.text.String() != right || tab.source.text.String() != left {
		t.Fatal("undo did not restore the right-hand input")
	}
	check(s.ClickLabel("合并到左侧 /new"))
	check(s.ClickLabel("合并到左侧 /value"))
	check(s.ClickLabel("合并到左侧 /old"))
	if tab.source.text.String() != right || tab.comparison.text.String() != right || len(tab.differences) != 0 || !tab.modified() {
		t.Fatal("adopting the right's addition, change and deletion did not resolve the differences")
	}
	check(s.Keys("cmd+z"))
	if !strings.Contains(tab.source.text.String(), `"old":null`) || tab.comparison.text.String() != right || len(tab.differences) != 1 {
		t.Fatal("one undo did not restore just the last left-side merge")
	}
}

func TestJSONMergeSplices(t *testing.T) {
	for _, test := range []struct {
		name, left, right, path, want string
		intoLeft                      bool
	}{
		{"escaped pointer and exact neighbors", `{"keep" : "\u4E2D","a/b~":{"":null},"end":[0, 1]}`, `{"keep":"中","a/b~":{"":null,"~01/":900719925474099312345},"end":[0,1]}`, "/a~1b~0/~001~1", `{"keep" : "\u4E2D","a/b~":{"":null,"~01/":900719925474099312345},"end":[0, 1]}`, true},
		{"remove first member", `{ "gone":0, "keep"  : 1 }`, `{"keep":1}`, "/gone", `{ "keep"  : 1 }`, true},
		{"remove sole member", "{\n  \"gone\":0\n}", `{}`, "/gone", `{}`, true},
		{"remove middle array member from longer target", `[1]`, `[1, 2, 3]`, "/1", `[1, 3]`, false},
		{"append array member", `[1]`, `[1,2,3]`, "/1", `[1,2]`, true},
		{"replace root", `{"a":"旧"}`, `null`, "", `null`, true},
		{"append a pretty member", "{\n  \"keep\": [1,2]\n}", `{"keep":[1,2],"added":{"中文":true}}`, "/added", "{\n  \"keep\": [1,2],\n  \"added\": {\n    \"中文\": true\n  }\n}", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			differences, err := compareJSON(test.left, test.right)
			if err != nil {
				t.Fatal(err)
			}
			index := slices.IndexFunc(differences, func(d jsonDifference) bool { return d.path == test.path })
			if index < 0 {
				t.Fatalf("missing path %s", test.path)
			}
			span, value, err := jsonMergeEdit(test.left, test.right, differences[index], test.intoLeft, 2)
			if err != nil {
				t.Fatal(err)
			}
			target := test.right
			if test.intoLeft {
				target = test.left
			}
			got := target[:span.Start] + value + target[span.End:]
			if got != test.want {
				t.Fatalf("merge = %q, want %q", got, test.want)
			}
		})
	}
	differences, err := compareJSON(`[1]`, `[1,2,3]`)
	if err != nil || len(differences) != 2 || differences[0].arrayGap || !differences[1].arrayGap {
		t.Fatalf("incorrect array append availability: %v, %v", differences, err)
	}
	if _, _, err := jsonMergeEdit(`[1]`, `[1,2,3]`, differences[1], true, 2); err == nil {
		t.Fatalf("inserting %s would skip an array position", differences[1].path)
	}
}

func TestJSONMergeRootInUntitledAndRejectStaleDifference(t *testing.T) {
	m := newTestEditorWithDocument()
	tab := m.active
	tab.source.replace(`{"a":1}`)
	tab.comparison.replace("null")
	tab.jsonCompare = true
	tab.updateDiff()
	difference := tab.differences[0]
	tab.source.replace(`{"a":2}`)
	m.mergeJSONDifference(tab, difference, true)
	if tab.source.text.String() != `{"a":2}` {
		t.Fatal("a stale difference was applied to edited content")
	}
	tab.updateDiff()
	m.mergeJSONDifference(tab, tab.differences[0], true)
	tab.updateDiff()
	if tab.source.text.String() != "null" || tab.format() != "JSON" || !tab.jsonCompare || len(tab.differences) != 0 {
		t.Fatal("merging a scalar into an untitled tab lost its comparison view")
	}
}
