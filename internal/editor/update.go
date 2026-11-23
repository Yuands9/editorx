package editor

import (
	"cmp"
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
	"strconv"
	"strings"
	"time"

	"github.com/vivaui/viva/app/proxy"
)

const (
	editorRepositoryURL = "https://github.com/Yuands9/editorx"
	latestReleaseAPI    = "https://api.github.com/repos/Yuands9/editorx/releases/latest"
	updateExecutable    = "EditorX.exe"
	maxUpdateBytes      = 200 << 20
)

type githubReleaseAsset struct {
	Name, Digest, State string
	Size                int64
	URL                 string `json:"browser_download_url"`
}

type githubEditorRelease struct {
	Tag        string `json:"tag_name"`
	Body       string
	Draft      bool
	Prerelease bool
	Assets     []githubReleaseAsset
}

type editorRelease struct {
	version, page, notes, download string
	size                           int64
	checksum                       [sha256.Size]byte
	comparison                     int
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
		return result, fmt.Errorf("版本号无效 %q", value)
	}
	for index, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 || strconv.Itoa(number) != part {
			return result, fmt.Errorf("版本号无效 %q", value)
		}
		result[index] = number
	}
	return result, nil
}

func fetchEditorRelease(ctx context.Context, client *http.Client, endpoint, current string) (editorRelease, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return editorRelease{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	request.Header.Set("User-Agent", applicationName+"/"+current)
	response, err := client.Do(request)
	if err != nil {
		return editorRelease{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return editorRelease{}, fmt.Errorf("GitHub 返回 HTTP %d", response.StatusCode)
	}

	var record githubEditorRelease
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&record); err != nil {
		return editorRelease{}, fmt.Errorf("无法读取发布信息：%w", err)
	}
	if record.Draft || record.Prerelease {
		return editorRelease{}, errors.New("未找到公开稳定版本")
	}
	latest, err := editorVersionParts(record.Tag)
	if err != nil {
		return editorRelease{}, err
	}
	installed, err := editorVersionParts(current)
	if err != nil {
		return editorRelease{}, err
	}
	release := editorRelease{
		version: strings.TrimPrefix(record.Tag, "v"),
		page:    editorRepositoryURL + "/releases/tag/" + record.Tag,
		notes:   editorReleaseNotes(record.Body),
	}
	for index := range latest {
		if release.comparison = cmp.Compare(latest[index], installed[index]); release.comparison != 0 {
			break
		}
	}
	if release.comparison <= 0 {
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
			return editorRelease{}, errors.New("更新文件的大小或 SHA-256 无效")
		}
		release.download, release.size = asset.URL, asset.Size
		copy(release.checksum[:], checksum)
		return release, nil
	}
	return editorRelease{}, errors.New("发布页没有可下载的 EditorX.exe")
}

func editorReleaseNotes(body string) string {
	if _, notes, ok := strings.Cut(body, "## 本次更新"); ok {
		body = notes
	}
	notes, _, _ := strings.Cut(body, "## SHA-256")
	return strings.TrimSpace(notes)
}

func downloadEditorUpdate(ctx context.Context, client *http.Client, release editorRelease, target string) error {
	complete := false
	defer func() {
		if !complete {
			os.Remove(target)
		}
	}()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, release.download, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("下载返回 HTTP %d", response.StatusCode)
	}

	file, err := os.OpenFile(target, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	digest := sha256.New()
	size, err := io.Copy(io.MultiWriter(file, digest), io.LimitReader(response.Body, release.size+1))
	if err != nil {
		return err
	}
	if size != release.size || [sha256.Size]byte(digest.Sum(nil)) != release.checksum {
		return errors.New("更新文件的大小或 SHA-256 校验失败")
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	exe, err := pe.Open(target)
	if err != nil {
		return fmt.Errorf("更新文件不是有效的 Windows EXE：%w", err)
	}
	exe.Close()
	if exe.Machine != pe.IMAGE_FILE_MACHINE_AMD64 {
		return errors.New("更新文件与 Windows x64 不兼容")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	complete = true
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
	process := exec.Command(update.path)
	process.Dir = filepath.Dir(update.path)
	if err := process.Start(); err != nil {
		restoreErr := os.Rename(update.backup, update.path)
		return fmt.Errorf("无法启动更新后的 EditorX：%w", errors.Join(err, restoreErr))
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
