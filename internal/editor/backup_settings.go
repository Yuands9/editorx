package editor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
)

var backupFileFilters = []app.FileFilter{
	{Name: "EditorX 会话备份", Pattern: "*.editorx-backup.json;*.json"},
}

type editorBackupResult struct {
	path    string
	windows []windowSession
}

type editorBackupState struct {
	request *ui.EffectState[editorBackupResult, int]
	message string
	problem string
}

func (m *editorApp) backupSettings(c ui.C) {
	working := m.backup.request != nil
	m.applicationSetting(c.Part("export"), "导出会话备份",
		"保存已打开的标签、草稿、分组和项目列表。", func() {
			if ui.Button(c, "导出备份").Height(32).Disabled(working).Clicked() {
				c.After(m.exportBackup)
			}
		})
	m.applicationSetting(c.Part("restore"), "恢复会话备份",
		"在新窗口恢复备份中的工作区。", func() {
			if ui.Button(c, "选择备份…").Outline().Height(32).Disabled(working || m.desktop == nil).Clicked() {
				c.After(m.importBackup)
			}
		})
	m.applicationSetting(c.Part("directory"), "本地会话数据",
		"EditorX 会自动保存会话和草稿。", func() {
			if ui.Button(c, "打开数据目录").Outline().Height(32).Clicked() {
				c.After(func() {
					if err := app.OpenPath(m.dataDirectory); err != nil {
						m.backup.problem = "无法打开数据目录：" + err.Error()
					}
				})
			}
		})
	switch {
	case working:
		ui.Text(c, "正在处理会话备份…").Muted().FontSize(12)
	case m.backup.problem != "":
		ui.Wrap(c, m.backup.problem).FillWidth().Color(m.theme.Danger).FontSize(12)
	case m.backup.message != "":
		ui.Wrap(c, m.backup.message).FillWidth().Color(m.theme.Success).FontSize(12)
	}
}

func (m *editorApp) exportBackup() {
	path, err := app.SaveFile(app.SaveFileOptions{
		Title:         "导出 EditorX 会话备份",
		SuggestedName: "EditorX-" + time.Now().Format("20060102-150405") + ".editorx-backup.json",
		Filters:       backupFileFilters,
	})
	if errors.Is(err, app.ErrCanceled) {
		return
	}
	if err != nil {
		m.backup.problem = "无法选择备份位置：" + err.Error()
		return
	}

	windows := []*editorApp{m}
	if m.store != nil {
		windows = m.store.windows
	}
	saved := savedSession{Version: editorSessionVersion}
	for _, window := range windows {
		if !window.windowClosed {
			state := window.snapshotSession()
			state.AI = nil
			saved.Windows = append(saved.Windows, state)
		}
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		m.backup.problem = "无法读取备份路径：" + err.Error()
		return
	}
	m.backup = editorBackupState{request: ui.NewEffect[editorBackupResult, int](func(ctx context.Context) (editorBackupResult, error) {
		data, err := json.Marshal(saved)
		if err != nil {
			return editorBackupResult{}, err
		}
		if err := ctx.Err(); err != nil {
			return editorBackupResult{}, err
		}
		return editorBackupResult{path: absolute}, writeFile(absolute, data)
	})}
}

func (m *editorApp) importBackup() {
	paths, err := app.OpenFiles(app.OpenFileOptions{Title: "打开 EditorX 会话备份", Filters: backupFileFilters})
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
		data, err := os.ReadFile(path)
		if err != nil {
			return editorBackupResult{}, err
		}
		saved, err := decodeEditorSession(data)
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
	state.request = nil
	if err != nil {
		state.problem = "会话备份操作失败：" + err.Error()
		return
	}
	if result.path != "" {
		state.message = "备份已保存到 " + result.path
		return
	}
	c.After(func() {
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
