package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/vivaui/viva/app"
)

func TestNativeTerminalInput(t *testing.T) {
	if os.Getenv("VIVA_NATIVE_SMOKE") != "1" {
		t.Skip("native terminal input is opt-in")
	}
	directory := t.TempDir()
	setNativeWebViewProfile(t, directory)
	m := newEditorApp()
	m.dataDirectory = directory
	m.addTab(document{saved: "editor keeps its text\n", eol: "LF"}, "editor.txt")

	var failure error
	completed := false
	stop := make(chan struct{})
	defer close(stop)
	err := app.RunApplication(func(application *app.Application) error {
		if err := openEditorWindow(application, m); err != nil {
			return err
		}
		window, _, _ := editorUser.NewProc("GetActiveWindow").Call()
		var owner uint32
		editorUser.NewProc("GetWindowThreadProcessId").Call(window, uintptr(unsafe.Pointer(&owner)))
		if window == 0 || owner != uint32(os.Getpid()) {
			application.Quit()
			return fmt.Errorf("could not verify the native terminal test window")
		}

		hostClass, _ := syscall.UTF16PtrFromString("VivaWebHost")
		findWindow := editorUser.NewProc("FindWindowExW")
		getFocus := editorUser.NewProc("GetFocus")
		isChild := editorUser.NewProc("IsChild")
		isVisible := editorUser.NewProc("IsWindowVisible")
		m.openLocalShell(directory)
		shell := m.tools.terminal
		m.invalidator.Invalidate()
		checks := [...]struct{ file, value string }{
			{"first-input.txt", "first"},
			{"reopened-input.txt", "中文"},
		}
		index := 0
		commandSent, waitingForHidden := false, false
		deadline := time.Now().Add(18 * time.Second)
		finish := func(err error) {
			failure, completed = err, true
			m.releaseWindowState()
			application.Quit()
		}

		go func() {
			ticker := time.NewTicker(50 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-stop:
					return
				case <-ticker.C:
					application.Post(func() {
						if completed {
							return
						}
						check := checks[index]
						if time.Now().After(deadline) || shell.issue != "" {
							finish(fmt.Errorf("%s: command sent=%t, terminal error=%q", check.file, commandSent, shell.issue))
							return
						}
						if m.tools.terminal != shell {
							finish(fmt.Errorf("reopening replaced the terminal session"))
							return
						}
						host, _, _ := findWindow.Call(window, 0, uintptr(unsafe.Pointer(hostClass)), 0)
						if host == 0 || shell.process == nil || !shell.viewReady.Load() {
							return
						}
						visible, _, _ := isVisible.Call(host)
						if waitingForHidden {
							if visible == 0 {
								m.useTool("终端")
								m.invalidator.Invalidate()
								waitingForHidden = false
							}
							return
						}
						if !commandSent {
							focused, _, _ := getFocus.Call()
							inTerminal, _, _ := isChild.Call(host, focused)
							if visible == 0 || inTerminal == 0 {
								return
							}
							command := "Set-Content -LiteralPath " + check.file + " -Value " + check.value + " -Encoding UTF8"
							if err := postNativeTerminalCommand(focused, command); err != nil {
								finish(err)
								return
							}
							commandSent = true
							return
						}
						data, _ := os.ReadFile(filepath.Join(directory, check.file))
						if strings.TrimSpace(strings.TrimPrefix(string(data), "\ufeff")) != check.value {
							return
						}
						t.Logf("PowerShell accepted keyboard input: %s", check.file)
						index++
						if index == len(checks) {
							finish(nil)
							return
						}
						commandSent, waitingForHidden = false, true
						deadline = time.Now().Add(8 * time.Second)
						m.applyShortcutBindings(shortcutPreset(vsCodeShortcutPreset), vsCodeShortcutPreset)
						shell.view.Eval(`document.activeElement.dispatchEvent(new KeyboardEvent("keydown", {
							key: String.fromCharCode(96), ctrlKey: true, bubbles: true, cancelable: true
						}))`)
					})
				}
			}
		}()
		return nil
	})
	if err != nil || failure != nil || !completed {
		t.Fatalf("native terminal input failed: %v, %v", err, failure)
	}
}

func postNativeTerminalCommand(window uintptr, command string) error {
	const wmChar, wmKeyDown, wmKeyUp, vkReturn = 0x0102, 0x0100, 0x0101, 0x0d
	post := editorUser.NewProc("PostMessageW")
	for _, r := range command {
		if ok, _, err := post.Call(window, wmChar, uintptr(r), 1); ok == 0 {
			return fmt.Errorf("posting terminal text: %w", err)
		}
	}
	if ok, _, err := post.Call(window, wmKeyDown, vkReturn, 0x001c0001); ok == 0 {
		return fmt.Errorf("posting terminal Return: %w", err)
	}
	if ok, _, err := post.Call(window, wmKeyUp, vkReturn, 0xc01c0001); ok == 0 {
		return fmt.Errorf("releasing terminal Return: %w", err)
	}
	return nil
}
