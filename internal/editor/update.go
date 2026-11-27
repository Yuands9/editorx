package editor

import (
	"context"
	"crypto/sha256"
	"debug/pe"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/vivaui/viva/app/proxy"
)

const (
	editorRepositoryURL = "https://github.com/Yuands9/editorx"
	latestReleaseAPI    = "https://api.github.com/repos/Yuands9/editorx/releases/latest"
	latestReleasePage   = editorRepositoryURL + "/releases/latest"
	updateExecutable    = "EditorX.exe"
	maxUpdateBytes      = 200 << 20
)

type githubReleaseAsset struct {
	Name, Digest, State string
	Size                int64
	URL                 string `json:"browser_download_url"`
}

type githubEditorRelease struct {
	Tag        string    `json:"tag_name"`
	Published  time.Time `json:"published_at"`
	Body       string
	Draft      bool
	Prerelease bool
	Assets     []githubReleaseAsset
}

type editorRelease struct {
	Version    string            `json:"version"`
	Page       string            `json:"page"`
	Notes      string            `json:"notes"`
	Download   string            `json:"download,omitempty"`
	Size       int64             `json:"size,omitempty"`
	Checksum   [sha256.Size]byte `json:"checksum"`
	PageOnly   bool              `json:"pageOnly,omitempty"`
	comparison int
}

var editorUpdateHTTP = func() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = func(request *http.Request) (*url.URL, error) {
		return proxy.SystemProxy(request.URL)
	}
	return &http.Client{
		Transport: transport,
		Timeout:   3 * time.Minute,
		CheckRedirect: func(request *http.Request, previous []*http.Request) error {
			if len(previous) >= 6 || request.URL.Scheme != "https" {
				return errors.New("更新链接跳转无效")
			}
			switch request.URL.Host {
			case "github.com", "api.github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com", "github-releases.githubusercontent.com":
				return nil
			default:
				return errors.New("更新链接跳转到了未知地址")
			}
		},
	}
}()

func editorVersionParts(value string) ([3]int, error) {
	var result [3]int
	parts := strings.Split(strings.TrimPrefix(value, "v"), ".")
	if len(parts) != len(result) {
		return result, fmt.Errorf(uiText("版本号无效 %q"), value)
	}
	for index, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 || strconv.Itoa(number) != part {
			return result, fmt.Errorf(uiText("版本号无效 %q"), value)
		}
		result[index] = number
	}
	return result, nil
}

func editorReleaseForTag(tag, current string) (editorRelease, error) {
	latest, err := editorVersionParts(tag)
	if err != nil {
		return editorRelease{}, err
	}
	installed, err := editorVersionParts(current)
	if err != nil {
		return editorRelease{}, err
	}
	return editorRelease{Version: strings.TrimPrefix(tag, "v"), Page: editorRepositoryURL + "/releases/tag/" + tag,
		comparison: slices.Compare(latest[:], installed[:])}, nil
}

type githubUpdateResponseError struct {
	status     int
	retryAfter time.Time
}

func (err *githubUpdateResponseError) Error() string {
	return fmt.Sprintf(uiText("GitHub 返回 HTTP %d"), err.status)
}

func githubUpdateRetry(header http.Header, now time.Time) time.Time {
	var deadline time.Time
	if seconds, err := strconv.Atoi(header.Get("Retry-After")); err == nil && seconds >= 0 && seconds <= 86400 {
		deadline = now.Add(time.Duration(seconds) * time.Second)
	} else if date, err := http.ParseTime(header.Get("Retry-After")); err == nil {
		deadline = date
	}
	if header.Get("X-RateLimit-Remaining") == "0" {
		if epoch, err := strconv.ParseInt(header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			if reset := time.Unix(epoch, 0); reset.After(deadline) {
				deadline = reset
			}
		}
	}
	if !deadline.After(now) {
		return now.Add(time.Minute)
	}
	if maxRetry := now.Add(24 * time.Hour); deadline.After(maxRetry) {
		return maxRetry
	}
	return deadline
}

