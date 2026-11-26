package windows

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"
	"unsafe"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/internal/platform"
)

func TestNativeFileDrop(t *testing.T) {
	if os.Getenv("VIVA_NATIVE_SMOKE") != "1" {
		t.Skip("native Windows file drop is opt-in")
	}
	directory := t.TempDir()
	t.Setenv("WEBVIEW2_USER_DATA_FOLDER", filepath.Join(directory, "WebView2"))
	path := filepath.Join(directory, "拖放 测试.txt")
	if err := os.WriteFile(path, []byte("file drop"), 0600); err != nil {
		t.Fatal(err)
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var received int
	var failure error
	var timeout *time.Timer
	err := RunApplication(func(application platform.Application) error {
		finish := func(err error) {
			if failure == nil {
				failure = err
			}
			application.Quit()
		}
		window, err := application.OpenWindow(platform.Config{
			Title: "EditorX drop test", Width: 900, Height: 560, Web: &platform.WebHost{},
			NativeViews: []platform.NativeView{{ID: 1, Bounds: gfx.Rect{X: 400, Y: 80, Width: 380, Height: 350}}},
			OnFileDrop: func(event platform.FileDropEvent) (platform.Frame, bool, error) {
				if event.Phase == platform.FileDropDrop {
					received++
					if !slices.Equal(event.Paths, []string{path}) {
						finish(fmt.Errorf("unexpected dropped paths: %v", event.Paths))
					} else if received == 2 {
						finish(nil)
					}
				}
				return platform.Frame{}, true, nil
			},
		})
		if err != nil {
			return err
		}
		state := window.(*nativeWindow).state
		style, _, _ := readCompositionWindowStyle.Call(state.window, ^uintptr(19)) // GWL_EXSTYLE
		if style&0x10 == 0 {                                                       // WS_EX_ACCEPTFILES
			return fmt.Errorf("window did not register for WM_DROPFILES")
		}
		drop := platform.DropFilesBytes([]string{path})
		handle, _, err := globalAlloc.Call(globalMoveable, uintptr(len(drop)))
		if handle == 0 {
			return err
		}
		address, _, err := globalLock.Call(handle)
		if address == 0 {
			globalFree.Call(handle)
			return err
		}
		copy(unsafe.Slice((*byte)(pointer(address)), len(drop)), drop)
		globalUnlock.Call(handle)
		windowProc(state.window, wmDropFiles, handle, 0) // the receiver frees the drop handle

		loaded := false
		_, err = state.web.open(platform.WebViewConfig{ID: 1,
			HTML: `<!doctype html><iframe style="width:100%;height:300px" srcdoc="File drop target"></iframe>`,
			OnLoad: func(event platform.WebLoad) {
				if event.Phase != platform.WebLoadFinished || loaded {
					return
				}
				loaded = true
				application.Post(func() {
					page := state.web.pages[1]
					for _, phase := range []string{"dragEnter", "dragOver", "drop"} {
						data, _ := json.Marshal(map[string]any{"type": phase, "x": 100, "y": 100,
							"data": map[string]any{"items": []string{}, "files": []string{path}, "dragOperationsMask": 1}})
						handler := newHandler(func(status, _ uintptr) uintptr {
							if err := hresult(status); err != nil {
								finish(err)
							}
							return 0
						})
						status := comCall(page.view, 36, uintptr(unsafe.Pointer(wide("Input.dispatchDragEvent"))),
							uintptr(unsafe.Pointer(wide(string(data)))), handler) // CallDevToolsProtocolMethod
						comCall(handler, 2)
						if err := hresult(status); err != nil {
							finish(err)
							break
						}
					}
				})
			},
		})
		timeout = time.AfterFunc(12*time.Second, application.Quit)
		return err
	})
	if timeout != nil {
		timeout.Stop()
	}
	if err != nil || failure != nil || received != 2 {
		t.Fatalf("expected WM_DROPFILES and an embedded WebView2 file drop, received %d: %v, %v", received, err, failure)
	}
}
