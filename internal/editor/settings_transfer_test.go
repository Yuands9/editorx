package editor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestSettingsJSONExportImport(t *testing.T) {
	dir := t.TempDir()
	m := newEditorApp()
	m.dataDirectory = dir
	m.newDocument()
	draft := m.active
	draft.source.replace("保留此草稿")
	s := openTestSession(t, m)
	m.showSettings(settingsAbout)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(s.Frame())
	path := filepath.Join(dir, "portable.json")
	check(s.Driver().QueueSaveFile(path, nil))
	check(s.ClickLabel("导出配置"))
	data, err := os.ReadFile(path)
	check(err)
	exported, err := decodeSettingsExport(data)
	check(err)
	if exported.Theme != themeSystem || !exported.ExitOnClose {
		t.Fatalf("did not export current settings: %+v", exported)
	}
	exported.Theme = themeDark
	exported.CNBRepository = "https://cnb.cool/group/repo"
	exported.ExitOnClose = false
	data, err = encodeSettingsExport(exported)
	check(err)
	check(os.WriteFile(path, data, 0600))

	check(s.Driver().QueueOpenFiles([]string{path}, nil))
	check(s.ClickLabel("导入配置"))
	if m.appSettings.importPreferences == nil || m.appSettings.draft.Theme != themeSystem {
		t.Fatal("import applied before confirmation")
	}
	check(s.Driver().Key(gfx.KeyEscape, 0))
	check(s.Frame())
	if !m.appSettings.open || m.appSettings.importPreferences != nil || m.appSettings.draft.Theme != themeSystem {
		t.Fatal("Escape should cancel only the import")
	}
	check(s.Driver().QueueOpenFiles([]string{path}, nil))
	check(s.ClickLabel("导入配置"))
	check(s.ClickLabel("确认导入"))
	saved, err := readApplicationPreferences(dir)
	check(err)
	if saved.Theme != themeDark || saved.CNBRepository != exported.CNBRepository || saved.ExitOnClose {
		t.Fatalf("settings were not restored: %+v", saved)
	}

	check(os.WriteFile(path, []byte("{\"format\":\"EditorX settings\",\"version\":2,\"preferences\":{}}"), 0600))
	check(s.Driver().QueueOpenFiles([]string{path}, nil))
	check(s.ClickLabel("导入配置"))
	if m.appSettings.importPreferences != nil || m.appSettings.problem == "" {
		t.Fatal("unknown configuration version was accepted")
	}

	check(s.ClickLabel("恢复默认配置"))
	check(s.ClickLabel("button:取消"))
	saved, err = readApplicationPreferences(dir)
	check(err)
	if saved.Theme != themeDark || m.appSettings.importPreferences != nil {
		t.Fatal("canceling the reset changed the preferences")
	}
	check(s.ClickLabel("恢复默认配置"))
	check(s.ClickLabel("确认恢复"))
	saved, err = readApplicationPreferences(dir)
	check(err)
	if saved.Theme != themeSystem || !saved.ExitOnClose || saved.CNBRepository != "" || saved.updateSource() != updateSourceGitHub ||
		m.appSettings.draft.Theme != themeSystem || !s.HasText("已恢复默认配置。") {
		t.Fatalf("default settings were not applied and saved: %+v", saved)
	}
	if m.active != draft || draft.source.text.String() != "保留此草稿" || !draft.modified() {
		t.Fatal("restoring defaults changed the active draft")
	}
	if folder := os.Getenv("VIVA_PREVIEW_DIR"); folder != "" {
		check(s.Resize(900, 820))
		check(s.Screenshot(filepath.Join(folder, "about-configuration.png")))
	}
}

func TestEditorDebugLogLimitAndClose(t *testing.T) {
	m := newEditorApp()
	m.debug.record("disabled", nil)
	if len(m.debug.data) != 0 {
		t.Fatal("disabled diagnostics wrote data")
	}
	m.debug.enabled = true
	for index := 0; index < 25000; index++ {
		m.debug.record("document.open", fmt.Errorf("private-path: %w", os.ErrNotExist))
	}
	m.debug.record("settings.import", errors.New("private-content"))
	report := m.diagnosticReport()
	if len(report) > editorDebugLimit || !strings.Contains(report, "settings.import") ||
		!strings.Contains(report, "not-found") || strings.Contains(report, "private-") {
		t.Fatal("debug report exceeded the limit, lost recent events, or leaked error content")
	}
	m.releaseWindowState()
	if m.debug.enabled || m.debug.data != nil {
		t.Fatal("closing the editor left diagnostics behind")
	}
}
