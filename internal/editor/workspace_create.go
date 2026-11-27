package editor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vivaui/viva/ui"
)

type projectFolderDialog struct {
	open               bool
	directory, problem string
	name               ui.Field
}

func (m *editorApp) projectCreateActions(c ui.C, menu *ui.MenuItems, directory string) {
	if menu.Item(m.menuText("新建文件")).Icon(fileIcon).Clicked() {
		c.After(func() { m.openTemplatePicker(directory, false) })
	}
	if menu.Item(m.menuText("新建文件夹")).Icon(folderIcon).Clicked() {
		c.After(func() { m.projectFolder = &projectFolderDialog{open: true, directory: directory} })
	}
}

func (m *editorApp) showProjectFolderDialog(c ui.C) {
	state := m.projectFolder
	if state == nil {
		return
	}
	submitted := false
	dismissed := ui.ModalWith(c, &state.open, uiText("新建文件夹"), ui.ModalSpec{Width: 460}, func() {
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			ui.Wrap(c, state.directory).FillWidth().FontSize(12).Muted()
			submitted = ui.TextField(c.Part("name"), &state.name, uiText("文件夹名称"), uiText("输入文件夹名称")).FillWidth().Height(36).AutoFocus().Submitted()
			if state.problem != "" {
				ui.Wrap(c, state.problem).FillWidth().FontSize(12).Color(m.theme.Danger)
			}
		})
	}, func() {
		if ui.Button(c, uiText("取消")).Ghost().Clicked() {
			c.After(func() { m.projectFolder = nil })
		}
		ready := strings.TrimSpace(state.name.Text()) != ""
		if ui.Button(c, uiText("创建文件夹")).Disabled(!ready).Clicked() || ready && submitted {
			c.After(func() { m.createProjectFolder(state) })
		}
	})
	if dismissed {
		m.projectFolder = nil
	}
}

func (m *editorApp) createProjectFolder(state *projectFolderDialog) {
	if _, composing := state.name.GetEditor().Composition(); composing {
		state.problem = uiText("请先完成中文选词")
		return
	}
	name := strings.TrimSpace(state.name.Text())
	if !validWindowsEntryName(name) {
		state.problem = uiText("请输入有效的 Windows 文件夹名称")
		return
	}
	path := filepath.Join(state.directory, name)
	if err := os.Mkdir(path, 0755); err != nil {
		if errors.Is(err, os.ErrExist) {
			state.problem = uiText("目录中已有同名文件或文件夹，请换一个名称")
		} else {
			state.problem = fmt.Sprintf(uiText("无法创建文件夹：%v"), err)
		}
		return
	}
	m.projectFolder = nil
	if root := m.projectRoot(state.directory); root != "" {
		m.refreshProject(m.workspace.project(root), path)
	}
	m.status = uiText("已创建文件夹 ") + name
}
