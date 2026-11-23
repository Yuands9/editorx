package editor

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
)

func TestEditorSettingsApplyAcrossWindowsAndRestore(t *testing.T) {
	store, err := openSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	preferences, err := readApplicationPreferences(store.directory)
	if err != nil {
		t.Fatal(err)
	}
	preferences.ShowTrayIcon = false
	desktop := &editorDesktop{store: store, preferences: preferences}
	m, other := newEditorApp(), newEditorApp()
	for _, window := range []*editorApp{m, other} {
		store.attach(window)
		window.desktop = desktop
	}
	plain := m.addTab(document{saved: "文本", eol: "LF"}, "settings.txt")
	jsonTab := m.addTab(document{saved: `{"field":[1,2]}`, eol: "LF"}, "inherited.json")
	overridden := other.addTab(document{saved: `{"field":[3,4]}`, eol: "LF"}, "local.json")

	otherSession := openTestSession(t, other)
	for _, label := range []string{"4 空格", "结果折行"} {
		if err := otherSession.ClickLabel(label); err != nil {
			t.Fatal(err)
		}
	}
	if overridden.source.view.Snapshot.Wrap || !overridden.wrapOverride || !overridden.indentOverride {
		t.Fatal("per-tab controls did not record their overrides")
	}

	session := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	click := func(label string) {
		t.Helper()
		check(session.ClickLabel(label))
	}
	check(session.Resize(900, 560))
	click("button:设置")
	click("编辑器")
	deadline := time.Now().Add(5 * time.Second)
	for m.appSettings.fontRequest != nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		check(session.Frame())
	}
	if len(m.appSettings.fonts) == 0 {
		t.Fatal("the font selector did not finish loading")
	}
	fontFamily := ""
	if len(m.appSettings.fonts) > 1 {
		fontFamily = m.appSettings.fonts[1]
		click("combobox:编辑区字体")
		click("option:" + fontFamily)
	}
	if preview := os.Getenv("VIVA_PREVIEW_DIR"); preview != "" {
		check(session.Screenshot(filepath.Join(preview, "editor-settings.png")))
	}
	click("combobox:编辑区字号")
	click("option:18 px")
	click("combobox:Tab 宽度")
	click("option:8 列")
	click("combobox:Tab 键输入")
	click("option:Tab 字符")
	click("combobox:默认自动折行")
	click("option:开启")
	click("combobox:JSON 默认缩进")
	click("option:2 个空格")
	if !m.appSettings.open || jsonTab.jsonIndent != 2 || !plain.source.view.Snapshot.Wrap {
		t.Fatal("settings did not take effect immediately while the settings page was open")
	}
	want := editorPreferences{FontFamily: fontFamily, FontSize: 18, TabWidth: 8, HardTabs: true, Wrap: wrapEnabled, JSONIndent: 2}
	saved, err := readApplicationPreferences(store.directory)
	if err != nil || saved.Editor != want {
		t.Fatalf("saved editor settings = %+v, %v", saved.Editor, err)
	}
	checkPanes := func(tab *editorTab, wrap bool) {
		t.Helper()
		for _, pane := range []*editorPane{tab.source, tab.result, tab.comparison} {
			snapshot := pane.view.Snapshot
			code, _ := pane.text.Code()
			face := gfx.MonoFamily(fontFamily)
			if snapshot.MonoFace != face || pane.input.MonoFace != face || snapshot.FontSize != 18 ||
				snapshot.TabWidth != 8 || snapshot.Wrap != wrap || code.TabWidth != 8 || !code.HardTabs {
				t.Fatalf("%s editor options: font %v, size %v, wrap %v, tab %+v", tab.title(), snapshot.MonoFace, snapshot.FontSize, snapshot.Wrap, code)
			}
		}
	}
	checkPanes(plain, true)
	checkPanes(jsonTab, true)
	checkPanes(overridden, false)
	click("关闭设置")
	if jsonTab.jsonIndent != 2 || overridden.jsonIndent != 4 ||
		jsonTab.source.text.String() != "{\n  \"field\": [\n    1,\n    2\n  ]\n}" ||
		jsonTab.source.text.String() != jsonTab.result.text.String() {
		t.Fatal("JSON did not use the correct global or per-tab indentation")
	}

	m.activate(plain)
	check(session.Frame())
	plain.source.text.MoveTo(plain.source.text.RuneCount(), false)
	check(session.Frame())
	check(session.Driver().Key(gfx.KeyTab, 0))
	if plain.source.text.String() != "文本\t" {
		t.Fatal("Tab did not insert the configured character")
	}
	fresh := m.addTab(document{saved: `{"field":5}`}, "new.json")
	checkPanes(fresh, true)
	if fresh.jsonIndent != 2 {
		t.Fatal("new JSON tab did not inherit the global indentation")
	}

	check(store.save())
	reopened, err := openSessionStore(store.directory)
	check(err)
	if len(reopened.windows) != 2 || reopened.windows[0].tabs[1].jsonIndent != 2 ||
		reopened.windows[1].tabs[0].jsonIndent != 4 ||
		!reopened.windows[1].tabs[0].wrapOverride || !reopened.windows[1].tabs[0].indentOverride {
		t.Fatal("session restore lost global defaults or per-tab overrides")
	}
	checkPanes(reopened.windows[0].tabs[1], true)
	checkPanes(reopened.windows[1].tabs[0], false)
}

func TestLegacyEditorSettingsPreserveManualOverrides(t *testing.T) {
	m := newEditorApp()
	preferences := defaultEditorPreferences()
	preferences.Wrap, preferences.JSONIndent = wrapDisabled, 2
	m.editorPrefs = preferences
	m.restoreSession(windowSession{Tabs: []tabSession{
		{Name: "legacy-default.json", JSONIndent: 4, JSONWrapSet: true, Source: paneSession{Wrap: true}},
		{Name: "legacy-wrap.txt", Source: paneSession{Wrap: true}},
		{Name: "legacy-override.json", JSONIndent: 2, JSONWrapSet: true},
	}})
	inherited, textTab, custom := m.tabs[0], m.tabs[1], m.tabs[2]
	if inherited.indentOverride || inherited.jsonIndent != 2 || inherited.wrapOverride ||
		inherited.source.view.Snapshot.Wrap {
		t.Fatal("legacy defaults did not start following the new application defaults")
	}
	preferences.Wrap, preferences.JSONIndent = wrapEnabled, 4
	m.applyEditorPreferences(preferences)
	if !textTab.wrapOverride || !textTab.source.view.Snapshot.Wrap ||
		!custom.indentOverride || custom.jsonIndent != 2 ||
		!custom.wrapOverride || custom.source.view.Snapshot.Wrap {
		t.Fatal("legacy manual preferences were lost")
	}
}
