package editor

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
)

const backupSessionEntry = "session.json"

type editorBackupResult struct {
	path    string
	size    int64
	windows []*editorApp
}

type backupTabChoice struct {
	window  *editorApp
	tab     *editorTab
	checked bool
}

type backupImportProgress struct {
	total     atomic.Int64
	completed atomic.Int64
}

type editorBackupState struct {
	request   *ui.EffectState[editorBackupResult, int]
	progress  *backupImportProgress
	applying  bool
	selecting bool
	choices   []backupTabChoice
	message   string
	problem   string
}

func (m *editorApp) backupWindows() []*editorApp {
	if m.store != nil {
		return m.store.windows
	}
	return []*editorApp{m}
}

func (m *editorApp) backupSettings(c ui.C) {
	state := &m.backup
	working := state.request != nil
	hasTabs := slices.ContainsFunc(m.backupWindows(), func(window *editorApp) bool {
		return !window.windowClosed && slices.ContainsFunc(window.tabs, func(tab *editorTab) bool { return !tab.isTemporaryLink() })
	})
	m.applicationSetting(c.Part("export"), uiText("导出会话备份"),
		uiText("勾选要备份的文件和草稿，保存为 ZIP 压缩包。"), func() {
			if ui.Button(c, uiText("导出备份")).Height(32).Disabled(working || !hasTabs).Clicked() {
				c.After(m.chooseBackupTabs)
			}
		})
	m.applicationSetting(c.Part("restore"), uiText("恢复会话备份"),
		uiText("打开 EditorX 导出的 ZIP 备份，将文件和项目恢复到当前窗口。"), func() {
			if ui.Button(c, uiText("选择备份…")).Outline().Height(32).Disabled(working || m.post == nil).Clicked() {
				c.After(m.importBackup)
			}
		})
	switch {
	case working && state.progress != nil:
		m.backupImportProgress(c.Part("import-progress"))
	case working:
		ui.Text(c, uiText("正在导出会话备份…")).Muted().FontSize(12)
	case state.problem != "":
		ui.Wrap(c, state.problem).FillWidth().Color(m.theme.Danger).FontSize(12)
	case state.message != "":
		ui.Wrap(c, state.message).FillWidth().Color(m.theme.Success).FontSize(12)
	}
}

func (m *editorApp) backupImportProgress(c ui.C) {
	state := &m.backup
	fraction := float32(-1)
	label := "正在读取 ZIP 备份…"
	if total := state.progress.total.Load(); total > 0 {
		fraction = float32(state.progress.completed.Load()) / float32(total)
		label = fmt.Sprintf(uiText("正在恢复文件和项目 · %.0f%%"), fraction*100)
	}
	if state.applying {
		label = "正在恢复到当前窗口…"
	}
	ui.Row(c.Part("status")).FillWidth().Gap(8).Items(ui.Center).Children(func() {
		ui.Text(c, label).Grow(1).Muted().FontSize(12)
		if !state.applying && ui.Button(c.Part("cancel"), uiText("取消导入")).Ghost().Height(26).Clicked() {
			c.After(func() {
				m.backup = editorBackupState{message: uiText("已取消备份导入")}
			})
		}
	})
	bar := ui.Progress(c.Part("bar"), fraction).FillWidth().Height(6)
	if fraction < 0 {
		bar.Indeterminate()
	}
}

func (m *editorApp) chooseBackupTabs() {
	state := &m.backup
	state.choices = nil
	for _, window := range m.backupWindows() {
		if window.windowClosed {
			continue
		}
		for _, tab := range window.tabs {
			if tab.isTemporaryLink() {
				continue
			}
			state.choices = append(state.choices, backupTabChoice{
				window: window, tab: tab, checked: window == m && tab == m.active,
			})
		}
	}
	state.selecting = len(state.choices) > 0
}

func (m *editorApp) backupSelectionDialog(c ui.C) {
	state := &m.backup
	if !state.selecting {
		return
	}
	selected := 0
	dismissed := ui.ModalWith(c, &state.selecting, uiText("选择要备份的文件"), ui.ModalSpec{Width: 536}, func() {
		ui.Row(c).FillWidth().Gap(6).Items(ui.Center).Children(func() {
			if ui.Button(c.Part("all"), uiText("全选")).Ghost().Height(28).Clicked() {
				for i := range state.choices {
					state.choices[i].checked = true
				}
			}
			if ui.Button(c.Part("none"), uiText("清空选择")).Ghost().Height(28).Clicked() {
				for i := range state.choices {
					state.choices[i].checked = false
				}
			}
		})
		height := min(float32(264), float32(len(state.choices))*50+8)
		ui.Scroll(c.Part("file-list")).FillWidth().Height(height).Scrollbars().Children(func() {
			ui.Column(c).FillWidth().Gap(3).PadRight(8).Children(func() {
				for i := range state.choices {
					choice := &state.choices[i]
					tab := choice.tab
					ui.Column(c.KeyInt(i)).FillWidth().PadX(6).PadBottom(4).Height(47).Radius(8).
						Background(m.theme.Card).Children(func() {
						ui.Checkbox(c, &choice.checked, tab.tabTitle()).FillWidth().Height(27).Tooltip(tab.title())
						detail := tab.doc.path
						if detail == "" {
							detail = "未保存草稿"
						}
						ui.Text(c, detail).FillWidth().PadLeft(26).FontSize(11).Muted().Ellipsis()
					})
					if choice.checked {
						selected++
					}
				}
			})
		})
	}, func() {
		ui.Text(c, fmt.Sprintf(uiText("已选 %d 个文件"), selected)).FontSize(12).Muted()
		if ui.Button(c.Part("save-backup"), uiText("选择保存位置…")).Disabled(selected == 0).Clicked() {
			c.After(m.exportBackup)
		}
	})
	if dismissed {
		state.choices = nil
	}
}

