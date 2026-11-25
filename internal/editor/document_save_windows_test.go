package editor

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestAtomicSaveWithTemporaryWindowsFileLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	target, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := syscall.CreateFile(target, syscall.GENERIC_READ, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE,
		nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFile(path, []byte("blocked")); err == nil {
		syscall.CloseHandle(handle)
		t.Fatal("an open handle without delete sharing did not block replacement")
	}
	released := make(chan error, 1)
	go func() {
		time.Sleep(50 * time.Millisecond)
		released <- syscall.CloseHandle(handle)
	}()
	saveErr := writeFile(path, []byte("saved"))
	if err := <-released; err != nil {
		t.Fatal(err)
	}
	if saveErr != nil {
		t.Fatal(saveErr)
	}
	actual, err := os.ReadFile(path)
	if err != nil || string(actual) != "saved" {
		t.Fatalf("save after release = %q, %v", actual, err)
	}
}
