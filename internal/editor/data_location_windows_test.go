package editor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

func TestDataLocationSettingsAndMigration(t *testing.T) {
	folder := t.TempDir()
	path := filepath.Join(folder, editorDataLocationFile)
	location, err := readEditorDataLocation(path)
	if err != nil || location.Directory != filepath.Join(folder, applicationName, "data") {
		t.Fatalf("default data location = %+v, %v", location, err)
	}
	store, err := openSessionStore(location.Directory)
	if err != nil {
		t.Fatal(err)
	}
	m := newEditorApp()
	store.attach(m)
	preferences, err := readApplicationPreferences(location.Directory)
	if err != nil {
		t.Fatal(err)
	}
	preferences.ExitOnClose = false
	if err := preferences.save(location.Directory); err != nil {
		t.Fatal(err)
	}
	m.desktop = &editorDesktop{store: store, preferences: preferences, dataLocation: location}
	m.newDocument()
	m.active.source.replace("重启后应恢复的未保存草稿")
	if err := store.save(); err != nil {
		t.Fatal(err)
	}
	component := "translation-offline/fixture/model.bin"
	putDataLocationFixture(t, location.Directory, component, "installed model fixture")
	putDataLocationFixture(t, location.Directory, "WebView2/EBWebView/Local State", "{}")

	target := filepath.Join(folder, "迁移目的地")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	s := openTestSession(t, m)
	for _, label := range []string{"设置", "存储管理"} {
		if err := s.ClickLabel(label); err != nil {
			t.Fatal(err)
		}
	}
	if _, shown := s.AccessByRole(gfx.RoleButton, "数据目录"); shown {
		t.Fatal("data directory still has its own settings section")
	}
	if !s.HasText(location.Directory) {
		t.Fatal("settings did not show the active directory")
	}
	change, err := s.Locate("button:更换位置…")
	if err != nil {
		t.Fatal(err)
	}
	clean, err := s.Locate("button:清理缓存")
	if err != nil || change.Bounds.Y != clean.Bounds.Y || change.Bounds.X >= clean.Bounds.X {
		t.Fatalf("the directory and cleanup actions are out of order: %+v, %+v, %v", change.Bounds, clean.Bounds, err)
	}
	if err := s.Driver().QueueOpenDirectory("", app.ErrCanceled); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("更换位置…"); err != nil {
		t.Fatal(err)
	}
	if m.appSettings.problem != "" || m.desktop.dataLocation.Pending != "" {
		t.Fatal("canceling the folder picker changed the settings")
	}
	chooseTarget := func() {
		t.Helper()
		if err := s.Driver().QueueOpenDirectory(target, nil); err != nil {
			t.Fatal(err)
		}
		if err := s.ClickLabel("更换位置…"); err != nil {
			t.Fatal(err)
		}
	}
	chooseTarget()
	if !s.HasText(target) || m.dataDirectory != location.Directory {
		t.Fatal("the selected destination did not appear as a pending change")
	}
	if err := s.ClickLabel("取消更改"); err != nil {
		t.Fatal(err)
	}
	if canceled, err := readEditorDataLocation(path); err != nil || canceled.Pending != "" {
		t.Fatalf("pending change after cancellation: %+v, %v", canceled, err)
	}
	chooseTarget()
	pending, err := readEditorDataLocation(path)
	if err != nil || pending.Pending != target || pending.Directory != location.Directory {
		t.Fatalf("pending location = %+v, %v", pending, err)
	}
	if preview := os.Getenv("VIVA_PREVIEW_DIR"); preview != "" {
		if err := s.Screenshot(filepath.Join(preview, "settings-data-directory.png")); err != nil {
			t.Fatal(err)
		}
	}

	migrated, release, status, err := claimEditorDataLocation(pending, nil)
	if err != nil || release == nil || migrated.Directory != target || migrated.Pending != "" {
		t.Fatalf("migration = %+v, %q, %v", migrated, status, err)
	}
	defer release()
	if unlock, err := lockSessionDirectory(target); !errors.Is(err, errSessionDirectoryInUse) {
		if unlock != nil {
			unlock()
		}
		t.Fatalf("new data directory is not protected by the instance lock: %v", err)
	}
	reopened, err := openSessionStore(migrated.Directory)
	if err != nil || len(reopened.windows) != 1 || reopened.windows[0].active.source.text.String() != "重启后应恢复的未保存草稿" {
		t.Fatalf("unsaved draft did not reopen: %v", err)
	}
	if restored, err := readApplicationPreferences(target); err != nil || restored.ExitOnClose {
		t.Fatalf("preferences were not migrated: %+v, %v", restored, err)
	}
	for _, directory := range []string{location.Directory, target} {
		if got, err := os.ReadFile(filepath.Join(directory, component)); err != nil || string(got) != "installed model fixture" {
			t.Fatalf("model at %s = %q, %v", directory, got, err)
		}
		if _, err := os.Stat(filepath.Join(directory, "WebView2")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("browser cache remained in %s: %v", directory, err)
		}
	}
	if settings, err := readEditorDataLocation(path); err != nil || settings != migrated {
		t.Fatalf("final startup location = %+v, %v", settings, err)
	}
}

