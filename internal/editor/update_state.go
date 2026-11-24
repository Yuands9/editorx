package editor

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Yuands9/editorx/internal/version"
	uimarkdown "github.com/vivaui/viva/ui/markdown"
)

const (
	editorUpdateStartupDelay = 10 * time.Second
	editorUpdateInterval     = 6 * time.Hour
	editorUpdateCacheFile    = "update-state.json"
)

type editorUpdateCache struct {
	CheckedAt      time.Time     `json:"checkedAt"`
	ETag           string        `json:"etag,omitempty"`
	SkippedVersion string        `json:"skippedVersion,omitempty"`
	Release        editorRelease `json:"release"`
}

type editorUpdateCheckResult struct {
	release editorRelease
	etag    string
	err     error
}

type editorUpdateCheck struct {
	cancel context.CancelFunc
	result chan editorUpdateCheckResult
}

type editorUpdateDownload struct {
	cancel context.CancelFunc
	result chan error
}

type editorUpdateState struct {
	// Dialog visibility and Markdown layout belong to each window. The desktop
	// owns the remaining state so closing one window cannot cancel shared work.
	open            bool
	notes           uimarkdown.State
	initialized     bool
	cache           editorUpdateCache
	nextCheck       time.Time
	manual          bool
	check           *editorUpdateCheck
	download        *editorUpdateDownload
	progress        *dictionaryProgress
	staged, problem string
}

func readEditorUpdateCache(directory, current string) (editorUpdateCache, error) {
	var cache editorUpdateCache
	if directory == "" {
		return cache, nil
	}
	data, err := readTextAsset(filepath.Join(directory, editorUpdateCacheFile), 2<<20)
	if errors.Is(err, os.ErrNotExist) {
		return cache, nil
	}
	if err != nil {
		return cache, err
	}
	if err := json.Unmarshal(data, &cache); err != nil {
		return editorUpdateCache{}, err
	}
	release := &cache.Release
	if release.Version == "" {
		cache.ETag = ""
		return cache, nil
	}
	latest, err := editorVersionParts(release.Version)
	if err != nil {
		return editorUpdateCache{}, err
	}
	installed, err := editorVersionParts(current)
	if err != nil {
		return editorUpdateCache{}, err
	}
	for index := range latest {
		if release.comparison = cmp.Compare(latest[index], installed[index]); release.comparison != 0 {
			break
		}
	}
	tag, valid := strings.CutPrefix(release.Page, editorRepositoryURL+"/releases/tag/")
	if !valid || strings.TrimPrefix(tag, "v") != release.Version {
		return editorUpdateCache{}, errors.New("更新缓存的版本地址无效")
	}
	if release.comparison > 0 && (release.Download != editorRepositoryURL+"/releases/download/"+tag+"/"+updateExecutable || release.Size <= 0 || release.Size > maxUpdateBytes || release.Checksum == [sha256.Size]byte{}) {
		return editorUpdateCache{}, errors.New("更新缓存的下载信息无效")
	}
	return cache, nil
}

func (state *editorUpdateState) initialize(directory string, now time.Time) {
	if state.initialized {
		return
	}
	state.initialized = true
	state.cache, _ = readEditorUpdateCache(directory, version.Number)
	state.nextCheck = now.Add(editorUpdateStartupDelay)
	if !state.cache.CheckedAt.After(now) && now.Sub(state.cache.CheckedAt) < editorUpdateInterval {
		state.nextCheck = state.cache.CheckedAt.Add(editorUpdateInterval)
	}
}

func (state *editorUpdateState) save(directory string) error {
	data, err := json.Marshal(state.cache)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	return writeFile(filepath.Join(directory, editorUpdateCacheFile), data)
}

func (m *editorApp) editorUpdates() *editorUpdateState {
	state := &m.update
	if m.desktop != nil {
		state = &m.desktop.update
	}
	state.initialize(m.dataDirectory, time.Now())
	return state
}

func (state *editorUpdateState) available() bool {
	return state.cache.Release.comparison > 0 &&
		(state.cache.Release.Version != state.cache.SkippedVersion || state.download != nil || state.staged != "")
}

func (m *editorApp) notifyEditorUpdate() {
	if m.desktop != nil && m.desktop.application != nil {
		m.desktop.application.Post(func() {
			m.updateProgress()
			for _, window := range m.desktop.store.windows {
				if !window.windowClosed {
					window.invalidator.Invalidate()
				}
			}
		})
		return
	}
	m.invalidator.Invalidate()
}

