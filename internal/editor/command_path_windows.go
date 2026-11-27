package editor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

const editorCommandHome = "EDITORX_HOME"
const editorCommandRegistration = "EDITORX_REGISTER_COMMAND"
const editorCommandHomeReference = "%" + editorCommandHome + "%"

var setRegistryValue = syscall.NewLazyDLL("advapi32.dll").NewProc("RegSetValueExW")

func registerEditorCommand(executable string) error {
	if os.Getenv(editorCommandRegistration) == "0" || !strings.EqualFold(filepath.Base(executable), updateExecutable) {
		return nil
	}
	directory := filepath.Dir(executable)
	if !filepath.IsAbs(directory) || strings.ContainsRune(directory, ';') {
		return errors.New(uiText("此目录无法加入 Windows PATH"))
	}
	subkey, _ := syscall.UTF16PtrFromString("Environment")
	var registry syscall.Handle
	if err := syscall.RegOpenKeyEx(syscall.HKEY_CURRENT_USER, subkey, 0, syscall.KEY_QUERY_VALUE|syscall.KEY_SET_VALUE, &registry); err != nil {
		return err
	}
	defer syscall.RegCloseKey(registry)
	oldHome, _, err := readUserEnvironmentValue(registry, editorCommandHome)
	if err != nil {
		return err
	}
	path, pathKind, err := readUserEnvironmentValue(registry, "Path")
	if err != nil {
		return err
	}
	expandable := pathKind != syscall.REG_SZ || !strings.ContainsRune(path, '%')
	updated := editorCommandPath(path, oldHome, directory, expandable)
	if len(updated) > 32766 {
		return errors.New(uiText("Windows 用户 PATH 已达到长度上限"))
	}
	if !strings.EqualFold(directory, oldHome) {
		if err := writeRegistryString(registry, editorCommandHome, directory, syscall.REG_SZ); err != nil {
			return err
		}
	}
	if updated != path {
		if expandable {
			pathKind = syscall.REG_EXPAND_SZ
		}
		if err := writeRegistryString(registry, "Path", updated, pathKind); err != nil {
			return err
		}
	}
	if updated != path || !strings.EqualFold(directory, oldHome) {
		name, _ := syscall.UTF16PtrFromString("Environment")
		var result uintptr
		editorUser.NewProc("SendMessageTimeoutW").Call(0xffff, 0x001a, 0, uintptr(unsafe.Pointer(name)), 2, 500, uintptr(unsafe.Pointer(&result)))
	}
	return nil
}

func editorCommandPath(path, previous, directory string, expandable bool) string {
	parts := strings.Split(path, ";")
	for _, part := range parts {
		trimmed := strings.Trim(strings.TrimSpace(part), `"`)
		if strings.EqualFold(filepath.Clean(trimmed), directory) || expandable && strings.EqualFold(trimmed, editorCommandHomeReference) {
			return path
		}
	}
	entry := directory
	if expandable {
		entry = editorCommandHomeReference
	} else if last := len(parts) - 1; previous != "" && strings.EqualFold(parts[last], previous) {
		return strings.Join(append(parts[:last], entry), ";")
	}
	if path == "" {
		return entry
	}
	if strings.HasSuffix(path, ";") {
		return path + entry
	}
	return path + ";" + entry
}

func readUserEnvironmentValue(registry syscall.Handle, name string) (string, uint32, error) {
	key, _ := syscall.UTF16PtrFromString(name)
	var kind, length uint32
	if err := syscall.RegQueryValueEx(registry, key, nil, &kind, nil, &length); err != nil {
		if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
			return "", syscall.REG_EXPAND_SZ, nil
		}
		return "", 0, err
	}
	if length > 65536 || kind != syscall.REG_SZ && kind != syscall.REG_EXPAND_SZ {
		return "", 0, errors.New(uiText("Windows 环境变量格式无效"))
	}
	if length == 0 {
		return "", kind, nil
	}
	value := make([]uint16, (length+1)/2)
	if err := syscall.RegQueryValueEx(registry, key, nil, &kind, (*byte)(unsafe.Pointer(&value[0])), &length); err != nil {
		return "", 0, err
	}
	return syscall.UTF16ToString(value), kind, nil
}

func writeRegistryString(registry syscall.Handle, name, value string, kind uint32) error {
	key, _ := syscall.UTF16PtrFromString(name)
	encoded, err := syscall.UTF16FromString(value)
	if err != nil {
		return err
	}
	if result, _, _ := setRegistryValue.Call(uintptr(registry), uintptr(unsafe.Pointer(key)), 0, uintptr(kind), uintptr(unsafe.Pointer(&encoded[0])), uintptr(len(encoded)*2)); result != 0 {
		return syscall.Errno(result)
	}
	return nil
}
