package editor

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/vivaui/viva/app"
)

func TestNativeEditorBackground(t *testing.T) {
	if os.Getenv("VIVA_NATIVE_SMOKE") != "1" {
		t.Skip("native background check is opt-in")
	}
	executable, err := filepath.Abs(os.Getenv("VIVA_EDITOR_TEST_EXE"))
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(executable); err != nil || info.IsDir() {
		t.Fatal("set VIVA_EDITOR_TEST_EXE to a packaged EditorX.exe")
	}

	newLaunch := func(t *testing.T, directory string, paths ...string) *exec.Cmd {
		ctx, cancel := context.WithTimeout(context.Background(), 28*time.Second)
		t.Cleanup(cancel)
		command := exec.CommandContext(ctx, executable, paths...)
		command.Dir = filepath.Dir(directory)
		command.Env = append(os.Environ(), "VIVA_EDITOR_DATA_DIR="+directory)
		return command
	}
	openWindow := func(t *testing.T, command *exec.Cmd) (uintptr, <-chan error) {
		t.Helper()
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		exited := make(chan error, 1)
		go func() { exited <- command.Wait(); close(exited) }()
		t.Cleanup(func() {
			select {
			case <-exited:
			default:
				command.Process.Kill()
				<-exited
			}
		})

		name, _ := syscall.UTF16PtrFromString("VivaNativeWindow")
		title, _ := syscall.UTF16PtrFromString(applicationName)
		var window uintptr
		waitForNativeEditor(t, "main window appears", func() bool {
			window, _, _ = findWindowEx.Call(0, 0, uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(title)))
			if window == 0 {
				return false
			}
			var process uint32
			getWindowProcess.Call(window, uintptr(unsafe.Pointer(&process)))
			return process == uint32(command.Process.Pid)
		})
		return window, exited
	}

	post := editorUser.NewProc("PostMessageW")
	visible := editorUser.NewProc("IsWindowVisible")
	iconic := editorUser.NewProc("IsIconic")
	send := func(t *testing.T, window, message, param uintptr) {
		t.Helper()
		if result, _, err := post.Call(window, message, param, 0); result == 0 {
			t.Fatal(err)
		}
	}
	const closeMessage = 0x0010

	t.Run("hide and activate the same window", func(t *testing.T) {
		root := t.TempDir()
		directory := filepath.Join(root, "data")
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
		preferences := applicationPreferences{ShowTrayIcon: false, ExitOnClose: false}
		if err := preferences.save(directory); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, "唤起测试.json")
		if err := os.WriteFile(path, []byte(`{"activated":true}`), 0600); err != nil {
			t.Fatal(err)
		}

		window, exited := openWindow(t, newLaunch(t, directory))
		waitForNativeEditor(t, "window initially visible", func() bool {
			value, _, _ := visible.Call(window)
			return value != 0
		})
		send(t, window, closeMessage, 0)
		waitForNativeEditor(t, "close hides the window", func() bool {
			value, _, _ := visible.Call(window)
			return value == 0
		})
		select {
		case <-exited:
			t.Fatal("close terminated the background editor")
		default:
		}

		if err := newLaunch(t, directory).Run(); err != nil {
			t.Fatalf("second launch: %v", err)
		}
		waitForNativeEditor(t, "same window restored without a tray icon", func() bool {
			value, _, _ := visible.Call(window)
			return value != 0
		})

		const systemCommand = 0x0112
		const minimize = 0xf020
		send(t, window, systemCommand, minimize)
		waitForNativeEditor(t, "window minimized", func() bool {
			value, _, _ := iconic.Call(window)
			return value != 0
		})
		if err := newLaunch(t, directory, filepath.Base(path)).Run(); err != nil {
			t.Fatalf("launch with a relative path: %v", err)
		}
		waitForNativeEditor(t, "same window restored after minimize", func() bool {
			minimized, _, _ := iconic.Call(window)
			shown, _, _ := visible.Call(window)
			return minimized == 0 && shown != 0
		})
		waitForNativeEditor(t, "relative Unicode path opened in the original process", func() bool {
			raw, err := os.ReadFile(filepath.Join(directory, "session.json"))
			if err != nil {
				return false
			}
			var saved savedSession
			if json.Unmarshal(raw, &saved) != nil || len(saved.Windows) != 1 {
				return false
			}
			tabs := saved.Windows[0].Tabs
			return len(tabs) == 1 && tabs[0].Path == path
		})
	})

	t.Run("default startup follows system and close saves session", func(t *testing.T) {
		directory := filepath.Join(t.TempDir(), "data")
		window, exited := openWindow(t, newLaunch(t, directory))
		send(t, window, closeMessage, 0)
		select {
		case err := <-exited:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(8 * time.Second):
			t.Fatal("close did not terminate the editor")
		}
		data, err := os.ReadFile(filepath.Join(directory, "session.json"))
		if err != nil {
			t.Fatalf("session was not saved before exit: %v", err)
		}
		saved, err := decodeEditorSession(data)
		wantLight := app.SystemAppearance() == app.AppearanceLight
		if err != nil || len(saved.Windows) != 1 || saved.Windows[0].Light != wantLight {
			t.Fatalf("a fresh EXE did not follow the Windows app theme: want light=%v, %v", wantLight, err)
		}
	})
}

func waitForNativeEditor(t *testing.T, description string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out while waiting for %s", description)
		}
		time.Sleep(30 * time.Millisecond)
	}
}
