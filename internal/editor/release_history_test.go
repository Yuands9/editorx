package editor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Yuands9/editorx/internal/version"
	"github.com/vivaui/viva/app"
)

func TestUpdateSettingsAndDiagnosticsControls(t *testing.T) {
	var moreRequests atomic.Int32
	originalHTTP := editorUpdateHTTP
	editorUpdateHTTP = &http.Client{Transport: updateTestTransport(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/repos/Yuands9/editorx/releases/latest" {
			body := fmt.Sprintf(`{"tag_name":"v%s"}`, version.Number)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
		}
		if request.URL.Path != "/repos/Yuands9/editorx/releases" {
			return nil, fmt.Errorf("unexpected request: %v", request.URL)
		}
		if request.URL.Query().Get("per_page") != "5" {
			return nil, fmt.Errorf("unexpected page size: %v", request.URL)
		}
		if request.URL.Query().Get("page") == "2" {
			moreRequests.Add(1)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(
				`[{"tag_name":"v0.39.0","body":"上一个版本。"}]`,
			))}, nil
		}
		header := http.Header{"Link": {`<https://api.github.com/repos/Yuands9/editorx/releases?per_page=5&page=2>; rel="next"`}}
		return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader(
			`[
				{"tag_name":"v0.42.0","published_at":"2026-11-28T03:00:00Z","body":"### 改进\n- 调整更新设置。"},
				{"tag_name":"v0.41.0","published_at":"2026-11-27T03:00:00Z","body":"### 问题修复\n- 已修复保存提示\n- 保留外部修改和本地草稿。\n\n### 改进\n- 更新日志按版本展开。"},
				{"tag_name":"v0.40.0","published_at":"2026-11-26T03:00:00Z","body":"优化项目文件定位。"}
			]`,
		))}, nil
	})}
	t.Cleanup(func() { editorUpdateHTTP = originalHTTP })
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(s.Resize(1200, 900))
	m.showSettings(settingsUpdate)
	check(s.Frame())
	check(s.ClickLabel("combobox:更新下载源"))
	check(s.ClickLabel("option:CNB"))
	preferences, err := readApplicationPreferences(m.dataDirectory)
	check(err)
	if preferences.updateRepository() != "https://cnb.cool/yuands9/editorx" || !s.HasText(officialCNBRepository) {
		t.Fatal("selecting CNB did not activate the public EditorX mirror")
	}
	check(s.ClickLabel("更改镜像地址"))
	check(s.ClickLabel("textfield:CNB 镜像仓库"))
	check(s.Type("https://cnb.cool/group/repository"))
	check(s.ClickLabel("保存下载源"))
	preferences, err = readApplicationPreferences(m.dataDirectory)
	check(err)
	if preferences.CNBRepository != "https://cnb.cool/group/repository" {
		t.Fatal("the configured mirror was not saved")
	}
	check(s.ClickLabel("combobox:更新下载源"))
	check(s.ClickLabel("option:GitHub（默认）"))
	preferences, err = readApplicationPreferences(m.dataDirectory)
	check(err)
	if preferences.updateRepository() != "" || preferences.CNBRepository != "https://cnb.cool/group/repository" {
		t.Fatal("switching to GitHub did not retain the custom mirror for later")
	}
	check(s.ClickLabel("combobox:更新下载源"))
	check(s.ClickLabel("option:CNB"))
	check(s.WaitFor(func(app.Snapshot) bool { return len(m.appSettings.releases.entries) == 3 }, 3*time.Second))
	if moreRequests.Load() != 0 {
		t.Fatal("loaded another page before the user asked")
	}
	check(s.ClickLabel("button:v0.41.0"))
	if err := s.WaitFor(func(app.Snapshot) bool {
		return s.HasText("已修复保存提示") && s.HasText("更新日志按版本展开。")
	}, time.Second); err != nil {
		if folder := os.Getenv("VIVA_PREVIEW_DIR"); folder != "" {
			check(s.Screenshot(filepath.Join(folder, "update-settings.png")))
		}
		t.Fatalf("release notes did not finish expanding (height %.0f): %v", m.appSettings.releases.notes.Height(), err)
	}
	check(s.ClickLabel("button:GitHub Release"))
	if url, ok := s.Driver().LastOpenedURL(); !ok || url != editorReleasesPage+"/tag/v0.41.0" {
		t.Fatalf("history link opened %q", url)
	}
	check(s.ClickLabel("button:GitHub Releases"))
	if url, ok := s.Driver().LastOpenedURL(); !ok || url != editorReleasesPage {
		t.Fatalf("release index link opened %q", url)
	}
	check(s.ClickLabel("button:检查更新"))
	check(s.WaitFor(func(app.Snapshot) bool {
		return m.update.open && m.editorUpdates().check == nil && m.editorUpdates().cache.Release.Version == version.Number
	}, 3*time.Second))
	check(s.ClickLabel("button:关闭"))
	if folder := os.Getenv("VIVA_PREVIEW_DIR"); folder != "" {
		check(s.Screenshot(filepath.Join(folder, "update-settings.png")))
		m.setLightMode(true)
		check(s.Resize(900, 820))
		check(s.Screenshot(filepath.Join(folder, "update-settings-light.png")))
		m.setLightMode(false)
		check(s.Resize(1200, 900))
	}
	check(s.ClickLabel("加载更多版本"))
	check(s.WaitFor(func(app.Snapshot) bool { return len(m.appSettings.releases.entries) == 4 }, 3*time.Second))
	if moreRequests.Load() != 1 || m.appSettings.releases.nextPage != -1 {
		t.Fatal("loading more did not fetch exactly one page")
	}
	check(s.ClickLabel("关于"))
	check(s.ClickLabel("button:打开 GitHub"))
	if url, ok := s.Driver().LastOpenedURL(); !ok || url != editorRepositoryURL {
		t.Fatalf("project link opened %q", url)
	}
	check(s.ClickLabel("switch:启用调试日志"))
	check(s.ClickLabel("复制日志"))
	if clipboard := s.Driver().Clipboard(); !strings.Contains(clipboard, "diagnostics.enabled") {
		t.Fatalf("debug log was not copied: %q", clipboard)
	}
	path := filepath.Join(m.dataDirectory, "debug.log")
	check(s.Driver().QueueSaveFile(path, nil))
	check(s.ClickLabel("下载日志"))
	data, err := os.ReadFile(path)
	check(err)
	if !bytes.Contains(data, []byte("diagnostics.enabled")) {
		t.Fatal("debug log was not exported")
	}
	if folder := os.Getenv("VIVA_PREVIEW_DIR"); folder != "" {
		check(s.Screenshot(filepath.Join(folder, "debug-settings.png")))
		m.setLightMode(true)
		check(s.Resize(900, 820))
		check(s.Screenshot(filepath.Join(folder, "debug-settings-light.png")))
	}
	check(s.ClickLabel("清空日志"))
	if !m.debug.enabled || len(m.debug.data) != 0 {
		t.Fatal("clear did not empty the active log")
	}
}