func TestDataLocationMigrationFailurePreservesSource(t *testing.T) {
	folder := t.TempDir()
	location, err := readEditorDataLocation(filepath.Join(folder, editorDataLocationFile))
	if err != nil {
		t.Fatal(err)
	}
	putDataLocationFixture(t, location.Directory, "templates.json", "source templates")
	location.Pending = filepath.Join(folder, "target")
	putDataLocationFixture(t, location.Pending, "unrelated.txt", "existing data")
	if err := location.save(); err != nil {
		t.Fatal(err)
	}
	active, release, status, err := claimEditorDataLocation(location, nil)
	if err != nil || release == nil || active != location || !strings.Contains(status, "已继续使用原目录") {
		t.Fatalf("unexpected fallback = %+v, %q, %v", active, status, err)
	}
	release()
	if content, err := os.ReadFile(filepath.Join(location.Pending, "unrelated.txt")); err != nil || string(content) != "existing data" {
		t.Fatalf("existing destination data changed: %q, %v", content, err)
	}
	if err := os.Remove(filepath.Join(location.Pending, "unrelated.txt")); err != nil {
		t.Fatal(err)
	}
	if err := copyEditorData(location.Directory, editorDataLocation{Directory: location.Pending, path: folder}); err == nil {
		t.Fatal("expected updating the location record to fail")
	}
	if entries, err := os.ReadDir(location.Pending); err != nil || len(entries) != 0 {
		t.Fatalf("failed migration was not rolled back: %v, %v", entries, err)
	}
	if content, err := os.ReadFile(filepath.Join(location.Directory, "templates.json")); err != nil || string(content) != "source templates" {
		t.Fatalf("source data changed: %q, %v", content, err)
	}
	t.Setenv(editorDataDirectoryEnv, location.Pending)
	if override, err := currentEditorDataLocation(); err != nil || override.path != "" || override.Directory != location.Pending {
		t.Fatalf("environment override = %+v, %v", override, err)
	}
}

func TestEditorWebCache(t *testing.T) {
	temporary := t.TempDir()
	t.Setenv("TEMP", temporary)
	t.Setenv("TMP", temporary)
	t.Setenv(editorWebCacheEnv, "previous-web-cache-setting")
	directory := t.TempDir()
	putDataLocationFixture(t, directory, "WebView2/EBWebView/Local State", "{}")
	putDataLocationFixture(t, directory, "WebView2/EBWebView/cache", "old browser cache")
	putDataLocationFixture(t, directory, "WebView2/personal.txt", "useful file")
	parent := filepath.Join(temporary, "EditorX-WebView2-"+sessionDirectoryKey(directory))
	putDataLocationFixture(t, parent, "run-previous/EBWebView/cache", "previous browser cache")

	cleanup := prepareEditorWebCache(directory)
	cache := os.Getenv(editorWebCacheEnv)
	if filepath.Dir(cache) != parent {
		t.Fatalf("WebView2 cache is not in the system temporary folder: %s", cache)
	}
	putDataLocationFixture(t, cache, "EBWebView/cache", "current browser cache")
	cleanup()
	for _, path := range []string{filepath.Join(directory, "WebView2", "EBWebView"), parent} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("browser cache remains at %s: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(directory, "WebView2", "personal.txt")); err != nil {
		t.Fatal(err)
	}
	if os.Getenv(editorWebCacheEnv) != "previous-web-cache-setting" {
		t.Fatal("the original WebView2 environment was not restored")
	}
}

