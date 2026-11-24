package editor

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func TestOfflineComponentIntegrityAndCleanup(t *testing.T) {
	for _, test := range []struct {
		name, entry       string
		wrongHash, cancel bool
	}{
		{"valid", "worker.txt", false, false}, {"checksum", "worker.txt", true, false},
		{"traversal", "../outside.txt", false, false}, {"cancel", "worker.txt", false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var buffer bytes.Buffer
			archive := zip.NewWriter(&buffer)
			writer, _ := archive.Create(test.entry)
			_, _ = writer.Write([]byte("test worker"))
			if err := archive.Close(); err != nil {
				t.Fatal(err)
			}
			data := buffer.Bytes()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(data) }))
			defer server.Close()
			digest := sha256.Sum256(data)
			spec := offlineComponent{ID: "test-runtime", Required: []string{"worker.txt"}, Archives: []offlineArchive{{server.URL, hex.EncodeToString(digest[:]), int64(len(data))}}}
			if test.wrongHash {
				spec.Archives[0].SHA256 = strings.Repeat("0", 64)
			}
			directory := t.TempDir()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if test.cancel {
				cancel()
			}
			info, err := installOfflineComponent(ctx, server.Client(), directory, spec, &dictionaryProgress{})
			if test.name == "valid" {
				if err != nil || info.Bytes != 11 {
					t.Fatal("install failed", info, err)
				}
				server.Close()
				if _, err := installOfflineComponent(ctx, server.Client(), directory, spec, &dictionaryProgress{}); err != nil {
					t.Fatal("installed component required network", err)
				}
			} else {
				if err == nil {
					t.Fatal("invalid component installed")
				}
				if info, readErr := readOfflineInstallation(directory, spec); readErr != nil || info != nil {
					t.Fatal("partial installation published", readErr)
				}
				entries, _ := os.ReadDir(filepath.Join(directory, offlineTranslationDirectory))
				if len(entries) != 0 {
					t.Fatal("partial download remained", entries)
				}
			}
			if _, err := os.Stat(filepath.Join(directory, "outside.txt")); !os.IsNotExist(err) {
				t.Fatal("ZIP escaped its destination")
			}
		})
	}
}

func TestOfflineTranslationOptIn(t *testing.T) {
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	m.translationEnabled = true
	m.openTranslation(false)
	s := openTestSession(t, m)
	if m.translation.mode != translationLocalMode || m.translation.offline != nil {
		t.Fatal("offline engine initialized by default")
	}
	if err := s.ClickLabel("button:离线整句"); err != nil {
		t.Fatal(err)
	}
	state := m.translation
	if state.offline == nil || state.offline.download != nil || state.offline.request != nil {
		t.Fatal("selecting offline mode must not download or run")
	}
	if _, err := os.Stat(filepath.Join(m.dataDirectory, offlineTranslationDirectory)); !os.IsNotExist(err) {
		t.Fatal("choosing a mode created storage", err)
	}
	download, err := s.Locate("button:下载离线组件")
	if err != nil {
		t.Fatal(err)
	}
	ai, err := s.Locate("button:AI 翻译")
	if err != nil || download.Bounds.Y != ai.Bounds.Y || download.Bounds.X >= ai.Bounds.X {
		t.Fatal("AI should be beside the offline action", download.Bounds, ai.Bounds, err)
	}
	if _, err := s.Locate("button:开始离线翻译"); err == nil {
		t.Fatal("translation action exposed before installing components")
	}
	if _, err := translateOffline(context.Background(), m.dataDirectory, "en", "zh-cn", "Hello."); err == nil {
		t.Fatal("translation started without installed components")
	}
	state.query.SetText("Hello.")
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("button:AI 翻译"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Locate("menuitem:打开 AI 设置…"); err != nil || state.mode != translationOfflineMode {
		t.Fatal("AI setup should be available without changing the offline view", err)
	}
	if _, err := os.Stat(filepath.Join(m.dataDirectory, offlineTranslationDirectory)); !os.IsNotExist(err) {
		t.Fatal("using uninstalled offline mode created storage", err)
	}
}

func TestOfflineDownloadStopsWithPanel(t *testing.T) {
	for _, action := range []string{"取消组件下载", "关闭翻译"} {
		t.Run(action, func(t *testing.T) {
			var started, stopped atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("PK"))
				w.(http.Flusher).Flush()
				started.Store(true)
				<-r.Context().Done()
				stopped.Store(true)
			}))
			defer server.Close()
			previous := offlineRuntime
			defer server.CloseClientConnections()
			offlineRuntime = offlineComponent{ID: "test-runtime", Required: []string{"worker.txt"}, Archives: []offlineArchive{{server.URL, strings.Repeat("0", 64), 1024}}}
			defer func() { offlineRuntime = previous }()
			m := newEditorApp()
			m.dataDirectory = t.TempDir()
			m.translationEnabled = true
			m.openTranslation(false)
			s := openTestSession(t, m)
			for _, label := range []string{"离线整句", "下载离线组件"} {
				if err := s.ClickLabel("button:" + label); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.WaitFor(func(app.Snapshot) bool { return started.Load() }, time.Second); err != nil {
				t.Fatal(err)
			}
			if action == "取消组件下载" {
				task := m.translation.offline.download
				for _, label := range []string{"本地词库", "词库管理"} {
					if err := s.ClickLabel("button:" + label); err != nil {
						t.Fatal(err)
					}
				}
				if m.translation.offline.download != task || stopped.Load() {
					t.Fatal("moving to the library manager stopped the pending download")
				}
			}
			if err := s.ClickLabel("button:" + action); err != nil {
				t.Fatal(err)
			}
			if err := s.WaitFor(func(app.Snapshot) bool {
				files, _ := os.ReadDir(filepath.Join(m.dataDirectory, offlineTranslationDirectory))
				return stopped.Load() && len(files) == 0
			}, 2*time.Second); err != nil {
				t.Fatal("canceled component download retained resources", err)
			}
		})
	}
}

