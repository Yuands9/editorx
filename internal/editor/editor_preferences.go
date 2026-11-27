package editor

import (
	"slices"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

const (
	minEditorFontSize = 10
	maxEditorFontSize = 32
)

var editorTabWidths = []int{2, 4, 8}

var editorWrapLanguages = func() []string {
	languages := slices.Concat(syntaxLanguages[:], dataSyntaxLanguages[:])
	for _, syntax := range codeSyntaxes {
		languages = append(languages, syntax.name)
	}
	return languages
}()

type editorPreferences struct {
	FontFamily          string   `json:"fontFamily,omitempty"`
	FontSize            int      `json:"fontSize"`
	TabWidth            int      `json:"tabWidth"`
	HardTabs            bool     `json:"hardTabs,omitempty"`
	WrapFileTypes       []string `json:"wrapFileTypes"`
	LegacyWrap          string   `json:"wrap,omitempty"`
	JSONIndent          int      `json:"jsonIndent"`
	AutomaticCompletion bool     `json:"automaticCompletion,omitempty"`
}

func defaultEditorPreferences() editorPreferences {
	return editorPreferences{FontSize: 16, TabWidth: 4, WrapFileTypes: []string{"JSON"}, JSONIndent: 4}
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
	if p.WrapFileTypes == nil {
		switch p.LegacyWrap {
		case "on":
			p.WrapFileTypes = slices.Clone(editorWrapLanguages)
		case "off":
			p.WrapFileTypes = []string{}
		default:
			p.WrapFileTypes = defaults.WrapFileTypes
		}
	}
	p.LegacyWrap = ""
	selected := make([]string, 0, len(p.WrapFileTypes))
	for _, language := range editorWrapLanguages {
		if p.wraps(language) {
			selected = append(selected, language)
		}
	}
	p.WrapFileTypes = selected
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
	if draft.AutomaticCompletion != original.AutomaticCompletion {
		p.AutomaticCompletion = draft.AutomaticCompletion
	}
	for _, language := range editorWrapLanguages {
		if enabled := draft.wraps(language); enabled != original.wraps(language) {
			p.setWrapFileType(language, enabled)
		}
	}
	if draft.JSONIndent != original.JSONIndent {
		p.JSONIndent = draft.JSONIndent
	}
	return p.normalized()
}

func (p editorPreferences) equal(other editorPreferences) bool {
	return p.FontFamily == other.FontFamily && p.FontSize == other.FontSize &&
		p.TabWidth == other.TabWidth && p.HardTabs == other.HardTabs &&
		p.AutomaticCompletion == other.AutomaticCompletion &&
		p.JSONIndent == other.JSONIndent && slices.Equal(p.WrapFileTypes, other.WrapFileTypes)
}

func (p editorPreferences) wraps(language string) bool {
	return slices.Contains(p.WrapFileTypes, language)
}

func (p *editorPreferences) setWrapFileType(language string, enabled bool) {
	if enabled {
		p.WrapFileTypes = append(slices.Clone(p.WrapFileTypes), language)
		return
	}
	p.WrapFileTypes = slices.DeleteFunc(slices.Clone(p.WrapFileTypes), func(value string) bool {
		return value == language
	})
}

func (m *editorApp) applyEditorPreferences(preferences editorPreferences) {
	if preferences.equal(m.editorPrefs) {
		return
	}
	m.editorPrefs = preferences
	for _, tab := range m.tabs {
		tab.applyEditorPreferences(preferences)
	}
	if state := m.historyCompare; state != nil {
		state.invalidator.Invalidate()
	}
}

func (t *editorTab) applyEditorPreferences(preferences editorPreferences) {
	for _, pane := range []*editorPane{t.source, t.result, t.comparison} {
		pane.applyEditorPreferences(preferences)
	}
	if !t.indentOverride {
		t.setJSONIndent(preferences.JSONIndent)
	}
	t.applyDefaultWrap(preferences)
}

func (p *editorPane) applyEditorPreferences(preferences editorPreferences) {
	p.automaticCompletion = preferences.AutomaticCompletion
	face := gfx.MonoFamily(preferences.FontFamily)
	p.input.MonoFace = face
	p.view.Snapshot.MonoFace = face
	p.view.Snapshot.FontSize = float32(preferences.FontSize)
	p.view.Snapshot.TabWidth = preferences.TabWidth
	p.text.SetCode(text.CodeOptions{TabWidth: preferences.TabWidth, HardTabs: preferences.HardTabs, AutoIndent: true})
}

func (t *editorTab) applyDefaultWrap(preferences editorPreferences) {
	if !t.wrapOverride && t.largeText == nil {
		t.setWrap(preferences.wraps(t.format()))
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
