package editor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Yuands9/editorx/internal/version"
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

type updateTestTransport func(*http.Request) (*http.Response, error)

func (transport updateTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func testEditorRelease(t *testing.T, patchOffset int) editorRelease {
	t.Helper()
	current, err := editorVersionParts(version.Number)
	if err != nil {
		t.Fatal(err)
	}
	offered := fmt.Sprintf("%d.%d.%d", current[0], current[1], current[2]+patchOffset)
	return editorRelease{
		Version: offered, Page: editorRepositoryURL + "/releases/tag/v" + offered,
		Download: editorRepositoryURL + "/releases/download/v" + offered + "/EditorX.exe", Size: 38615552,
		Checksum: sha256.Sum256([]byte("fixture")), comparison: 1,
		Notes: "## 新功能\n\n- 支持中英双向离线整句翻译，组件按需下载。\n- 新版本在首页显示提示，查看说明后再下载。\n\n## 改进\n\n- 模板库和翻译面板跟随侧栏位置。\n- 下载时仍可继续编辑。\n\n## 修复\n\n- 修复首次打开时的白屏闪烁。\n- 修复未打开文件时侧栏按钮没有显示。",
	}
}

func TestUpdateConditionalRequestAndPersistentCache(t *testing.T) {
	release := testEditorRelease(t, 1)
	record := githubEditorRelease{Tag: "v" + release.Version, Body: release.Notes, Assets: []githubReleaseAsset{{
		Name: updateExecutable, Digest: "sha256:" + hex.EncodeToString(release.Checksum[:]), State: "uploaded", Size: release.Size, URL: release.Download,
	}}}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("If-None-Match") == `"stable"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"stable"`)
		_ = json.NewEncoder(w).Encode(record)
	}))
	defer server.Close()
	directory := t.TempDir()
	state := editorUpdateState{}
	var err error
	state.cache.Release, err = fetchEditorRelease(context.Background(), server.Client(), server.URL, version.Number, &state.cache)
	if err != nil || state.cache.ETag != `"stable"` || state.cache.Release.Version != release.Version {
		t.Fatal("fresh release missing", state.cache, err)
	}
	state.cache.CheckedAt, state.cache.SkippedVersion = time.Now(), release.Version
	if err := state.save(directory); err != nil {
		t.Fatal(err)
	}
	cache, err := readEditorUpdateCache(directory, version.Number)
	if err != nil || cache.Release.comparison != 1 || cache.SkippedVersion != release.Version {
		t.Fatal(cache, err)
	}
	cached, err := fetchEditorRelease(context.Background(), server.Client(), server.URL, version.Number, &cache)
	if err != nil || cached != release || requests.Load() != 2 {
		t.Fatal("conditional request lost cached notes", cached, err)
	}
	installed, err := readEditorUpdateCache(directory, release.Version)
	if err != nil || installed.Release.comparison != 0 {
		t.Fatal("installed version still offered", installed, err)
	}
	newState := editorUpdateState{}
	newState.initialize(directory, time.Now())
	if newState.available() || !newState.nextCheck.Equal(cache.CheckedAt.Add(editorUpdateInterval)) {
		t.Fatal("restart ignored skip or last check", newState.nextCheck)
	}
	state.cache.Release.Download = "https://example.com/replacement.exe"
	if err := state.save(directory); err != nil {
		t.Fatal(err)
	}
	if _, err := readEditorUpdateCache(directory, version.Number); err == nil {
		t.Fatal("untrusted cached download accepted")
	}
}

