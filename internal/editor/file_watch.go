package editor

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
)

type diskChangeState struct {
	path                                                     string
	followRequested                                          bool
	pending, changed, missing, ignoredMissing, confirmReload bool
	ignored, digest                                          [32]byte
	problem                                                  string
	request                                                  *ui.EffectState[diskReadResult, int]
	action                                                   string
	before                                                   string
	beforeFormat                                             document
}
type diskReadResult struct {
	digest     [32]byte
	missing    bool
	page       *largeTextPage
	file       openedEditorFile
	comparison *localHistoryComparison
}

func (m *editorApp) closeFileWatches() {
	for _, watch := range m.fileWatches {
		if watch != nil {
			watch.Close()
		}
	}
	m.fileWatches = nil
}

func watchedTextTab(tab *editorTab) bool {
	return tab != nil && tab.doc.path != "" && tab.office == nil && tab.picture == nil && tab.preview == nil
}

func (m *editorApp) updateFileWatches(c ui.C) {
	if !m.watchFiles || m.windowClosed {
		return
	}
	if m.fileWatches == nil {
		m.fileWatches = map[string]*app.DirectoryWatch{}
	}
	directories := map[string]bool{}
	for _, tab := range m.tabs {
		if !watchedTextTab(tab) {
			continue
		}
		if tab.diskChange == nil || tab.diskChange.path != tab.doc.path {
			tab.diskChange = &diskChangeState{path: tab.doc.path, pending: true}
		}
		directory := filepath.Dir(tab.doc.path)
		directories[directory] = true
		if _, exists := m.fileWatches[directory]; !exists || m.checkFiles && m.fileWatches[directory] == nil {
			watch, _ := app.WatchDirectory(directory, m.invalidator.Invalidate)
			m.fileWatches[directory] = watch // Retry unavailable directories on activation, not on every frame.
		}
	}
	for directory, watch := range m.fileWatches {
		if !directories[directory] {
			if watch != nil {
				watch.Close()
			}
			delete(m.fileWatches, directory)
			continue
		}
		if watch == nil {
			continue
		}
		changes := watch.Changes(nil)
		for _, tab := range m.tabs {
			if tab.diskChange == nil || filepath.Dir(tab.doc.path) != directory {
				continue
			}
			changed := changes.Resync
			for _, name := range changes.Names {
				if strings.EqualFold(name, filepath.Base(tab.doc.path)) {
					changed = true
					break
				}
			}
			tab.diskChange.pending = tab.diskChange.pending || changed
		}
		if changes.Resync {
			watch.Close()
			m.fileWatches[directory] = nil
		}
	}
	for _, tab := range m.tabs {
		s := tab.diskChange
		if s == nil || !watchedTextTab(tab) {
			continue
		}
		s.pending = s.pending || m.checkFiles
		if s.request != nil {
			ui.Effect(c.Part("read").KeyInt(tab.id), s.request, 0)
			if result, err, ready := s.request.Take(); ready {
				s.request = nil
				m.acceptDiskRead(tab, s, result, err)
				s.before, s.beforeFormat = "", document{}
			}
		}
		if s.request == nil && s.pending && tab.savingText == nil && (tab.largeText == nil || !tab.largeText.busyEditing()) {
			action := "check"
			if s.followRequested || tab.largeText != nil && tab.largeText.following {
				action = "follow"
			}
			m.startDiskRead(tab, action)
			if s.request != nil {
				ui.Effect(c.Part("read").KeyInt(tab.id), s.request, 0)
			}
		}
	}
	m.checkFiles = false
}

func inspectDiskFile(ctx context.Context, path string) (diskReadResult, error) {
	file, err := openSharedTextFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return diskReadResult{missing: true}, nil
	}
	if err != nil {
		return diskReadResult{}, err
	}
	defer file.Close()
	digest := sha256.New()
	_, err = io.Copy(digest, largeEditContextReader{ctx: ctx, Reader: file})
	result := diskReadResult{}
	copy(result.digest[:], digest.Sum(nil))
	return result, err
}

