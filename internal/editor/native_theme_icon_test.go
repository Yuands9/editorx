package editor

import (
	"fmt"
	"os"
	"testing"

	"github.com/vivaui/viva/app"
)

func TestNativeThemeIconSwitch(t *testing.T) {
	if os.Getenv("VIVA_NATIVE_SMOKE") != "1" {
		t.Skip("native window icon check is opt-in")
	}
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	preferences, err := readApplicationPreferences(m.dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	preferences.Theme = themeDark
	if err := preferences.save(m.dataDirectory); err != nil {
		t.Fatal(err)
	}

	var verifyErr error
	err = app.RunApplication(func(application *app.Application) error {
		if err := openEditorWindow(application, m); err != nil {
			return err
		}
		hwnd, _, _ := editorUser.NewProc("GetActiveWindow").Call()
		if hwnd == 0 {
			application.Quit()
			return fmt.Errorf("the native test window has no handle")
		}
		const wmGetIcon = 0x007f
		getIcons := func() [2]uintptr {
			send := editorUser.NewProc("SendMessageW")
			small, _, _ := send.Call(hwnd, wmGetIcon, 0, 0)
			large, _, _ := send.Call(hwnd, wmGetIcon, 1, 0)
			return [2]uintptr{small, large}
		}
		checkChange := func(previous, current [2]uintptr, light bool) error {
			if m.light != light || current[0] == 0 || current[1] == 0 || current[0] == previous[0] || current[1] == previous[1] {
				return fmt.Errorf("theme switch did not update both native icon sizes (light=%t)", light)
			}
			return nil
		}

		dark := getIcons()
		if dark[0] == 0 || dark[1] == 0 {
			application.Quit()
			return fmt.Errorf("the first window is missing its icon")
		}
		m.cycleTheme()
		application.Post(func() {
			light := getIcons()
			if verifyErr = checkChange(dark, light, true); verifyErr != nil {
				application.Quit()
				return
			}
			m.cycleTheme()
			application.Post(func() {
				verifyErr = checkChange(light, getIcons(), false)
				application.Quit()
			})
		})
		return nil
	})
	if err != nil || verifyErr != nil {
		t.Fatalf("native theme icons: %v, %v", err, verifyErr)
	}
}