func TestUpdateAutomaticChecksShareWorkAndRespectOptOut(t *testing.T) {
	store, err := openSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	preferences, err := readApplicationPreferences(store.directory)
	if err != nil || preferences.DisableAutoUpdate {
		t.Fatal("automatic checks should default on", err)
	}
	desktop := &editorDesktop{store: store, preferences: preferences}
	first, second := newEditorApp(), newEditorApp()
	for _, window := range []*editorApp{first, second} {
		store.attach(window)
		window.desktop = desktop
	}
	session := openTestSession(t, second)
	started, finish, canceled := make(chan struct{}, 4), make(chan struct{}, 4), make(chan struct{}, 4)
	var requests atomic.Int32
	previous := editorUpdateHTTP
	editorUpdateHTTP = &http.Client{Transport: updateTestTransport(func(request *http.Request) (*http.Response, error) {
		requests.Add(1)
		started <- struct{}{}
		select {
		case <-finish:
		case <-request.Context().Done():
			canceled <- struct{}{}
			return nil, request.Context().Err()
		}
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})}
	defer func() { desktop.update.close(); editorUpdateHTTP = previous }()
	now := time.Now()
	desktop.update = editorUpdateState{}
	desktop.update.initialize(store.directory, now)
	desktop.checkEditorUpdates(now.Add(9 * time.Second))
	if desktop.update.check != nil {
		t.Fatal("startup check was not delayed")
	}
	desktop.checkEditorUpdates(now.Add(10 * time.Second))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("automatic check missing")
	}
	desktop.checkEditorUpdates(now.Add(11 * time.Second))
	if requests.Load() != 1 || first.update.open || second.update.open {
		t.Fatal("duplicate check or unsolicited dialog")
	}
	first.releaseWindowState()
	finish <- struct{}{}
	if err := session.WaitFor(func(app.Snapshot) bool { second.updateProgress(); return desktop.update.check == nil }, time.Second); err != nil {
		t.Fatal(err)
	}
	if desktop.update.problem != "" || desktop.update.cache.CheckedAt.IsZero() || desktop.update.cache.CheckedAt.Before(now) {
		t.Fatal("automatic failure was not quiet")
	}
	desktop.checkEditorUpdates(now.Add(time.Hour))
	if desktop.update.check != nil {
		t.Fatal("checking more than once per interval")
	}

	second.showSettings(settingsAbout)
	if err := session.Frame(); err != nil {
		t.Fatal(err)
	}
	if err := session.ClickLabel("switch:自动检查更新"); err != nil {
		t.Fatal(err)
	}
	saved, err := readApplicationPreferences(store.directory)
	if err != nil || !saved.DisableAutoUpdate || !desktop.preferences.DisableAutoUpdate {
		t.Fatal("opt-out not persisted", err)
	}
	desktop.checkEditorUpdates(now.Add(24 * time.Hour))
	if desktop.update.check != nil {
		t.Fatal("disabled checker started")
	}
	second.checkEditorUpdate()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("manual check disabled with auto check")
	}
	finish <- struct{}{}
	if err := session.WaitFor(func(app.Snapshot) bool { second.updateProgress(); return desktop.update.check == nil }, time.Second); err != nil {
		t.Fatal(err)
	}
	if !second.update.open || !strings.Contains(desktop.update.problem, "503") {
		t.Fatal("manual check did not report failure")
	}
	second.update.open = false
	if err := session.Frame(); err != nil {
		t.Fatal(err)
	}
	if err := session.ClickLabel("switch:自动检查更新"); err != nil {
		t.Fatal(err)
	}
	desktop.checkEditorUpdates(now.Add(24 * time.Hour))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("re-enabling checks had no effect")
	}
	if err := session.ClickLabel("switch:自动检查更新"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("opt-out did not cancel the running automatic check")
	}
	if desktop.update.check != nil {
		t.Fatal("canceled automatic check remains active")
	}
}

func TestUpdateHomeIndicatorSkipAndManualCheck(t *testing.T) {
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	offered := testEditorRelease(t, 1)
	indicator := "button:发现新版本 " + offered.Version
	dialogTitle := "EditorX " + offered.Version + " 可更新"
	m.update = editorUpdateState{initialized: true, cache: editorUpdateCache{Release: offered}}
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	click := func(label string) { t.Helper(); check(s.ClickLabel(label)) }
	check(s.Resize(900, 560))
	button, err := s.Locate(indicator)
	check(err)
	if len(m.tabs) != 0 || button.Bounds.X < 850 {
		t.Fatal("update indicator missing on empty home")
	}
	if directory := os.Getenv("VIVA_PREVIEW_DIR"); directory != "" {
		check(s.Screenshot(filepath.Join(directory, "update-home.png")))
	}
	click(indicator)
	if !m.update.open || m.appSettings.open || !s.HasText(dialogTitle) {
		t.Fatal("update details require settings")
	}
	check(s.Frame())
	if directory := os.Getenv("VIVA_PREVIEW_DIR"); directory != "" {
		check(s.Screenshot(filepath.Join(directory, "update-notes.png")))
	}
	check(s.Driver().Scroll(gfx.ScrollEvent{X: 450, Y: 300, DeltaY: 500}))
	check(s.Frame())
	if directory := os.Getenv("VIVA_PREVIEW_DIR"); directory != "" {
		check(s.Screenshot(filepath.Join(directory, "update-notes-scrolled.png")))
	}
	click("稍后")
	if _, err := s.Locate(indicator); err != nil {
		t.Fatal("Later removed the update indicator", err)
	}
	m.sidebarPrefs.Position = "left"
	check(s.Frame())
	button, err = s.Locate(indicator)
	check(err)
	if button.Bounds.X > 44 {
		t.Fatal("update indicator did not follow sidebar")
	}
	click(indicator)
	click("跳过此版本")
	if m.update.open || m.update.available() {
		t.Fatal("skip did not dismiss this version")
	}
	saved, err := readEditorUpdateCache(m.dataDirectory, version.Number)
	if err != nil || saved.SkippedVersion != offered.Version {
		t.Fatal("skip not persistent", err)
	}
	previous := editorUpdateHTTP
	editorUpdateHTTP = &http.Client{Transport: updateTestTransport(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("offline")
	})}
	defer func() { m.update.close(); editorUpdateHTTP = previous }()
	m.checkEditorUpdate()
	check(s.WaitFor(func(app.Snapshot) bool { m.updateProgress(); return m.update.check == nil }, time.Second))
	check(s.Frame())
	if !m.update.open || !s.HasText(dialogTitle) {
		t.Fatal("manual check cannot open skipped cached release")
	}
	m.update.open = false
	next := testEditorRelease(t, 2)
	m.update.cache.Release = next
	check(s.Frame())
	if _, err := s.Locate("button:发现新版本 " + next.Version); err != nil {
		t.Fatal("skipping one version hid the next", err)
	}
}

