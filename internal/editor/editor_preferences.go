package editor

import (
	"slices"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

const (
	wrapByFileType = ""
	wrapEnabled    = "on"
	wrapDisabled   = "off"

	minEditorFontSize = 10
	maxEditorFontSize = 32
)

var editorTabWidths = []int{2, 4, 8}

type editorPreferences struct {
	FontFamily string `json:"fontFamily,omitempty"`
	FontSize   int    `json:"fontSize"`
	TabWidth   int    `json:"tabWidth"`
	HardTabs   bool   `json:"hardTabs,omitempty"`
	Wrap       string `json:"wrap,omitempty"`
	JSONIndent int    `json:"jsonIndent"`
}

func defaultEditorPreferences() editorPreferences {
	return editorPreferences{FontSize: 16, TabWidth: 4, JSONIndent: 4}
}

func (p editorPreferences) normalized() editorPreferences {
	defaults := defaultEditorPreferences()
	p.FontFamily = strings.TrimSpace(p.FontFamily)
	if gfx.MonoFamily(p.FontFamily) == gfx.MonoSystem {
		p.FontFamily = ""
	}
	if p.FontSize < minEditorFontSize || p.FontSize > maxEditorFontSize {
		p.FontSize = defaults.FontSize
	}
	if !slices.Contains(editorTabWidths, p.TabWidth) {
		p.TabWidth = defaults.TabWidth
	}
	if p.Wrap != wrapEnabled && p.Wrap != wrapDisabled {
		p.Wrap = wrapByFileType
	}
	if p.JSONIndent != 2 && p.JSONIndent != 4 {
		p.JSONIndent = defaults.JSONIndent
	}
	return p
}

func (p editorPreferences) withChanges(original, draft editorPreferences) editorPreferences {
	if draft.FontFamily != original.FontFamily {
		p.FontFamily = draft.FontFamily
	}
	if draft.FontSize != original.FontSize {
		p.FontSize = draft.FontSize
	}
	if draft.TabWidth != original.TabWidth {
		p.TabWidth = draft.TabWidth
	}
	if draft.HardTabs != original.HardTabs {
		p.HardTabs = draft.HardTabs
	}
	if draft.Wrap != original.Wrap {
		p.Wrap = draft.Wrap
	}
	if draft.JSONIndent != original.JSONIndent {
		p.JSONIndent = draft.JSONIndent
	}
	return p.normalized()
}

func (m *editorApp) applyEditorPreferences(preferences editorPreferences) {
	if preferences == m.editorPrefs {
		return
	}
	m.editorPrefs = preferences
	for _, tab := range m.tabs {
		tab.applyEditorPreferences(preferences)
	}
}

func (t *editorTab) applyEditorPreferences(preferences editorPreferences) {
	for _, pane := range []*editorPane{t.source, t.result, t.comparison} {
		pane.applyEditorPreferences(preferences)
	}
	if !t.indentOverride {
		t.setJSONIndent(preferences.JSONIndent)
	}
	t.applyDefaultWrap(preferences, t.format() == "JSON")
}

func (p *editorPane) applyEditorPreferences(preferences editorPreferences) {
	face := gfx.MonoFamily(preferences.FontFamily)
	p.input.MonoFace = face
	p.view.Snapshot.MonoFace = face
	p.view.Snapshot.FontSize = float32(preferences.FontSize)
	p.view.Snapshot.TabWidth = preferences.TabWidth
	p.text.SetCode(text.CodeOptions{TabWidth: preferences.TabWidth, HardTabs: preferences.HardTabs, AutoIndent: true})
}

func (t *editorTab) applyDefaultWrap(preferences editorPreferences, isJSON bool) {
	if !t.wrapOverride {
		t.setWrap(preferences.Wrap == wrapEnabled || preferences.Wrap == wrapByFileType && isJSON)
	}
}

func (t *editorTab) setWrap(wrap bool) {
	t.source.view.Snapshot.Wrap = wrap
	t.result.view.Snapshot.Wrap = wrap
	t.comparison.view.Snapshot.Wrap = wrap
}

func (t *editorTab) toggleWrap() {
	t.wrapOverride = true
	t.setWrap(!t.source.view.Snapshot.Wrap)
}

func (t *editorTab) setJSONIndent(indent int) {
	if t.jsonIndent != indent {
		t.jsonIndent = indent
		t.resultReady = false
	}
}
