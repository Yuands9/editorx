package editor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
)

func TestJSONConversions(t *testing.T) {
	source := `{"name":"中文😀","text":"a\"b\\c\nline","number":900719925474099312345,"same":1,"same":2}`
	for _, action := range []string{"格式化", "键排序", "Unicode 编码", "Unicode 解码"} {
		output, kind, err := transformJSON(source, action, 2)
		if err != nil || kind != "JSON" || !json.Valid([]byte(output)) || !strings.Contains(output, "900719925474099312345") || strings.Count(output, `"same"`) != 2 {
			t.Fatalf("%s: %q %v", action, output, err)
		}
	}
	encoded, _, err := transformJSON(source, "Unicode 编码", 2)
	if err != nil || !strings.Contains(encoded, `\u4e2d\u6587\ud83d\ude00`) {
		t.Fatalf("unicode encoding: %s %v", encoded, err)
	}
	decoded, _, err := transformJSON(encoded, "Unicode 解码", 2)
	if err != nil || !strings.Contains(decoded, "中文😀") {
		t.Fatalf("unicode decode: %q %v", decoded, err)
	}
	escaped, _, err := transformJSON(source, "转义", 2)
	if err != nil {
		t.Fatal(err)
	}
	unescaped, _, err := transformJSON(escaped, "去转义", 2)
	if err != nil {
		t.Fatal(err)
	}
	compact, _, err := transformJSON(unescaped, "压缩", 2)
	if err != nil || compact != source {
		t.Fatalf("escape round trip lost content: %q %v", compact, err)
	}
	formatted, _, err := transformJSON(source, "格式化", 2)
	if err != nil || !strings.Contains(formatted, "\n  \"name\"") || strings.Contains(formatted, "\n    \"name\"") {
		t.Fatal("2-space indentation ignored")
	}
	form, kind, err := transformJSON(`{"name":"中文","number":900719925474099312345,"on":false,"empty":null,"items":[1,2],"object":{"key":true}}`, "Form-Data", 4)
	want := "name:中文\nnumber:900719925474099312345\non:false\nempty:null\nitems:[1,2]\nobject:{\"key\":true}\n"
	if err != nil || kind != "Form-Data" || form != want {
		t.Fatalf("form-data: %q %v", form, err)
	}
	for _, source := range []string{`[]`, `{"a:b":1}`, `{"name":"one\ntwo"}`, `{`} {
		if _, _, err := transformJSON(source, "Form-Data", 4); err == nil {
			t.Fatalf("accepted ambiguous form-data: %s", source)
		}
	}
	for _, source := range []string{`123`, `"not json"`, `"{"`} {
		if _, _, err := transformJSON(source, "去转义", 4); err == nil {
			t.Fatalf("accepted invalid escaped JSON: %s", source)
		}
	}
}

func TestJSONStructuralDiff(t *testing.T) {
	for _, sample := range []struct{ left, right string }{
		{`{"a":1,"b":{"c":true}}`, `{"b":{"c":true},"a":1.0}`},
		{`[1.2300e4,-0,900719925474099312345]`, `[12300,0.000,900719925474099312345.0]`},
		{`1e999999999999999999999999999999`, `10e999999999999999999999999999998`},
		{`"中文"`, `"\u4e2d\u6587"`},
	} {
		differences, err := compareJSON(sample.left, sample.right)
		if err != nil || len(differences) != 0 {
			t.Fatalf("equal JSON differs: %+v %v", differences, err)
		}
	}
	left := `{"removed":null,"value":900719925474099312345,"kind":true,"array":[1,2],"a/b~":{"x":1}}`
	right := `{"added":null,"value":900719925474099312346,"kind":"true","array":[2,1,3],"a/b~":{"x":2}}`
	differences, err := compareJSON(left, right)
	if err != nil || len(differences) != 8 {
		t.Fatalf("unexpected differences: %+v %v", differences, err)
	}
	want := map[string]string{"/removed": "删除", "/added": "新增", "/value": "值变化", "/kind": "类型变化", "/array/0": "值变化", "/array/1": "值变化", "/array/2": "新增", "/a~1b~0/x": "值变化"}
	for _, difference := range differences {
		if want[difference.path] != difference.kind {
			t.Fatalf("wrong path/category: %+v", difference)
		}
		for _, side := range []struct {
			source, display string
			start, end      int
		}{
			{left, difference.before, difference.left.Start, difference.left.End},
			{right, difference.after, difference.right.Start, difference.right.End},
		} {
			if side.end > side.start {
				parsed, err := parseJSON(side.source[side.start:side.end])
				if err != nil || jsonValueText(parsed) != side.display {
					t.Fatalf("highlight does not match value: %+v %v", side, err)
				}
			}
		}
		delete(want, difference.path)
	}
	if len(want) != 0 {
		t.Fatal("missing difference")
	}
	for _, invalid := range []string{`{`, `{"x":1,"x":2}`, `{"list":[{"x":1,"x":2}]}`} {
		if _, err := compareJSON(invalid, `{}`); err == nil {
			t.Fatalf("accepted ambiguous comparison: %s", invalid)
		}
		if _, err := compareJSON(`{}`, invalid); err == nil {
			t.Fatalf("accepted invalid comparison on right: %s", invalid)
		}
	}
}