func putDataLocationFixture(t *testing.T, directory, name, contents string) {
	t.Helper()
	path := filepath.Join(directory, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyDefaultDataMigration(t *testing.T) {
	folder := t.TempDir()
	settings := filepath.Join(folder, editorDataLocationFile)
	old := filepath.Join(folder, "data")
	target := filepath.Join(folder, applicationName, "data")

	store, err := openSessionStore(old)
	if err != nil {
		t.Fatal(err)
	}
	window := newEditorApp()
	store.attach(window)
	window.newDocument()
	window.active.source.replace("只存在于旧数据目录的草稿")
	if err := store.save(); err != nil {
		t.Fatal(err)
	}
	putDataLocationFixture(t, old, "WebView2/cache", "temporary")
	putDataLocationFixture(t, old, offlineTranslationDirectory+"/model.bin", "model")
	location, err := readEditorDataLocation(settings)
	if err != nil || location.Directory != old || location.legacyTarget != target {
		t.Fatalf("could not detect legacy data: %+v, %v", location, err)
	}

	moved, release, status, err := claimEditorDataLocation(location, nil)
	if err != nil || release == nil || moved.Directory != target || moved.legacyTarget != "" || status == "" {
		t.Fatalf("legacy migration failed: %+v, %q, %v", moved, status, err)
	}
	defer release()
	if unlock, err := lockSessionDirectory(target); !errors.Is(err, errSessionDirectoryInUse) {
		if unlock != nil {
			unlock()
		}
		t.Fatalf("migrated directory is not locked: %v", err)
	}
	for _, directory := range []string{old, target} {
		restored, err := openSessionStore(directory)
		if err != nil || len(restored.windows) != 1 || restored.windows[0].active.source.text.String() != "只存在于旧数据目录的草稿" {
			t.Fatalf("missing draft at %q: %v", directory, err)
		}
	}
	if _, err := os.Stat(filepath.Join(target, "WebView2")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("migration copied a cache: %v", err)
	}
	if _, err := os.Stat(settings); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("migration wrote a nonportable location override: %v", err)
	}
	if latest, err := readEditorDataLocation(settings); err != nil || latest != moved {
		t.Fatalf("relaunch did not choose the migrated data: %+v, %v", latest, err)
	}
}

func TestDefaultDataLocationPortableFolderAndExistingCopies(t *testing.T) {
	folder := t.TempDir()
	settings := filepath.Join(folder, editorDataLocationFile)
	target := filepath.Join(folder, applicationName, "data")
	putDataLocationFixture(t, filepath.Join(folder, "data"), "unrelated.txt", "another program")
	if location, err := readEditorDataLocation(settings); err != nil || location.Directory != target || location.legacyTarget != "" {
		t.Fatalf("an unrelated data folder was mistaken for EditorX data: %+v, %v", location, err)
	}
	putDataLocationFixture(t, filepath.Join(folder, "data"), templateLibraryFile, "older")
	putDataLocationFixture(t, target, templateLibraryFile, "copied")
	settingsInAppFolder := filepath.Join(folder, applicationName, editorDataLocationFile)
	for _, settingsPath := range []string{settings, settingsInAppFolder} {
		if location, err := readEditorDataLocation(settingsPath); err != nil || location.Directory != target || location.legacyTarget != "" {
			t.Fatalf("existing portable data was ignored: %+v, %v", location, err)
		}
	}
}

func TestLegacyDefaultMigrationFailureKeepsActiveDirectory(t *testing.T) {
	folder := t.TempDir()
	settings := filepath.Join(folder, editorDataLocationFile)
	old := filepath.Join(folder, "data")
	putDataLocationFixture(t, old, templateLibraryFile, "source templates")
	if err := os.WriteFile(filepath.Join(folder, applicationName), []byte("filesystem conflict"), 0600); err != nil {
		t.Fatal(err)
	}
	location, err := readEditorDataLocation(settings)
	if err != nil {
		t.Fatal(err)
	}
	active, release, status, err := claimEditorDataLocation(location, nil)
	if err != nil || release == nil || active.Directory != old || !strings.Contains(status, "已继续使用原目录") {
		t.Fatalf("failed migration did not keep the old directory: %+v, %q, %v", active, status, err)
	}
	defer release()
	if got, err := os.ReadFile(filepath.Join(old, templateLibraryFile)); err != nil || string(got) != "source templates" {
		t.Fatalf("legacy data changed after failed migration: %q, %v", got, err)
	}
}
