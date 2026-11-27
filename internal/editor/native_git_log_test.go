package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/vivaui/viva/app"
)

func TestNativeGitLogWindow(t *testing.T) {
	if os.Getenv("VIVA_NATIVE_SMOKE") != "1" {
		t.Skip("native Git window check is opt-in")
	}
	root, git := gitReviewFixture(t)
	file := filepath.Join(root, "note.txt")
	if err := os.WriteFile(file, []byte("fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-m", "Git window fixture")
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	done := make(chan struct{})
	defer close(done)
	var failure error
	var completed bool
	err := app.RunApplication(func(application *app.Application) error {
		if err := openEditorWindow(application, m); err != nil {
			return err
		}
		m.openGitLogAt(root, file, false)
		state := m.gitLogWindow
		if state == nil || state.window == nil || state.window == m.nativeWindow {
			return fmt.Errorf("Git log did not create a second window")
		}
		logWindow := state.window
		m.openGitLogAt(root, file, false)
		if m.gitLogWindow != state {
			return fmt.Errorf("opening the same scope twice did not reuse the window")
		}
		title, _ := syscall.UTF16PtrFromString(uiText("Git 提交记录") + " · note.txt")
		hwnd, _, _ := editorUser.NewProc("FindWindowW").Call(0, uintptr(unsafe.Pointer(title)))
		var process uint32
		editorUser.NewProc("GetWindowThreadProcessId").Call(hwnd, uintptr(unsafe.Pointer(&process)))
		const caption, thickFrame = 0x00C00000, 0x00040000
		style, _, _ := editorUser.NewProc("GetWindowLongPtrW").Call(hwnd, ^uintptr(15))
		if hwnd == 0 || process != uint32(os.Getpid()) || style&(caption|thickFrame) != caption|thickFrame {
			return fmt.Errorf("Git log window is missing its native title or resize border")
		}
		before, _ := logWindow.Frame()
		if moved, _, err := editorUser.NewProc("SetWindowPos").Call(hwnd, 0, 56, 68, 900, 590, 0x0014); moved == 0 {
			return fmt.Errorf("cannot move and resize Git log: %w", err)
		}
		deadline := time.Now().Add(10 * time.Second)
		closing := false
		go func() {
			ticker := time.NewTicker(30 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-done:
					return
				case <-ticker.C:
					application.Post(func() {
						if completed {
							return
						}
						if closing && logWindow.Closed() && m.gitLogWindow == nil && !m.nativeWindow.Closed() {
							completed = true
							application.Quit()
							return
						}
						after, ok := logWindow.Frame()
						if !closing && ok && before.X != after.X && before.Width != after.Width &&
							state.log == nil && state.files == nil && len(state.commits) == 1 && len(state.changes) == 1 {
							t.Logf("Git log read its commit and moved from %+v to %+v", before, after)
							closing = true
							logWindow.Close()
						}
						if time.Now().After(deadline) {
							failure = fmt.Errorf("Git log did not settle: %q, frame=%+v, closing=%v", state.problem, after, closing)
							completed = true
							application.Quit()
						}
					})
				}
			}
		}()
		return nil
	})
	if err != nil || failure != nil || !completed {
		t.Fatalf("native Git log failed: %v, %v", err, failure)
	}
}
