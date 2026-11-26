package editor

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const editorWebCacheEnv = "WEBVIEW2_USER_DATA_FOLDER"

// EditorX uses private WebView2 pages. Give each run a fresh, shared temporary
// profile so that neither the browser cache nor its many profiles accumulate.
func prepareEditorWebCache(directory string) func() {
	removeLegacyEditorWebCache(directory)
	parent := filepath.Join(os.TempDir(), "EditorX-WebView2-"+sessionDirectoryKey(directory))
	if info, err := os.Lstat(parent); err == nil && !info.IsDir() {
		parent += "-" + rand.Text()
	}
	if root, err := os.OpenRoot(parent); err == nil {
		if entries, err := os.ReadDir(parent); err == nil {
			for _, entry := range entries {
				if entry.IsDir() && strings.HasPrefix(entry.Name(), "run-") {
					_ = root.RemoveAll(entry.Name())
				}
			}
		}
		root.Close()
	}

	name := "run-" + rand.Text()
	previous, wasSet := os.LookupEnv(editorWebCacheEnv)
	_ = os.Setenv(editorWebCacheEnv, filepath.Join(parent, name))
	return func() {
		if wasSet {
			_ = os.Setenv(editorWebCacheEnv, previous)
		} else {
			_ = os.Unsetenv(editorWebCacheEnv)
		}
		if root, err := os.OpenRoot(parent); err == nil {
			// App windows and controllers are closed at this point. Allow the
			// WebView2 process a short time to release the profile's files.
			for attempt := 0; attempt < 10; attempt++ {
				if err := root.RemoveAll(name); err == nil {
					break
				}
				time.Sleep(80 * time.Millisecond)
			}
			root.Close()
			_ = os.Remove(parent)
		}
	}
}

func removeLegacyEditorWebCache(directory string) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return
	}
	defer root.Close()
	// Recognize the old WebView2 layout before removing browser-managed data.
	for _, name := range []string{"WebView2", "WebView2/EBWebView"} {
		if info, err := root.Lstat(name); err != nil || !info.IsDir() {
			return
		}
	}
	if info, err := root.Lstat("WebView2/EBWebView/Local State"); err == nil && info.Mode().IsRegular() {
		_ = root.RemoveAll("WebView2/EBWebView")
		_ = root.Remove("WebView2")
	}
}
