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

type editorActivationHandlers struct {
	activate      func([]string) bool
	lockPasswords func()
}

var activationHandlers sync.Map // HWND -> editorActivationHandlers
var passwordWTS = syscall.NewLazyDLL("wtsapi32.dll")
var passwordRegisterSession = passwordWTS.NewProc("WTSRegisterSessionNotification")
var passwordUnregisterSession = passwordWTS.NewProc("WTSUnRegisterSessionNotification")

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
		return nil, errors.New(uiText("启动参数过长"))
	}

	title := activationWindowTitle(directory)
	deadline := time.Now().Add(8 * time.Second)
	for {
		if activateEditorInstance(title, payload) {
			return nil, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf(uiText("无法唤起已打开的 %s，请稍后再试：%w"), applicationName, errSessionDirectoryInUse)
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

func listenForEditorActivation(directory string, activate func([]string) bool, lockPasswords func()) (func(), func() bool, error) {
	module, err := registerActivationClass()
	if err != nil {
		return nil, nil, fmt.Errorf(uiText("无法注册窗口唤起服务：%w"), err)
	}
	title := activationWindowTitle(directory)
	window, _, err := editorUser.NewProc("CreateWindowExW").Call(0,
		uintptr(unsafe.Pointer(activationClassName)), uintptr(unsafe.Pointer(title)),
		0, 0, 0, 0, 0, messageOnlyWindow, 0, module, 0)
	if window == 0 {
		return nil, nil, fmt.Errorf(uiText("无法启动窗口唤起服务：%w"), err)
	}
	activationHandlers.Store(window, editorActivationHandlers{activate: activate, lockPasswords: lockPasswords})
	watching := false
	watch := func() bool {
		if !watching && passwordRegisterSession.Find() == nil {
			result, _, _ := passwordRegisterSession.Call(window, 0) // NOTIFY_FOR_THIS_SESSION
			watching = result != 0
		}
		return watching
	}
	return func() {
		if watching {
			passwordUnregisterSession.Call(window)
		}
		activationHandlers.Delete(window)
		destroyActivationWindow.Call(window)
	}, watch, nil
}

func activationWindowProcedure(window, message, wParam, lParam uintptr) uintptr {
	const sessionChange = 0x02B1 // WM_WTSSESSION_CHANGE
	if message == sessionChange {
		switch wParam {
		case 2, 4, 6, 7: // Console disconnect, remote disconnect, logoff, and session lock.
			if handler, ok := activationHandlers.Load(window); ok {
				handler.(editorActivationHandlers).lockPasswords()
			}
		}
		return 0
	}
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
	if handler.(editorActivationHandlers).activate(paths) {
		return 1
	}
	return 0
}