func checkEditorRelease(ctx context.Context, client *http.Client, api, page, current string, cache *editorUpdateCache) (editorRelease, error) {
	now := time.Now()
	var apiError *githubUpdateResponseError
	if !now.Before(cache.APIRetryAfter) {
		release, err := fetchEditorRelease(ctx, client, api, current, cache)
		if err == nil {
			cache.APIRetryAfter = time.Time{}
			return release, nil
		}
		if !errors.As(err, &apiError) || apiError.status != http.StatusForbidden && apiError.status != http.StatusTooManyRequests {
			return editorRelease{}, err
		}
		cache.APIRetryAfter = apiError.retryAfter
	}
	release, err := fetchEditorReleasePage(ctx, client, page, current)
	if err != nil {
		if apiError != nil {
			return editorRelease{}, fmt.Errorf(uiText("%v，公开发布页也无法访问：%w"), apiError, err)
		}
		return editorRelease{}, err
	}
	if release.Version == cache.Release.Version && !cache.Release.PageOnly {
		return cache.Release, nil
	}
	cache.ETag = ""
	return release, nil
}

func fetchEditorReleasePage(ctx context.Context, client *http.Client, endpoint, current string) (editorRelease, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return editorRelease{}, err
	}
	request.Header.Set("User-Agent", applicationName+"/"+current)
	redirectClient := *client
	redirectClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := redirectClient.Do(request)
	if err != nil {
		return editorRelease{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusFound && response.StatusCode != http.StatusMovedPermanently && response.StatusCode != http.StatusTemporaryRedirect && response.StatusCode != http.StatusPermanentRedirect {
		return editorRelease{}, fmt.Errorf(uiText("公开发布页返回 HTTP %d"), response.StatusCode)
	}
	location, err := response.Location()
	if err != nil || location.Scheme != request.URL.Scheme || !strings.EqualFold(location.Host, request.URL.Host) || location.User != nil {
		return editorRelease{}, errors.New(uiText("公开发布页跳转无效"))
	}
	prefix := strings.TrimSuffix(request.URL.Path, "/latest") + "/tag/"
	tag, ok := strings.CutPrefix(location.Path, prefix)
	if !ok {
		return editorRelease{}, errors.New(uiText("公开发布页没有返回稳定版标签"))
	}
	release, err := editorReleaseForTag(tag, current)
	release.PageOnly = true
	return release, err
}

func fetchEditorRelease(ctx context.Context, client *http.Client, endpoint, current string, cache *editorUpdateCache) (editorRelease, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return editorRelease{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	request.Header.Set("User-Agent", applicationName+"/"+current)
	if cache.Release.Version != "" && cache.ETag != "" {
		request.Header.Set("If-None-Match", cache.ETag)
	}
	response, err := client.Do(request)
	if err != nil {
		return editorRelease{}, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotModified && cache.Release.Version != "" {
		return cache.Release, nil
	}
	if response.StatusCode != http.StatusOK {
		return editorRelease{}, &githubUpdateResponseError{status: response.StatusCode, retryAfter: githubUpdateRetry(response.Header, time.Now())}
	}

	var record githubEditorRelease
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&record); err != nil {
		return editorRelease{}, fmt.Errorf(uiText("无法读取发布信息：%w"), err)
	}
	if record.Draft || record.Prerelease {
		return editorRelease{}, errors.New(uiText("未找到公开稳定版本"))
	}
	release, err := editorReleaseForTag(record.Tag, current)
	if err != nil {
		return editorRelease{}, err
	}
	release.Notes = editorReleaseNotes(record.Body)
	if release.comparison <= 0 {
		cache.ETag = response.Header.Get("ETag")
		return release, nil
	}
	download := editorRepositoryURL + "/releases/download/" + record.Tag + "/" + updateExecutable
	for _, asset := range record.Assets {
		if asset.Name != updateExecutable || asset.State != "uploaded" || asset.URL != download {
			continue
		}
		digest, valid := strings.CutPrefix(asset.Digest, "sha256:")
		checksum, err := hex.DecodeString(digest)
		if !valid || err != nil || len(checksum) != sha256.Size || asset.Size <= 0 || asset.Size > maxUpdateBytes {
			return editorRelease{}, errors.New(uiText("更新文件的大小或 SHA-256 无效"))
		}
		release.Download, release.Size = asset.URL, asset.Size
		copy(release.Checksum[:], checksum)
		cache.ETag = response.Header.Get("ETag")
		return release, nil
	}
	return editorRelease{}, errors.New(uiText("发布页没有可下载的 EditorX.exe"))
}

func editorReleaseNotes(body string) string {
	if _, notes, ok := strings.Cut(body, "## 本次更新"); ok {
		body = notes
	}
	notes, _, _ := strings.Cut(body, "## SHA-256")
	return strings.TrimSpace(notes)
}

func downloadEditorUpdate(ctx context.Context, client *http.Client, release editorRelease, target string, progress io.Writer) error {
	err := receiveEditorUpdate(ctx, client, release, target, progress)
	if err != nil {
		os.Remove(target)
	}
	return err
}

func receiveEditorUpdate(ctx context.Context, client *http.Client, release editorRelease, target string, progress io.Writer) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, release.Download, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf(uiText("下载返回 HTTP %d"), response.StatusCode)
	}

	file, err := os.OpenFile(target, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	digest := sha256.New()
	size, err := io.Copy(io.MultiWriter(file, digest, progress), io.LimitReader(response.Body, release.Size+1))
	if err != nil {
		return err
	}
	if size != release.Size || [sha256.Size]byte(digest.Sum(nil)) != release.Checksum {
		return errors.New(uiText("更新文件的大小或 SHA-256 校验失败"))
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	exe, err := pe.Open(target)
	if err != nil {
		return fmt.Errorf(uiText("更新文件不是有效的 Windows EXE：%w"), err)
	}
	exe.Close()
	if exe.Machine != pe.IMAGE_FILE_MACHINE_AMD64 {
		return errors.New(uiText("更新文件与 Windows x64 不兼容"))
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

const updateBackupSuffix = ".editorx-previous"

type installedEditorUpdate struct {
	path, backup string
}

func replaceEditorExecutable(path, staged string) (installedEditorUpdate, error) {
	reservation, err := os.CreateTemp(filepath.Dir(path), ".editorx-previous-*")
	if err != nil {
		return installedEditorUpdate{}, err
	}
	update := installedEditorUpdate{path: path, backup: reservation.Name()}
	if err := reservation.Close(); err != nil {
		os.Remove(update.backup)
		return installedEditorUpdate{}, err
	}
	if err := os.Rename(path, update.backup); err != nil {
		os.Remove(update.backup)
		return installedEditorUpdate{}, err
	}
	if err := os.Rename(staged, path); err != nil {
		return installedEditorUpdate{}, errors.Join(err, os.Rename(update.backup, path))
	}
	previous := path + updateBackupSuffix
	if err := os.Rename(update.backup, previous); err == nil {
		update.backup = previous
	}
	return update, nil
}

func (update installedEditorUpdate) restart() error {
	if err := startEditorExecutable(update.path); err != nil {
		restoreErr := os.Rename(update.backup, update.path)
		return fmt.Errorf(uiText("无法启动更新后的 EditorX：%w"), errors.Join(err, restoreErr))
	}
	return nil
}

func startEditorExecutable(path string) error {
	process := exec.Command(path)
	process.Dir = filepath.Dir(path)
	if err := process.Start(); err != nil {
		return err
	}
	process.Process.Release()
	return nil
}

// A running Windows process can hold the old image briefly after the replacement starts.
func cleanupPreviousEditorExecutable(executable string) {
	path := executable + updateBackupSuffix
	previous, err := os.Lstat(path)
	if err != nil || !previous.Mode().IsRegular() {
		return
	}
	for attempt := 0; attempt < 12; attempt++ {
		if attempt > 0 {
			time.Sleep(250 * time.Millisecond)
			current, err := os.Lstat(path)
			if err != nil || !os.SameFile(previous, current) {
				return
			}
		}
		if err := os.Remove(path); err == nil || errors.Is(err, os.ErrNotExist) {
			return
		}
	}
}
