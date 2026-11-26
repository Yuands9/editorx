package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"
	"weak"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

func TestNativeHistoryComparisonWindow(t *testing.T) {
	if os.Getenv("VIVA_NATIVE_SMOKE") != "1" {
		t.Skip("native history window check is opt-in")
	}
	const scale = 2
	t.Setenv("VIVA_SCALE", "2")
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	path := filepath.Join(t.TempDir(), "history-native.txt")
	recordHistoryFixture(t, m.documentHistory(), path, "previous", time.Now())
	if err := os.WriteFile(path, []byte("current"), 0600); err != nil {
		t.Fatal(err)
	}
	var failure error
	var completed bool
	done := make(chan struct{})
	defer close(done)
	err := app.RunApplication(func(application *app.Application) error {
		if err := openEditorWindow(application, m); err != nil {
			return err
		}
		m.openHistoryComparison(path, localHistoryVersion{})
		state := m.historyCompare
		if state == nil || state.window == nil || state.window == m.nativeWindow {
			return fmt.Errorf("history comparison did not create a second native window")
		}
		name, err := syscall.UTF16PtrFromString(state.windowConfig().Title)
		if err != nil {
			return err
		}
		hwnd, _, _ := editorUser.NewProc("FindWindowW").Call(0, uintptr(unsafe.Pointer(name)))
		var process uint32
		editorUser.NewProc("GetWindowThreadProcessId").Call(hwnd, uintptr(unsafe.Pointer(&process)))
		if hwnd == 0 || process != uint32(os.Getpid()) {
			return fmt.Errorf("history comparison HWND was not a window of this test process")
		}
		const caption, thickFrame = 0x00C00000, 0x00040000
		style, _, _ := editorUser.NewProc("GetWindowLongPtrW").Call(hwnd, ^uintptr(15)) // GWL_STYLE
		if style&(caption|thickFrame) != caption|thickFrame {
			return fmt.Errorf("history window has no native title bar or resize border: %#x", style)
		}
		before, ok := state.window.Frame()
		if !ok {
			return fmt.Errorf("history window has no native frame")
		}
		if moved, _, err := editorUser.NewProc("SetWindowPos").Call(hwnd, 0, 48, 64, 900, 610, 0x0014); moved == 0 {
			return fmt.Errorf("cannot move and resize the history window: %w", err)
		}
		mainWindow, comparisonWindow := m.nativeWindow, state.window
		closing := false
		editingStep := 0
		deadline := time.Now().Add(4 * time.Second)
		go func() {
			ticker := time.NewTicker(20 * time.Millisecond)
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
						if closing && comparisonWindow.Closed() && m.historyCompare == nil && !mainWindow.Closed() {
							closedState := weak.Make(state)
							state, comparisonWindow = nil, nil
							runtime.GC()
							if closedState.Value() != nil {
								failure = fmt.Errorf("closed history window still retains its comparison state")
							}
							completed = true
							application.Quit()
							return
						}
						after, ok := state.window.Frame()
						if !closing && ok && (before.X != after.X || before.Y != after.Y) && before.Width != after.Width && before.Height != after.Height &&
							state.editor != nil && state.diff.Code.Geometry.Viewport.Width > 0 {
							if editingStep == 0 {
								t.Logf("history window moved and resized from %+v to %+v", before, after)
							}
							checked, err := checkNativeHistoryEditing(hwnd, state, scale, editingStep)
							if err != nil {
								failure, completed = err, true
								application.Quit()
								return
							}
							editingStep++
							if checked {
								closing = true
								state.window.Close()
							}
						}
						if time.Now().After(deadline) {
							failure, completed = fmt.Errorf("native history window did not settle: %+v, editing step=%d, closing=%v", after, editingStep, closing), true
							application.Quit()
						}
					})
				}
			}
		}()
		return nil
	})
	if err != nil || failure != nil || !completed {
		t.Fatalf("native history window failed: %v, %v", err, failure)
	}
}

