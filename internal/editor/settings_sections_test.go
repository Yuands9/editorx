package editor

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Yuands9/editorx/internal/version"
)

func TestSettingsSectionsSearchAndUpdateDialog(t *testing.T) {
	store, err := openSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := newEditorApp()
	store.attach(m)
	m.newDocument()
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
	if _, err := s.Locate("button:备份"); err == nil {
		t.Fatal("the removed Backup section is still visible")
	}
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
	if directory := os.Getenv("VIVA_PREVIEW_DIR"); directory != "" {
		if err := s.Screenshot(filepath.Join(directory, "settings-about.png")); err != nil {
			t.Fatal(err)
		}
	}
	previousHTTP := editorUpdateHTTP
	editorUpdateHTTP = &http.Client{Transport: updateTestTransport(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("[]"))}, nil
	})}
	t.Cleanup(func() { editorUpdateHTTP = previousHTTP })
	click("button:更新")
	if !s.HasText("每次启动约 10 秒后检查，运行期间每 6 小时检查一次。") {
		t.Fatal("Updates does not state the automatic check schedule")
	}
	offered := testEditorRelease(t, 1)
	offered.Notes = "新增更新设置。"
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
	if m.update.open || !m.appSettings.open || m.appSettings.section != settingsUpdate {
		t.Fatal("closing the update dialog should return to Updates")
	}
}

func TestMergeRestoredWindowsKeepsSeparateDraftsAndGroupIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "configuration.txt")
	current := newEditorApp()
	first := current.addTab(document{path: path, saved: "saved", eol: "LF"}, "configuration.txt")
	first.source.replace("current draft")
	current.newTabGroup(first)

	for _, text := range []string{"first imported draft", "second imported draft"} {
		restored := newEditorApp()
		imported := restored.addTab(document{path: path, saved: "saved", eol: "LF"}, "configuration.txt")
		imported.source.replace(text)
		restored.newTabGroup(imported)
		if active := current.mergeRestoredWindow(restored, nil); active != imported || imported.doc.path != "" ||
			imported.source.text.String() != text || !imported.modified() {
			t.Fatal("a conflicting restored tab did not remain editable as a separate draft")
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
