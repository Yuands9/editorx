package editor

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func TestDocumentTemplatesThroughUI(t *testing.T) {
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	const selected = "发布 v1"
	const original = "其他正文\n" + selected + "\n"
	source := m.addTab(document{saved: original, eol: "CRLF"}, "发布.txt")
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	click := func(label string) { t.Helper(); check(s.ClickLabel(label)) }
	check(s.Resize(900, 560))
	handled, err := s.Driver().ShortcutChord(gfx.ModifierControl, 'n')
	check(err)
	if !handled || m.templateDialog != nil || m.active.source.text.CommittedString() != "" || m.active.title() != "未命名-1" {
		t.Fatal("Ctrl+N must open an empty tab directly")
	}
	m.activate(source)
	check(s.Frame())
	click("主菜单")
	click("menuitem:工具与模板")
	click("menuitem:将文档存为模板…")
	if m.templateDialog == nil || m.templateDialog.body.Text() != original {
		t.Fatal("the whole document was not copied into the template")
	}
	click("button:取消")
	check(s.RightClickLabel(source.title()))
	if _, err := s.Locate("menuitem:将文档存为模板…"); err == nil {
		t.Fatal("template saving still appears in the tab context menu")
	}
	check(s.Driver().Key(gfx.KeyEscape, 0))

	start := strings.Index(original, selected)
	source.source.text.SetSelection(text.Range{
		Start: source.source.text.RuneIndex(start), End: source.source.text.RuneIndex(start + len(selected)),
	})
	check(s.Frame())
	check(s.RightClickLabel("textfield:代码编辑区"))
	for _, removed := range []string{"将选区存为模板…", "用模板替换选区…"} {
		if _, err := s.Locate("menuitem:" + removed); err == nil {
			t.Fatalf("removed context action is still visible: %s", removed)
		}
	}
	check(s.Driver().Key(gfx.KeyEscape, 0))
	click("主菜单")
	click("menuitem:工具与模板")
	click("menuitem:将选区存为模板…")
	form := m.templateDialog
	if form == nil || form.body.Text() != selected {
		t.Fatal("the selected text was not copied")
	}
	if _, err := s.Locate("textfield:变量名"); err == nil {
		t.Fatal("the optional variable field was visible before it was opened")
	}
	form.body.GetEditor().SetSelection(text.Range{Start: 3, End: 5})
	form.name.SetText("周报固定段落")
	check(s.Frame())
	click("button:设为变量")
	check(s.Type("版本"))
	click("button:添加变量")
	if form.body.Text() != "发布 {{版本}}" || len(form.fields) != 1 {
		t.Fatalf("variable not created: %q %+v", form.body.Text(), form.fields)
	}
	if _, err := s.Locate("textfield:变量名"); err == nil {
		t.Fatal("the variable field should collapse after adding the variable")
	}
	click("button:保存并预览")
	picker := m.templatePanel
	if picker == nil || picker.saving || picker.choices[picker.selected].scope != "个人" ||
		picker.choices[picker.selected].template.Name != "周报固定段落" ||
		source.source.text.CommittedString() != original {
		t.Fatal("saving did not preselect the template or changed its source")
	}
	click("button:返回模板列表")
	click("button:会议纪要 · 内置")
	if picker.choices[picker.selected].template.Name != "会议纪要" {
		t.Fatal("clicking the list did not select a template")
	}
	click("button:返回模板列表")
	click("textfield:搜索模板")
	check(s.Type("周报"))
	if len(picker.options) != 1 || picker.choices[picker.selected].template.Name != "周报固定段落" {
		t.Fatal("search did not filter and select the matching template")
	}
	click("button:周报固定段落 · 个人")
	picker.values[0].SetText("v2")
	check(s.Frame())
	foundPreview := false
	for _, line := range s.Snapshot().Texts {
		foundPreview = foundPreview || strings.Contains(line.Value, "发布 v2")
	}
	if !foundPreview {
		t.Fatal("the template preview did not reflect the filled variable")
	}
	click("button:创建草稿")
	if m.active == source || !m.active.modified() || m.active.doc.path != "" ||
		m.active.source.text.CommittedString() != "发布 v2" {
		t.Fatal("the rendered template was not opened as a new unsaved draft")
	}
	draft := m.active
	draft.source.text.MoveTo(draft.source.text.RuneCount(), false)
	check(s.Frame())
	click("button:编辑模板…")
	m.templateDialog.name.SetText("周报常用段落")
	check(s.Frame())
	click("button:保存并预览")
	if choice := m.templatePanel.choices[m.templatePanel.selected]; choice.template.Name != "周报常用段落" {
		t.Fatalf("the updated template was not selected: %+v", choice)
	}
	templates, err := loadTemplates(filepath.Join(m.dataDirectory, templateLibraryFile))
	check(err)
	if len(templates) != 1 || templates[0].Name != "周报常用段落" {
		t.Fatalf("editing created an extra template: %+v", templates)
	}
	m.templatePanel.values[0].SetText("v3")
	check(s.Frame())
	click("button:插入当前文档")
	if draft.source.text.CommittedString() != "发布 v2发布 v3" {
		t.Fatal("template did not insert at the caret")
	}
	_, err = s.Driver().ShortcutChord(gfx.ModifierControl, 'z')
	check(err)
	if draft.source.text.CommittedString() != "发布 v2" {
		t.Fatal("template insertion was not a single undo unit")
	}

	project := t.TempDir()
	m.addProjects(project)
	check(s.Frame())
	click("主菜单")
	click("menuitem:工具与模板")
	click("menuitem:将文档存为模板…")
	click("combobox:保存到")
	click("option:项目 · " + filepath.Base(project))
	click("button:保存")
	templates, err = loadTemplates(filepath.Join(project, ".editorx", templateLibraryFile))
	check(err)
	if len(templates) != 1 || templates[0].Body != "发布 v2" {
		t.Fatal("choosing the project destination did not save to the project template library")
	}
}