func TestOfflineTranslationNetworkInstall(t *testing.T) {
	if os.Getenv("EDITORX_OFFLINE_NETWORK") != "1" {
		archives := os.Getenv("EDITORX_OFFLINE_DOWNLOADS")
		if archives == "" {
			t.Skip("set EDITORX_OFFLINE_NETWORK=1 or provide the cached official archives")
		}
		previous := editorUpdateHTTP
		client := *previous
		client.Transport = offlineArchiveFiles(archives)
		editorUpdateHTTP = &client
		t.Cleanup(func() { editorUpdateHTTP = previous })
	}
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	m.translationEnabled = true
	m.openTranslation(false)
	s := openTestSession(t, m)
	size := offlineRuntime.downloadBytes() + offlineModels[0].downloadBytes()
	for _, label := range []string{"词库管理", fmt.Sprintf("下载 %.1f MiB", float64(size)/(1<<20))} {
		if err := s.ClickLabel("button:" + label); err != nil {
			t.Fatal(err)
		}
	}
	progress := m.translation.offline.download.progress
	started := time.Now()
	if err := s.WaitFor(func(app.Snapshot) bool { return m.translation.offline.download == nil }, 2*time.Minute); err != nil {
		t.Fatal(err)
	}
	state := m.translation
	runtime, model := state.offline.runtime.installed, state.offline.models[offlineModels[0].ID].installed
	if state.problem != "" || state.offline.problem != "" || runtime == nil || model == nil || state.offline.models[offlineModels[1].ID].installed != nil {
		t.Fatal("installation from the library manager failed", state.problem, state.offline.problem)
	}
	if progress.received.Load() != offlineRuntime.downloadBytes()+offlineModels[0].downloadBytes() {
		t.Fatal("network byte count differs from package sizes", progress.received.Load())
	}
	t.Logf("UI download=%d bytes; installed=%d bytes; download/install=%s", progress.received.Load(), runtime.Bytes+model.Bytes, time.Since(started))
	for _, label := range []string{"返回翻译", "离线整句"} {
		if err := s.ClickLabel("button:" + label); err != nil {
			t.Fatal(err)
		}
	}
	state.query.SetText("Please save your changes before closing this window.")
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("button:开始离线翻译"); err != nil {
		t.Fatal(err)
	}
	if err := s.WaitFor(func(app.Snapshot) bool { return state.offline.request == nil }, 15*time.Second); err != nil {
		t.Fatal(err)
	}
	if state.problem != "" || state.output.Text() == "" {
		t.Fatal("downloaded engine did not translate", state.problem)
	}
	t.Log(state.output.Text())
	for _, label := range []string{"词库管理", "移除离线组件…", "确认移除离线组件"} {
		if err := s.ClickLabel("button:" + label); err != nil {
			t.Fatal(err)
		}
	}
	files, err := os.ReadDir(filepath.Join(m.dataDirectory, offlineTranslationDirectory))
	if err != nil || len(files) != 0 {
		t.Fatal("offline uninstall did not release disk space", files, err)
	}
	if err := s.ClickLabel("button:返回翻译"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Locate("button:下载离线组件"); err != nil {
		t.Fatal("removal did not restore the inline download action", err)
	}
}

// Downloading and running the real models is explicitly opt-in. This transports
// the previously measured official downloads through the production installer.
type offlineArchiveFiles string

func (directory offlineArchiveFiles) RoundTrip(request *http.Request) (*http.Response, error) {
	if err := request.Context().Err(); err != nil {
		return nil, err
	}
	file, err := os.Open(filepath.Join(string(directory), filepath.Base(request.URL.Path)))
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	return &http.Response{StatusCode: 200, Body: file, ContentLength: info.Size(), Header: make(http.Header), Request: request}, nil
}

func TestOfflineTranslationRealModels(t *testing.T) {
	downloads, directory := os.Getenv("EDITORX_OFFLINE_DOWNLOADS"), os.Getenv("EDITORX_OFFLINE_DATA")
	if downloads == "" || directory == "" {
		t.Skip("real model verification is opt-in")
	}
	client := &http.Client{Transport: offlineArchiveFiles(downloads)}
	for _, component := range append([]offlineComponent{offlineRuntime}, offlineModels[:]...) {
		info, err := installOfflineComponent(context.Background(), client, directory, component, &dictionaryProgress{})
		if err != nil {
			t.Fatal(component.ID, err)
		}
		t.Logf("%s download=%d installed=%d", component.ID, component.downloadBytes(), info.Bytes)
	}
	type measurement struct {
		Case, Source, Target, Input string
		Run                         int
		Result                      offlineTranslationResult
	}
	var measurements []measurement
	for _, sample := range []struct{ name, source, target, input string }{
		{"english-short", "en", "zh-cn", "Please save your changes before closing this window."},
		{"chinese-short", "zh-cn", "en", "请在关闭窗口前保存更改。"},
		{"english-paragraph", "en", "zh-cn", "The application could not connect to the server. Please check your network settings and try again. Your unsaved changes will remain available when you reopen the editor."},
		{"chinese-paragraph", "zh-cn", "en", "应用程序无法连接到服务器。请检查网络设置后重试。重新打开编辑器时，未保存的更改仍然可用。"},
		{"placeholders", "en", "zh-cn", "Saved {count} files to ${folder}.\nUse `Ctrl+S` to save."},
	} {
		for run := 1; run <= 3; run++ {
			result, err := translateOffline(context.Background(), directory, sample.source, sample.target, sample.input)
			if err != nil {
				t.Fatal(sample.name, err)
			}
			if result.Text == sample.input || result.PeakWorkingSet == 0 || strings.Contains(result.Text, "▁") {
				t.Fatal("model did not translate or report memory", result)
			}
			if err := validateTranslationMarkers(sample.input, result.Text); err != nil {
				t.Fatal(err)
			}
			if strings.Count(sample.input, "\n") != strings.Count(result.Text, "\n") {
				t.Fatal("newlines changed")
			}
			measurements = append(measurements, measurement{sample.name, sample.source, sample.target, sample.input, run, result})
			t.Logf("%s #%d %.0fms peak=%.1fMiB: %s", sample.name, run, result.ElapsedMS, float64(result.PeakWorkingSet)/(1<<20), result.Text)
		}
	}
	encoded, err := json.MarshalIndent(measurements, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "offline-measurements.json"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := translateOffline(ctx, directory, "en", "zh-cn", strings.Repeat("Please save your changes. ", 200)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("inference did not cancel", err)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("worker did not exit promptly after cancellation")
	}
	// Exercise the real UI, selection protection and undo with the installed model.
	m := newEditorApp()
	m.dataDirectory = directory
	m.translationEnabled = true
	tab := m.addTab(document{saved: "Please save your changes.", eol: "LF"}, "offline.txt")
	tab.source.text.SetSelection(text.Range{Start: 0, End: tab.source.text.RuneCount()})
	m.openTranslation(true)
	s := openTestSession(t, m)
	if err := s.Resize(900, 560); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("button:离线整句"); err != nil {
		t.Fatal(err)
	}
	offline, err := s.Locate("button:开始离线翻译")
	if err != nil {
		t.Fatal(err)
	}
	ai, err := s.Locate("button:AI 翻译")
	if err != nil || ai.Bounds.Y != offline.Bounds.Y || offline.Bounds.X >= ai.Bounds.X {
		t.Fatal("AI and offline translation should share one action row", offline.Bounds, ai.Bounds, err)
	}
	if err := s.ClickLabel("button:开始离线翻译"); err != nil {
		t.Fatal(err)
	}
	if err := s.WaitFor(func(app.Snapshot) bool { return m.translation.offline.request == nil }, 15*time.Second); err != nil {
		t.Fatal(err)
	}
	if m.translation.problem != "" || m.translation.output.Text() == "" || !m.translation.hasOutput {
		t.Fatal("offline UI failed", m.translation.problem)
	}
	if _, err := s.Locate("button:离线组件管理"); err == nil {
		t.Fatal("the obsolete inline settings button is still present")
	}
	for _, item := range s.Snapshot().Texts {
		for _, diagnostic := range []string{"占用", "引擎峰值", "释放内存", "内存已释放", "Argos"} {
			if strings.Contains(item.Value, diagnostic) {
				t.Fatalf("offline diagnostics still shown after translation: %q", item.Value)
			}
		}
	}
	if preview := os.Getenv("VIVA_PREVIEW_DIR"); preview != "" {
		if err := s.Screenshot(filepath.Join(preview, "offline-result.png")); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ClickLabel("button:替换选区"); err != nil {
		t.Fatal(err)
	}
	if tab.source.text.String() == tab.doc.saved {
		t.Fatal("translation not applied")
	}
	if _, err := s.Driver().ShortcutChord(gfx.ModifierControl, 'z'); err != nil {
		t.Fatal(err)
	}
	if tab.source.text.String() != tab.doc.saved {
		t.Fatal("offline replacement not undoable")
	}
}
