package editor

import (
	"fmt"
	"os"
	"slices"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

type editorDesktop struct {
	application          *app.Application
	store                *sessionStore
	preferences          applicationPreferences
	tray                 *app.StatusItem
	lastActive           *editorApp
	exiting              bool
	updateFile           string
	updated              installedEditorUpdate
	update               editorUpdateState
	watchPasswordSession func() bool
}

func (d *editorDesktop) syncTray() error {
	if !d.preferences.ShowTrayIcon {
		if d.tray != nil {
			d.tray.Remove()
			d.tray = nil
		}
		return nil
	}
	logo := themedApplicationIcon(editorThemeIsLight(d.preferences.Theme))
	icon := app.StatusIcon{Size: logo.Size, RGBA: logo.RGBA}
	if d.tray != nil {
		return d.tray.SetIcon(icon)
	}
	item, err := d.application.AddStatusItem(app.StatusItemConfig{
		Tooltip: applicationName,
		Icon:    icon,
		OnClick: func(click app.StatusClick) {
			if click.Button == gfx.PointerButtonPrimary {
				d.present(nil)
			}
		},
		Menu: func() []app.MenuItem {
			return []app.MenuItem{
				{Title: "显示 EditorX", Disabled: d.exiting, OnSelect: func() { d.present(nil) }},
				{Title: "设置…", Disabled: d.exiting, OnSelect: func() {
					if window := d.present(nil); window != nil {
						window.openApplicationSettings()
					}
				}},
				{Separator: true},
				{Title: "退出 EditorX", Disabled: d.exiting, OnSelect: d.quit},
			}
		},
	})
	if err != nil {
		return err
	}
	d.tray = item
	return nil
}

func (d *editorDesktop) present(paths []string) *editorApp {
	if d.exiting {
		return nil
	}
	target := d.lastActive
	if target != nil && target.nativeWindow.Closed() {
		target = nil
	}
	for _, window := range d.store.windows {
		if window.nativeWindow.Closed() {
			continue
		}
		if target == nil {
			target = window
		}
		// Opening a file from the shell selects its existing tab in any window.
		if len(paths) == 1 && window.tabAtPath(paths[0]) != nil && target.tabAtPath(paths[0]) == nil {
			target = window
		}
	}
	if target == nil {
		return nil
	}
	for _, window := range d.store.windows {
		if window != target && window.nativeWindow.Hidden() {
			window.nativeWindow.Show()
		}
	}
	target.nativeWindow.Show()
	d.lastActive = target
	target.openLaunchPaths(paths)
	if len(paths) > 0 {
		target.invalidator.Invalidate()
	}
	return target
}

func (d *editorDesktop) close(window *editorApp, allow func()) {
	if d.exiting || window.windowClosed || window.windowClosePending || window.confirm {
		return
	}
	if window.largeEditOperationPending() {
		window.invalidator.Invalidate()
		return
	}
	d.lastActive = window
	if len(d.store.windows) == 1 {
		d.closeLastWindow(window)
		return
	}
	if window.appSettings.open {
		window.closeSettings()
	}
	window.windowClosePending = true
	window.flushOfficeTabs(0, func(err error) {
		window.windowClosePending = false
		if err != nil {
			window.fail(err)
			window.invalidator.Invalidate()
			return
		}
		d.closeWindowAfterDrafts(window, slices.Clone(window.tabs), allow)
		window.invalidator.Invalidate()
	})
}

func (d *editorDesktop) lockPasswordVaults() {
	for _, window := range d.store.windows {
		if window.passwordVault != nil {
			window.passwordVault.lock()
			window.invalidator.Invalidate()
		}
	}
}

func (d *editorDesktop) closeLastWindow(window *editorApp) {
	d.lastActive = window
	if d.preferences.ExitOnClose {
		d.quit()
		return
	}
	window.nativeWindow.Hide()
}

func (d *editorDesktop) closeWindowAfterDrafts(window *editorApp, remaining []*editorTab, allow func()) {
	if d.exiting || window.windowClosed {
		return
	}
	for index, tab := range remaining {
		if tab.modified() && slices.Contains(window.tabs, tab) {
			window.requestTab(tab, func() {
				d.closeWindowAfterDrafts(window, remaining[index+1:], allow)
			})
			return
		}
	}

	previous := d.store.windows
	index := slices.Index(previous, window)
	if index < 0 {
		return
	}
	if len(previous) == 1 {
		d.closeLastWindow(window)
		return
	}
	d.store.windows = slices.Delete(slices.Clone(previous), index, index+1)
	if err := d.store.save(); err != nil {
		d.store.windows = previous
		window.fail(fmt.Errorf("无法保存本次会话，窗口保持打开：%w", err))
		window.invalidator.Invalidate()
		return
	}
	if d.lastActive == window {
		d.lastActive = nil
	}
	allow()
}

func (d *editorDesktop) restartForUpdate(path string) {
	if d.exiting || hasWordEditor {
		return
	}
	d.updateFile = path
	d.quit()
}

func (d *editorDesktop) quit() {
	if d.exiting {
		return
	}
	d.exiting = true
	d.lockPasswordVaults()
	d.flushForExit(0)
}

func (d *editorDesktop) flushForExit(index int) {
	for index < len(d.store.windows) {
		window := d.store.windows[index]
		index++
		if window.nativeWindow.Closed() {
			continue
		}
		if window.largeEditOperationPending() {
			d.cancelExit(window, fmt.Errorf("请等待大文件保存完成"))
			return
		}
		if window.appSettings.open {
			window.closeSettings()
		}
		window.flushOfficeTabs(0, func(err error) {
			if err != nil {
				d.cancelExit(window, err)
				return
			}
			d.flushForExit(index)
		})
		return
	}
	if err := d.store.save(); err != nil {
		d.cancelExit(d.lastActive, fmt.Errorf("无法保存本次会话：%w", err))
		return
	}
	if d.updateFile != "" {
		executable, err := os.Executable()
		if err == nil {
			d.updated, err = replaceEditorExecutable(executable, d.updateFile)
		}
		if err != nil {
			d.cancelExit(d.lastActive, fmt.Errorf("无法替换 EditorX 程序：%w", err))
			return
		}
	}
	if d.tray != nil {
		d.tray.Remove()
		d.tray = nil
	}
	d.application.Quit()
}

func (d *editorDesktop) cancelExit(window *editorApp, err error) {
	d.exiting, d.updateFile = false, ""
	if window != nil && !window.nativeWindow.Closed() {
		d.lastActive = window
	}
	if target := d.present(nil); target != nil {
		target.fail(err)
		target.invalidator.Invalidate()
	}
}
