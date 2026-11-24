package editor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTextAssetsPreviews(t *testing.T) {
	directory := os.Getenv("VIVA_PREVIEW_DIR")
	if directory == "" {
		t.Skip("visual review is opt-in")
	}
	for _, sample := range []struct {
		name, mode    string
		light         bool
		width, height int
	}{
		{"templates-dark", "templates", false, 1200, 800},
		{"templates-light-small", "templates", true, 900, 560},
		{"template-save-small", "save", false, 900, 560},
		{"template-save-variable-light", "save-variable", true, 900, 560},
		{"translation-empty", "empty", false, 1200, 800},
		{"translation-result-light", "result", true, 1200, 800},
		{"translation-library-small", "library", false, 900, 560},
		{"translation-ai-menu", "ai", true, 900, 560},
		{"sidebar-settings", "sidebar", true, 900, 560},
		{"sidebar-settings-wide", "sidebar", false, 1440, 900},
		{"sidebar-empty", "sidebar-empty", false, 1200, 800},
		{"offline-download", "offline", true, 900, 560},
		{"templates-left", "templates-left", false, 1200, 800},
	} {
		t.Run(sample.name, func(t *testing.T) {
			m := newEditorApp()
			m.dataDirectory = t.TempDir()
			m.setLightMode(sample.light)
			tab := m.addTab(document{saved: "Hello world.\nSelect text to look up a word, reuse a translation, or ask AI to translate.\n", eol: "LF"}, "reading.txt")
			if sample.mode == "sidebar" || sample.mode == "sidebar-empty" {
				m.tabs, m.active = nil, nil
			}
			s := openTestSession(t, m)
			if err := s.Resize(sample.width, sample.height); err != nil {
				t.Fatal(err)
			}
			switch sample.mode {
			case "templates":
				m.openTemplatePicker("", false)
				m.templatePanel.details = true
				m.templatePanel.values[0].SetText("v0.33")
				m.templatePanel.values[1].SetText("模板与翻译体验更新")
			case "save":
				m.openTemplateSave(tab, false)
			case "save-variable":
				m.addProjects(t.TempDir())
				m.openTemplateSave(tab, false)
				m.templateDialog.variableOpen = true
			case "templates-left":
				m.sidebarPrefs.Position = "left"
				m.sidebarPrefs.Templates = true
				m.openTemplatePicker("", false)
			case "sidebar":
				m.showSettings(settingsSidebar)
			case "sidebar-empty":
				m.sidebarPrefs.Templates, m.translationEnabled = true, true
			case "empty", "library", "result", "ai", "offline":
				m.translationEnabled = true
				m.openTranslation(false)
				m.translation.managing = sample.mode == "library"
				if sample.mode == "ai" {
					m.translation.query.SetText("Please save your changes before closing this window.")
					m.chooseTranslationAI()
				}
				if sample.mode == "offline" {
					m.selectTranslationMode(translationOfflineMode)
					m.translation.query.SetText("Please save your changes before closing this window.")
				}
				if sample.mode == "result" {
					_, _, err := mergeTranslationLibrary(filepath.Join(m.dataDirectory, translationLibraryFile), []translationEntry{{"en", "zh-cn", translationSentence, "Hello world.", "你好，世界。", "产品欢迎语"}})
					if err != nil {
						t.Fatal(err)
					}
					m.translation.query.SetText("Hello world.")
					m.lookupTypedTranslation()
				}
			}
			if err := s.Frame(); err != nil {
				t.Fatal(err)
			}
			if err := s.Screenshot(filepath.Join(directory, sample.name+".png")); err != nil {
				t.Fatal(err)
			}
		})
	}
}