// The test runs on an isolated desktop; send input only to the verified child HWND.
func checkNativeHistoryEditing(hwnd uintptr, state *historyCompareState, scale float32, step int) (bool, error) {
	const wmLeftDown, wmLeftUp, wmKeyDown, wmChar = 0x0201, 0x0202, 0x0100, 0x0102
	const mkShift, vkBack, vkReturn, vkControl = 0x0004, 0x08, 0x0d, 0x11
	const current, changed = "current", "currentx中文\n!"
	send := editorUser.NewProc("SendMessageW")
	viewport := state.diff.Code.Geometry.Viewport
	click := func(point gfx.Point, modifiers uintptr) {
		coordinates := uintptr(uint16(point.X*scale)) | uintptr(uint16(point.Y*scale))<<16
		send.Call(hwnd, wmLeftDown, modifiers|1, coordinates)
		send.Call(hwnd, wmLeftUp, modifiers, coordinates)
	}
	rightLineEnd := gfx.Point{X: viewport.X + viewport.Width*0.8, Y: viewport.Y + 10}
	// The Windows adapter publishes the focused text input on its next frame.
	switch step {
	case 0:
		editorUser.NewProc("SetActiveWindow").Call(hwnd)
		editorUser.NewProc("SetFocus").Call(hwnd)
		click(gfx.Point{X: viewport.X + 40, Y: viewport.Y + 10}, 0)
		return false, nil
	case 1:
		send.Call(hwnd, wmChar, 'L', 1)
		if state.tab != nil || state.editor.String() != current {
			return false, fmt.Errorf("native typing on the historical side changed the document: %q", state.editor.String())
		}
		click(rightLineEnd, 0)
		return false, nil
	case 2:
		if state.editor.Caret() != len(current) || !state.caretFocused {
			return false, fmt.Errorf("the right pane did not place the caret at the clicked line end: offset=%d focused=%v", state.editor.Caret(), state.caretFocused)
		}
		for _, character := range "x中文" {
			send.Call(hwnd, wmChar, uintptr(character), 1)
		}
		send.Call(hwnd, wmKeyDown, vkReturn, 1)
		send.Call(hwnd, wmChar, '!', 1)
		if state.tab == nil || state.tab.source.text != state.editor || state.editor.CommittedString() != changed {
			return false, fmt.Errorf("native typing did not update the current buffer: %q (tab attached=%v)", state.editor.CommittedString(), state.tab != nil)
		}
		return false, nil
	}
	click(rightLineEnd, mkShift)
	if selection := state.editor.Selection(); selection.Start != len([]rune("currentx中文")) || selection.End != len([]rune(changed)) {
		return false, fmt.Errorf("the right pane could not select across lines with Shift-click: %+v", selection)
	}
	send.Call(hwnd, wmKeyDown, vkBack, 1)
	if state.editor.CommittedString() != "currentx中文" {
		return false, fmt.Errorf("native Backspace did not remove the selected text: %q", state.editor.CommittedString())
	}
	var keys [256]byte
	if result, _, err := editorUser.NewProc("GetKeyboardState").Call(uintptr(unsafe.Pointer(&keys[0]))); result == 0 {
		return false, fmt.Errorf("cannot read the test thread keyboard state: %w", err)
	}
	previousKeys := keys
	setKeys := editorUser.NewProc("SetKeyboardState")
	defer setKeys.Call(uintptr(unsafe.Pointer(&previousKeys[0])))
	keys[vkControl] |= 0x80
	if result, _, err := setKeys.Call(uintptr(unsafe.Pointer(&keys[0]))); result == 0 {
		return false, fmt.Errorf("cannot hold Ctrl on the test thread: %w", err)
	}
	send.Call(hwnd, wmKeyDown, 'Z', 1)
	if state.editor.CommittedString() != changed {
		return false, fmt.Errorf("native Ctrl+Z did not restore the deleted selection: %q", state.editor.CommittedString())
	}
	return true, nil
}
