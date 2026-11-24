// Added for EditorX to verify its local Viva changes.
package windows

import (
	"fmt"
	"os"
	"runtime"
	"testing"

	"github.com/vivaui/viva/internal/platform"
)

func TestNativeStartupFrame(t *testing.T) {
	if os.Getenv("VIVA_NATIVE_SMOKE") != "1" {
		t.Skip("native first-frame test is opt-in")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	for _, value := range []struct {
		name  string
		color platform.Color
	}{
		{"dark", platform.Color{Red: .114, Green: .118, Blue: .125, Alpha: 1}},
		{"light", platform.Color{Red: .961, Green: .965, Blue: .973, Alpha: 1}},
	} {
		t.Run(value.name, func(t *testing.T) {
			var observed, resized bool
			var problem error
			err := RunApplication(func(application platform.Application) error {
				_, err := application.OpenWindow(platform.Config{
					Title: "EditorX first frame test", Width: 900, Height: 560, ClearColor: value.color,
					OnResize: func(size platform.Size) (platform.Frame, error) {
						resized = size.Width > 0 && size.Height > 0
						return platform.Frame{}, nil
					},
					OnVisibility: func(visible bool) {
						if !visible || observed {
							return
						}
						observed = true
						windowStates.Range(func(_, item any) bool {
							state := item.(*windowState)
							if !resized || state.renderer.target == 0 || state.renderer.width == 0 || state.renderer.height == 0 || state.renderer.color != value.color {
								problem = fmt.Errorf("window became visible before its themed, DPI-sized frame was rendered (resized=%t, target=%d, size=%dx%d)", resized, state.renderer.target, state.renderer.width, state.renderer.height)
							}
							return true
						})
					},
				})
				if err != nil {
					return err
				}
				application.Quit()
				return nil
			})
			if err != nil || problem != nil || !observed {
				t.Fatalf("first visible frame: %v; %v; observed=%t", err, problem, observed)
			}
		})
	}
}
