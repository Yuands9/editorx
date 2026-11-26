package editor

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Monitoring must not prevent the writer from rotating/replacing its log.
func openSharedTextFile(path string) (*os.File, error) {
	full, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if len(full) >= 248 && !strings.HasPrefix(full, `\\?\`) {
		if strings.HasPrefix(full, `\\`) {
			full = `\\?\UNC\` + full[2:]
		} else {
			full = `\\?\` + full
		}
	}
	name, err := syscall.UTF16PtrFromString(full)
	if err != nil {
		return nil, err
	}
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(handle), path), nil
}
