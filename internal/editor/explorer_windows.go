package editor

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

var explorerShellExecute = syscall.NewLazyDLL("shell32.dll").NewProc("ShellExecuteW")

func (m *editorApp) openExplorerFolder(path string) {
	info, err := os.Stat(path)
	if err != nil {
		m.fail(fmt.Errorf("无法打开文件夹：%w", err))
		return
	}
	if !info.IsDir() {
		m.fail(fmt.Errorf("路径不是文件夹：%s", path))
		return
	}
	directory, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		m.fail(err)
		return
	}
	verb, _ := syscall.UTF16PtrFromString("explore")
	result, _, _ := explorerShellExecute.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(directory)), 0, 0, 1)
	if result <= 32 {
		m.fail(fmt.Errorf("资源管理器无法打开 %s（错误码 %d）", path, result))
	}
}
