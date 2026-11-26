package editor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
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
	if _, err := readEditorBackup(legacyPath); err == nil {
		t.Fatal("a legacy JSON backup was accepted")
	}
	mislabeled := filepath.Join(store.directory, "legacy.zip")
	if err := os.WriteFile(mislabeled, legacy, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readEditorBackup(mislabeled); err == nil {
		t.Fatal("JSON renamed to ZIP was accepted")
	}

	legacyBackup := filepath.Join(store.directory, "multiple-windows.editorx-backup.zip")
	saved.Windows = append(saved.Windows, windowSession{
		Tabs: []tabSession{{Name: "来自旧版第二个窗口.md", Source: paneSession{Content: "# 第二个窗口"}}},
	})
	if err := writeEditorBackup(context.Background(), legacyBackup, saved); err != nil {
		t.Fatal(err)
	}
	var restoreOnUI func()
	m.post = func(callback func()) { restoreOnUI = callback }
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if err := s.Driver().QueueOpenFiles([]string{legacyBackup}, nil); err != nil {
		t.Fatal(err)
	}
	click("选择备份…")
	if request, ok := s.Driver().LastOpenFilesRequest(); !ok || len(request.Filters) != 1 || request.Filters[0].Pattern != "*.zip" {
		t.Fatalf("backup picker must offer only ZIP files: %+v", request)
	}
	if err := s.WaitFor(func(app.Snapshot) bool { return restoreOnUI != nil || m.backup.problem != "" }, 3*time.Second); err != nil {
		t.Fatal("ZIP import did not schedule restoration in the current window: ", err)
	}
	if restoreOnUI == nil {
		t.Fatal(m.backup.problem)
	}
	if !m.backup.applying || m.backup.progress.total.Load() != 4 || m.backup.progress.completed.Load() != 2 {
		t.Fatal("the import did not report the prepared file while waiting to apply the restored tabs")
	}
	if !s.HasText("正在恢复到当前窗口…") {
		t.Fatal("the import progress was not visible in settings")
	}
	restoreOnUI()
	if m.backup.request != nil {
		t.Fatal("backup import is still pending after the restore callback")
	}
	if len(store.windows) != 1 || len(m.tabs) != 4 || m.active == included ||
		m.active.source.text.String() != draft || !m.active.modified() {
		t.Fatal("ZIP import did not restore the draft in the existing window")
	}
	if m.tabs[0] != included || m.tabs[1].source.text.String() != "未勾选的草稿" {
		t.Fatal("import changed one of the existing tabs")
	}
	if m.tabs[3].source.text.String() != "# 第二个窗口" {
		t.Fatal("a second window in the ZIP was not imported into the current window")
	}
	m.post = nil
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	click("设置")
	click("关于")
	for _, value := range []string{"版本信息", "v" + version.Number, "运行平台", "Windows · ", "构建", "运行时", runtime.Version()} {
		if !s.HasText(value) {
			t.Fatalf("About does not display %q", value)
		}
	}
	click("复制版本信息")
	report := s.Driver().Clipboard()
	if !strings.HasPrefix(report, "EditorX\n版本 v"+version.Number) ||
		!strings.Contains(report, "\n运行平台 Windows · ") || !strings.Contains(report, "\n构建 "+version.Build()) ||
		!strings.HasSuffix(report, "\n运行时 "+runtime.Version()) || !s.HasText("已复制") {
		t.Fatalf("About did not copy the displayed version information: %q", report)
	}
	if !s.HasText("每次启动约 10 秒后检查，运行期间每 6 小时检查一次。") {
		t.Fatal("About does not state the automatic check schedule")
	}
	if directory := os.Getenv("VIVA_PREVIEW_DIR"); directory != "" {
		if err := s.Screenshot(filepath.Join(directory, "settings-about.png")); err != nil {
			t.Fatal(err)
		}
	}
	offered := testEditorRelease(t, 1)
	offered.Notes = "新增设置分类和备份。"
	m.update = editorUpdateState{
		open: true, initialized: true,
		cache: editorUpdateCache{Release: offered},
	}
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if !s.HasText("EditorX " + offered.Version + " 可更新") {
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

func TestMergeBackupKeepsSeparateDraftsAndGroupIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "configuration.txt")
	current := newEditorApp()
	first := current.addTab(document{path: path, saved: "saved", eol: "LF"}, "configuration.txt")
	first.source.replace("current draft")
	current.newTabGroup(first)

	for _, text := range []string{"first imported draft", "second imported draft"} {
		backup := newEditorApp()
		imported := backup.addTab(document{path: path, saved: "saved", eol: "LF"}, "configuration.txt")
		imported.source.replace(text)
		backup.newTabGroup(imported)
		if active := current.mergeRestoredWindow(backup, nil); active != imported || imported.doc.path != "" ||
			imported.source.text.String() != text || !imported.modified() {
			t.Fatal("a conflicting backup did not remain editable as a separate draft")
		}
	}

	if len(current.tabs) != 3 || current.tabs[0] != first ||
		first.source.text.String() != "current draft" || current.tabAtPath(path) != first {
		t.Fatal("an existing file or draft was replaced")
	}
	for index, tab := range current.tabs {
		if tab.id != index+1 || tab.group == nil || tab.group.ID != index+1 {
			t.Fatal("a restored tab or group collided with another tab's identity")
		}
	}
}
