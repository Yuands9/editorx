package editor

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

func TestLocalHistoryCapturesOverwrittenBytes(t *testing.T) {
	const original = "\xff\xfe\x2d\x4e\x87\x65\x0d\x00\x0a\x00"
	path := filepath.Join(t.TempDir(), "历史.txt")
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	m.load(path)
	tab := m.active
	if tab == nil {
		t.Fatal(m.errorText)
	}
	tab.source.replace("新版\n")
	if !m.saveTab(tab, false) || !m.saveTab(tab, false) {
		t.Fatal(m.errorText)
	}
	store := m.documentHistory()
	catalog, err := store.catalog()
	if err != nil || catalog.count != 1 || len(catalog.files) != 1 || catalog.files[0].source != path || catalog.bytes <= 0 {
		t.Fatalf("unexpected history: %+v, %v", catalog, err)
	}
	var recovered bytes.Buffer
	version := catalog.files[0].versions[0]
	if err := store.copyVersion(context.Background(), version, &recovered); err != nil || recovered.String() != original {
		t.Fatalf("history lost encoding, BOM or line endings: %x, %v", recovered.Bytes(), err)
	}
	if info, err := os.Stat(filepath.Join(store.directory, version.key())); err != nil || info.Size() != catalog.bytes {
		t.Fatalf("storage display does not match the snapshot file: %v, %v", info, err)
	}
	wantErr := errors.New("failed document write")
	if _, err := writeDocumentAtomically(context.Background(), path, nil, store, func(io.Writer) error {
		return wantErr
	}); !errors.Is(err, wantErr) {
		t.Fatal(err)
	}
	catalog, err = store.catalog()
	if err != nil || catalog.count != 1 {
		t.Fatalf("failed save changed history: %+v, %v", catalog, err)
	}
	store.setPolicy(localHistoryPreferences{Disabled: true})
	tab.source.replace("关闭记录后的内容\n")
	if !m.saveTab(tab, false) {
		t.Fatal(m.errorText)
	}
	catalog, err = store.catalog()
	if err != nil || catalog.count != 1 {
		t.Fatalf("disabled recording changed existing history: %+v, %v", catalog, err)
	}
}

