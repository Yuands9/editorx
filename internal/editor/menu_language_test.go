package editor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func TestMenuLanguageUIAndPersistence(t *testing.T) {
	t.Cleanup(func() { setUILanguage(menuChinese) })
	store, err := openSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	preferences, err := readApplicationPreferences(store.directory)
	if err != nil || preferences.MenuLanguage != menuChinese {
		t.Fatalf("default menu language = %q, %v", preferences.MenuLanguage, err)
	}
	desktop := &editorDesktop{store: store, preferences: preferences}
	m, other := newEditorApp(), newEditorApp()
	for _, window := range []*editorApp{m, other} {
		window.desktop = desktop
		store.attach(window)
	}
	project := filepath.Join(store.directory, "example-project")
	if err := os.Mkdir(project, 0700); err != nil {
		t.Fatal(err)
	}
	projectFile := filepath.Join(project, "项目文档.txt")
	if err := os.WriteFile(projectFile, []byte("example"), 0600); err != nil {
		t.Fatal(err)
	}
	m.addProjects(project)
	tab := m.addTab(document{saved: "中文\nexample"}, "设置.txt")
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(s.ClickLabel("button:设置"))
	check(s.ClickLabel("combobox:界面语言"))
	check(s.ClickLabel("option:English"))
	saved, err := readApplicationPreferences(store.directory)
	check(err)
	if saved.MenuLanguage != menuEnglish || other.menuLanguage != menuEnglish {
		t.Fatal("menu language was not saved and applied to the other window")
	}
	preview := os.Getenv("VIVA_PREVIEW_DIR")
	if preview != "" {
		check(s.Screenshot(filepath.Join(preview, "english-menu-language.png")))
	}
	check(s.ClickLabel("button:Shortcuts"))
	_, err = s.Locate("combobox:Keyboard Shortcut Preset")
	check(err)
	_, err = s.Locate("textfield:Search Shortcuts")
	check(err)
	check(s.ClickLabel("button:Editor"))
	if !s.HasText("Editor Font") || !s.HasText("Live Preview") {
		t.Fatal("editor settings did not switch to English", s.Texts())
	}
	check(s.ClickLabel("button:Close Settings Page"))
	if !s.HasText("Line 1, Column 1") {
		t.Fatal("editor status bar did not switch to English", s.Texts())
	}
	check(s.ClickLabel("button:Main Menu"))
	check(s.ClickLabel("menuitem:Edit and Find"))
	if preview != "" {
		check(s.Screenshot(filepath.Join(preview, "english-main-menu.png")))
	}
	check(s.ClickLabel("menuitem:Find"))
	if !tab.search.open {
		t.Fatal("English menu item did not dispatch Find")
	}
	check(s.Driver().Key(gfx.KeyEscape, 0))
	if err := s.RightClickLabel("textfield:Code Editor"); err != nil {
		_, chinese := s.AccessByRole(gfx.RoleTextField, "代码编辑区")
		t.Fatalf("%v, active language %s, caption %q, Chinese editor shown %t", err, currentUILanguage(), uiText("代码编辑区"), chinese)
	}
	if preview != "" {
		check(s.Screenshot(filepath.Join(preview, "english-context-menu.png")))
	}
	check(s.ClickLabel("menuitem:Select All"))
	if tab.source.text.Selection() != (text.Range{Start: 0, End: 10}) || tab.source.text.String() != "中文\nexample" || tab.tabTitle() != "设置.txt" {
		t.Fatal("switching menus changed the content or the file name")
	}
	check(s.RightClickLabel("项目文档.txt"))
	check(s.ClickLabel("menuitem:Copy Path"))
	check(s.ClickLabel("menuitem:Full Path"))
	if s.Driver().Clipboard() != projectFile {
		t.Fatalf("translated project menu copied %q, want %q", s.Driver().Clipboard(), projectFile)
	}
	check(store.save())
	restarted, err := openSessionStore(store.directory)
	check(err)
	if len(restarted.windows) == 0 || restarted.windows[0].menuText("主菜单") != "Main Menu" {
		t.Fatal("the restored window did not use English")
	}
	check(s.ClickLabel("button:Settings"))
	check(s.ClickLabel("combobox:Interface Language"))
	check(s.ClickLabel("option:简体中文"))
	check(s.ClickLabel("button:关闭设置页"))
	check(s.ClickLabel("button:主菜单"))
	if _, found := s.AccessByRole(gfx.RoleMenuItem, "文件与标签"); !found {
		t.Fatal("switching back did not restore Chinese menus")
	}
}