func (m *editorApp) checkEditorUpdate() {
	m.update.open = true
	m.startEditorUpdateCheck(true)
}

func (m *editorApp) startEditorUpdateCheck(manual bool) {
	state := m.editorUpdates()
	if manual {
		state.manual = true
	}
	if state.check != nil || state.download != nil || state.staged != "" {
		return
	}
	state.problem, state.manual = "", manual
	ctx, cancel := context.WithTimeout(context.Background(), 18*time.Second)
	job := &editorUpdateCheck{cancel: cancel, result: make(chan editorUpdateCheckResult, 1)}
	state.check = job
	cache := state.cache
	go func() {
		defer cancel()
		release, err := fetchEditorRelease(ctx, editorUpdateHTTP, latestReleaseAPI, version.Number, &cache)
		job.result <- editorUpdateCheckResult{release, cache.ETag, err}
		m.notifyEditorUpdate()
	}()
}

func (d *editorDesktop) checkEditorUpdates(now time.Time) {
	if d.exiting || d.preferences.DisableAutoUpdate || now.Before(d.update.nextCheck) || d.update.check != nil || d.update.download != nil || d.update.staged != "" {
		return
	}
	for _, window := range d.store.windows {
		if !window.windowClosed {
			window.startEditorUpdateCheck(false)
			return
		}
	}
}

func (m *editorApp) downloadEditorRelease() {
	state := m.editorUpdates()
	if state.download != nil || state.staged != "" || state.cache.Release.comparison <= 0 || hasWordEditor {
		return
	}
	executable, err := os.Executable()
	if err != nil {
		state.problem = "无法定位当前程序：" + err.Error()
		return
	}
	file, err := os.CreateTemp(filepath.Dir(executable), ".editorx-update-*.exe")
	if err != nil {
		state.problem = "无法在程序目录创建更新文件：" + err.Error()
		return
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		os.Remove(path)
		state.problem = "无法准备下载文件：" + err.Error()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	job := &editorUpdateDownload{cancel: cancel, result: make(chan error, 1)}
	progress := &dictionaryProgress{invalidate: m.notifyEditorUpdate}
	state.staged, state.problem, state.download, state.progress = path, "", job, progress
	release := state.cache.Release
	go func() {
		defer cancel()
		err := downloadEditorUpdate(ctx, editorUpdateHTTP, release, path, progress)
		job.result <- err
		m.notifyEditorUpdate()
	}()
}

func (m *editorApp) updateProgress() {
	state := m.editorUpdates()
	if !m.update.open {
		m.update.notes = uimarkdown.State{}
	}
	if state.check != nil {
		select {
		case result := <-state.check.result:
			state.check = nil
			state.cache.CheckedAt = time.Now()
			state.nextCheck = state.cache.CheckedAt.Add(editorUpdateInterval)
			if result.err == nil {
				state.cache.Release, state.cache.ETag = result.release, result.etag
			} else if state.manual {
				state.problem = "检查更新失败：" + result.err.Error()
			}
			if err := state.save(m.dataDirectory); err != nil && state.manual {
				state.problem = "无法保存更新检查记录：" + err.Error()
			}
		default:
		}
	}
	if state.download != nil {
		select {
		case err := <-state.download.result:
			state.download, state.progress = nil, nil
			if err != nil {
				state.staged = ""
				state.problem = "下载更新失败：" + err.Error()
			}
		default:
		}
	}
}

func (m *editorApp) cancelEditorDownload() {
	state := m.editorUpdates()
	job, path := state.download, state.staged
	state.download, state.progress, state.staged = nil, nil, ""
	if job == nil {
		if path != "" {
			os.Remove(path)
		}
		return
	}
	job.cancel()
	go func() { <-job.result; os.Remove(path) }()
}

func (m *editorApp) skipEditorUpdate() {
	state := m.editorUpdates()
	previous := state.cache.SkippedVersion
	state.cache.SkippedVersion = state.cache.Release.Version
	if err := state.save(m.dataDirectory); err != nil {
		state.cache.SkippedVersion = previous
		state.problem = fmt.Sprintf("无法保存跳过记录：%v", err)
		return
	}
	m.cancelEditorDownload()
	m.update.open = false
	m.notifyEditorUpdate()
}

func (state *editorUpdateState) close() {
	if state.check != nil {
		state.check.cancel()
	}
	if state.download != nil {
		state.download.cancel()
		select {
		case <-state.download.result:
		case <-time.After(2 * time.Second):
		}
	}
	if state.staged != "" {
		os.Remove(state.staged)
	}
}