func TestEditorReleaseHistoryPagination(t *testing.T) {
	client := &http.Client{Transport: updateTestTransport(func(request *http.Request) (*http.Response, error) {
		if request.URL.Query().Get("per_page") != "5" || request.Header.Get("Accept") != "application/vnd.github+json" {
			return nil, fmt.Errorf("wrong history request: %v", request.URL)
		}
		response := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}
		switch request.URL.Query().Get("page") {
		case "1":
			response.Header.Add("Link", "<https://api.github.com/repos/Yuands9/editorx/releases?page=2>; rel=\"next\"")
			response.Body = io.NopCloser(strings.NewReader(`[
				{"tag_name":"v0.41.0","body":"## 本次更新\n修复保存\n## SHA-256\n摘要","published_at":"2026-11-27T03:00:00Z"},
				{"tag_name":"v0.42.0","draft":true}
			]`))
		case "2":
			response.Body = io.NopCloser(strings.NewReader(`[{"tag_name":"v0.40.0","body":"旧版本"}]`))
		default:
			return nil, fmt.Errorf("unexpected page")
		}
		return response, nil
	})}
	first, err := fetchEditorReleaseHistory(context.Background(), client, editorReleasesAPI, 1)
	if err != nil || !first.more || len(first.versions) != 1 {
		t.Fatalf("unexpected first page: %+v, %v", first, err)
	}
	release := first.versions[0]
	if release.tag != "v0.41.0" || release.page != editorReleasesPage+"/tag/v0.41.0" ||
		release.notes != "修复保存" || release.published.IsZero() {
		t.Fatalf("incomplete version entry: %+v", release)
	}
	second, err := fetchEditorReleaseHistory(context.Background(), client, editorReleasesAPI, 2)
	if err != nil || second.more || len(second.versions) != 1 || second.versions[0].tag != "v0.40.0" {
		t.Fatalf("unexpected second page: %+v, %v", second, err)
	}
}

func TestEditorCNBMirrorVerificationAndFallback(t *testing.T) {
	repository, err := normalizeCNBRepository(" https://cnb.cool/group/sub/repository.git/ ")
	if err != nil || repository != "https://cnb.cool/group/sub/repository" {
		t.Fatalf("CNB URL = %q, %v", repository, err)
	}
	for _, address := range []string{"http://cnb.cool/group/repo", "https://cnb.cool.example/group/repo", "https://other@cnb.cool/group/repo", "https://cnb.cool/group/../repo", "https://cnb.cool/group/repo?id=1"} {
		if _, err := normalizeCNBRepository(address); err == nil {
			t.Fatalf("accepted CNB URL %s", address)
		}
	}
	binary, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	release := testEditorRelease(t, 1)
	release.Size, release.Checksum = int64(len(binary)), sha256.Sum256(binary)
	wantMirrorURL := repository + "/-/releases/download/v" + release.Version + "/EditorX.exe"
	for _, corrupted := range []bool{false, true} {
		t.Run(fmt.Sprintf("corrupted=%t", corrupted), func(t *testing.T) {
			var calls []string
			clientFor := func(expected string, payload []byte) *http.Client {
				return &http.Client{Transport: updateTestTransport(func(request *http.Request) (*http.Response, error) {
					calls = append(calls, request.URL.String())
					if request.URL.String() != expected {
						return nil, fmt.Errorf("unexpected URL: %v", request.URL)
					}
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(payload))}, nil
				})}
			}
			payload := binary
			if corrupted {
				payload = []byte("invalid mirror file")
			}
			path := filepath.Join(t.TempDir(), "staged.exe")
			if err := os.WriteFile(path, nil, 0600); err != nil {
				t.Fatal(err)
			}
			progress := &dictionaryProgress{invalidate: func() {}}
			err := downloadPreferredEditorUpdate(context.Background(), clientFor(release.Download, binary),
				clientFor(wantMirrorURL, payload), repository, release, path, progress)
			if err != nil {
				t.Fatal(err)
			}
			if len(calls) != 1 && !corrupted || len(calls) != 2 && corrupted || calls[0] != wantMirrorURL {
				t.Fatalf("unexpected source order: %v", calls)
			}
			if data, err := os.ReadFile(path); err != nil || sha256.Sum256(data) != release.Checksum ||
				progress.received.Load() != release.Size {
				t.Fatalf("staged update did not pass the intended verification: %v", err)
			}
		})
	}
}
