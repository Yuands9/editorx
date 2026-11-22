package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
)

func TestSessionPreviews(t *testing.T) {
	directory := os.Getenv("VIVA_PREVIEW_DIR")
	if directory == "" {
		t.Skip("preview is opt-in")
	}
	for _, light := range []bool{false, true} {
		name := "preview-history-dark"
		if light {
			name = "preview-history-light"
		}
		t.Run(name, func(t *testing.T) {
			store, err := openSessionStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			m := newEditorApp()
			store.attach(m)
			m.setLightMode(light)
			m.addProjects("examples")
			m.load("examples/配置.json")
			m.load("examples/使用指南.md")
			m.load("examples/页面.html")
			m.load("assets/icon.png")
			m.showHistory = true
			page := ui.NewPage(ui.PageConfig{View: m.page, Theme: &m.theme})
			session, err := headless.Open(ui.Configure(page, app.Config{Width: 1100, Height: 760}), nil, headless.Options{Scale: 1})
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			if err := session.Screenshot(filepath.Join(directory, name+".png")); err != nil {
				t.Fatal(err)
			}
			m.showHistory = false
			m.newDocument()
			if err := session.Frame(); err != nil {
				t.Fatal(err)
			}
			if err := session.Screenshot(filepath.Join(directory, name+"-empty-tab.png")); err != nil {
				t.Fatal(err)
			}
			m.showFiles = false
			if err := session.Resize(900, 560); err != nil {
				t.Fatal(err)
			}
			if err := session.Screenshot(filepath.Join(directory, name+"-empty-tab-small.png")); err != nil {
				t.Fatal(err)
			}
		})
	}
}
