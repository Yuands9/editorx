package editor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
	draft := strings.Repeat("备份中保留的未保存草稿\r\n", 256)
	m.active.source.replace(draft)
	included := m.active
	m.newDocument()
	m.active.source.replace("未勾选的草稿")
	m.activate(included)
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
	if s.HasText("本地会话数据") {
		t.Fatal("unused local data card is still shown")
	}
	click("导出备份")
	if !s.HasText("已选 1 个文件") {
		t.Fatal("backup selection did not start with only the active file")
	}
	click("清空选择")
	click("checkbox:" + included.tabTitle())
	if !s.HasText("已选 1 个文件") {
		t.Fatal("manually reselecting a file failed")
	}
	if directory := os.Getenv("VIVA_PREVIEW_DIR"); directory != "" {
		if err := s.Screenshot(filepath.Join(directory, "settings-backup-tabs.png")); err != nil {
			t.Fatal(err)
		}
	}
	destination := filepath.Join(store.directory, "test.editorx-backup.zip")
	if err := s.Driver().QueueSaveFile(destination, nil); err != nil {
		t.Fatal(err)
	}
	click("选择保存位置…")
	if err := s.WaitFor(func(app.Snapshot) bool { return m.backup.message != "" }, 3*time.Second); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(destination)
	if err != nil || info.Size() >= int64(len(draft)/4) {
		t.Fatalf("ZIP did not compress repeated text: %v, %v", info, err)
	}
	saved, err := readEditorBackup(destination)
	if err != nil || len(saved.Windows) != 1 || len(saved.Windows[0].Tabs) != 1 {
		t.Fatalf("invalid session export: %+v, %v", saved, err)
	}
	restored := newEditorApp()
	restored.restoreSession(saved.Windows[0])
	if restored.active.source.text.String() != draft || !restored.active.modified() {
		t.Fatal("restoring the backup lost the unsaved text")
	}

	legacy, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(store.directory, "legacy.editorx-backup.json")
	if err := os.WriteFile(legacyPath, legacy, 0600); err != nil {
		t.Fatal(err)
	}
	if version1, err := readEditorBackup(legacyPath); err != nil || len(version1.Windows) != 1 {
		t.Fatalf("legacy backup could not be read: %v", err)
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
