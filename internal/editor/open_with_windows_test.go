package editor

import (
	"crypto/rand"
	"path/filepath"
	"syscall"
	"testing"
	"unsafe"
)

func TestEditorOpenWithRegistration(t *testing.T) {
	// Write real Windows registry values in a private key. Explorer settings
	// and file defaults for the signed-in user are untouched by this test.
	classes := `Software\EditorX-Test-` + rand.Text()
	root := syscall.Handle(syscall.HKEY_CURRENT_USER)
	t.Cleanup(func() {
		path, _ := syscall.UTF16PtrFromString(classes)
		result, _, _ := syscall.NewLazyDLL("advapi32.dll").NewProc("RegDeleteTreeW").Call(uintptr(root), uintptr(unsafe.Pointer(path)))
		if result != 0 {
			t.Errorf("remove private test registry key: %v", syscall.Errno(result))
		}
	})
	check := func(path, name, want string) {
		t.Helper()
		registryPath, _ := syscall.UTF16PtrFromString(classes + `\` + path)
		var key syscall.Handle
		if err := syscall.RegOpenKeyEx(root, registryPath, 0, syscall.KEY_QUERY_VALUE, &key); err != nil {
			t.Fatal(err)
		}
		defer syscall.RegCloseKey(key)
		value, kind, err := readUserEnvironmentValue(key, name)
		if err != nil || value != want || kind != syscall.REG_SZ {
			t.Fatalf("%s %q = %q (type %d), want %q: %v", path, name, value, kind, want, err)
		}
	}

	if err := writeEditorClassValue(root, classes, ".txt", "", "EditorXTest.OtherDefault"); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(t.TempDir(), "Folder with spaces", "EditorX.exe")
	app := `Applications\EditorX.exe`
	for _, path := range []string{executable, filepath.Join(t.TempDir(), "Moved path", "EditorX.exe")} {
		if err := writeEditorOpenWith(root, classes, path); err != nil {
			t.Fatal(err)
		}
		command := `"` + path + `" "%1"`
		check(app+`\shell\open\command`, "", command)
		check(editorOpenWithProgID+`\shell\open\command`, "", command)
		check(`*\shell\EditorX.Open\command`, "", command)
		check(app+`\SupportedTypes`, ".json", "")
		check(`.txt\OpenWithProgids`, editorOpenWithProgID, "")
		check(".txt", "", "EditorXTest.OtherDefault")
	}
}