func TestSelectionTranslationAIThroughUI(t *testing.T) {
	const source = "Saved {count} files."
	const translated = "已保存 {count} 个文件。"
	const original = "Only for the local editor.\n" + source + "\nAnother private line."
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	tab := m.addTab(document{saved: original, eol: "LF"}, "messages.txt")
	editor := tab.source.text
	selection := text.Range{Start: editor.RuneIndex(strings.Index(original, source))}
	selection.End = selection.Start + len(source)
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	click := func(label string) { t.Helper(); check(s.ClickLabel(label)) }
	click("textfield:代码编辑区")
	for _, key := range []string{"ctrl+home", "down", "home", "shift+end"} {
		check(s.Keys(key))
	}
	if got := editor.Selection(); got != selection {
		t.Fatalf("Shift+End selected %+v, wanted %+v", got, selection)
	}
	check(s.RightClickLabel("textfield:代码编辑区"))
	click("menuitem:去翻译")
	state := m.translation
	if state == nil || m.translationEnabled || state.query.Text() != source ||
		state.target.selected != source || state.localLookup || state.request != nil || state.output.Text() != "" {
		t.Fatal("Go to Translation should carry the selection without showing the sidebar button or starting a lookup")
	}
	click("button:互换语言")
	if from, to, err := state.languagePair(); err != nil || from != "zh-cn" || to != "en" || state.request != nil {
		t.Fatal("swapping languages should update the direction without sending anything")
	}
	click("button:互换语言")
	click("button:AI 翻译")
	if !state.aiMenu || state.request != nil {
		t.Fatal("missing AI configuration should show its setup entry")
	}
	if _, err := s.Locate("menuitem:打开 AI 设置…"); err != nil {
		t.Fatal(err)
	}
	check(s.Driver().Key(gfx.KeyEscape, 0))

	requests := make(chan []aiMessage, 2)
	replies := make(chan string, 1)
	canceled := make(chan struct{}, 1)
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Messages []aiMessage }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		requests <- body.Messages
		select {
		case reply := <-replies:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []any{map[string]any{"message": aiMessage{Role: "assistant", Content: reply}}},
			})
		case <-r.Context().Done():
			canceled <- struct{}{}
		case <-time.After(4 * time.Second):
			http.Error(w, "test timed out", http.StatusGatewayTimeout)
		}
	}))
	t.Cleanup(service.Close)
	profiles := aiProfiles{DefaultID: 1, Profiles: []aiSettings{
		{ID: 1, Name: "测试翻译服务", Provider: "custom", BaseURL: service.URL, Model: "test", Protocol: aiChat, Auth: aiAuthNone},
		{ID: 2, Name: "备用测试服务", Provider: "custom", BaseURL: service.URL, Model: "backup", Protocol: aiChat, Auth: aiAuthNone},
	}}
	check(profiles.save(m.dataDirectory))

	beginRequest := func() {
		t.Helper()
		click("button:AI 翻译")
		if !state.aiMenu || state.request != nil {
			t.Fatal("opening the service list should not start translation")
		}
		select {
		case <-requests:
			t.Fatal("text was sent without starting translation")
		default:
		}
		click("menuitem:测试翻译服务 · test")
		select {
		case messages := <-requests:
			if len(messages) != 2 || messages[1] != (aiMessage{Role: "user", Content: source}) ||
				!strings.Contains(messages[0].Content, "from en to zh-cn") {
				t.Fatalf("translation did not send just the requested selection and direction: %+v", messages)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("translation request did not reach the selected service")
		}
	}
	beginRequest()
	replies <- translated
	deadline := time.Now().Add(2 * time.Second)
	for state.request != nil && time.Now().Before(deadline) {
		check(s.Frame())
		time.Sleep(5 * time.Millisecond)
	}
	if state.output.Text() != translated || !state.hasOutput || state.message != "" || editor.CommittedString() != original {
		t.Fatalf("the result did not stay in preview: %q, %s", state.output.Text(), state.problem)
	}
	if s.HasText("测试翻译服务 · test") || state.offline != nil {
		t.Fatal("AI exposed service details in the result or changed the active view")
	}
	click("button:保存到库…")
	click("button:保存词条")
	entries, err := readTranslationLibrary(filepath.Join(m.dataDirectory, translationLibraryFile), false)
	check(err)
	if len(entries) != 1 || entries[0].source != source || entries[0].target != translated {
		t.Fatalf("the approved AI translation did not enter the local library: %+v", entries)
	}
	click("button:替换选区")
	if editor.CommittedString() != strings.Replace(original, source, translated, 1) {
		t.Fatal("reviewed translation did not replace just the selected line")
	}
	_, err = s.Driver().ShortcutChord(gfx.ModifierControl, 'z')
	check(err)
	if editor.CommittedString() != original {
		t.Fatal("translated replacement was not undoable")
	}
	editor.SetSelection(selection)
	check(s.Frame())
	click("button:查选区")
	if state.request != nil || state.output.Text() != translated || len(state.suggestions) != 1 {
		t.Fatal("repeat lookup did not reuse the saved translation locally")
	}
	beginRequest()
	click("button:取消 AI 翻译")
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("canceling the translation did not stop the request")
	}
	if state.request != nil || editor.CommittedString() != original {
		t.Fatal("canceling affected the editor or retained the request")
	}
}

