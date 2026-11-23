package editor

import (
	"regexp"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/text/find"
	"github.com/vivaui/viva/ui"
)

type searchMode uint8

const (
	searchText searchMode = iota
	searchEscapes
	searchRegexp
)

var searchEscapeCharacters = strings.NewReplacer(
	"\\r\\n", "\n",
	"\\\\", "\\",
	"\\n", "\n",
	"\\r", "\r",
	"\\t", "\t",
)

type searchOptions struct {
	mode                     searchMode
	CaseSensitive, WholeWord bool
}

func (o searchOptions) findOptions() find.Options {
	return find.Options{CaseSensitive: o.CaseSensitive, WholeWord: o.WholeWord, Regexp: o.mode == searchRegexp}
}

func (o searchOptions) compile(pattern string) (*find.Query, error) {
	pattern = strings.ReplaceAll(pattern, "\r\n", "\n")
	if o.mode == searchEscapes {
		pattern = searchEscapeCharacters.Replace(pattern)
	}
	options := o.findOptions()
	// Literal multiline input matches both line ending styles in mixed files.
	if !options.Regexp && strings.Contains(pattern, "\n") {
		pattern = strings.ReplaceAll(regexp.QuoteMeta(pattern), "\n", "\\r?\\n")
		options.Regexp = true
	}
	return find.Compile(pattern, options)
}

type documentSearch struct {
	open, focus           bool
	centerOnOpen          bool
	replacing             bool
	query, replacement    *text.Editor
	field, replaceField   ui.Field
	compiled              *find.Query
	inputKey              func(gfx.KeyEvent) gfx.EventResult
	header                ui.Handlers
	bounds                gfx.Rect
	dragStart, dragOrigin gfx.Point
	dragging              bool
	wrap                  bool
	message               string
	target                *editorPane
	options               searchOptions
	matches               []text.Span
	current               int
	truncated             bool
	errorText             string
	ready                 bool
	source, pattern       string
	lastOptions           searchOptions
}

