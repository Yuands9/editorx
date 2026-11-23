package editor

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	editorActivationClass = "EditorXActivationWindow"
	messageOnlyWindow     = ^uintptr(2) // HWND_MESSAGE
	copyDataMessage       = 0x004a      // WM_COPYDATA
	editorActivationTag   = 0x45445831
	maxActivationBytes    = 256 * 1024
)

type activationCopyData struct {
	tag  uintptr
	size uint32
	data unsafe.Pointer
}

var findWindowEx = editorUser.NewProc("FindWindowExW")
var getWindowProcess = editorUser.NewProc("GetWindowThreadProcessId")
var allowForegroundWindow = editorUser.NewProc("AllowSetForegroundWindow")
var sendMessageTimeout = editorUser.NewProc("SendMessageTimeoutW")
var destroyActivationWindow = editorUser.NewProc("DestroyWindow")
var defaultActivationProcedure = editorUser.NewProc("DefWindowProcW")
var activationClassName, _ = syscall.UTF16PtrFromString(editorActivationClass)
var activationHandlers sync.Map // HWND -> func([]string) bool

// The same mutex also guards the session against older EditorX builds.
func claimEditorInstance(directory string, paths []string) (func(), error) {
	release, err := lockSessionDirectory(directory)
	if !errors.Is(err, errSessionDirectoryInUse) {
		return release, err
	}

	absolutePaths := make([]string, len(paths))
	for index, path := range paths {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		absolutePaths[index] = absolute
	}
	payload, err := json.Marshal(absolutePaths)
	if err != nil {
		return nil, err
	}
	if len(payload) > maxActivationBytes {
		return nil, errors.New("启动参数过长")
	}

	title := activationWindowTitle(directory)
	deadline := time.Now().Add(8 * time.Second)
	for {
		if activateEditorInstance(title, payload) {
			return nil, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("无法唤起已打开的 %s，请稍后再试：%w", applicationName, errSessionDirectoryInUse)
		}
		time.Sleep(40 * time.Millisecond)
		// The previous process may still be starting, or may have just exited.
		release, err = lockSessionDirectory(directory)
		if !errors.Is(err, errSessionDirectoryInUse) {
			return release, err
		}
	}
}

func activationWindowTitle(directory string) *uint16 {
	title, _ := syscall.UTF16PtrFromString(sessionMutexName(directory) + "-activate")
	return title
}

func activateEditorInstance(title *uint16, payload []byte) bool {
	window, _, _ := findWindowEx.Call(messageOnlyWindow, 0, uintptr(unsafe.Pointer(activationClassName)), uintptr(unsafe.Pointer(title)))
	if window == 0 {
		return false
	}
	var process uint32
	getWindowProcess.Call(window, uintptr(unsafe.Pointer(&process)))
	if process != 0 {
		// Windows lets a user-launched process pass its foreground rights to the editor.
		allowForegroundWindow.Call(uintptr(process))
	}
	data := activationCopyData{tag: editorActivationTag, size: uint32(len(payload)), data: unsafe.Pointer(&payload[0])}
	var accepted uintptr
	const blockAndAbortIfHung = 0x0003 // SMTO_BLOCK | SMTO_ABORTIFHUNG
	sent, _, _ := sendMessageTimeout.Call(window, copyDataMessage, 0, uintptr(unsafe.Pointer(&data)), blockAndAbortIfHung, 1000, uintptr(unsafe.Pointer(&accepted)))
	runtime.KeepAlive(payload)
	return sent != 0 && accepted == 1
}

var registerActivationClass = sync.OnceValues(func() (uintptr, error) {
	module, _, err := editorKernel.NewProc("GetModuleHandleW").Call(0)
	if module == 0 {
		return 0, err
	}
	// WNDCLASSEXW layout, with uintptr for handles and function pointers.
	var class struct {
		size, style                        uint32
		procedure                          uintptr
		classExtra, windowExtra            int32
		instance, icon, cursor, background uintptr
		menu, name                         *uint16
		smallIcon                          uintptr
	}
	class.size = uint32(unsafe.Sizeof(class))
	class.instance, class.name = module, activationClassName
	class.procedure = syscall.NewCallback(activationWindowProcedure)
	atom, _, err := editorUser.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&class)))
	if atom == 0 {
		return 0, err
	}
	return module, nil
})

func listenForEditorActivation(directory string, activate func([]string) bool) (func(), error) {
	module, err := registerActivationClass()
	if err != nil {
		return nil, fmt.Errorf("无法注册窗口唤起服务：%w", err)
	}
	title := activationWindowTitle(directory)
	window, _, err := editorUser.NewProc("CreateWindowExW").Call(0,
		uintptr(unsafe.Pointer(activationClassName)), uintptr(unsafe.Pointer(title)),
		0, 0, 0, 0, 0, messageOnlyWindow, 0, module, 0)
	if window == 0 {
		return nil, fmt.Errorf("无法启动窗口唤起服务：%w", err)
	}
	activationHandlers.Store(window, activate)
	return func() {
		activationHandlers.Delete(window)
		destroyActivationWindow.Call(window)
	}, nil
}

func activationWindowProcedure(window, message, wParam, lParam uintptr) uintptr {
	if message != copyDataMessage {
		result, _, _ := defaultActivationProcedure.Call(window, message, wParam, lParam)
		return result
	}
	handler, ok := activationHandlers.Load(window)
	if !ok || lParam == 0 {
		return 0
	}
	data := (*activationCopyData)(unsafe.Pointer(lParam))
	if data.tag != editorActivationTag || data.size == 0 || data.size > maxActivationBytes || data.data == nil {
		return 0
	}
	var paths []string
	if err := json.Unmarshal(unsafe.Slice((*byte)(data.data), data.size), &paths); err != nil {
		return 0
	}
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			return 0
		}
	}
	if handler.(func([]string) bool)(paths) {
		return 1
	}
	return 0
}
