package editor

import (
	"errors"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
)

func (m *editorApp) dataLocationSettings(c ui.C) {
	desktop := m.desktop
	canChange := desktop != nil && desktop.dataLocation.path != ""
	m.applicationSetting(c.Part("directory"), "当前数据目录", m.dataDirectory, func() {
		ui.Row(c.Part("actions")).Gap(6).Children(func() {
			if ui.Button(c.Part("open"), "打开文件夹").Ghost().Height(32).Disabled(m.dataDirectory == "").Clicked() {
				c.After(func() { m.openExplorerFolder(m.dataDirectory) })
			}
			if ui.Button(c.Part("change"), "更换位置…").Outline().Height(32).Disabled(!canChange).Clicked() {
				c.After(m.chooseDataLocation)
			}
		})
	})
	if !canChange && desktop != nil {
		ui.Wrap(c, "当前目录由 VIVA_EDITOR_DATA_DIR 指定。").FillWidth().Muted().FontSize(12)
	}
	if pending := desktop != nil && desktop.dataLocation.Pending != ""; pending {
		m.applicationSetting(c.Part("pending"), "重启后的数据目录", desktop.dataLocation.Pending, func() {
			ui.Row(c.Part("pending-actions")).Gap(6).Children(func() {
				if ui.Button(c.Part("cancel"), "取消更改").Ghost().Height(32).Clicked() {
					c.After(func() { m.saveDataLocation("") })
				}
				if ui.Button(c.Part("restart"), "迁移并重启").Height(32).Clicked() {
					c.After(func() {
						if !desktop.exiting {
							desktop.restart = true
							desktop.quit()
						}
					})
				}
			})
		})
		ui.Wrap(c, "重启时会复制会话、草稿、设置和已安装的组件，原数据保留为备份。").FillWidth().Muted().FontSize(12)
	}
}

func (m *editorApp) chooseDataLocation() {
	path, err := app.OpenDirectory(app.OpenDirectoryOptions{Title: "选择一个空文件夹存放 EditorX 数据"})
	if errors.Is(err, app.ErrCanceled) {
		return
	}
	if err != nil {
		m.appSettings.problem = "无法选择数据目录：" + err.Error()
		return
	}
	destination, err := editorDataDestination(m.dataDirectory, path)
	if err != nil {
		m.appSettings.problem = "无法使用所选目录：" + err.Error()
		return
	}
	m.saveDataLocation(destination)
}

func (m *editorApp) saveDataLocation(destination string) {
	if m.desktop == nil || m.desktop.dataLocation.path == "" {
		return
	}
	location := m.desktop.dataLocation
	location.Pending = destination
	if err := location.save(); err != nil {
		m.appSettings.problem = "无法保存数据目录设置：" + err.Error()
		return
	}
	m.desktop.dataLocation = location
	m.appSettings.problem = ""
	for _, window := range m.desktop.store.windows {
		window.invalidator.Invalidate()
	}
}