func (t *editorTab) initSearch(m *editorApp) {
	s := &t.search
	s.query, s.replacement = text.NewEditor(""), text.NewEditor("")
	s.field.Bind(s.query)
	s.replaceField.Bind(s.replacement)
	s.target = t.source
	s.current = -1
	s.wrap = true
	s.header.Pointer = func(event gfx.PointerEvent) gfx.EventResult {
		switch event.Kind {
		case gfx.PointerDown:
			if event.Button != gfx.PointerButtonPrimary {
				return gfx.EventResult{}
			}
			s.dragging = true
			s.dragStart = gfx.Point{X: event.X, Y: event.Y}
			s.dragOrigin = gfx.Point{X: s.bounds.X, Y: s.bounds.Y}
		case gfx.PointerMove, gfx.PointerUp:
			if !s.dragging || (event.Kind == gfx.PointerUp && event.Button != gfx.PointerButtonPrimary) {
				return gfx.EventResult{}
			}
			x := max(12, min(s.dragOrigin.X+event.X-s.dragStart.X, event.Viewport.Width-s.bounds.Width-12))
			y := max(12, min(s.dragOrigin.Y+event.Y-s.dragStart.Y, event.Viewport.Height-s.bounds.Height-12))
			moved := x != s.bounds.X || y != s.bounds.Y
			s.bounds.X, s.bounds.Y = x, y
			if event.Kind == gfx.PointerUp {
				s.dragging = false
			}
			return gfx.EventResult{Handled: true, Repaint: moved, PreferLatestFrame: moved}
		case gfx.PointerExit:
			s.dragging = false
		default:
			return gfx.EventResult{}
		}
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	t.bindSearchWindow(m)
}

func (t *editorTab) bindSearchWindow(m *editorApp) {
	s := &t.search
	s.inputKey = func(event gfx.KeyEvent) gfx.EventResult {
		if result := m.searchFieldShortcut(event); result.Handled {
			return result
		}
		if result := m.searchNavigation(t, event, nil); result.Handled {
			return result
		}
		return m.functionShortcut(event)
	}
	for _, pane := range []*editorPane{t.source, t.result, t.comparison} {
		pane.searchKey = func(event gfx.KeyEvent) gfx.EventResult {
			if result := m.searchNavigation(t, event, pane); result.Handled {
				return result
			}
			return m.functionShortcut(event)
		}
	}
}

func (t *editorTab) canSearchText() bool {
	return t.office == nil && !t.readOnlyPreview()
}

func (t *editorTab) openSearch() { t.showSearch(false) }

func (t *editorTab) openReplace() { t.showSearch(true) }

func (t *editorTab) showSearch(replacing bool) {
	if !t.canSearchText() {
		return
	}
	s := &t.search
	target := t.source
	if !replacing {
		if s.open && t.searchPaneVisible(s.target) {
			target = s.target
		}
		for _, pane := range []*editorPane{t.source, t.result, t.comparison} {
			if pane.focused && t.searchPaneVisible(pane) {
				target = pane
				break
			}
		}
	}
	target.text.CancelComposition()
	s.query.CancelComposition()
	s.replacement.CancelComposition()
	if !s.open || target.focused {
		selection := target.text.Selection()
		selected := target.text.String()[target.text.ByteOffset(selection.Start):target.text.ByteOffset(selection.End)]
		if selected != "" {
			if s.options.mode == searchEscapes {
				selected = strings.ReplaceAll(selected, "\\", "\\\\")
			}
			s.field.SetText(selected)
		}
	}
	s.query.SetSelection(text.Range{Start: 0, End: s.query.RuneCount()})
	s.centerOnOpen = !s.open
	s.target, s.open, s.focus, s.ready, s.replacing = target, true, true, false, replacing
	if t.format() == "HTML" && t.htmlMode == "预览" {
		t.htmlMode = "分屏"
	}
	if t.format() == "Markdown" && t.markdownMode == "预览" {
		t.markdownMode = "分屏"
	}
	for _, pane := range []*editorPane{t.source, t.result, t.comparison} {
		pane.focus = false
	}
}

func (t *editorTab) searchPaneVisible(p *editorPane) bool {
	return t.canSearchText() && (p == t.source || t.format() == "JSON" && (p == t.comparison && t.jsonCompare || p == t.result && !t.jsonCompare))
}

func (m *editorApp) searchNavigation(t *editorTab, event gfx.KeyEvent, pane *editorPane) gfx.EventResult {
	s := &t.search
	if event.Release || m.interactionBlocked() || !s.open && s.query.String() == "" || s.composing() {
		return gfx.EventResult{}
	}
	if event.Key == gfx.KeyEscape && event.Modifiers == 0 && s.open {
		s.close()
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	chord, ok := keymap.FromEvent(event)
	if !ok {
		return gfx.EventResult{}
	}
	direction := 0
	switch chord {
	case m.keyBindings.chord(shortcutFindNext):
		direction = 1
	case m.keyBindings.chord(shortcutFindPrevious):
		direction = -1
	}
	if direction == 0 {
		return gfx.EventResult{}
	}
	if pane != nil && !s.replacing && s.target != pane {
		s.target, s.ready = pane, false
	}
	s.step(direction)
	return gfx.EventResult{Handled: true, Repaint: true}
}

func (s *documentSearch) close() {
	s.query.CancelComposition()
	s.replacement.CancelComposition()
	s.open, s.focus, s.dragging = false, false, false
	s.target.view.Snapshot.Marks = nil
	s.target.view.Snapshot.CurrentMark = text.Span{}
	s.target.focus = true
}

func (s *documentSearch) refresh() {
	if _, composing := s.query.Composition(); composing {
		return
	}
	if _, composing := s.target.text.Composition(); composing {
		return
	}
	source, pattern := s.target.text.String(), s.query.String()
	if s.ready && s.source == source && s.pattern == pattern && s.lastOptions == s.options {
		return
	}
	queryChanged := !s.ready || s.pattern != pattern || s.lastOptions != s.options
	if queryChanged {
		s.message = ""
	}
	s.source, s.pattern, s.lastOptions, s.ready = source, pattern, s.options, true
	s.matches, s.errorText, s.truncated, s.current = s.matches[:0], "", false, -1
	query, err := s.options.compile(pattern)
	s.compiled = query
	if err != nil {
		s.errorText = "正则表达式无效：" + err.Error()
	} else {
		s.matches, s.truncated = query.Matches(s.matches, source, find.DefaultLimit)
		selection := s.target.text.Selection()
		s.current = find.Next(s.matches, s.target.text.ByteOffset(selection.Start))
	}
	s.target.view.Snapshot.Marks = s.matches
	s.target.view.Snapshot.CurrentMark = text.Span{}
	if s.current >= 0 {
		// 查询变化时选中当前项；正文继续编辑时保留原选区。
		s.target.view.Snapshot.CurrentMark = s.matches[s.current]
		if queryChanged {
			s.reveal()
		}
	}
}

func (s *documentSearch) step(direction int) {
	s.refresh()
	if len(s.matches) == 0 {
		return
	}
	next := s.current + direction
	if !s.wrap && (next < 0 || next >= len(s.matches)) {
		s.message = "已到达文档边界"
		return
	}
	s.message = ""
	s.current = (next + len(s.matches)) % len(s.matches)
	s.reveal()
}

func (s *documentSearch) reveal() {
	match := s.matches[s.current]
	p := s.target
	p.updateFolds()
	line := strings.Count(p.text.String()[:match.Start], "\n")
	for _, fold := range p.folds {
		if line >= fold.Header && line <= fold.LastLine {
			delete(p.collapsed, fold.Header)
		}
	}
	p.view.Snapshot.CurrentMark = match
	p.text.SetSelection(text.Range{Start: p.text.RuneIndex(match.Start), End: p.text.RuneIndex(match.End)})
	p.view.Code.Reveal(match.Start)
}

func (m *editorApp) searchView(c ui.C, t *editorTab, blocked bool) {
	s := &t.search
	for _, p := range []*editorPane{t.source, t.result, t.comparison} {
		if !s.open || p != s.target {
			p.view.Snapshot.Marks = nil
			p.view.Snapshot.CurrentMark = text.Span{}
		}
	}
	if !s.open {
		return
	}
	if !t.searchPaneVisible(s.target) || s.replacing && s.target != t.source {
		s.target, s.ready = t.source, false
	}
	s.refresh()
}
