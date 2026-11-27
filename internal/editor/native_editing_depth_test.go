package editor

import (
	"fmt"
	"os"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/vivaui/viva/app"
)

func TestNativeEditingDepth(t *testing.T) {
	if os.Getenv("VIVA_NATIVE_SMOKE") != "1" {
		t.Skip("native editing check is opt-in")
	}
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	tab := m.addTab(document{saved: "abcde\n12345\nABCDE"}, "columns.txt")
	var failure error
	complete := false
	done := make(chan struct{})
	defer close(done)
	err := app.RunApplication(func(application *app.Application) error {
		if err := openEditorWindow(application, m); err != nil {
			return err
		}
		title, _ := syscall.UTF16PtrFromString(applicationName)
		hwnd, _, _ := editorUser.NewProc("FindWindowW").Call(0, uintptr(unsafe.Pointer(title)))
		var process uint32
		editorUser.NewProc("GetWindowThreadProcessId").Call(hwnd, uintptr(unsafe.Pointer(&process)))
		if hwnd == 0 || process != uint32(os.Getpid()) {
			return fmt.Errorf("native fixture window not found")
		}
		editorUser.NewProc("SetActiveWindow").Call(hwnd)
		editorUser.NewProc("SetFocus").Call(hwnd)
		getKeyboardState := editorUser.NewProc("GetKeyboardState")
		setKeyboardState := editorUser.NewProc("SetKeyboardState")
		post := editorUser.NewProc("PostMessageW")
		var originalKeys [256]byte
		restoreKeys := false
		postChord := func(message, key uintptr, held ...byte) bool {
			getKeyboardState.Call(uintptr(unsafe.Pointer(&originalKeys[0])))
			keys := originalKeys
			for _, modifier := range held {
				keys[modifier] = 0x80
			}
			setKeyboardState.Call(uintptr(unsafe.Pointer(&keys[0])))
			restoreKeys = true
			// Queued messages exercise the shortcut hook before TranslateMessage,
			// which a synchronous SendMessage would bypass.
			lParam := uintptr(1)
			if message == 0x104 {
				lParam |= 1 << 29
			}
			ok, _, _ := post.Call(hwnd, message, key, lParam)
			return ok != 0
		}
		step := 0
		deadline := time.Now().Add(8 * time.Second)
		go func() {
			ticker := time.NewTicker(60 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-done:
					return
				case <-ticker.C:
					application.Post(func() {
						if complete {
							return
						}
						if restoreKeys {
							setKeyboardState.Call(uintptr(unsafe.Pointer(&originalKeys[0])))
							restoreKeys = false
						}
						finish := func(err error) { failure = err; complete = true; application.Quit() }
						if time.Now().After(deadline) {
							finish(fmt.Errorf("native editing timed out at step %d", step))
							return
						}
						if !tab.source.focused {
							tab.source.focus = true
							m.invalidator.Invalidate()
							return
						}
						send := editorUser.NewProc("SendMessageW")
						switch step {
						case 0:
							tab.source.text.MoveTo(1, false)
							m.invalidator.Invalidate()
						case 1:
							var keys [256]byte
							editorUser.NewProc("GetKeyboardState").Call(uintptr(unsafe.Pointer(&keys[0])))
							original := keys
							keys[0x12], keys[0x10] = 0x80, 0x80
							editorUser.NewProc("SetKeyboardState").Call(uintptr(unsafe.Pointer(&keys[0])))
							for _, key := range []uintptr{0x27, 0x27, 0x28, 0x28} {
								send.Call(hwnd, 0x100, key, 1)
							}
							editorUser.NewProc("SetKeyboardState").Call(uintptr(unsafe.Pointer(&original[0])))
						case 2:
							if tab.source.text.SelectionCount() != 3 {
								finish(fmt.Errorf("native Alt+Shift arrows produced %+v", tab.source.text.Selections()))
								return
							}
							send.Call(hwnd, 0x102, 'x', 1)
							if tab.source.text.String() != "axde\n1x45\nAxDE" {
								finish(fmt.Errorf("native column edit: %q", tab.source.text.String()))
								return
							}
						case 3:
							var keys [256]byte
							editorUser.NewProc("GetKeyboardState").Call(uintptr(unsafe.Pointer(&keys[0])))
							original := keys
							keys[0x11] = 0x80
							editorUser.NewProc("SetKeyboardState").Call(uintptr(unsafe.Pointer(&keys[0])))
							send.Call(hwnd, 0x100, 'Z', 1)
							editorUser.NewProc("SetKeyboardState").Call(uintptr(unsafe.Pointer(&original[0])))
							if tab.source.text.String() != "abcde\n12345\nABCDE" {
								finish(fmt.Errorf("native column undo failed"))
								return
							}
							tab.source.replace("completionWord\ncomple")
							tab.source.text.MoveTo(tab.source.text.RuneCount(), false)
							m.invalidator.Invalidate()
						case 4:
							if !postChord(0x104, 0xbf, 0x12) {
								finish(fmt.Errorf("posting native Alt+/ failed"))
								return
							}
						case 5:
							if tab.source.completion == nil || len(tab.source.completion.items) != 1 {
								finish(fmt.Errorf("native Alt+/ did not open completion"))
								return
							}
							send.Call(hwnd, 0x100, 0x1b, 1)
							m.keyBindings = shortcutPreset(ideaShortcutPreset)
							m.invalidator.Invalidate()
						case 6:
							if !postChord(0x100, 0x20, 0x11, 0x12) {
								finish(fmt.Errorf("posting native Ctrl+Alt+Space failed"))
								return
							}
						case 7:
							if tab.source.completion == nil || len(tab.source.completion.items) != 1 {
								finish(fmt.Errorf("native Ctrl+Alt+Space did not open completion"))
								return
							}
							if !postChord(0x100, 0x0d) {
								finish(fmt.Errorf("posting native Enter failed"))
								return
							}
						case 8:
							if tab.source.text.String() != "completionWord\ncompletionWord" {
								finish(fmt.Errorf("native Enter did not accept completion: %q", tab.source.text.String()))
								return
							}
							if !postChord(0x100, 'Y', 0x11) {
								finish(fmt.Errorf("posting native Ctrl+Y failed"))
								return
							}
						case 9:
							if tab.source.text.String() != "completionWord" {
								finish(fmt.Errorf("native Ctrl+Y did not delete one line: %q", tab.source.text.String()))
								return
							}
							preferences := m.editorPrefs
							preferences.AutomaticCompletion = true
							m.applyEditorPreferences(preferences)
							tab.source.replace("anotherWord\n")
							tab.source.text.MoveTo(tab.source.text.RuneCount(), false)
							m.invalidator.Invalidate()
						case 10:
							send.Call(hwnd, 0x102, 'a', 1)
							send.Call(hwnd, 0x102, 'n', 1)
						case 11:
							if tab.source.completion == nil {
								return // The scheduled paint must open the popup without more input.
							}
							if !tab.source.completion.automatic || len(tab.source.completion.items) != 1 {
								finish(fmt.Errorf("unexpected native automatic completion"))
								return
							}
							if !postChord(0x100, 0x09) {
								finish(fmt.Errorf("posting native Tab failed"))
								return
							}
						case 12:
							if tab.source.text.String() != "anotherWord\nanotherWord" {
								finish(fmt.Errorf("native Tab did not accept automatic completion"))
								return
							}
							m.keyBindings = shortcutPreset(vsCodeShortcutPreset)
							tab.source.replace("one\ntwo\nthree")
							tab.source.text.MoveTo(5, false)
							m.invalidator.Invalidate()
						case 13:
							if !postChord(0x104, 0x28, 0x12, 0x10) {
								finish(fmt.Errorf("posting native Alt+Shift+Down failed"))
								return
							}
						case 14:
							if tab.source.text.String() != "one\ntwo\ntwo\nthree" {
								finish(fmt.Errorf("native VS Code copy line: %q", tab.source.text.String()))
								return
							}
							m.tools.terminalOpen = true
							if !postChord(0x100, 0xc0, 0x11) {
								finish(fmt.Errorf("posting native Ctrl+backtick failed"))
								return
							}
						case 15:
							if m.tools.terminalOpen {
								finish(fmt.Errorf("native VS Code terminal toggle failed"))
								return
							}
							finish(nil)
						}
						step++
					})
				}
			}
		}()
		return nil
	})
	if err != nil || failure != nil || !complete {
		t.Fatalf("native editing: %v / %v", err, failure)
	}
}
