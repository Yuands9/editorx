package editor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
)

func TestMediaPreviews(t *testing.T) {
	directory := os.Getenv("VIVA_PREVIEW_DIR")
	if directory == "" {
		t.Skip("preview is opt-in")
	}
	for _, kind := range []string{"image", "html"} {
		for _, light := range []bool{false, true} {
			name := "preview-" + kind + "-dark"
			if light {
				name = "preview-" + kind + "-light"
			}
			t.Run(name, func(t *testing.T) {
				m := newEditorApp()
				m.setLightMode(light)
				if kind == "image" {
					m.load("assets/icon.png")
				} else {
					m.load(filepath.Join(examplesDirectory, "页面.html"))
				}
				if m.showError {
					t.Fatal(m.errorText)
				}
				page := ui.NewPage(ui.PageConfig{View: m.page, Theme: &m.theme})
				session, err := headless.Open(ui.Configure(page, app.Config{Width: 1100, Height: 760}), nil, headless.Options{Scale: 1})
				if err != nil {
					t.Fatal(err)
				}
				defer session.Close()
				if err := session.Screenshot(filepath.Join(directory, name+".png")); err != nil {
					t.Fatal(err)
				}
				if kind == "image" {
					if err := session.Resize(900, 560); err != nil {
						t.Fatal(err)
					}
					if err := session.ClickLabel("原始尺寸"); err != nil {
						t.Fatal(err)
					}
					if err := session.ClickLabel("放大图片"); err != nil {
						t.Fatal(err)
					}
					if err := session.Screenshot(filepath.Join(directory, name+"-zoom.png")); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}
