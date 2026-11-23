package editor

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Yuands9/editorx/internal/version"
	"github.com/vivaui/viva/app"
)

func TestSettingsSectionsSearchBackupAndUpdateDialog(t *testing.T) {
	store, err := openSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := newEditorApp()
	store.attach(m)
	m.newDocument()
	m.active.source.replace("备份中保留的未保存草稿")
	s := openTestSession(t, m)
	click := func(label string) {
		t.Helper()
		if err := s.ClickLabel(label); err != nil {
			t.Fatal(err)
		}
	}
	click("设置")
	for _, section := range settingSections {
		if _, err := s.Locate("button:" + section.label); err != nil {
			t.Fatal(err)
		}
	}
	click("搜索设置")
	if err := s.Type("快捷键"); err != nil {
		t.Fatal(err)
	}
	click("打开 快捷键")
	if m.appSettings.section != settingsShortcuts || !s.HasText("Ctrl+R") {
		t.Fatal("settings search did not open the shortcut reference")
	}
	click("编辑器")
	if m.appSettings.editorPreview == nil || !s.HasText("实时预览") {
		t.Fatal("editor settings preview is absent")
	}
	click("备份")
	destination := filepath.Join(store.directory, "test.editorx-backup.json")
	if err := s.Driver().QueueSaveFile(destination, nil); err != nil {
		t.Fatal(err)
	}
	click("导出备份")
	if err := s.WaitFor(func(app.Snapshot) bool { return m.backup.message != "" }, 3*time.Second); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := decodeEditorSession(data)
	if err != nil || len(saved.Windows) != 1 || len(saved.Windows[0].Tabs) != 1 {
		t.Fatalf("invalid session export: %+v, %v", saved, err)
	}
	restored := newEditorApp()
	restored.restoreSession(saved.Windows[0])
	if restored.active.source.text.String() != "备份中保留的未保存草稿" || !restored.active.modified() {
		t.Fatal("restoring the backup lost the unsaved text")
	}

	click("关于")
	if !s.HasText("版本 " + version.Number) {
		t.Fatal("About does not display the executable version")
	}
	if directory := os.Getenv("VIVA_PREVIEW_DIR"); directory != "" {
		if err := s.Screenshot(filepath.Join(directory, "settings-about.png")); err != nil {
			t.Fatal(err)
		}
	}
	m.update = editorUpdateState{
		open: true, release: editorRelease{version: "0.33.0", comparison: 1, size: 38615552, notes: "新增设置分类和备份。"},
	}
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if !s.HasText("发现新版本") || !s.HasText("EditorX 0.33.0") {
		t.Fatal("the update dialog did not show the offered version")
	}
	action := "下载更新"
	if hasWordEditor {
		action = "下载公开版"
	}
	if _, err := s.Locate("button:" + action); err != nil {
		t.Fatal(err)
	}
	if directory := os.Getenv("VIVA_PREVIEW_DIR"); directory != "" {
		if err := s.Screenshot(filepath.Join(directory, "settings-update.png")); err != nil {
			t.Fatal(err)
		}
	}
	click("稍后")
	if m.update.open || !m.appSettings.open {
		t.Fatal("closing the update dialog should return to About")
	}
}
