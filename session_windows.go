package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var editorKernel = syscall.NewLazyDLL("kernel32.dll")
var editorUser = syscall.NewLazyDLL("user32.dll")

// 每个便携数据目录只有一个写入进程；进程异常结束时 Windows 自动释放锁。
func lockSessionDirectory(directory string) (func(), error) {
	hash := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(directory))))
	name, _ := syscall.UTF16PtrFromString(fmt.Sprintf("Local\\VivaEditorSession-%x", hash[:16]))
	handle, _, err := editorKernel.NewProc("CreateMutexW").Call(0, 0, uintptr(unsafe.Pointer(name)))
	if handle == 0 {
		return nil, err
	}
	if err == syscall.ERROR_ALREADY_EXISTS {
		syscall.CloseHandle(syscall.Handle(handle))
		return nil, fmt.Errorf("此数据目录已由另一个 Viva Editor 窗口使用，请从已打开的编辑器打开文件。")
	}
	return func() { syscall.CloseHandle(syscall.Handle(handle)) }, nil
}

func maximizeEditorWindow() {
	handle, _, _ := editorUser.NewProc("GetActiveWindow").Call()
	if handle == 0 {
		return
	}
	var process uint32
	editorUser.NewProc("GetWindowThreadProcessId").Call(handle, uintptr(unsafe.Pointer(&process)))
	if process == uint32(os.Getpid()) {
		editorUser.NewProc("ShowWindow").Call(handle, 3)
	}
}
