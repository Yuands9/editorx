package editor

import (
	"fmt"
	"os"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

type editorDesktop struct {
	application *app.Application
	store       *sessionStore
	preferences applicationPreferences
	tray        *app.StatusItem
	lastActive  *editorApp
	exiting     bool
	updateFile  string
	updated     installedEditorUpdate
}

func (d *editorDesktop) syncTray() error {
	if !d.preferences.ShowTrayIcon {
		if d.tray != nil {
			d.tray.Remove()
			d.tray = nil
		}
		return nil
	}
	if d.tray != nil {
		return nil
	}
	item, err := d.application.AddStatusItem(app.StatusItemConfig{
		Tooltip: applicationName,
		Icon:    app.StatusIcon{Size: applicationIcon.Size, RGBA: applicationIcon.RGBA},
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

func (d *editorDesktop) close(window *editorApp) {
	if d.exiting {
		return
	}
	d.lastActive = window
	if d.preferences.ExitOnClose {
		d.quit()
		return
	}
	for _, other := range d.store.windows {
		other.nativeWindow.Hide()
	}
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
	d.flushForExit(0)
}

func (d *editorDesktop) flushForExit(index int) {
	for index < len(d.store.windows) {
		window := d.store.windows[index]
		index++
		if window.nativeWindow.Closed() {
			continue
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
