package editor

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
)

const settingsExportFormat = "EditorX settings"
const maxSettingsExportBytes = 1 << 20

type settingsExport struct {
	Format      string          `json:"format"`
	Version     int             `json:"version"`
	Preferences json.RawMessage `json:"preferences"`
}

func encodeSettingsExport(preferences applicationPreferences) ([]byte, error) {
	payload, err := json.Marshal(preferences)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(settingsExport{Format: settingsExportFormat, Version: 1, Preferences: payload}, "", "  ")
}

func decodeSettingsExport(data []byte) (applicationPreferences, error) {
	var backup settingsExport
	if err := json.Unmarshal(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}), &backup); err != nil {
		return applicationPreferences{}, err
	}
	payload := bytes.TrimSpace(backup.Preferences)
	if backup.Format != settingsExportFormat || backup.Version != 1 || len(payload) == 0 || payload[0] != '{' {
		return applicationPreferences{}, errors.New(uiText("请选择受支持的 EditorX 配置备份。"))
	}
	return decodeApplicationPreferences(payload)
}

func (m *editorApp) configurationTransferSettings(c ui.C) {
	settings := &m.appSettings
	ui.Column(c).FillWidth().Pad(14).Gap(8).Radius(12).Border(1, m.theme.Border).
		Background(m.theme.Background).Children(func() {
		ui.Text(c, uiText("应用配置")).FontSize(14).Bold()
		ui.Wrap(c, uiText("用 JSON 备份外观、编辑器、快捷键等应用设置。AI 配置、密码本、文件和草稿另行保存。")).
			FillWidth().FontSize(12).Muted()
		ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
			ui.Text(c.Part("notice"), settings.transferNotice).Grow(1).MinWidth(0).Ellipsis().FontSize(12).Color(m.theme.Success)
			if ui.Button(c.Part("export"), uiText("导出配置")).Secondary().Height(32).Shrink(0).Clicked() {
				c.After(m.exportConfiguration)
			}
			if ui.Button(c.Part("import"), uiText("导入配置")).Secondary().Height(32).Shrink(0).Clicked() {
				c.After(m.chooseConfiguration)
			}
			if ui.Button(c.Part("defaults"), uiText("恢复默认配置")).Icon(refreshIcon).Secondary().Height(32).Shrink(0).Clicked() {
				c.After(func() {
					defaults := defaultApplicationPreferences()
					settings.importPreferences, settings.restoreDefaults = &defaults, true
					settings.problem, settings.transferNotice = "", ""
				})
			}
		})
	})
}

func (m *editorApp) exportConfiguration() {
	path, err := app.SaveFile(app.SaveFileOptions{
		Title: uiText("导出配置"), SuggestedName: "EditorX-settings.json", DefaultExtension: "json",
		Filters: []app.FileFilter{{Name: "JSON", Pattern: "*.json"}},
	})
	if errors.Is(err, app.ErrCanceled) {
		return
	}
	if err == nil {
		err = m.writeConfiguration(path)
	}
	m.configurationTransferResult("settings.export", "配置已导出。", "无法导出配置：%v", err)
}

func (m *editorApp) writeConfiguration(path string) error {
	if strings.EqualFold(filepath.Base(path), applicationSettingsFile) {
		return errors.New(uiText("请使用其他文件名保存配置备份。"))
	}
	preferences, err := m.applicationPreferences()
	if err != nil {
		return err
	}
	data, err := encodeSettingsExport(preferences)
	if err != nil {
		return err
	}
	return writeFile(path, append(data, '\n'))
}

func (m *editorApp) chooseConfiguration() {
	paths, err := app.OpenFiles(app.OpenFileOptions{
		Title: uiText("导入配置"), Filters: []app.FileFilter{{Name: "JSON", Pattern: "*.json"}},
	})
	if errors.Is(err, app.ErrCanceled) || err == nil && len(paths) == 0 {
		return
	}
	if err != nil {
		m.configurationTransferResult("settings.import", "", "无法导入配置：%v", err)
		return
	}
	data, err := readTextAsset(paths[0], maxSettingsExportBytes)
	if err != nil {
		m.configurationTransferResult("settings.import", "", "无法导入配置：%v", err)
		return
	}
	preferences, err := decodeSettingsExport(data)
	if err != nil {
		m.configurationTransferResult("settings.import", "", "无法导入配置：%v", err)
		return
	}
	m.appSettings.problem, m.appSettings.transferNotice = "", ""
	m.appSettings.importPreferences, m.appSettings.restoreDefaults = &preferences, false
}

func (m *editorApp) configurationImportDialog(c ui.C) {
	settings := &m.appSettings
	preferences := settings.importPreferences
	if preferences == nil {
		return
	}
	title, description := "导入配置？", "导入会替换当前应用设置并应用到所有窗口。"
	confirm, notice, event := "确认导入", "配置已导入。", "settings.import"
	if settings.restoreDefaults {
		title, description = "恢复默认配置？", "将所有窗口的应用设置恢复为默认值。已打开的文件、草稿、AI 配置和密码本会保留。"
		confirm, notice, event = "确认恢复", "已恢复默认配置。", "settings.reset"
	}
	dismiss := func() {
		settings.importPreferences, settings.restoreDefaults = nil, false
	}
	open := true
	ui.ModalWith(c, &open, uiText(title), ui.ModalSpec{
		Description: uiText(description), Width: 460,
	}, nil, func() {
		if ui.Button(c.Part("cancel"), uiText("取消")).Outline().AutoFocus().Clicked() {
			c.After(dismiss)
		}
		if ui.Button(c.Part("accept"), uiText(confirm)).Clicked() {
			c.After(func() {
				dismiss()
				if m.persistApplicationSettings(*preferences) {
					settings.transferNotice = uiText(notice)
					m.debug.record(event, nil)
				}
			})
		}
	})
	if !open {
		dismiss()
	}
}

func (m *editorApp) configurationTransferResult(event, success, failure string, err error) {
	m.debug.record(event, err)
	settings := &m.appSettings
	settings.problem, settings.transferNotice = "", ""
	if err != nil {
		settings.problem = fmt.Sprintf(uiText(failure), err)
		return
	}
	settings.transferNotice = uiText(success)
}