func TestCreateTextFileKeepsExistingFile(t *testing.T) {
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	directory := t.TempDir()
	doc := document{eol: "CRLF", bom: true}
	path := filepath.Join(directory, "notes.txt")
	if err := m.createTextFile(directory, "notes.txt", "第一行\n第二行\n", doc); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(original, []byte("\ufeff第一行\r\n第二行\r\n")) {
		t.Fatalf("created bytes = %q, %v", original, err)
	}
	m.closeTab(m.active)
	if err := m.createTextFile(directory, "notes.txt", "覆盖", doc); err == nil {
		t.Fatal("creating a matching filename overwrote an existing file")
	}
	current, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(current, original) {
		t.Fatalf("existing bytes changed = %q, %v", current, err)
	}
}

func TestLocalTranslationThroughUI(t *testing.T) {
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	project := t.TempDir()
	file := filepath.Join(project, "copy.txt")
	const source = "Saved {count} files."
	const translated = "已保存 {count} 个文件。"
	personalPath := filepath.Join(m.dataDirectory, translationLibraryFile)
	if err := os.WriteFile(personalPath, []byte("invalid csv"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	m.addProjects(project)
	m.load(file)
	tab := m.active
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	click := func(label string) { t.Helper(); check(s.ClickLabel(label)) }
	if _, shown := s.AccessByRole(gfx.RoleButton, "翻译"); shown || m.translation != nil || m.translationEnabled {
		t.Fatal("the translation sidebar button should be hidden by default")
	}
	click("button:设置")
	click("侧栏")
	click("switch:翻译")
	click("关闭设置页")
	prefs, err := readApplicationPreferences(m.dataDirectory)
	check(err)
	if !prefs.TranslationEnabled || !m.translationEnabled || m.translation != nil {
		t.Fatal("enabling the feature should persist without opening its panel")
	}
	click("button:翻译")
	if m.translation == nil || m.translation.problem != "" || len(m.translation.suggestions) != 0 {
		t.Fatal("opening the panel should defer library reading until lookup")
	}
	check(os.Remove(personalPath))
	projectPath := filepath.Join(project, ".editorx", translationLibraryFile)
	_, _, err = mergeTranslationLibrary(personalPath, []translationEntry{
		{"en", "zh-CN", translationSentence, source, "保存了 {count} 份文件。", ""},
	})
	check(err)
	_, _, err = mergeTranslationLibrary(projectPath, []translationEntry{
		{"en", "zh-CN", translationSentence, source, translated, "保存结果"},
		{"en", "zh-CN", translationTerm, "files", "文件", ""},
		{"en", "zh-CN", translationTerm, "file", "文件", ""},
	})
	check(err)
	tab.source.text.SetSelection(text.Range{End: tab.source.text.RuneCount()})
	check(s.Frame())
	click("button:查选区")
	state := m.translation
	if len(state.suggestions) != 2 || state.suggestions[0].entry.context != "保存结果" ||
		state.suggestions[1].scope != "个人" || state.output.Text() != translated ||
		len(state.terms) != 1 || state.terms[0].entry.source != "files" {
		t.Fatalf("unexpected matches: %+v, %+v", state.suggestions, state.terms)
	}
	state.output.SetText("文件已保存。")
	check(s.Frame())
	button, err := s.Locate("button:替换选区")
	check(err)
	if !button.Disabled || state.validateOutput() == nil {
		t.Fatal("the replacement remained available when {count} was removed")
	}
	state.output.SetText(translated)
	tab.source.text.MoveTo(0, false)
	check(s.Frame())
	click("button:替换选区")
	if tab.source.text.CommittedString() != source || !strings.Contains(state.problem, "选区已变化") {
		t.Fatal("a stale selection was replaced")
	}
	tab.source.text.SetSelection(text.Range{End: tab.source.text.RuneCount()})
	check(s.Frame())
	click("button:查选区")
	click("button:替换选区")
	if tab.source.text.CommittedString() != translated {
		t.Fatal("the approved translation was not applied")
	}
	_, err = s.Driver().ShortcutChord(gfx.ModifierControl, 'z')
	check(err)
	if tab.source.text.CommittedString() != source {
		t.Fatal("the replacement could not be undone in one step")
	}
	otherProject := t.TempDir()
	otherFile := filepath.Join(otherProject, "copy.txt")
	check(os.WriteFile(otherFile, []byte(source), 0600))
	m.addProjects(otherProject)
	m.load(otherFile)
	m.active.source.text.SetSelection(text.Range{End: m.active.source.text.RuneCount()})
	check(s.Frame())
	click("button:查选区")
	if len(state.suggestions) != 1 || state.suggestions[0].scope != "个人" || len(state.terms) != 0 {
		t.Fatal("another project received terms or sentences from a different project")
	}
	click("button:关闭翻译")
	if m.translation != nil {
		t.Fatal("closing the panel retained its state and matches")
	}
	click("button:翻译")
	click("button:设置")
	click("侧栏")
	click("switch:翻译")
	click("关闭设置页")
	if _, shown := s.AccessByRole(gfx.RoleButton, "翻译"); shown || m.translation != nil {
		t.Fatal("disabling the feature did not release the panel and its entry")
	}
}

func TestTranslationLibraryMergeAndFormatting(t *testing.T) {
	path := filepath.Join(t.TempDir(), translationLibraryFile)
	first := translationEntry{"en", "zh-cn", translationSentence, "Open {name}", "打开 {name}", "按钮"}
	if added, _, err := mergeTranslationLibrary(path, []translationEntry{first}); err != nil || added != 1 {
		t.Fatalf("first import: %d, %v", added, err)
	}
	if added, duplicates, err := mergeTranslationLibrary(path, []translationEntry{first, first}); err != nil || added != 0 || duplicates != 2 {
		t.Fatalf("duplicate import: %d, %d, %v", added, duplicates, err)
	}
	second, conflict := first, first
	second.context, conflict.target = "菜单", "开启 {name}"
	if _, _, err := mergeTranslationLibrary(path, []translationEntry{second, conflict}); err == nil {
		t.Fatal("import accepted a conflicting translation")
	}
	entries, err := readTranslationLibrary(path, false)
	if err != nil || len(entries) != 1 || entries[0] != first {
		t.Fatalf("conflicting import partially wrote the library: %+v, %v", entries, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append([]byte("\ufeff"), data...), 0600); err != nil {
		t.Fatal(err)
	}
	if withBOM, err := readTranslationLibrary(path, false); err != nil || len(withBOM) != 1 {
		t.Fatalf("UTF-8 BOM CSV: %+v, %v", withBOM, err)
	}
	if err := os.Truncate(path, translationLibraryLimit+1); err != nil {
		t.Fatal(err)
	}
	if _, err := readTranslationLibrary(path, false); err == nil || !strings.Contains(err.Error(), "2 MiB") {
		t.Fatalf("oversized translation library was not rejected: %v", err)
	}
	for _, pair := range [][2]string{{"{count} {count}", "{count}"}, {"Value %s then %d", "%d %s"}, {"Open \x24{name}", "打开 {name}"}} {
		if err := validateTranslationMarkers(pair[0], pair[1]); err == nil {
			t.Fatalf("missed invalid format change: %q → %q", pair[0], pair[1])
		}
	}
	if err := validateTranslationMarkers("{count} %1$d %2$s", "%2$s {count} %1$d"); err != nil {
		t.Fatal(err)
	}
	const raw = "{\"label\":\"Saved {count}\",\"path\":\"\\\\new\"}"
	start := strings.Index(raw, "Saved")
	if value, err := jsonTranslationReplacement(raw, start, start+len("Saved"), "\"已保存\""); err != nil || value != "\\\"已保存\\\"" {
		t.Fatalf("JSON value replacement = %q, %v", value, err)
	}
	if _, err := jsonTranslationReplacement(raw, 2, 7, "标签"); err == nil {
		t.Fatal("a JSON key was accepted as a value")
	}
	start = strings.Index(raw, "\\\\")
	if _, err := jsonTranslationReplacement(raw, start+1, start+3, "译文"); err == nil {
		t.Fatal("part of a JSON escape was accepted")
	}
	response := builtInTemplates()[2]
	rendered, err := renderTemplate(response, response.Filename, []string{"\"成功\""}, false)
	if err != nil || !strings.Contains(rendered, "\\\"成功\\\"") {
		t.Fatalf("quoted JSON template value = %q, %v", rendered, err)
	}
}
