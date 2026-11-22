package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
	"unsafe"

	"github.com/vivaui/viva/app"
)

// The runner creates a private desktop. Only the window owned by this test receives the key.
func TestNativeAIEnter(t *testing.T) {
	if os.Getenv("VIVA_NATIVE_SMOKE") != "1" {
		t.Skip("native AI key check is opt-in")
	}
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	const source = "host: 本机\n"
	path := filepath.Join(t.TempDir(), "配置.yaml")
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	m.load(path)
	requests := serveAIContextTest(t, m)
	m.openAIAt(m.active, "", false)
	const prompt = "解释配置"
	m.ai.prompt.SetText(prompt)

	var failure error
	completed := false
	err := app.RunApplication(func(application *app.Application) error {
		if err := openEditorWindow(application, m); err != nil {
			return err
		}
		if err := pressNativeTestEnter(); err != nil {
			application.Quit()
			return err
		}
		go func() {
			var result error
			select {
			case request := <-requests:
				if request.Instruction != prompt || len(request.Files) != 1 || request.Files[0].Text != source {
					result = fmt.Errorf("native Return sent the wrong request: %+v", request)
				}
			case <-time.After(8 * time.Second):
				result = fmt.Errorf("native Return did not send the prompt")
			}
			application.Post(func() {
				failure, completed = result, true
				application.Quit()
			})
		}()
		return nil
	})
	if err != nil || failure != nil || !completed {
		t.Fatalf("native Return failed: %v, %v", err, failure)
	}
}

func pressNativeTestEnter() error {
	hwnd, _, _ := editorUser.NewProc("GetActiveWindow").Call()
	var owner uint32
	if hwnd != 0 {
		editorUser.NewProc("GetWindowThreadProcessId").Call(hwnd, uintptr(unsafe.Pointer(&owner)))
	}
	if owner != uint32(os.Getpid()) {
		return fmt.Errorf("could not verify the native test window")
	}
	const wmKeyDown, vkReturn = 0x0100, 0x0d
	if sent, _, err := editorUser.NewProc("PostMessageW").Call(hwnd, wmKeyDown, vkReturn, 0x001c0001); sent == 0 {
		return fmt.Errorf("posting Return to the test window: %w", err)
	}
	return nil
}