func TestUpdateDownloadContinuesAcrossWindowsAndCleansCancellation(t *testing.T) {
	if hasWordEditor {
		t.Skip("local Word build does not replace itself with the public binary")
	}
	binary, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	allow := make(chan struct{}, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(binary[:512<<10])
		w.(http.Flusher).Flush()
		select {
		case <-allow:
			_, _ = w.Write(binary[512<<10:])
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	previous := editorUpdateHTTP
	editorUpdateHTTP = server.Client()
	defer func() { editorUpdateHTTP = previous }()
	store, err := openSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	desktop := &editorDesktop{store: store, update: editorUpdateState{initialized: true, cache: editorUpdateCache{Release: testEditorRelease(t, 1)}}}
	release := &desktop.update.cache.Release
	release.Download, release.Size, release.Checksum = server.URL, int64(len(binary)), sha256.Sum256(binary)
	desktop.update.cache.SkippedVersion = release.Version
	first, second := newEditorApp(), newEditorApp()
	for _, window := range []*editorApp{first, second} {
		store.attach(window)
		window.desktop = desktop
	}
	s := openTestSession(t, second)
	defer desktop.update.close()
	first.downloadEditorRelease()
	if !desktop.update.available() {
		t.Fatal("manual download of a skipped version has no progress entry")
	}
	path, job := desktop.update.staged, desktop.update.download
	second.downloadEditorRelease()
	if desktop.update.download != job {
		t.Fatal("second window started a duplicate download")
	}
	if err := s.WaitFor(func(app.Snapshot) bool { return desktop.update.progress.received.Load() >= 512<<10 }, time.Second); err != nil {
		t.Fatal(err)
	}
	first.releaseWindowState()
	if desktop.update.download != job {
		t.Fatal("closing one window stopped the download")
	}
	allow <- struct{}{}
	if err := s.WaitFor(func(app.Snapshot) bool { second.updateProgress(); return desktop.update.download == nil }, 4*time.Second); err != nil {
		t.Fatal(err)
	}
	if desktop.update.problem != "" || desktop.update.staged != path {
		t.Fatal("download not ready", desktop.update.problem)
	}
	data, err := os.ReadFile(path)
	if err != nil || sha256.Sum256(data) != release.Checksum {
		t.Fatal("staged file not verified", err)
	}
	second.update.open = true
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Locate("button:安装并重启"); err != nil {
		t.Fatal(err)
	}
	if directory := os.Getenv("VIVA_PREVIEW_DIR"); directory != "" {
		if err := s.Screenshot(filepath.Join(directory, "update-ready.png")); err != nil {
			t.Fatal(err)
		}
	}
	second.cancelEditorDownload()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("discarding ready update left staged file", err)
	}
	second.downloadEditorRelease()
	path = desktop.update.staged
	if err := s.WaitFor(func(app.Snapshot) bool { return desktop.update.progress.received.Load() >= 512<<10 }, time.Second); err != nil {
		t.Fatal(err)
	}
	second.cancelEditorDownload()
	if err := s.WaitFor(func(app.Snapshot) bool { _, err := os.Stat(path); return os.IsNotExist(err) }, 2*time.Second); err != nil {
		t.Fatal("canceled download left a file", err)
	}
}
