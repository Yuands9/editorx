package editor

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

func TestSidebarPreferencesAndTemplatePanel(t *testing.T) {
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	m.newDocument()
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	click := func(label string) { t.Helper(); check(s.ClickLabel(label)) }
	check(s.Resize(900, 560))
	for _, label := range []string{"button:模板库", "button:翻译"} {
		if _, err := s.Locate(label); err == nil {
			t.Fatal("optional sidebar button visible by default", label)
		}
	}
	click("button:设置")
	click("button:侧栏")
	click("switch:模板库")
	click("switch:翻译")
	click("switch:Git")
	click("button:左侧")
	click("button:关闭设置页")
	preferences, err := readApplicationPreferences(m.dataDirectory)
	check(err)
	if preferences.Sidebar.Position != "left" || !preferences.Sidebar.Templates || !preferences.Sidebar.HideGit || !preferences.TranslationEnabled || m.translation != nil {
		t.Fatal("sidebar choices were not saved without starting translation")
	}
	if _, err := s.Locate("button:Git"); err == nil {
		t.Fatal("hidden Git button remains on rail")
	}
	for _, label := range []string{"button:模板库", "button:翻译", "button:设置"} {
		hit, err := s.Locate(label)
		check(err)
		if hit.Bounds.X > 44 {
			t.Fatal("rail did not move left", label, hit.Bounds)
		}
	}
	click("button:模板库")
	if m.templatePanel == nil || m.templateDialog != nil || m.interactionBlocked() {
		t.Fatal("template catalog must be a nonmodal side panel")
	}
	click("button:发布说明 · 内置")
	if !m.templatePanel.details {
		t.Fatal("template card did not open its preview")
	}
	before := len(m.tabs)
	m.templatePanel.values[0].SetText("v1")
	m.templatePanel.values[1].SetText("侧栏模板")
	check(s.Frame())
	click("button:创建草稿")
	if len(m.tabs) != before+1 || !m.active.modified() || !strings.Contains(m.active.source.text.String(), "#") {
		t.Fatal("sidebar template did not create a draft")
	}
	click("button:翻译")
	if m.templatePanel != nil || m.translation == nil {
		t.Fatal("translation did not replace the template side panel")
	}
	input, err := s.Locate("textfield:查询文本")
	check(err)
	editor, err := s.Locate("textfield:代码编辑区")
	check(err)
	if input.Bounds.X >= editor.Bounds.X {
		t.Fatal("translation panel did not follow the left rail")
	}
	click("button:设置")
	click("button:侧栏")
	click("button:右侧")
	click("button:关闭设置页")
	input, err = s.Locate("textfield:查询文本")
	check(err)
	editor, err = s.Locate("textfield:代码编辑区")
	check(err)
	if input.Bounds.X <= editor.Bounds.X {
		t.Fatal("open translation panel did not move right")
	}
	click("button:设置")
	click("button:侧栏")
	click("switch:翻译")
	click("switch:模板库")
	click("button:关闭设置页")
	if m.translation != nil || m.templatePanel != nil {
		t.Fatal("disabling the optional panels retained state")
	}
	if _, err := s.Locate("button:设置"); err != nil {
		t.Fatal("sidebar preferences hid the recovery entry", err)
	}
	preferences, err = readApplicationPreferences(m.dataDirectory)
	check(err)
	reopened := newEditorApp()
	check(reopened.applyApplicationPreferences(preferences))
	if reopened.sidebarPrefs.Position != "right" || !reopened.sidebarPrefs.HideGit || reopened.sidebarPrefs.Templates || reopened.translationEnabled {
		t.Fatal("sidebar preferences were not restored")
	}
}

