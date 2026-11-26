package editor

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var editorKernel = syscall.NewLazyDLL("kernel32.dll")
var editorUser = syscall.NewLazyDLL("user32.dll")

var errSessionDirectoryInUse = errors.New("此数据目录已由另一个 EditorX 进程使用")

func sessionMutexName(directory string) string {
	return "Local\\VivaEditorSession-" + sessionDirectoryKey(directory)
}

func sessionDirectoryKey(directory string) string {
	hash := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(directory))))
	return fmt.Sprintf("%x", hash[:16])
}

// 每个便携数据目录只有一个写入进程；进程异常结束时 Windows 自动释放锁。
func lockSessionDirectory(directory string) (func(), error) {
	name, _ := syscall.UTF16PtrFromString(sessionMutexName(directory))
	handle, _, err := editorKernel.NewProc("CreateMutexW").Call(0, 0, uintptr(unsafe.Pointer(name)))
	if handle == 0 {
		return nil, err
	}
	if err == syscall.ERROR_ALREADY_EXISTS {
		syscall.CloseHandle(syscall.Handle(handle))
		return nil, errSessionDirectoryInUse
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
