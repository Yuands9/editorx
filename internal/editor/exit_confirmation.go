package editor

import (
	"fmt"

	"github.com/vivaui/viva/ui"
)

const visibleExitFileLimit = 5

type exitSaveFile struct {
	window *editorApp
	tab    *editorTab
}

type exitSaveConfirmation struct {
	window                       *editorApp
	files                        []exitSaveFile
	open, focus, saving, dontAsk bool
}

func (d *editorDesktop) confirmExitSave() {
	if d.restart || d.updateFile != "" || d.preferences.SkipExitSaveConfirmation {
		d.finishExit()
		return
	}
	var files []exitSaveFile
	for _, window := range d.store.windows {
		if window.windowClosed {
			continue
		}
		for _, tab := range window.tabs {
			if tab.modified() {
				files = append(files, exitSaveFile{window, tab})
			}
		}
	}
	if len(files) == 0 {
		d.finishExit()
		return
	}
	window := d.lastActive
	if window == nil || window.windowClosed {
		window = files[0].window
	}
	d.lastActive = window
	d.exitConfirmation = &exitSaveConfirmation{window: window, files: files, open: true, focus: true}
	window.nativeWindow.Show()
	window.invalidator.Invalidate()
}

func (d *editorDesktop) saveExitFiles(prompt *exitSaveConfirmation, index int) {
	if d.exitConfirmation != prompt || !prompt.open {
		return
	}
	for index < len(prompt.files) && !prompt.files[index].tab.modified() {
		index++
	}
	if index == len(prompt.files) {
		d.finishExit()
		return
	}

	file := prompt.files[index]
	if native := file.window.nativeWindow; file.tab.largeText != nil && file.window != prompt.window &&
		(native.Hidden() || native.Minimized()) {
		// Large-file effects are driven by the owner window's paint.
		native.Show()
		prompt.window.nativeWindow.Show()
	}
	complete := func(saved bool) {
		if d.exitConfirmation != prompt || !prompt.open {
			return
		}
		if !saved {
			file.window.activate(file.tab)
			d.cancelExit(file.window, nil)
			return
		}
		next := func() { d.saveExitFiles(prompt, index+1) }
		if prompt.window.post != nil {
			prompt.window.post(next)
		} else {
			next()
		}
	}
	save := func() {
		file.window.saveBeforeClosing(file.tab, complete)
		file.window.invalidator.Invalidate()
	}
	if state := file.tab.largeText; state != nil && state.editing == nil {
		// An inactive large-file draft may still need its editing buffer restored.
		state.resumeEdit = false
		if state.edit == nil && !file.window.startLargeTextEdit(file.tab, file.tab.doc.encoding) {
			complete(false)
			return
		}
		state.afterOpen = func(opened bool) {
			if d.exitConfirmation != prompt || !prompt.open {
				return
			}
			if !opened {
				complete(false)
				return
			}
			save()
		}
		file.window.invalidator.Invalidate()
		return
	}
	save()
}

func (m *editorApp) exitConfirmationDialog(c ui.C) {
	d := m.desktop
	if d == nil || d.exitConfirmation == nil || d.exitConfirmation.window != m {
		return
	}
	prompt := d.exitConfirmation
	description := fmt.Sprintf(uiText("有 %d 个文件尚未保存。"), len(prompt.files))
	dismissed := ui.ModalWith(c, &prompt.open, uiText("退出 EditorX 前保存？"), ui.ModalSpec{Description: description, Width: 480}, func() {
		ui.Column(c.Part("files")).FillWidth().Gap(4).Children(func() {
			for _, file := range prompt.files[:min(visibleExitFileLimit, len(prompt.files))] {
				ui.Text(c, file.tab.tabTitle()).FillWidth().FontSize(13).Ellipsis()
			}
			if len(prompt.files) > visibleExitFileLimit {
				ui.Text(c, "…").Muted().FontSize(13)
			}
		})
		ui.Checkbox(c.Part("exit-preference"), &prompt.dontAsk, uiText("以后退出时不再询问")).Height(28).MarginTop(8).Disabled(prompt.saving)
	}, func() {
		if ui.Button(c, uiText("保留草稿并退出")).Outline().Disabled(prompt.saving).Clicked() {
			c.After(d.finishExit)
		}
		save := ui.Button(c, uiText("保存并退出")).Disabled(prompt.saving)
		if prompt.focus {
			save.AutoFocus()
			prompt.focus = false
		}
		if save.Clicked() {
			c.After(func() {
				if d.exitConfirmation == prompt && prompt.open && !prompt.saving {
					prompt.saving = true
					d.saveExitFiles(prompt, 0)
				}
			})
		}
	})
	if dismissed {
		c.After(func() {
			d.cancelExit(m, nil)
			m.focusActiveSource()
		})
	}
}
