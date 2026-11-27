package editor

import (
	"bytes"
	"crypto/rand"
	"errors"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/vivaui/viva/app"
)

var (
	passwordOpenClipboard           = editorUser.NewProc("OpenClipboard")
	passwordCloseClipboard          = editorUser.NewProc("CloseClipboard")
	passwordEmptyClipboard          = editorUser.NewProc("EmptyClipboard")
	passwordSetClipboardData        = editorUser.NewProc("SetClipboardData")
	passwordGetClipboardData        = editorUser.NewProc("GetClipboardData")
	passwordRegisterClipboardFormat = editorUser.NewProc("RegisterClipboardFormatW")
	passwordGlobalAlloc             = editorKernel.NewProc("GlobalAlloc")
	passwordGlobalLock              = editorKernel.NewProc("GlobalLock")
	passwordGlobalUnlock            = editorKernel.NewProc("GlobalUnlock")
	passwordGlobalFree              = editorKernel.NewProc("GlobalFree")
	passwordGlobalSize              = editorKernel.NewProc("GlobalSize")
)

// No plaintext comparison is needed to clear the native clipboard. The private marker
// is written in the same transaction and checked while the clipboard is still locked.
func setPasswordClipboard(value string, native bool) (func() bool, error) {
	if !native {
		if err := app.SetClipboardText(value); err != nil {
			return nil, err
		}
		return func() bool {
			current, err := app.ClipboardText()
			if err != nil {
				return false
			}
			return current != value || app.SetClipboardText("") == nil
		}, nil
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	encoded, err := syscall.UTF16FromString(value)
	if err != nil {
		return nil, errors.New(uiText("内容含有剪贴板不支持的字符"))
	}
	defer clear(encoded)
	excludeName, _ := syscall.UTF16PtrFromString("ExcludeClipboardContentFromMonitorProcessing")
	tokenName, _ := syscall.UTF16PtrFromString("EditorX.PasswordVault.Clipboard")
	exclude, _, _ := passwordRegisterClipboardFormat.Call(uintptr(unsafe.Pointer(excludeName)))
	format, _, _ := passwordRegisterClipboardFormat.Call(uintptr(unsafe.Pointer(tokenName)))
	owner, _, _ := editorUser.NewProc("GetActiveWindow").Call()
	if exclude == 0 || format == 0 || owner == 0 {
		return nil, errors.New(uiText("无法打开受保护的 Windows 剪贴板"))
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return nil, err
	}
	if opened, _, _ := passwordOpenClipboard.Call(owner); opened == 0 {
		return nil, errors.New(uiText("剪贴板正被占用，请重试"))
	}
	defer passwordCloseClipboard.Call()
	if emptied, _, _ := passwordEmptyClipboard.Call(); emptied == 0 {
		return nil, errors.New(uiText("无法准备剪贴板"))
	}
	if !passwordSetClipboardBlock(exclude, []byte{1}) || !passwordSetClipboardBlock(format, token[:]) ||
		!passwordSetClipboardBlock(13, unsafe.Slice((*byte)(unsafe.Pointer(&encoded[0])), len(encoded)*2)) { // CF_UNICODETEXT
		passwordEmptyClipboard.Call()
		return nil, errors.New(uiText("无法写入受保护的 Windows 剪贴板"))
	}
	return func() bool { return clearPasswordClipboard(format, token) }, nil
}

func passwordSetClipboardBlock(format uintptr, data []byte) bool {
	handle, _, _ := passwordGlobalAlloc.Call(0x0042, uintptr(len(data))) // GMEM_MOVEABLE | GMEM_ZEROINIT
	if handle == 0 {
		return false
	}
	pointer, _, _ := passwordGlobalLock.Call(handle)
	if pointer != 0 {
		copy(unsafe.Slice((*byte)(unsafe.Pointer(pointer)), len(data)), data)
		passwordGlobalUnlock.Call(handle)
		if written, _, _ := passwordSetClipboardData.Call(format, handle); written != 0 {
			return true
		}
	}
	passwordGlobalFree.Call(handle)
	return false
}

func clearPasswordClipboard(format uintptr, token [16]byte) bool {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if opened, _, _ := passwordOpenClipboard.Call(0); opened == 0 {
		return false
	}
	defer passwordCloseClipboard.Call()
	handle, _, _ := passwordGetClipboardData.Call(format)
	if handle == 0 {
		return true
	}
	if size, _, _ := passwordGlobalSize.Call(handle); size < uintptr(len(token)) {
		return true
	}
	pointer, _, _ := passwordGlobalLock.Call(handle)
	if pointer == 0 {
		return false
	}
	matches := bytes.Equal(token[:], unsafe.Slice((*byte)(unsafe.Pointer(pointer)), len(token)))
	passwordGlobalUnlock.Call(handle)
	if !matches {
		return true
	}
	cleared, _, _ := passwordEmptyClipboard.Call()
	return cleared != 0
}