func (m *editorApp) exportBackup() {
	path, err := app.SaveFile(app.SaveFileOptions{
		Title:            uiText("导出 EditorX 会话备份"),
		SuggestedName:    "EditorX-" + time.Now().Format("20060102-150405") + ".editorx-backup.zip",
		DefaultExtension: "zip",
		Filters:          []app.FileFilter{{Name: uiText("EditorX 压缩备份"), Pattern: "*.zip"}},
	})
	if errors.Is(err, app.ErrCanceled) {
		return
	}
	if err != nil {
		m.backup.problem = uiText("无法选择备份位置：") + err.Error()
		m.backup.selecting, m.backup.choices = false, nil
		return
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		m.backup.problem = uiText("无法读取备份路径：") + err.Error()
		m.backup.selecting, m.backup.choices = false, nil
		return
	}

	included := make(map[*editorApp][]*editorTab)
	for _, choice := range m.backup.choices {
		window := choice.window
		if choice.checked && !window.windowClosed && slices.Contains(window.tabs, choice.tab) {
			included[window] = append(included[window], choice.tab)
		}
	}
	saved := savedSession{Version: editorSessionVersion}
	for _, window := range m.backupWindows() {
		if tabs := included[window]; len(tabs) > 0 {
			saved.Windows = append(saved.Windows, window.snapshotWorkspace(tabs, false))
		}
	}
	m.backup = editorBackupState{}
	if len(saved.Windows) == 0 {
		m.backup.problem = uiText("所选文件已关闭，请重新选择。")
		return
	}
	m.backup.request = ui.NewEffect[editorBackupResult, int](func(ctx context.Context) (editorBackupResult, error) {
		if err := writeEditorBackup(ctx, absolute, saved); err != nil {
			return editorBackupResult{}, err
		}
		info, err := os.Stat(absolute)
		if err != nil {
			return editorBackupResult{}, err
		}
		return editorBackupResult{path: absolute, size: info.Size()}, nil
	})
}

func writeEditorBackup(ctx context.Context, path string, saved savedSession) error {
	return writeFileAtomically(path, func(output io.Writer) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		archive := zip.NewWriter(output)
		entry, err := archive.Create(backupSessionEntry)
		if err != nil {
			return err
		}
		if err := json.NewEncoder(entry).Encode(saved); err != nil {
			return err
		}
		if err := includeLargeEditDrafts(ctx, archive, saved); err != nil {
			return err
		}
		if err := archive.Close(); err != nil {
			return err
		}
		return ctx.Err()
	})
}

func readEditorBackup(path string) (savedSession, error) {
	return readEditorBackupDrafts(path, "")
}

func readEditorBackupDrafts(path, directory string) (savedSession, error) {
	if !strings.EqualFold(filepath.Ext(path), ".zip") {
		return savedSession{}, errors.New(uiText("请选择 EditorX 导出的 ZIP 备份"))
	}
	archive, err := zip.OpenReader(path)
	if archive != nil {
		defer archive.Close()
	}
	if err != nil {
		return savedSession{}, fmt.Errorf(uiText("无法打开 ZIP 备份：%w"), err)
	}
	entry, err := archive.Open(backupSessionEntry)
	if err != nil {
		return savedSession{}, fmt.Errorf(uiText("ZIP 备份中缺少会话数据：%w"), err)
	}
	defer entry.Close()
	decoder := json.NewDecoder(entry)
	var saved savedSession
	if err := decoder.Decode(&saved); err != nil {
		return savedSession{}, err
	}
	// 读取到末尾，校验 ZIP 内容并拒绝附加的 JSON 数据。
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			err = errors.New(uiText("备份包含多余的会话数据"))
		}
		return savedSession{}, err
	}
	if err := saved.validateVersion(); err != nil {
		return savedSession{}, err
	}
	if err := importLargeEditDrafts(archive, saved, directory); err != nil {
		return savedSession{}, err
	}
	return saved, nil
}