func (m *editorApp) startDiskRead(tab *editorTab, action string) {
	if !watchedTextTab(tab) || tab.source.text.CompositionActive() || tab.savingText != nil {
		return
	}
	if tab.diskChange == nil {
		tab.diskChange = &diskChangeState{path: tab.doc.path}
	}
	s := tab.diskChange
	if s.request != nil {
		return
	}
	s.pending, s.action, s.problem = false, action, ""
	s.before, s.beforeFormat = tab.source.text.CommittedString(), tab.doc
	path, enc, large := tab.doc.path, tab.doc.encoding, tab.largeText
	var largeFile os.FileInfo
	if large != nil {
		largeFile = large.file
	}
	current := tab.doc
	current.saved = s.before
	s.request = ui.NewEffect[diskReadResult, int](func(ctx context.Context) (diskReadResult, error) {
		switch action {
		case "follow":
			page, err := readLargeTextPage(path, -1, -1, "", enc)
			return diskReadResult{page: &page, missing: errors.Is(err, os.ErrNotExist)}, err
		case "reload":
			file, err := readEditorFile(path, enc)
			return diskReadResult{file: file}, err
		case "compare":
			doc, err := readHistoryCurrentFile(ctx, path)
			if err == nil {
				err = historyCompareSize(current.saved)
			}
			if err != nil {
				return diskReadResult{}, err
			}
			comparison := &localHistoryComparison{old: doc, current: current, fromEditor: true}
			comparison.compareLines()
			return diskReadResult{comparison: comparison}, ctx.Err()
		default:
			if large != nil {
				info, err := os.Stat(path)
				if errors.Is(err, os.ErrNotExist) {
					return diskReadResult{missing: true}, nil
				}
				if err != nil {
					return diskReadResult{}, err
				}
				// Large-file browsing checks metadata; no full-file hashing on each append.
				if sameTextFile(largeFile, info) {
					return diskReadResult{}, nil
				}
				return diskReadResult{digest: sha256.Sum256([]byte(info.ModTime().String() + historySize(info.Size())))}, nil
			}
			return inspectDiskFile(ctx, path)
		}
	})
	m.invalidator.Invalidate()
}

func (m *editorApp) acceptDiskRead(tab *editorTab, s *diskChangeState, result diskReadResult, err error) {
	if !m.hasTab(tab) || s != tab.diskChange || s.path != tab.doc.path {
		return
	}
	if err != nil {
		if s.action == "follow" && tab.largeText == nil {
			s.followRequested = false
		}
		s.problem = uiText("无法读取磁盘文件：") + err.Error()
		return
	}
	switch s.action {
	case "compare":
		m.openTextComparison(*result.comparison, tab, tab.tabTitle()+" · 磁盘内容")
	case "reload":
		if tab.source.text.CommittedString() != s.before || tab.doc != s.beforeFormat || tab.hasResultDraft() || tab.source.text.CompositionActive() {
			s.problem = uiText("读取期间编辑内容发生变化，请确认后重新读取。")
			return
		}
		caret := tab.source.text.Caret()
		tab.doc = result.file.doc
		tab.source.setResult(tab.doc.saved)
		if result.file.largeText != nil {
			tab.startLargeText(*result.file.largeText)
		} else if tab.largeText != nil {
			tab.largeText.close()
			tab.largeText = nil
			tab.source.readOnly, tab.source.view.Snapshot.ReadOnly = false, false
			tab.source.view.Pointer, tab.source.view.Scroll = tab.source.pointer, nil
			tab.applyEditorPreferences(m.editorPrefs)
		}
		tab.source.text.MoveTo(min(caret, tab.source.text.RuneCount()), false)
		tab.resultReady, tab.resultDraft, tab.diffReady = false, false, false
		s.changed, s.confirmReload, s.ignoredMissing = false, false, false
		s.ignored, s.problem = [32]byte{}, ""
	case "follow":
		if s.followRequested {
			s.followRequested = false
			if tab.source.text.CommittedString() != s.before || tab.doc != s.beforeFormat || tab.source.text.CompositionActive() {
				s.problem = uiText("读取期间编辑内容变化，未开启日志跟随")
				return
			}
			if tab.largeText == nil {
				tab.startLargeText(*result.page)
				tab.doc.saved = ""
			}
			tab.largeText.following = true
			tab.largeText.indexing, tab.largeText.lines, tab.largeText.search, tab.largeText.allMatches = nil, largeLineIndex{}, nil, nil
		}
		if tab.largeText == nil || !tab.largeText.following {
			return
		}
		tab.applyLargeTextPage(*result.page)
		tab.source.text.MoveTo(tab.source.text.RuneCount(), false)
		tab.source.view.Code.Reveal(len(result.page.value))
		s.changed, s.problem = false, ""
	default:
		s.digest, s.missing = result.digest, result.missing
		if !result.missing {
			s.ignoredMissing = false
		}
		s.changed = result.missing && !s.ignoredMissing || !result.missing && result.digest != tab.doc.diskHash && result.digest != s.ignored
	}
}

