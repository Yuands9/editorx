package editor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func nestedJSONSample(t *testing.T) (string, string) {
	t.Helper()
	quote := func(value string) string {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return string(encoded)
	}
	body := `{"code":"000000","data":{"contract":[{"name":"示例商户","end":"2029-08-21"}],"sequence":900719925474099312345,"note":"保留\"引号\"和\\反斜杠","same":1,"same":2}}`
	envelope := quote(`{"body":` + quote(body) + `,"respData":{"code":"00000000"}}`)
	prefix := `{"msg":"交易成功","code":"00000000","directory":"D:\\examples\\","bizContent":`
	suffix := `,"sign":"DEMO"}`
	return prefix + envelope + suffix, prefix + strings.ReplaceAll(envelope, `\"`, `\\"`) + suffix
}

func TestUnescapeNestedJSONFields(t *testing.T) {
	regular, extraEscapes := nestedJSONSample(t)
	if json.Valid([]byte(extraEscapes)) {
		t.Fatal("the log fixture did not include extra escapes")
	}
	for _, sample := range []struct{ name, source string }{
		{"standard", regular}, {"extra quote escapes", extraEscapes},
	} {
		t.Run(sample.name, func(t *testing.T) {
			got, kind, err := transformJSON(sample.source, "去转义", 2)
			if err != nil || kind != "JSON" {
				t.Fatalf("unescape failed: %v", err)
			}
			var decoded struct {
				Code, Directory, Sign string
				BizContent            struct {
					Body struct {
						Code string
						Data struct {
							Sequence json.Number
							Note     string
							Contract []struct{ Name string }
						}
					}
					RespData struct{ Code string }
				}
			}
			if err := json.Unmarshal([]byte(got), &decoded); err != nil {
				t.Fatal(err)
			}
			content := decoded.BizContent
			if decoded.Code != "00000000" || decoded.Directory != `D:\examples\` || decoded.Sign != "DEMO" ||
				content.Body.Code != "000000" || content.RespData.Code != "00000000" ||
				content.Body.Data.Sequence != "900719925474099312345" || content.Body.Data.Note != `保留"引号"和\反斜杠` ||
				len(content.Body.Data.Contract) != 1 || content.Body.Data.Contract[0].Name != "示例商户" ||
				strings.Count(got, `"same"`) != 2 || !strings.Contains(got, "\n  \"bizContent\": {") {
				t.Fatal("nested JSON lost content, precision, or chosen indentation")
			}
		})
	}
	formatted := formatJSONPaste(extraEscapes, 4)
	if !json.Valid([]byte(formatted)) || !strings.Contains(formatted, "\n    \"bizContent\": \"") {
		t.Fatal("paste failed to repair only the extra escapes")
	}
	if original := `{ "unfinished":`; formatJSONPaste(original, 4) != original {
		t.Fatal("paste rewrote an incomplete JSON fragment")
	}
	if original := `{"text":"{\\"这只是文本\\"}"}`; formatJSONPaste(original, 4) != original {
		t.Fatal("paste repaired text that is not an embedded JSON value")
	}
	want, _, err := transformJSON(regular, "格式化", 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{regular, extraEscapes} {
		escaped, _, err := transformJSON(source, "转义", 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, input := range []string{escaped, escaped[1 : len(escaped)-1]} {
			unquoted, _, err := transformJSON(input, "去转义", 2)
			if err != nil || unquoted != want {
				t.Fatalf("escape round trip did not preserve the original JSON: %v", err)
			}
		}
	}
}

func TestJSONWorkbenchPastesEditsAndCopiesBothPanes(t *testing.T) {
	m := newEditorApp()
	tab := m.addTab(document{saved: `{`, eol: "LF"}, "样例.json")
	s := openTestSession(t, m)

	click := func(label string) {
		t.Helper()
		if err := s.ClickLabel(label); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
	}
	shortcut := func(key rune) {
		t.Helper()
		if _, err := s.Driver().ShortcutChord(gfx.ModifierControl, key); err != nil {
			t.Fatalf("Ctrl+%c: %v", key, err)
		}
	}
	paste := func(label, value string) {
		t.Helper()
		click(label)
		s.Driver().SetClipboard(value)
		shortcut('a')
		shortcut('v')
	}

	expectBoth := func(want string) {
		t.Helper()
		if tab.source.text.String() != want || tab.result.text.String() != want {
			t.Fatalf("left = %q and right = %q, want %q", tab.source.text.String(), tab.result.text.String(), want)
		}
	}
	click("2 空格")
	paste("JSON 处理结果", `{"右侧":[1,2]}`)
	right := "{\n  \"右侧\": [\n    1,\n    2\n  ]\n}"
	expectBoth(right)
	shortcut('a')
	shortcut('c')
	if s.Driver().Clipboard() != right {
		t.Fatal("right editor did not copy the formatted text")
	}
	shortcut('z')
	expectBoth("{")
	shortcut('y')
	expectBoth(right)
	number := tab.result.text.RuneIndex(strings.Index(right, "1"))
	tab.result.text.SetSelection(text.Range{Start: number, End: number + 1})
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if err := s.Type("3"); err != nil {
		t.Fatal(err)
	}
	right = strings.Replace(right, "1", "3", 1)
	expectBoth(right)

	paste("代码编辑区", `{"左侧":true}`)
	left := `{"左侧":true}`
	leftResult := "{\n  \"左侧\": true\n}"
	if tab.source.text.String() != left || tab.result.text.String() != leftResult {
		t.Fatal("left paste must stay raw while the result is formatted")
	}
	shortcut('a')
	shortcut('c')
	if s.Driver().Clipboard() != left {
		t.Fatal("left editor did not copy the formatted text")
	}
	value := tab.source.text.RuneIndex(strings.Index(left, "true"))
	tab.source.text.SetSelection(text.Range{Start: value, End: value + len("true")})
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if err := s.Compose("zhongwen"); err != nil {
		t.Fatal(err)
	}
	if tab.result.text.String() != leftResult {
		t.Fatal("uncommitted IME text leaked to the other pane")
	}
	if err := s.Type("false"); err != nil {
		t.Fatal(err)
	}
	if tab.source.text.String() != strings.Replace(left, "true", "false", 1) || tab.result.text.String() != strings.Replace(leftResult, "true", "false", 1) {
		t.Fatal("IME commit did not preserve raw source and update the preview")
	}

	_, extraEscapes := nestedJSONSample(t)
	paste("JSON 处理结果", extraEscapes)
	original := formatJSONPaste(extraEscapes, 2)
	expectBoth(original)
	click("button:更多")
	click("menuitem:去转义")
	unescaped := tab.result.text.String()
	if !strings.Contains(unescaped, `"bizContent": {`) || !strings.Contains(unescaped, `"body": {`) ||
		tab.source.text.String() != original {
		t.Fatal("preview failed or changed the source")
	}

	restored := newEditorApp()
	restored.restoreSession(m.snapshotSession())
	restoredTab := restored.active
	if restoredTab == nil || restoredTab.jsonAction != "去转义" || restoredTab.result.text.String() != unescaped ||
		restoredTab.source.text.String() != original {
		t.Fatal("the current conversion preview did not survive session restore")
	}
	click("button:应用到原文")
	expectBoth(unescaped)
}

func TestJSONRestoresDraftFromResultMenu(t *testing.T) {
	const oldSession = `{
		"Tabs": [{
			"Name": "预览.json",
			"Source": {"Content": "{\"body\":\"{\\\"name\\\":\\\"示例\\\"}\"}"},
			"Result": {"Content": "{\"body\":{\"name\":\"示例\"}}"},
			"JSONAction": "转义",
			"JSONResultKind": "JSON",
			"JSONResultAction": "去转义"
		}]
	}`
	var state windowSession
	if err := json.Unmarshal([]byte(oldSession), &state); err != nil {
		t.Fatal(err)
	}
	m := newEditorApp()
	m.restoreSession(state)
	tab := m.active
	const source = `{"body":"{\"name\":\"示例\"}"}`
	const draft = `{"body":{"name":"示例"}}`
	if tab == nil || !tab.resultDraft || tab.resultKind != "JSON" ||
		tab.source.text.String() != source || tab.result.text.String() != draft {
		t.Fatal("restoring a right-side draft lost or changed either pane")
	}
	s := openTestSession(t, m)
	if err := s.ClickLabel("button:应用到原文"); err != nil {
		t.Fatal(err)
	}
	if tab.resultDraft || tab.source.text.String() != draft {
		t.Fatal("applying a restored draft did not update the source")
	}
}

func TestJSONAutoWrapAndReversibleConversions(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "长行.json")
	longValue := strings.Repeat("中文😀", 550)
	raw := `{"body":"{\"text\":\"示例\"}","long":"` + longValue + `","id":900719925474099312345}`
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	m := newEditorApp()
	m.load(path)
	tab := m.active
	s := openTestSession(t, m)

	original := raw
	formatted, _, err := transformJSON(raw, "格式化", 4)
	if err != nil {
		t.Fatal(err)
	}
	if tab.source.text.String() != original || tab.result.text.String() != formatted || tab.modified() {
		t.Fatal("opening JSON must preserve the original and format only the preview")
	}
	onDisk, err := os.ReadFile(path)
	if err != nil || string(onDisk) != raw {
		t.Fatal("opening the file changed its contents on disk")
	}
	for _, pane := range []*editorPane{tab.source, tab.result} {
		if !pane.view.Snapshot.Wrap || len(pane.view.Code.SoftWrapOffsets(nil)) == 0 {
			t.Fatal("a long JSON value did not wrap visually")
		}
	}
	click := func(label string) {
		t.Helper()
		if err := s.ClickLabel(label); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
	}
	for _, action := range jsonExtraActions {
		click("更多")
		click("menuitem:" + action)
		output, kind, err := transformJSON(original, action, 4)
		if err != nil || tab.jsonAction != action || tab.result.text.String() != output || tab.resultKind != kind ||
			tab.source.text.String() != original || tab.modified() {
			t.Fatalf("%s did not activate its preview: %v", action, err)
		}
		click("更多")
		click("menuitem:" + action)
		if tab.jsonAction != "格式化" || tab.result.text.String() != formatted || tab.source.text.String() != original {
			t.Fatalf("%s did not cancel its preview", action)
		}
	}
	focusResult := func() {
		t.Helper()
		hit, err := s.Locate("textfield:JSON 处理结果")
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Click(hit.Bounds.X+80, hit.Bounds.Y+14); err != nil {
			t.Fatal(err)
		}
		if !tab.result.focused {
			t.Fatal("the right editor did not receive focus")
		}
	}
	focusResult()
	if _, err := s.Driver().ShortcutChord(gfx.ModifierControl, 'z'); err != nil {
		t.Fatal(err)
	}
	if tab.source.text.String() != original || tab.result.text.String() != formatted {
		t.Fatal("undo restored a canceled conversion")
	}
	click("结果折行")
	if tab.source.view.Snapshot.Wrap || tab.result.view.Snapshot.Wrap {
		t.Fatal("disabling wrap did not affect both panes")
	}
	restored := newEditorApp()
	restored.restoreSession(m.snapshotSession())
	if restored.active == nil || !restored.active.wrapOverride || restored.active.result.view.Snapshot.Wrap {
		t.Fatal("wrap selection was lost during session restore")
	}
	click("结果折行")
	if !tab.source.view.Snapshot.Wrap || !tab.result.view.Snapshot.Wrap {
		t.Fatal("enabling wrap did not affect both panes")
	}
	focusResult()
	offset := tab.result.text.RuneIndex(strings.LastIndex(formatted, "345") + len("34"))
	tab.result.text.SetSelection(text.Range{Start: offset, End: offset + 1})
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if err := s.Type("6"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Driver().ShortcutChord(gfx.ModifierControl, 's'); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(path)
	if err != nil || string(saved) != strings.Replace(formatted, "345", "346", 1) || tab.modified() {
		t.Fatal("saving from the right pane did not write the synchronized edit")
	}
}

func TestJSONPasteKeepsNonJSONTextAndFormatsCompare(t *testing.T) {
	m := newEditorApp()
	tab := m.addTab(document{saved: `{"x":1}`, eol: "LF"}, "对比.json")
	s := openTestSession(t, m)
	if err := s.ClickLabel("4 空格"); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("对比"); err != nil {
		t.Fatal(err)
	}
	s.Driver().SetClipboard(`{"x":2}`)
	if err := s.ClickLabel("粘贴对比内容"); err != nil {
		t.Fatal(err)
	}
	if tab.comparison.text.String() != "{\n    \"x\": 2\n}" || len(tab.differences) != 1 {
		t.Fatal("comparison paste ignored the JSON indentation")
	}

	plain := m.addTab(document{eol: "LF"}, "普通文本.txt")
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	s.Driver().SetClipboard(`{"x":2}`)
	if _, err := s.Driver().ShortcutChord(gfx.ModifierControl, 'v'); err != nil {
		t.Fatal(err)
	}
	if plain.source.text.String() != `{"x":2}` {
		t.Fatal("ordinary text paste was formatted as JSON")
	}
}
