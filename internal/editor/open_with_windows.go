package editor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

const editorOpenWithProgID = "EditorX.File"

const editorOpenWithExtensions = ".txt .log .md .markdown .json .jsonl .ndjson .xml .xhtml .html .htm " +
	".yaml .yml .properties .ini .toml .env .cfg .conf .csv .tsv " +
	".go .js .ts .jsx .tsx .css .scss .svg .java .py .sql .ps1 " +
	".c .cpp .h .hpp .cs .rs .sh .bat .cmd " +
	".docx .xlsx .pdf .xmind .png .jpg .jpeg .gif .bmp .webp .tif .tiff"

var createEditorRegistryKey = syscall.NewLazyDLL("advapi32.dll").NewProc("RegCreateKeyExW")
var notifyEditorShell = syscall.NewLazyDLL("shell32.dll").NewProc("SHChangeNotify")

func registerEditorOpenWith() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	if err := writeEditorOpenWith(syscall.HKEY_CURRENT_USER, `Software\Classes`, executable); err != nil {
		return err
	}
	const associationChanged = 0x08000000
	notifyEditorShell.Call(associationChanged, 0, 0, 0)
	return nil
}

func writeEditorOpenWith(root syscall.Handle, classes, executable string) error {
	if !filepath.IsAbs(executable) || !strings.EqualFold(filepath.Ext(executable), ".exe") {
		return errors.New(uiText("请选择要注册的 EditorX.exe 文件。"))
	}
	application := `Applications\` + filepath.Base(executable)
	verb := `*\shell\EditorX.Open`
	command := `"` + executable + `" "%1"`
	icon := `"` + executable + `",0`
	for _, entry := range []struct{ path, name, value string }{
		{application, "FriendlyAppName", applicationName},
		{application + `\DefaultIcon`, "", icon},
		{application + `\shell\open\command`, "", command},
		{editorOpenWithProgID, "", "EditorX"},
		{editorOpenWithProgID + `\DefaultIcon`, "", icon},
		{editorOpenWithProgID + `\shell\open\command`, "", command},
		{verb, "", uiText("使用 EditorX 打开")},
		{verb, "Icon", icon},
		{verb + `\command`, "", command},
	} {
		if err := writeEditorClassValue(root, classes, entry.path, entry.name, entry.value); err != nil {
			return err
		}
	}
	for extension := range strings.FieldsSeq(editorOpenWithExtensions) {
		if err := writeEditorClassValue(root, classes, application+`\SupportedTypes`, extension, ""); err != nil {
			return err
		}
		if err := writeEditorClassValue(root, classes, extension+`\OpenWithProgids`, editorOpenWithProgID, ""); err != nil {
			return err
		}
	}
	return nil
}

func writeEditorClassValue(root syscall.Handle, classes, path, name, value string) error {
	keyPath, err := syscall.UTF16PtrFromString(classes + `\` + path)
	if err != nil {
		return err
	}
	var key syscall.Handle
	result, _, _ := createEditorRegistryKey.Call(uintptr(root), uintptr(unsafe.Pointer(keyPath)), 0, 0, 0, syscall.KEY_SET_VALUE, 0, uintptr(unsafe.Pointer(&key)), 0)
	if result != 0 {
		return syscall.Errno(result)
	}
	defer syscall.RegCloseKey(key)
	return writeRegistryString(key, name, value, syscall.REG_SZ)
}