func (m *editorApp) toggleLogFollow(tab *editorTab) {
	if !watchedTextTab(tab) || tab.modified() || tab.hasResultDraft() || tab.source.text.CompositionActive() {
		m.status = uiText("请先保存文件，再跟随日志")
		return
	}
	if tab.diskChange != nil && tab.diskChange.followRequested {
		tab.diskChange.followRequested = false
		m.status = uiText("已暂停跟随")
		return
	}
	if tab.largeText != nil && tab.largeText.following {
		tab.largeText.following = false
		m.status = uiText("已暂停跟随")
		return
	}
	if tab.largeText != nil && (tab.largeText.editing != nil || tab.largeText.busyEditing()) {
		m.status = uiText("请先退出完整编辑模式")
		return
	}
	if tab.diskChange == nil {
		tab.diskChange = &diskChangeState{path: tab.doc.path}
	}
	tab.diskChange.followRequested = true
	m.startDiskRead(tab, "follow")
	if tab.diskChange != nil {
		tab.diskChange.pending = true
	}
	m.status = uiText("正在跟随日志 · 向上翻页暂停")
}

func (m *editorApp) fileChangeBar(c ui.C, tab *editorTab, blocked bool) {
	s := tab.diskChange
	if s == nil || !s.changed && s.problem == "" {
		return
	}
	ui.Row(c).FillWidth().MinHeight(34).Gap(6).PadX(10).Items(ui.Center).Children(func() {
		message := "磁盘文件已变化，编辑内容已保留"
		if s.missing {
			message = "磁盘文件已删除或移动，编辑内容已保留"
		}
		if s.problem != "" {
			message = s.problem
		}
		if s.confirmReload {
			message = "重新读取会替换未保存内容，确认继续？"
		}
		ui.Wrap(c, message).FontSize(12).Muted().Grow(1).MinWidth(0)
		busy := blocked || s.request != nil || tab.source.text.CompositionActive() || m.macro != nil
		if ui.Button(c, uiText("比对磁盘内容")).Ghost().FontSize(12).Height(28).Disabled(busy || s.missing || !historyEditableTab(tab)).Clicked() {
			c.After(func() { m.startDiskRead(tab, "compare") })
		}
		label := "重新读取"
		if s.confirmReload {
			label = "确认重新读取"
		}
		if ui.Button(c, label).Ghost().FontSize(12).Height(28).Disabled(busy || s.missing || tab.hasResultDraft() || tab.largeText != nil && tab.largeText.editing != nil).Clicked() {
			c.After(func() {
				if tab.modified() && !s.confirmReload {
					s.confirmReload = true
					return
				}
				m.startDiskRead(tab, "reload")
			})
		}
		if ui.Button(c, uiText("保留编辑内容")).Ghost().FontSize(12).Height(28).Disabled(busy).Clicked() {
			c.After(func() {
				s.ignored, s.ignoredMissing = s.digest, s.missing
				s.changed, s.confirmReload, s.problem = false, false, ""
			})
		}
	})
}
