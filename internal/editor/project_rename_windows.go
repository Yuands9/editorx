package editor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/vivaui/viva/ui"
)

type projectRenameDialog struct {
	open, directory bool
	path, problem   string
	name            ui.Field
}

func (m *editorApp) projectRenameAction(c ui.C, menu *ui.MenuItems, path string, directory bool) {
	if menu.Item(m.menuText("重命名")).Icon(modelEditIcon).Disabled(filepath.Dir(path) == path).Clicked() {
		c.After(func() {
			state := &projectRenameDialog{open: true, path: path, directory: directory}
			name := filepath.Base(path)
			state.name.SetText(name)
			selected := name
			if extension := filepath.Ext(name); !directory && extension != name {
				selected = strings.TrimSuffix(name, extension)
			}
			state.name.Select(0, utf8.RuneCountInString(selected))
			m.projectRename = state
		})
	}
}

func (m *editorApp) showProjectRenameDialog(c ui.C) {
	state := m.projectRename
	if state == nil {
		return
	}
	kind := "文件"
	if state.directory {
		kind = "文件夹"
	}
	closeDialog := func() {
		m.projectRename = nil
		m.focusActiveSource()
	}
	submitted := false
	dismissed := ui.ModalWith(c, &state.open, uiText("重命名")+kind, ui.ModalSpec{Width: 460}, func() {
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			ui.Wrap(c, filepath.Dir(state.path)).FillWidth().FontSize(12).Muted()
			field := ui.TextField(c.Part("name"), &state.name, kind+uiText("名称"), uiText("输入新名称")).FillWidth().Height(36).AutoFocus()
			submitted = field.Submitted()
			if state.problem != "" {
				ui.Wrap(c, state.problem).FillWidth().FontSize(12).Color(m.theme.Danger)
			}
		})
	}, func() {
		if ui.Button(c, uiText("取消")).Ghost().Clicked() {
			c.After(closeDialog)
		}
		name := strings.TrimSpace(state.name.Text())
		_, composing := state.name.GetEditor().Composition()
		ready := name != "" && !composing
		if ui.Button(c, uiText("重命名")).Disabled(!ready).Clicked() || ready && submitted {
			c.After(func() {
				if err := m.renameProjectEntry(state.path, name); err != nil {
					state.problem = err.Error()
					return
				}
				closeDialog()
			})
		}
	})
	if dismissed {
		closeDialog()
	}
}

func (m *editorApp) renameProjectEntry(path, name string) error {
	parent := filepath.Dir(path)
	if m.projectRoot(path) == "" || parent == path {
		return errors.New(uiText("请选择项目中的文件或文件夹"))
	}
	if !validWindowsEntryName(name) {
		return errors.New(uiText("请输入有效的 Windows 文件或文件夹名称"))
	}
	if name == filepath.Base(path) {
		return nil
	}
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return fmt.Errorf(uiText("无法访问所在文件夹：%w"), err)
	}
	resolved := filepath.Join(resolvedParent, filepath.Base(path))
	if gitPathWithin(path, m.dataDirectory) {
		return errors.New(uiText("这个目录包含 EditorX 正在使用的数据"))
	}
	if data, err := filepath.EvalSymlinks(m.dataDirectory); m.dataDirectory != "" && err == nil && gitPathWithin(resolved, data) {
		return errors.New(uiText("这个目录包含 EditorX 正在使用的数据"))
	}

	windows := []*editorApp{m}
	if m.store != nil {
		windows = m.store.windows
	}
	for _, window := range windows {
		for _, tab := range window.tabs {
			if state := tab.largeText; state != nil && state.saving != nil &&
				(gitPathWithin(path, tab.doc.path) || gitPathWithin(path, state.savePath)) {
				return errors.New(uiText("请等待大文件保存完成"))
			}
			if tab.preview != nil && tab.preview.pdf != nil && gitPathWithin(path, tab.doc.path) {
				tab.preview.close()
				if window.active == tab {
					defer window.openDocumentPreview(tab)
				}
			}
		}
	}
	// syscall.Rename uses MoveFileW, which refuses to overwrite an existing entry.
	err = syscall.Rename(resolved, filepath.Join(resolvedParent, name))
	if errors.Is(err, os.ErrExist) {
		return errors.New(uiText("目录中已有同名文件或文件夹，请换一个名称"))
	}
	if err != nil {
		return fmt.Errorf(uiText("无法重命名 %s：%w"), filepath.Base(path), err)
	}

	destination := filepath.Join(parent, name)
	renamePath := func(original string) string {
		if gitPathWithin(path, original) {
			relative, _ := filepath.Rel(path, original)
			return filepath.Join(destination, relative)
		}
		return original
	}
	if m.store != nil {
		for i := range m.store.history {
			item := &m.store.history[i]
			item.Path = renamePath(item.Path)
		}
	}
	for _, window := range windows {
		window.applyProjectRename(path, destination, renamePath)
	}
	m.status = uiText("已重命名为 ") + name
	return nil
}

func (m *editorApp) applyProjectRename(original, destination string, renamePath func(string) string) {
	for _, tab := range m.tabs {
		if state := tab.largeText; state != nil {
			if state.editing != nil && state.editing.basePath != "" {
				state.editing.basePath = renamePath(state.editing.basePath)
			}
			if state.editSession != nil && state.editSession.BasePath != "" {
				state.editSession.BasePath = renamePath(state.editSession.BasePath)
			}
		}
		path := renamePath(tab.doc.path)
		if path == tab.doc.path {
			continue
		}
		tab.doc.path = path
		if tab.html.view != nil {
			m.refreshHTML(tab)
		}
	}
	git := &m.tools.git
	if git.directory != "" && renamePath(git.directory) != git.directory {
		git.watch.close()
		*git = gitPanelState{}
	}
	if m.workspace != nil {
		var updatePaths func(*fileNode)
		updatePaths = func(node *fileNode) {
			if path := renamePath(node.path); path != node.path {
				node.path, node.name = path, filepath.Base(path)
			}
			for _, child := range node.children {
				updatePaths(child)
			}
		}
		for _, root := range m.workspace.roots {
			reveal := ""
			switch {
			case gitPathWithin(original, root.path):
			case gitPathWithin(root.path, original):
				reveal = destination
			default:
				continue
			}
			updatePaths(root)
			m.refreshProject(root, reveal)
		}
	}
	m.invalidator.Invalidate()
}
