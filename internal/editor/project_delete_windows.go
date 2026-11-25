package editor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"unsafe"

	"github.com/vivaui/viva/ui"
)

const projectTrashPrefix = ".editorx-undo-"

type projectDeleteDialog struct {
	open, directory bool
	path            string
}

func (m *editorApp) projectDeleteAction(c ui.C, menu *ui.MenuItems, path string, directory bool) {
	if menu.Item("删除").Disabled(filepath.Dir(path) == path).Clicked() {
		c.After(func() {
			m.projectDelete = &projectDeleteDialog{open: true, directory: directory, path: path}
		})
	}
}

func (m *editorApp) showProjectDeleteDialog(c ui.C) {
	dialog := m.projectDelete
	if dialog == nil {
		return
	}
	title := "删除文件？"
	message := "将此文件移至 Windows 回收站。"
	if dialog.directory {
		title = "删除文件夹？"
		message = "文件夹及其中的内容将移至 Windows 回收站。"
	}
	cancel := func() { m.projectDelete = nil }
	if ui.ModalWith(c, &dialog.open, title, ui.ModalSpec{Width: 460}, func() {
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			ui.Wrap(c, dialog.path).FillWidth()
			ui.Wrap(c, message).FillWidth().Muted().FontSize(12)
		})
	}, func() {
		if ui.Button(c, "取消").Ghost().AutoFocus().Clicked() {
			c.After(cancel)
		}
		if ui.Button(c, "确认删除").Destructive().Clicked() {
			c.After(func() {
				m.projectDelete = nil
				if err := m.deleteProjectEntry(dialog.path); err != nil {
					m.fail(fmt.Errorf("无法删除 %s：%w", filepath.Base(dialog.path), err))
				}
			})
		}
	}) {
		cancel()
	}
}

func (m *editorApp) deleteProjectEntry(path string) error {
	if m.workspace == nil || m.projectRoot(path) == "" || filepath.Dir(path) == path {
		return errors.New("请选择项目中的文件或文件夹")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return err
	}
	resolved := filepath.Join(parent, filepath.Base(path))
	if gitPathWithin(path, m.dataDirectory) {
		return errors.New("这个目录包含 EditorX 正在使用的数据")
	}
	if data, err := filepath.EvalSymlinks(m.dataDirectory); err == nil && gitPathWithin(resolved, data) {
		return errors.New("这个目录包含 EditorX 正在使用的数据")
	}
	if err := recycleProjectEntry(resolved); err != nil {
		return err
	}
	for _, root := range slices.Clone(m.workspace.roots) {
		switch {
		case gitPathWithin(path, root.path):
			m.removeProject(root)
		case gitPathWithin(root.path, path):
			m.refreshProject(root, "")
		}
	}
	m.status = "已删除 " + filepath.Base(path)
	return nil
}

var projectShellFileOperation = syscall.NewLazyDLL("shell32.dll").NewProc("SHFileOperationW")

// SHFILEOPSTRUCTW is shared with the Windows shell.
type projectShellOperation struct {
	window   uintptr
	function uint32
	from, to *uint16
	flags    uint16
	aborted  int32
	mappings uintptr
	title    *uint16
}

func recycleProjectEntry(path string) error {
	const (
		deleteOperation     = 3
		silent              = 0x0004
		noConfirmation      = 0x0010
		allowUndo           = 0x0040 // Use the Windows Recycle Bin.
		noErrorUI           = 0x0400
		noConnectedElements = 0x2000
		warnPermanent       = 0x4000
	)
	from, err := syscall.UTF16FromString(path)
	if err != nil {
		return err
	}
	from = append(from, 0) // The shell expects a double-NUL-terminated path list.
	operation := projectShellOperation{function: deleteOperation, from: &from[0],
		flags: silent | noConfirmation | allowUndo | noErrorUI | noConnectedElements | warnPermanent}
	result, _, _ := projectShellFileOperation.Call(uintptr(unsafe.Pointer(&operation)))
	if result != 0 {
		return fmt.Errorf("Windows 回收站操作失败（%#x）", result)
	}
	if operation.aborted != 0 {
		return errors.New("已取消删除")
	}
	return nil
}

func isProjectTrashDirectory(entry os.DirEntry) bool {
	if !entry.IsDir() || !isProjectTrashName(entry.Name()) {
		return false
	}
	info, err := entry.Info()
	if err != nil {
		return false
	}
	attributes, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return ok && attributes.FileAttributes&syscall.FILE_ATTRIBUTE_HIDDEN != 0
}

func isProjectTrashName(name string) bool {
	suffix, ok := strings.CutPrefix(name, projectTrashPrefix)
	return ok && suffix != "" && strings.Trim(suffix, "0123456789") == ""
}