func TestJSONWorkbenchActions(t *testing.T) {
	m := newTestEditorWithDocument()
	source := `{"name":"中文","port":8080}`
	tab := m.addTab(document{saved: source, eol: "LF"}, "配置.json")
	s := openTestSession(t, m)
	for _, label := range []string{"2 空格", "压缩", "更多", "Form-Data", "复制"} {
		if err := s.ClickLabel(label); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
	}
	if tab.jsonIndent != 2 || !tab.result.view.Snapshot.Wrap || !tab.source.view.Snapshot.Wrap ||
		s.Driver().Clipboard() != "name:中文\nport:8080\n" || tab.source.text.String() != source {
		t.Fatal("conversion modified source or ignored display settings")
	}
	if err := s.ClickLabel("右侧 JSON 菜单"); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("menuitem:在新标签打开结果"); err != nil {
		t.Fatal(err)
	}
	if m.active == tab || m.active.format() != "纯文本" || m.active.source.text.String() != "name:中文\nport:8080\n" || tab.source.text.String() != source {
		t.Fatal("form-data did not create a separate text tab")
	}
	m.activate(tab)
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("对比"); err != nil {
		t.Fatal(err)
	}
	s.Driver().SetClipboard(`{"name":"中文","port":9090,"enabled":true}`)
	if err := s.ClickLabel("粘贴对比内容"); err != nil {
		t.Fatal(err)
	}
	if len(tab.differences) != 2 || tab.diffError != "" {
		t.Fatalf("comparison did not refresh: %+v %s", tab.differences, tab.diffError)
	}
	if len(tab.source.view.Snapshot.MarkLayers) != 1 || len(tab.comparison.view.Snapshot.MarkLayers) != 1 {
		t.Fatal("missing diff highlights")
	}
	if err := s.ClickLabel("定位 /port"); err != nil {
		t.Fatal(err)
	}
	selection := tab.comparison.text.Selection()
	if string([]rune(tab.comparison.text.String())[selection.Start:selection.End]) != "9090" {
		t.Fatal("diff navigation did not select the changed value")
	}
	if err := s.ClickLabel("复制差异"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s.Driver().Clipboard(), "/port") || !strings.Contains(s.Driver().Clipboard(), "新增 /enabled") || tab.source.text.String() != source {
		t.Fatal("diff report incomplete or source modified")
	}
	comparison := tab.comparison.text.String()
	if err := s.Driver().QueueOpenFiles(nil, app.ErrCanceled); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("载入对比文件"); err != nil {
		t.Fatal(err)
	}
	if tab.comparison.text.String() != comparison {
		t.Fatal("cancel lost comparison")
	}
	path := filepath.Join(t.TempDir(), "比较文件")
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Driver().QueueOpenFiles([]string{path}, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("载入对比文件"); err != nil {
		t.Fatal(err)
	}
	if len(tab.differences) != 0 || tab.diffError != "" || tab.comparison.text.String() != source || len(m.tabs) != 3 {
		t.Fatal("comparison file loading changed tabs or missed equality")
	}
	if err := s.Compose("zhongwen"); err != nil {
		t.Fatal(err)
	}
	m.newDocument()
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if _, composing := tab.comparison.text.Composition(); composing || tab.comparison.text.String() != source {
		t.Fatal("comparison composition leaked across tabs")
	}
}

func TestJSONErrorLocationAndHiddenSidebar(t *testing.T) {
	m := newEditorApp()
	m.showFiles = true
	source := "{\n  \"名称\":\"中文\",\n  \"x\":truX\n}"
	tab := m.addTab(document{saved: source, eol: "LF"}, "错误.json")
	s := openTestSession(t, m)
	if !strings.Contains(tab.resultError, "第 3 行") || tab.resultErrorOffset != strings.Index(source, "X") {
		t.Fatalf("wrong error position: %s %d", tab.resultError, tab.resultErrorOffset)
	}
	if err := s.ClickLabel("定位错误"); err != nil {
		t.Fatal(err)
	}
	if tab.source.text.ByteOffset(tab.source.text.Caret()) != strings.Index(source, "X") {
		t.Fatal("jump ignored Chinese byte/rune offsets")
	}
	if err := s.ClickLabel("收起文件栏"); err != nil {
		t.Fatal(err)
	}
	hit, err := s.Locate("代码编辑区")
	if err != nil || m.showFiles || hit.Bounds.X > 25 {
		t.Fatalf("sidebar still reserves a rail: %+v %v", hit, err)
	}
	for _, label := range []string{"展开文件栏", "主菜单", "打开文件", "添加项目"} {
		hit, err := s.Locate(label)
		if err != nil || hit.Bounds.Y > 60 {
			t.Fatalf("hidden-sidebar top entry missing: %s %+v %v", label, hit, err)
		}
	}
	if err := s.Resize(900, 560); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"格式化", "压缩", "键排序", "转 XML", "对比", "更多", "4 空格"} {
		hit, err := s.Locate(label)
		if err != nil || hit.Bounds.X < 0 || hit.Bounds.X+hit.Bounds.Width > 900 {
			t.Fatalf("toolbar clipped: %s %+v %v", label, hit, err)
		}
	}
	tab.source.replace(`{"名称":"中文","x":true}`)
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if tab.resultError != "" || !strings.Contains(tab.result.text.String(), "中文") {
		t.Fatal("result did not recover after syntax correction")
	}
}

func TestJSONErrorPositionsAcrossActions(t *testing.T) {
	for _, source := range []string{"{\n\"中文\": true,\n\"enabled\":truX}", "{\n\"中文\":"} {
		want := strings.Index(source, "X")
		if want < 0 {
			want = len(source)
		}
		for _, action := range []string{"格式化", "压缩", "键排序", "Form-Data", "转 XML", "Unicode 解码"} {
			_, _, err := transformJSON(source, action, 2)
			if err == nil {
				t.Fatal("invalid JSON accepted")
			}
			message, offset := jsonErrorMessage(source, err)
			if offset != want {
				t.Fatalf("%s position=%d want=%d: %s", action, offset, want, message)
			}
		}
	}
}