func TestLocalHistorySnapshotSizeLimit(t *testing.T) {
	for _, test := range []struct {
		name  string
		size  int64
		count int
	}{
		{name: "at_limit", size: localHistoryFileSizeLimit, count: 1},
		{name: "above_limit", size: localHistoryFileSizeLimit + 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "history.txt")
			file, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := errors.Join(file.Truncate(test.size), file.Close()); err != nil {
				t.Fatal(err)
			}
			store := &localHistoryStore{directory: filepath.Join(t.TempDir(), localHistoryDirectory)}
			const saved = "已保存\n"
			if _, err := writeDocumentBytes(path, []byte(saved), nil, store); err != nil {
				t.Fatal(err)
			}
			if data, err := os.ReadFile(path); err != nil || string(data) != saved {
				t.Fatalf("saving the document failed: %q, %v", data, err)
			}
			catalog, err := store.catalog()
			if err != nil || catalog.count != test.count || catalog.problem != "" {
				t.Fatalf("unexpected history after replacing %d bytes: %+v, %v", test.size, catalog, err)
			}
			if test.count == 0 {
				if _, err := os.Stat(store.directory); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("skipping a snapshot created a history directory: %v", err)
				}
				return
			}
			if err := store.copyVersion(context.Background(), catalog.files[0].versions[0], io.Discard); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLocalHistoryRetentionAndClear(t *testing.T) {
	directory := t.TempDir()
	first, second := filepath.Join(directory, "a.txt"), filepath.Join(directory, "b.txt")
	store := &localHistoryStore{directory: filepath.Join(directory, localHistoryDirectory),
		policy: localHistoryPreferences{Days: -1, Versions: -1}}
	now := time.Now()
	recordHistoryFixture(t, store, first, "over the count limit", now.Add(-48*time.Hour))
	recordHistoryFixture(t, store, first, "keep", now.Add(-time.Hour))
	recordHistoryFixture(t, store, second, "past the time limit", now.Add(-10*24*time.Hour))
	store.setPolicy(localHistoryPreferences{Days: 7, Versions: 1})
	if removed, err := store.clean(now, false); err != nil || removed != 2 {
		t.Fatalf("retention removed %d versions: %v", removed, err)
	}
	catalog, err := store.catalog()
	if err != nil || catalog.count != 1 || catalog.files[0].source != first {
		t.Fatalf("wrong retained history: %+v, %v", catalog, err)
	}
	pending, _, err := store.stage(context.Background(), second, now)
	if err != nil || pending == nil {
		t.Fatalf("could not stage a pending write: %v", err)
	}
	defer os.Remove(pending.temporary)
	if removed, err := store.clean(now, true); err != nil || removed != 1 {
		t.Fatalf("clear removed %d versions: %v", removed, err)
	}
	if err := store.commit(pending); err != nil {
		t.Fatal(err)
	}
	if catalog, err := store.catalog(); err != nil || catalog.count != 0 || catalog.bytes != 0 {
		t.Fatalf("a cleared or pending version remained: %+v, %v", catalog, err)
	}
	if contents, err := os.ReadFile(first); err != nil || string(contents) != "keep" {
		t.Fatalf("cleanup changed the original: %q, %v", contents, err)
	}
}

func TestLocalHistoryThroughStorageSettings(t *testing.T) {
	directory := t.TempDir()
	first, second := filepath.Join(directory, "版本.txt"), filepath.Join(directory, "其他.txt")
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	store := m.documentHistory()
	now := time.Now()
	oldest := now.Add(-3 * time.Hour)
	newest := now.Add(-2 * time.Hour)
	recordHistoryFixture(t, store, first, "第一次保存", oldest)
	recordHistoryFixture(t, store, first, "第二次保存", newest)
	recordHistoryFixture(t, store, second, "另一个文件", now.Add(-time.Hour))
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(os.WriteFile(filepath.Join(m.dataDirectory, "sample-data.bin"), bytes.Repeat([]byte("a"), 2048), 0600))
	cache := filepath.Join(m.dataDirectory, editorPreviewCacheDirectory, "page-1234.html")
	check(os.MkdirAll(filepath.Dir(cache), 0700))
	check(os.WriteFile(cache, bytes.Repeat([]byte("c"), 3072), 0600))
	session := openTestSession(t, m)
	m.showSettings(settingsStorage)
	check(session.Frame())
	state := &m.appSettings.history
	check(session.WaitFor(func(app.Snapshot) bool { return state.overview.ready }, 3*time.Second))
	if state.overview.usage.data != 5120 || state.overview.usage.history <= 0 ||
		!session.HasText("应用数据") || !session.HasText("历史版本") ||
		!session.HasText("可清理 3.0 KiB") || !session.HasText("不可清理 2.0 KiB") {
		t.Fatalf("storage categories were not displayed separately: %+v", state.overview.usage)
	}
	if !session.HasText("只记录大小在 32 MiB 以内的版本。") {
		t.Fatal("storage overview omitted the history size limit")
	}
	if directory := os.Getenv("VIVA_PREVIEW_DIR"); directory != "" {
		check(session.Resize(900, 560))
		check(session.Screenshot(filepath.Join(directory, "storage-overview.png")))
		check(session.Resize(1440, 900))
	}
	historyBytes := state.overview.usage.history
	check(session.ClickLabel("button:清理缓存"))
	check(session.WaitFor(func(app.Snapshot) bool {
		return state.overview.request == nil && state.overview.notice == "已清理 3.0 KiB 缓存"
	}, 3*time.Second))
	if state.overview.usage.data != 2048 || state.overview.usage.history != historyBytes ||
		!session.HasText("可清理 0 B") {
		t.Fatalf("cache cleanup reported incorrect storage: %+v", state.overview.usage)
	}
	check(session.ClickLabel("button:管理历史版本"))
	waitForCount := func(count int) {
		t.Helper()
		check(session.WaitFor(func(app.Snapshot) bool {
			return state.loaded && state.action == nil && state.catalog.count == count
		}, 3*time.Second))
	}
	waitForCount(3)
	if !session.HasText("32 MiB 以内的旧版本") {
		t.Fatal("history settings omitted the snapshot size limit")
	}
	state.filter.SetText("版本.txt")
	check(session.Frame())
	check(session.ClickLabel("button:版本.txt"))
	if state.expanded == "" || len(state.selected) != 0 {
		t.Fatal("clicking the file did not expand only the version group")
	}
	check(session.ClickLabel("button:版本.txt"))
	if state.expanded != "" || len(state.selected) != 0 {
		t.Fatal("clicking an open file did not collapse only the version group")
	}
	check(session.ClickLabel("checkbox:选择 版本.txt 的所有版本"))
	if state.expanded != "" || len(state.selected) != 2 {
		t.Fatal("file checkbox did not select both versions without expanding")
	}
	check(session.ClickLabel("checkbox:选择 版本.txt 的所有版本"))
	check(session.ClickLabel("button:版本.txt"))
	check(session.ClickLabel("button:比对版本"))
	compareWindow := openHistoryTestSession(t, m)
	check(compareWindow.WaitFor(func(app.Snapshot) bool {
		compare := m.historyCompare
		return compare != nil && compare.source == first && compare.comparison != nil
	}, 3*time.Second))
	check(compareWindow.Driver().Key(gfx.KeyEscape, 0))
	check(compareWindow.Frame())
	check(session.Frame())
	if m.historyCompare != nil || !m.appSettings.open || !state.manage {
		t.Fatal("closing history comparison did not return to history management")
	}
	check(session.ClickLabel("checkbox:" + oldest.Local().Format(time.DateTime)))
	check(session.ClickLabel("button:清理所选 (1)"))
	check(session.ClickLabel("button:确认清理"))
	waitForCount(2)
	state.days.SetText("0")
	state.versions.SetText("2")
	check(session.Frame())
	check(session.ClickLabel("button:应用"))
	waitForCount(2)
	preferences, err := readApplicationPreferences(m.dataDirectory)
	check(err)
	if days, versions := preferences.LocalHistory.limits(); days != -1 || versions != 2 {
		t.Fatalf("retention did not persist: %d days, %d versions", days, versions)
	}
	if directory := os.Getenv("VIVA_PREVIEW_DIR"); directory != "" {
		check(session.Resize(900, 560))
		check(session.Screenshot(filepath.Join(directory, "storage-sense.png")))
		check(session.Resize(1440, 900))
	}
	check(session.ClickLabel("button:清空历史"))
	check(session.ClickLabel("button:确认清空"))
	waitForCount(0)
	check(session.ClickLabel("button:存储概览"))
	check(session.WaitFor(func(app.Snapshot) bool {
		return state.overview.ready && state.overview.usage.history == 0
	}, 3*time.Second))
	if state.overview.usage.data <= 2048 {
		t.Fatal("storage overview omitted the application preferences or fixture")
	}
}

func recordHistoryFixture(t *testing.T, store *localHistoryStore, path, contents string, at time.Time) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	draft, _, err := store.stage(context.Background(), path, at)
	if err != nil || draft == nil {
		t.Fatalf("stage history: %v", err)
	}
	defer os.Remove(draft.temporary)
	if err := store.commit(draft); err != nil {
		t.Fatal(err)
	}
}

func storedHistoryBytes(t *testing.T, store *localHistoryStore, path string) []byte {
	t.Helper()
	catalog, err := store.catalog()
	if err != nil || catalog.count != 1 || len(catalog.files) != 1 || catalog.files[0].source != path {
		t.Fatalf("one version expected for %s: %+v, %v", path, catalog, err)
	}
	var output bytes.Buffer
	if err := store.copyVersion(context.Background(), catalog.files[0].versions[0], &output); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
