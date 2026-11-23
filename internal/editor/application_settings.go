package editor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/vivaui/viva/app"
)

const (
	applicationSettingsFile = "application-settings.json"
	themeDark               = "dark"
	themeLight              = "light"
	themeSystem             = "system"
)

type applicationPreferences struct {
	ShowTrayIcon bool              `json:"showTrayIcon"`
	ExitOnClose  bool              `json:"exitOnClose"`
	Theme        string            `json:"theme,omitempty"`
	Editor       editorPreferences `json:"editor"`
}

func readApplicationPreferences(directory string) (applicationPreferences, error) {
	defaults := applicationPreferences{ShowTrayIcon: true, ExitOnClose: true, Editor: defaultEditorPreferences()}
	data, err := os.ReadFile(filepath.Join(directory, applicationSettingsFile))
	if errors.Is(err, os.ErrNotExist) {
		return defaults, nil
	}
	if err != nil {
		return defaults, err
	}
	preferences := defaults
	if err := json.Unmarshal(data, &preferences); err != nil {
		return defaults, err
	}
	if preferences.Theme != themeDark && preferences.Theme != themeLight && preferences.Theme != themeSystem {
		preferences.Theme = ""
	}
	preferences.Editor = preferences.Editor.normalized()
	return preferences, nil
}

func (preferences applicationPreferences) resolvedTheme() string {
	if preferences.Theme != themeSystem {
		return preferences.Theme
	}
	switch app.SystemAppearance() {
	case app.AppearanceLight:
		return themeLight
	case app.AppearanceDark:
		return themeDark
	default:
		return ""
	}
}

func (preferences applicationPreferences) save(directory string) error {
	data, err := json.MarshalIndent(preferences, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	return writeFile(filepath.Join(directory, applicationSettingsFile), data)
}

func (m *editorApp) applicationPreferences() (applicationPreferences, error) {
	if m.desktop != nil {
		preferences := m.desktop.preferences
		preferences.Editor = preferences.Editor.normalized()
		return preferences, nil
	}
	return readApplicationPreferences(m.dataDirectory)
}

func (m *editorApp) saveApplicationSettings() {
	settings := &m.appSettings
	if settings.draft == settings.original {
		return
	}
	preferences, err := m.applicationPreferences()
	if err != nil {
		settings.problem = "无法读取设置：" + err.Error()
		settings.draft = settings.original
		return
	}
	// 一个窗口只写入它修改的选项，保留其他窗口刚刚保存的设置。
	trayChanged := settings.draft.ShowTrayIcon != settings.original.ShowTrayIcon
	if trayChanged {
		preferences.ShowTrayIcon = settings.draft.ShowTrayIcon
	}
	if settings.draft.ExitOnClose != settings.original.ExitOnClose {
		preferences.ExitOnClose = settings.draft.ExitOnClose
	}
	if settings.draft.Theme != settings.original.Theme {
		preferences.Theme = settings.draft.Theme
	}
	preferences.Editor = preferences.Editor.withChanges(settings.original.Editor, settings.draft.Editor)
	if err := preferences.save(m.dataDirectory); err != nil {
		settings.problem = "无法保存设置：" + err.Error()
		settings.draft = settings.original
		return
	}
	settings.original, settings.draft, settings.problem = preferences, preferences, ""
	if err := m.applyApplicationPreferences(preferences); err != nil {
		settings.problem = fmt.Sprintf("设置已保存，无法显示系统托盘图标：%v", err)
	}
	m.status = "设置已保存"
}

func (m *editorApp) chooseTheme(theme string) {
	if m.dataDirectory == "" {
		directory, err := editorDataDirectory()
		if err != nil {
			m.fail(fmt.Errorf("无法读取设置目录：%w", err))
			return
		}
		m.dataDirectory = directory
	}
	preferences, err := m.applicationPreferences()
	if err != nil {
		m.fail(fmt.Errorf("无法读取设置：%w", err))
		return
	}
	if preferences.Theme == theme {
		return
	}
	preferences.Theme = theme
	if err := preferences.save(m.dataDirectory); err != nil {
		m.fail(fmt.Errorf("无法保存颜色主题：%w", err))
		return
	}
	m.status = "已切换颜色主题"
	if err := m.applyApplicationPreferences(preferences); err != nil {
		m.fail(err)
	}
}

func (m *editorApp) applyApplicationPreferences(preferences applicationPreferences) error {
	theme := preferences.resolvedTheme()
	if m.desktop != nil {
		desktop := m.desktop
		trayChanged := desktop.preferences.ShowTrayIcon != preferences.ShowTrayIcon
		desktop.preferences = preferences
		for _, window := range desktop.store.windows {
			if !window.windowClosed {
				window.applyThemePreference(theme)
				window.applyEditorPreferences(preferences.Editor)
				if window.appSettings.open {
					window.appSettings.original, window.appSettings.draft = preferences, preferences
				}
				window.invalidator.Invalidate()
			}
		}
		if trayChanged {
			return desktop.syncTray()
		}
		return nil
	}
	m.applyThemePreference(theme)
	m.applyEditorPreferences(preferences.Editor)
	m.invalidator.Invalidate()
	return nil
}