func (m *editorApp) importBackup() {
	paths, err := app.OpenFiles(app.OpenFileOptions{Title: uiText("打开 EditorX 会话备份"), Filters: []app.FileFilter{
		{Name: uiText("EditorX 压缩备份"), Pattern: "*.zip"},
	}})
	if errors.Is(err, app.ErrCanceled) {
		return
	}
	if err != nil {
		m.backup.problem = uiText("无法选择会话备份：") + err.Error()
		return
	}
	if len(paths) == 0 {
		return
	}
	path, preferences, directory := paths[0], m.editorPrefs, m.dataDirectory
	progress := &backupImportProgress{}
	repaint := m.invalidator.Invalidate
	request := ui.NewEffect[editorBackupResult, int](func(ctx context.Context) (editorBackupResult, error) {
		saved, err := readEditorBackupDrafts(path, directory)
		if err != nil {
			return editorBackupResult{}, err
		}
		if len(saved.Windows) == 0 {
			return editorBackupResult{}, errors.New(uiText("备份中没有可恢复的窗口"))
		}
		total := len(saved.Windows)
		for _, window := range saved.Windows {
			total += len(window.Tabs)
		}
		progress.total.Store(int64(total))
		repaint()

		// Prepare detached windows in the worker, including disk reads and editor buffers.
		windows := make([]*editorApp, 0, len(saved.Windows))
		tabRestored := func() {
			progress.completed.Add(1)
			repaint()
		}
		for _, window := range saved.Windows {
			if err := ctx.Err(); err != nil {
				return editorBackupResult{}, err
			}
			window.AI = nil
			restored := newEditorApp()
			restored.editorPrefs = preferences
			if err := restored.restoreSessionWithProgress(ctx, window, tabRestored); err != nil {
				return editorBackupResult{}, err
			}
			windows = append(windows, restored)
		}
		return editorBackupResult{windows: windows}, ctx.Err()
	})
	m.backup = editorBackupState{request: request, progress: progress}
}

func (m *editorApp) backupProgress(c ui.C) {
	state := &m.backup
	if state.request == nil {
		return
	}
	ui.Effect(c, state.request, 0)
	result, err, ready := state.request.Take()
	if !ready {
		return
	}
	if err != nil {
		state.request, state.progress = nil, nil
		state.problem = uiText("会话备份操作失败：") + err.Error()
		return
	}
	if result.path != "" {
		state.request = nil
		size := fmt.Sprintf("%.1f KB", float64(result.size)/1024)
		if result.size >= 1<<20 {
			size = fmt.Sprintf("%.1f MB", float64(result.size)/(1<<20))
		}
		state.message = uiText("备份已保存 · ") + size + " · " + result.path
		return
	}
	request := state.request
	state.applying = true
	m.post(func() {
		if state.request != request {
			return
		}
		defer func() {
			state.request, state.progress, state.applying = nil, nil, false
			m.invalidator.Invalidate()
		}()
		if m.windowClosed || (m.desktop != nil && m.desktop.exiting) {
			state.problem = uiText("EditorX 正在退出，请稍后恢复备份。")
			return
		}
		var active *editorTab
		for _, restored := range result.windows {
			tab := m.mergeRestoredWindow(restored, nil)
			if active == nil {
				active = tab
			}
			state.progress.completed.Add(1)
		}
		m.sortPinnedTabs()
		if active != nil {
			m.activate(active)
		}
		state.message = uiText("已将备份恢复到当前窗口")
		m.status = state.message
		if m.appSettings.open && m.appSettings.section == settingsBackup {
			m.closeSettings()
		}
	})
}

func (m *editorApp) mergeRestoredWindow(restored *editorApp, conversations *aiWindowSession) *editorTab {
	groups := make(map[*tabGroup]bool)
	var active *editorTab
	for index, tab := range restored.tabs {
		if existing := m.tabAtPath(tab.doc.path); existing != nil {
			sameContent := tab.source.text.CommittedString() == existing.source.text.CommittedString()
			if tab.readOnlyPreview() && !tab.modified() || (sameContent && !tab.modified() && !tab.hasResultDraft()) {
				tab.largeText.close()
				if tab == restored.active {
					active = existing
				}
				restored.tabs[index] = existing
				continue
			}
			// 同一路径已有标签时，保留备份版本为可另存的副本。
			tab.name = tab.title()
			tab.displayName = tab.tabTitle() + "（备份）"
			if state := tab.largeText; state != nil && state.editSession != nil && state.editSession.BasePath == "" {
				state.editSession.BasePath = tab.doc.path
			}
			tab.doc.path = ""
		}
		if tab.group != nil && !groups[tab.group] {
			m.nextGroupID++
			tab.group.ID = m.nextGroupID
			groups[tab.group] = true
		}
		m.nextID++
		tab.id = m.nextID
		tab.source.light, tab.result.light, tab.comparison.light = m.light, m.light, m.light
		if !m.editorPrefs.equal(restored.editorPrefs) {
			tab.applyEditorPreferences(m.editorPrefs)
		}
		tab.bindSearchWindow(m)
		m.tabs = append(m.tabs, tab)
		if tab == restored.active {
			active = tab
		}
	}
	if m.workspace == nil {
		m.workspace = restored.workspace
	} else if restored.workspace != nil {
		m.workspace.mergeProjects(restored.workspace)
	}
	if conversations != nil {
		for _, record := range conversations.History {
			m.ai.history = append(m.ai.history, record.restore(restored.tabs))
		}
		if current := conversations.Current.restore(restored.tabs); current.hasContent() {
			m.ai.history = append(m.ai.history, current)
		}
	}
	return active
}