func TestSidebarButtonsWithoutDocumentAndAlignment(t *testing.T) {
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	s := openTestSession(t, m)
	for _, label := range []string{"保存文件", "查找内容", "自动折行", "AI 助手", "Git", "终端"} {
		hit, err := s.Locate("button:" + label)
		if err != nil {
			t.Fatal("selected button missing without a document", label, err)
		}
		if (label == "保存文件" || label == "查找内容" || label == "自动折行") != hit.Disabled {
			t.Fatal("wrong button availability", label)
		}
	}
	m.showSettings(settingsSidebar)
	for _, width := range []int{900, 1440} {
		if err := s.Resize(width, 900); err != nil {
			t.Fatal(err)
		}
		var leftX, rightX float32
		for index, labels := range [][2]string{{"模板库", "翻译"}, {"保存文件", "查找内容"}, {"自动折行", "AI 助手"}, {"Git", "终端"}} {
			left, err := s.Locate("switch:" + labels[0])
			if err != nil {
				t.Fatal(err)
			}
			right, err := s.Locate("switch:" + labels[1])
			if err != nil {
				t.Fatal(err)
			}
			if index == 0 {
				leftX, rightX = left.Bounds.X, right.Bounds.X
			}
			if left.Bounds.X != leftX || right.Bounds.X != rightX {
				t.Fatal("switch columns do not align", labels, left.Bounds, right.Bounds)
			}
		}
		last, err := s.Locate("switch:颜色主题")
		if err != nil || last.Bounds.X != leftX {
			t.Fatal("last switch should align with the left column", last.Bounds, err)
		}
	}
	if err := s.ClickLabel("switch:翻译"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Locate("button:翻译"); err != nil {
		t.Fatal("settings did not immediately show the selected button", err)
	}
	if err := s.ClickLabel("button:翻译"); err != nil {
		t.Fatal(err)
	}
	if m.appSettings.open || m.translation == nil || len(m.tabs) != 0 {
		t.Fatal("sidebar could not open translation without a document")
	}
}

func TestSidebarAcrossWindowsAndSession(t *testing.T) {
	store, err := openSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	preferences, err := readApplicationPreferences(store.directory)
	if err != nil {
		t.Fatal(err)
	}
	m, another := newEditorApp(), newEditorApp()
	desktop := &editorDesktop{store: store, preferences: preferences}
	for _, window := range []*editorApp{m, another} {
		store.attach(window)
		window.desktop = desktop
		window.newDocument()
		window.aiEditorSplit.Ratio = .72
		window.showSettings(settingsSidebar)
	}
	m.appSettings.draft.Sidebar.Position = "left"
	m.appSettings.draft.Sidebar.Templates = true
	m.appSettings.draft.TranslationEnabled = true
	m.saveApplicationSettings()
	for _, window := range []*editorApp{m, another} {
		if window.sidebarPrefs.Position != "left" || !window.sidebarPrefs.Templates || !window.translationEnabled || math.Abs(float64(window.aiEditorSplit.Ratio-.28)) > .00001 {
			t.Fatal("sidebar preferences or panel width did not reach both windows")
		}
	}
	another.appSettings.draft.Sidebar.HideSearch = true
	another.saveApplicationSettings()
	if err := store.save(); err != nil {
		t.Fatal(err)
	}
	restored, err := openSessionStore(store.directory)
	if err != nil || len(restored.windows) != 2 {
		t.Fatal("could not restore both windows", err)
	}
	for _, window := range restored.windows {
		if window.sidebarPrefs.Position != "left" || !window.sidebarPrefs.HideSearch || !window.sidebarPrefs.Templates || math.Abs(float64(window.aiEditorSplit.Ratio-.28)) > .00001 {
			t.Fatal("sidebar or editor width changed after restart")
		}
		if math.Abs(float64(window.snapshotSession().AIEditorRatio-.72)) > .00001 {
			t.Fatal("session must store the editor width independently of sidebar position")
		}
		preferences := window.sidebarPrefs
		preferences.Position = "right"
		window.applySidebarPreferences(preferences)
		if math.Abs(float64(window.aiEditorSplit.Ratio-.72)) > .00001 {
			t.Fatal("moving the restored sidebar changed the editor width")
		}
	}
}

func TestAITranslationWithoutDictionary(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var request struct{ Messages []aiMessage }
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || len(request.Messages) != 2 || request.Messages[1].Content != "Hello world.\nPlease save {count} files." {
			t.Error("AI received the wrong input", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"你好，世界。\n请保存 {count} 个文件。"}}]}`))
	}))
	defer server.Close()
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	m.translationEnabled = true
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	profiles := aiProfiles{DefaultID: 1, Profiles: []aiSettings{{ID: 1, Name: "翻译测试", Provider: "custom", BaseURL: server.URL, Model: "test", Protocol: aiChat, Auth: aiAuthNone}}}
	check(profiles.save(m.dataDirectory))
	// An unreadable local library must not affect AI translation.
	check(os.WriteFile(filepath.Join(m.dataDirectory, translationLibraryFile), []byte("invalid csv"), 0600))
	m.openTranslation(false)
	s := openTestSession(t, m)
	check(s.Resize(900, 560))
	m.translation.query.SetText("Hello world.\nPlease save {count} files.")
	check(s.Frame())
	if calls.Load() != 0 {
		t.Fatal("AI sent text without a click")
	}
	check(s.ClickLabel("button:AI 翻译"))
	check(s.WaitFor(func(app.Snapshot) bool { return m.translation.request == nil }, 2*time.Second))
	state := m.translation
	if state.output.Text() != "你好，世界。\n请保存 {count} 个文件。" || state.problem != "" || state.dictionary != nil || calls.Load() != 1 {
		t.Fatal("AI-only translation failed", state.problem, state.output.Text())
	}
	for _, directory := range []string{dictionaryDirectory, offlineTranslationDirectory} {
		if _, err := os.Stat(filepath.Join(m.dataDirectory, directory)); !os.IsNotExist(err) {
			t.Fatal("AI translation created offline storage", directory, err)
		}
	}
	if _, err := s.Locate("button:替换选区"); err == nil {
		t.Fatal("typed input has no editor selection to replace")
	}
	if directory := os.Getenv("VIVA_PREVIEW_DIR"); directory != "" {
		check(s.Screenshot(filepath.Join(directory, "translation-ai-result.png")))
	}
	check(s.ClickLabel("button:复制"))
	if s.Driver().Clipboard() != state.output.Text() {
		t.Fatal("result could not be copied")
	}
	state.query.SetText("Changed input")
	check(s.Frame())
	if _, err := s.Locate("button:保存到库…"); err == nil {
		t.Fatal("a stale result can still be saved for edited input")
	}
}

func TestJSONSourceKeepsFreeInput(t *testing.T) {
	m := newEditorApp()
	file := filepath.Join(t.TempDir(), "free-input.json")
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(os.WriteFile(file, []byte("{}"), 0600))
	m.load(file)
	tab := m.active
	s := openTestSession(t, m)
	check(s.ClickLabel("textfield:代码编辑区"))
	tab.source.text.MoveTo(1, false)
	check(s.Frame())
	check(s.Driver().Key(gfx.KeyEnter, 0))
	first := tab.source.text.String()
	if !strings.Contains(first, "\n") || !tab.modified() {
		t.Fatalf("Enter inside braces was removed: %q", first)
	}
	check(s.Driver().Key(gfx.KeyEnter, 0))
	second := tab.source.text.String()
	if strings.Count(second, "\n") <= strings.Count(first, "\n") {
		t.Fatal("another blank line was collapsed")
	}
	_, err := s.Driver().ShortcutChord(gfx.ModifierControl, 'z')
	check(err)
	if tab.source.text.String() != first {
		t.Fatal("undo lost the original input")
	}
	_, err = s.Driver().ShortcutChord(gfx.ModifierControl, 'y')
	check(err)
	if tab.source.text.String() != second {
		t.Fatal("redo reformatted the original input")
	}
	for _, value := range []string{"{\n\n \t\"x\" : [  1,\n\n2  ]\n\n}\n", "{\n\n unfinished input\n"} {
		s.Driver().SetClipboard(value)
		_, err = s.Driver().ShortcutChord(gfx.ModifierControl, 'a')
		check(err)
		_, err = s.Driver().ShortcutChord(gfx.ModifierControl, 'v')
		check(err)
		if tab.source.text.String() != value {
			t.Fatal("source paste changed whitespace or incomplete input")
		}
		_, err = s.Driver().ShortcutChord(gfx.ModifierControl, 's')
		check(err)
		data, err := os.ReadFile(file)
		check(err)
		if string(data) != value {
			t.Fatalf("save changed raw input: %q", data)
		}
	}
}
