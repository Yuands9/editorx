package main

import (
	"errors"
	"runtime"
	"syscall"
	"unsafe"
)

type dataBlob struct {
	length uint32
	data   *byte
}

var (
	crypt32            = syscall.NewLazyDLL("crypt32.dll")
	cryptProtectData   = crypt32.NewProc("CryptProtectData")
	cryptUnprotectData = crypt32.NewProc("CryptUnprotectData")
	freeWindowsAIBytes = syscall.NewLazyDLL("kernel32.dll").NewProc("LocalFree")
)

func windowsAIKey(value []byte, encrypt bool) ([]byte, error) {
	if len(value) == 0 {
		return nil, nil
	}
	input := dataBlob{length: uint32(len(value)), data: &value[0]}
	var output dataBlob
	const forbidUI = 1
	call := cryptUnprotectData
	if encrypt {
		call = cryptProtectData
	}
	ok, _, _ := call.Call(uintptr(unsafe.Pointer(&input)), 0, 0, 0, 0, forbidUI, uintptr(unsafe.Pointer(&output)))
	runtime.KeepAlive(value)
	if ok == 0 {
		return nil, errors.New("Windows 无法保护或解锁 API 密钥，请在 AI 设置中重新输入")
	}
	defer freeWindowsAIBytes.Call(uintptr(unsafe.Pointer(output.data)))
	raw := unsafe.Slice(output.data, output.length)
	result := append([]byte(nil), raw...)
	clear(raw)
	return result, nil
}
