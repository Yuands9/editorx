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
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
)

const backupSessionEntry = "session.json"

type editorBackupResult struct {
	path    string
	size    int64
	windows []windowSession
}

type backupTabChoice struct {
	window  *editorApp
	tab     *editorTab
	checked bool
}

type editorBackupState struct {
	request   *ui.EffectState[editorBackupResult, int]
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
		return !window.windowClosed && len(window.tabs) > 0
	})
	m.applicationSetting(c.Part("export"), "导出会话备份",
		"勾选要备份的文件和草稿，保存为 ZIP 压缩包。", func() {
			if ui.Button(c, "导出备份").Height(32).Disabled(working || !hasTabs).Clicked() {
				c.After(m.chooseBackupTabs)
			}
		})
	m.applicationSetting(c.Part("restore"), "恢复会话备份",
		"打开 EditorX 导出的 ZIP 备份，在新窗口恢复所选文件。", func() {
			if ui.Button(c, "选择备份…").Outline().Height(32).Disabled(working || m.desktop == nil).Clicked() {
				c.After(m.importBackup)
			}
		})
	switch {
	case working:
		ui.Text(c, "正在处理会话备份…").Muted().FontSize(12)
	case state.problem != "":
		ui.Wrap(c, state.problem).FillWidth().Color(m.theme.Danger).FontSize(12)
	case state.message != "":
		ui.Wrap(c, state.message).FillWidth().Color(m.theme.Success).FontSize(12)
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
	dismissed := ui.ModalWith(c, &state.selecting, "选择要备份的文件", ui.ModalSpec{Width: 536}, func() {
		ui.Row(c).FillWidth().Gap(6).Items(ui.Center).Children(func() {
			if ui.Button(c.Part("all"), "全选").Ghost().Height(28).Clicked() {
				for i := range state.choices {
					state.choices[i].checked = true
				}
			}
			if ui.Button(c.Part("none"), "清空选择").Ghost().Height(28).Clicked() {
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
		ui.Text(c, fmt.Sprintf("已选 %d 个文件", selected)).FontSize(12).Muted()
		if ui.Button(c.Part("save-backup"), "选择保存位置…").Disabled(selected == 0).Clicked() {
			c.After(m.exportBackup)
		}
	})
	if dismissed {
		state.choices = nil
	}
}

func (m *editorApp) exportBackup() {
	path, err := app.SaveFile(app.SaveFileOptions{
		Title:            "导出 EditorX 会话备份",
		SuggestedName:    "EditorX-" + time.Now().Format("20060102-150405") + ".editorx-backup.zip",
		DefaultExtension: "zip",
		Filters:          []app.FileFilter{{Name: "EditorX 压缩备份", Pattern: "*.zip"}},
	})
	if errors.Is(err, app.ErrCanceled) {
		return
	}
	if err != nil {
		m.backup.problem = "无法选择备份位置：" + err.Error()
		m.backup.selecting, m.backup.choices = false, nil
		return
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		m.backup.problem = "无法读取备份路径：" + err.Error()
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
		m.backup.problem = "所选文件已关闭，请重新选择。"
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
		if err := archive.Close(); err != nil {
			return err
		}
		return ctx.Err()
	})
}

func readEditorBackup(path string) (savedSession, error) {
	if !strings.EqualFold(filepath.Ext(path), ".zip") {
		return savedSession{}, errors.New("请选择 EditorX 导出的 ZIP 备份")
	}
	archive, err := zip.OpenReader(path)
	if archive != nil {
		defer archive.Close()
	}
	if err != nil {
		return savedSession{}, fmt.Errorf("无法打开 ZIP 备份：%w", err)
	}
	entry, err := archive.Open(backupSessionEntry)
	if err != nil {
		return savedSession{}, fmt.Errorf("ZIP 备份中缺少会话数据：%w", err)
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
			err = errors.New("备份包含多余的会话数据")
		}
		return savedSession{}, err
	}
	if err := saved.validateVersion(); err != nil {
		return savedSession{}, err
	}
	return saved, nil
}

func (m *editorApp) importBackup() {
	paths, err := app.OpenFiles(app.OpenFileOptions{Title: "打开 EditorX 会话备份", Filters: []app.FileFilter{
		{Name: "EditorX 压缩备份", Pattern: "*.zip"},
	}})
	if errors.Is(err, app.ErrCanceled) {
		return
	}
	if err != nil {
		m.backup.problem = "无法选择会话备份：" + err.Error()
		return
	}
	if len(paths) == 0 {
		return
	}
	path := paths[0]
	m.backup = editorBackupState{request: ui.NewEffect[editorBackupResult, int](func(ctx context.Context) (editorBackupResult, error) {
		saved, err := readEditorBackup(path)
		if err != nil {
			return editorBackupResult{}, err
		}
		if len(saved.Windows) == 0 {
			return editorBackupResult{}, errors.New("备份中没有可恢复的窗口")
		}
		return editorBackupResult{windows: saved.Windows}, ctx.Err()
	})}
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
		state.request = nil
		state.problem = "会话备份操作失败：" + err.Error()
		return
	}
	if result.path != "" {
		state.request = nil
		size := fmt.Sprintf("%.1f KB", float64(result.size)/1024)
		if result.size >= 1<<20 {
			size = fmt.Sprintf("%.1f MB", float64(result.size)/(1<<20))
		}
		state.message = "备份已保存 · " + size + " · " + result.path
		return
	}
	// ZIP 读取在后台完成。将窗口创建投递给事件循环，普通重绘也能触发恢复。
	m.post(func() {
		defer func() {
			state.request = nil
			m.invalidator.Invalidate()
		}()
		if m.desktop.exiting {
			state.problem = "EditorX 正在退出，请稍后恢复备份。"
			return
		}
		count := 0
		for _, window := range result.windows {
			window.AI = nil
			restored := newEditorApp()
			restored.desktop = m.desktop
			restored.dataDirectory = m.dataDirectory
			restored.editorPrefs = m.editorPrefs
			restored.keyBindings = m.keyBindings
			restored.restoreSession(window)
			restored.applyThemePreference(m.desktop.preferences.Theme)
			if err := openEditorWindow(m.desktop.application, restored); err != nil {
				state.problem = fmt.Sprintf("已恢复 %d 个窗口，无法打开其余窗口：%v", count, err)
				return
			}
			m.store.attach(restored)
			count++
		}
		state.message = fmt.Sprintf("已在 %d 个新窗口中恢复备份", count)
	})
}
