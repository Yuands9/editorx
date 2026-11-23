package editor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

func TestApplicationSettingsPersistFromMenu(t *testing.T) {
	store, err := openSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	preferences, err := readApplicationPreferences(store.directory)
	if err != nil || !preferences.ShowTrayIcon || !preferences.ExitOnClose {
		t.Fatalf("initial application settings = %+v, %v", preferences, err)
	}
	m, another := newEditorApp(), newEditorApp()
	desktop := &editorDesktop{store: store, preferences: preferences}
	for _, window := range []*editorApp{m, another} {
		store.attach(window)
		window.desktop = desktop
	}
	session := openTestSession(t, m)
	click := func(label string) {
		t.Helper()
		if err := session.ClickLabel(label); err != nil {
			t.Fatal(err)
		}
	}
	if err := session.Resize(900, 560); err != nil {
		t.Fatal(err)
	}
	gear, err := session.Locate("button:设置")
	if err != nil {
		t.Fatal(err)
	}
	click("button:设置")
	if !m.appSettings.open || m.appSettings.section != settingsAppearance || len(m.tabs) != 0 {
		t.Fatal("the settings button must work with no tabs")
	}
	closeGear, err := session.Locate("button:关闭设置")
	if err != nil || closeGear.Bounds != gear.Bounds {
		t.Fatalf("settings gear moved from %v to %v: %v", gear.Bounds, closeGear.Bounds, err)
	}
	if preview := os.Getenv("VIVA_PREVIEW_DIR"); preview != "" {
		if err := session.Screenshot(filepath.Join(preview, "settings-general.png")); err != nil {
			t.Fatal(err)
		}
	}
	click("显示系统托盘图标")
	for _, label := range []string{"button:保存", "button:取消"} {
		if _, err := session.Locate(label); err == nil {
			t.Fatalf("auto-saved settings still show %s", label)
		}
	}
	saved, err := readApplicationPreferences(store.directory)
	if err != nil || saved.ShowTrayIcon || !saved.ExitOnClose {
		t.Fatalf("changing tray visibility should leave close behavior enabled: %+v, %v", saved, err)
	}
	closeButton, err := session.Locate("button:关闭设置页")
	if err != nil || closeButton.Bounds.X < 780 || closeButton.Bounds.Y >= 80 {
		t.Fatalf("settings close button is not in the upper right: %v, %v", closeButton.Bounds, err)
	}
	click("关闭设置页")
	click("主菜单")
	click("menuitem:设置")
	another.openApplicationSettings()
	click("外观")
	if _, err := session.Locate("button:跟随系统"); err != nil {
		t.Fatal("appearance settings must offer Windows system theme")
	}
	click("浅色")
	if !m.light || !another.light || !m.appSettings.open || another.appSettings.draft.Theme != themeLight {
		t.Fatal("the auto-saved theme did not immediately apply to both windows")
	}
	another.appSettings.draft.ExitOnClose = false
	another.saveApplicationSettings()
	saved, err = readApplicationPreferences(store.directory)
	if err != nil || saved.Theme != themeLight || saved.ShowTrayIcon || saved.ExitOnClose {
		t.Fatalf("saving a second window's settings lost existing preferences: %+v, %v", saved, err)
	}

	if _, err := session.Driver().ShortcutChord(gfx.ModifierControl, ','); err != nil || m.appSettings.open {
		t.Fatalf("Ctrl+, did not close settings: %v", err)
	}
	if _, err := session.Driver().ShortcutChord(gfx.ModifierControl, ','); err != nil || !m.appSettings.open {
		t.Fatalf("Ctrl+, did not reopen settings: %v", err)
	}
	click("AI")
	click("combobox:对话字号")
	click("option:16 px")
	click("combobox:新对话的默认模式")
	click("option:新建草稿")
	profiles, err := readAIProfiles(store.directory)
	if err != nil || profiles.ChatFont != 16 || profiles.DefaultMode != aiDraft ||
		another.ai.profiles.ChatFont != 16 || another.ai.profiles.DefaultMode != aiDraft {
		t.Fatalf("global AI preferences did not persist and update the other window: %+v, %v", profiles, err)
	}
	if _, err := session.Locate("新增配置"); err != nil {
		t.Fatal(err)
	}
	click("关闭设置")
	if m.appSettings.open || len(m.tabs) != 0 {
		t.Fatal("returning from settings changed the tabless workspace")
	}
}

func TestThemeSwitchAlternatesDarkAndLight(t *testing.T) {
	store, err := openSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	desktop := &editorDesktop{store: store, preferences: applicationPreferences{Theme: themeSystem}}
	m := newEditorApp()
	for _, window := range []*editorApp{m, newEditorApp()} {
		store.attach(window)
		window.desktop = desktop
		window.newDocument()
	}

	session := openTestSession(t, m)
	driver := session.Driver()
	t.Cleanup(app.WatchSystemAppearance(desktop.refreshSystemTheme))
	driver.SetSystemAppearance(app.AppearanceLight)
	initial, err := readApplicationPreferences(store.directory)
	if err != nil || initial.Theme != themeSystem {
		t.Fatalf("a fresh profile should follow Windows: %+v, %v", initial, err)
	}
	if err := os.WriteFile(filepath.Join(store.directory, applicationSettingsFile), []byte("{\"theme\":\"system\"}"), 0600); err != nil {
		t.Fatal(err)
	}
	legacy, err := readApplicationPreferences(store.directory)
	if err != nil || legacy.Theme != themeSystem {
		t.Fatalf("a saved system preference should still follow Windows: %+v, %v", legacy, err)
	}
	desktop.preferences = legacy

	cycleTheme := func(theme string) {
		t.Helper()
		if err := session.ClickLabel("button:切换颜色主题"); err != nil {
			t.Fatal(err)
		}
		if desktop.preferences.Theme != theme {
			t.Fatalf("one click selected %q, want %q", desktop.preferences.Theme, theme)
		}
		saved, err := readApplicationPreferences(store.directory)
		if err != nil || saved.Theme != theme {
			t.Fatalf("theme did not persist, want %q: %+v, %v", theme, saved, err)
		}
	}
	checkTheme := func(light bool) {
		t.Helper()
		for index, window := range store.windows {
			if window.light != light || window.active.source.light != light {
				t.Fatalf("window %d did not switch to light=%v", index, light)
			}
		}
	}

	checkTheme(true)
	cycleTheme(themeDark)
	checkTheme(false)
	cycleTheme(themeLight)
	driver.SetSystemAppearance(app.AppearanceDark)
	checkTheme(true)
	if err := session.ClickLabel("button:设置"); err != nil {
		t.Fatal(err)
	}
	if err := session.ClickLabel("跟随系统"); err != nil {
		t.Fatal(err)
	}
	checkTheme(false)
	if desktop.preferences.Theme != themeSystem || driver.NativeAppearance() != app.AppearanceUnknown {
		t.Fatal("system theme did not restore Windows control of the title bar")
	}
	driver.SetSystemAppearance(app.AppearanceLight)
	checkTheme(true)
}
