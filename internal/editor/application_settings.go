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
	themeSystem             = "system"
	themeDark               = "dark"
	themeLight              = "light"
)

type applicationPreferences struct {
	ShowTrayIcon         bool              `json:"showTrayIcon"`
	ExitOnClose          bool              `json:"exitOnClose"`
	SkipSaveConfirmation bool              `json:"skipSaveConfirmation,omitempty"`
	Theme                string            `json:"theme,omitempty"`
	Editor               editorPreferences `json:"editor"`
	Shortcuts            shortcutOverrides `json:"shortcuts,omitempty"`
}

func readApplicationPreferences(directory string) (applicationPreferences, error) {
	defaults := applicationPreferences{ShowTrayIcon: true, ExitOnClose: true, Theme: themeSystem, Editor: defaultEditorPreferences()}
	data, err := os.ReadFile(filepath.Join(directory, applicationSettingsFile))
	if errors.Is(err, os.ErrNotExist) {
		return defaults, nil
	}
	if err != nil {
		return defaults, err
	}
	preferences := defaults
	// 缺少新字段时，让旧版折行选项参与迁移。
	preferences.Editor.WrapFileTypes = nil
	if err := json.Unmarshal(data, &preferences); err != nil {
		return defaults, err
	}
	if preferences.Theme != themeSystem && preferences.Theme != themeDark && preferences.Theme != themeLight {
		preferences.Theme = themeSystem
	}
	preferences.Editor = preferences.Editor.normalized()
	return preferences, nil
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
	if settings.draft.ShowTrayIcon == settings.original.ShowTrayIcon && settings.draft.ExitOnClose == settings.original.ExitOnClose &&
		settings.draft.SkipSaveConfirmation == settings.original.SkipSaveConfirmation &&
		settings.draft.Theme == settings.original.Theme && settings.draft.Editor.equal(settings.original.Editor) {
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
	if settings.draft.SkipSaveConfirmation != settings.original.SkipSaveConfirmation {
		preferences.SkipSaveConfirmation = settings.draft.SkipSaveConfirmation
	}
	if settings.draft.Theme != settings.original.Theme {
		preferences.Theme = settings.draft.Theme
	}
	preferences.Editor = preferences.Editor.withChanges(settings.original.Editor, settings.draft.Editor)
	m.persistApplicationSettings(preferences)
}

func (m *editorApp) persistApplicationSettings(preferences applicationPreferences) bool {
	settings := &m.appSettings
	if err := preferences.save(m.dataDirectory); err != nil {
		settings.problem = "无法保存设置：" + err.Error()
		settings.draft = settings.original
		return false
	}
	settings.original, settings.draft, settings.problem = preferences, preferences, ""
	if err := m.applyApplicationPreferences(preferences); err != nil {
		settings.problem = fmt.Sprintf("设置已保存，无法更新系统托盘图标：%v", err)
	}
	m.status = "设置已保存"
	return true
}

func (m *editorApp) rememberSkippingSaveConfirmation() error {
	if m.dataDirectory == "" {
		directory, err := editorDataDirectory()
		if err != nil {
			return err
		}
		m.dataDirectory = directory
	}
	preferences, err := m.applicationPreferences()
	if err != nil {
		return err
	}
	if !preferences.SkipSaveConfirmation {
		preferences.SkipSaveConfirmation = true
		if !m.persistApplicationSettings(preferences) {
			return errors.New(m.appSettings.problem)
		}
	}
	return nil
}

func (m *editorApp) cycleTheme() {
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
	next, label := themeLight, "浅色"
	if editorThemeIsLight(preferences.Theme) {
		next, label = themeDark, "深色"
	}
	preferences.Theme = next
	if err := preferences.save(m.dataDirectory); err != nil {
		m.fail(fmt.Errorf("无法保存颜色主题：%w", err))
		return
	}
	m.status = "颜色主题 · " + label
	if err := m.applyApplicationPreferences(preferences); err != nil {
		m.fail(err)
	}
}

func (m *editorApp) applyApplicationPreferences(preferences applicationPreferences) error {
	app.SetNativeAppearance(themeAppearance(preferences.Theme))
	if m.desktop != nil {
		desktop := m.desktop
		trayChanged := desktop.preferences.ShowTrayIcon != preferences.ShowTrayIcon ||
			desktop.tray != nil && desktop.preferences.Theme != preferences.Theme
		desktop.preferences = preferences
		for _, window := range desktop.store.windows {
			if !window.windowClosed {
				window.applyThemePreference(preferences.Theme)
				window.applyEditorPreferences(preferences.Editor)
				window.keyBindings = preferences.Shortcuts
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
	m.applyThemePreference(preferences.Theme)
	m.applyEditorPreferences(preferences.Editor)
	m.keyBindings = preferences.Shortcuts
	m.invalidator.Invalidate()
	return nil
}
