package editor

import (
	"cmp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

const (
	automaticCompletionDelay  = 100 * time.Millisecond
	automaticCompletionRadius = 16 << 10
	manualCompletionRadius    = 128 << 10
	completionWordLimit       = 128
	completionItemLimit       = 200
	completionVisibleRows     = 8
)

type editorCompletion struct {
	editor                      *text.Editor
	revision                    uint64
	start, caret, end, selected int
	items                       []string
	buttons                     [completionVisibleRows]gfx.ID
	automatic, justOpened       bool
}

type completionTyping struct {
	editor    *text.Editor
	revision  uint64
	readyAt   time.Duration
	caret     int
	composing bool
}

func completionWord(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }

func completionFoldedPrefix(word, lowerPrefix string) bool {
	for _, expected := range lowerPrefix {
		if word == "" {
			return false
		}
		first, size := utf8.DecodeRuneInString(word)
		if first != expected && unicode.ToLower(first) != expected {
			return false
		}
		word = word[size:]
	}
	return true
}

func completionWordRange(value string, caret int) (start, end int) {
	start, end = caret, caret
	for start > 0 {
		r, size := utf8.DecodeLastRuneInString(value[:start])
		if !completionWord(r) {
			break
		}
		start -= size
		if caret-start > completionWordLimit {
			return -1, -1
		}
	}
	for end < len(value) {
		r, size := utf8.DecodeRuneInString(value[end:])
		if !completionWord(r) {
			break
		}
		end += size
		if end-start > completionWordLimit {
			return -1, -1
		}
	}
	return start, end
}

func completionCandidates(value, language string, start, caret, end, radius int) []string {
	prefix := value[start:caret]
	foldedPrefix := strings.ToLower(prefix)
	words := map[string]int{}
	add := func(word string, distance int) {
		if len(word) <= len(prefix) || len(word) > completionWordLimit ||
			word == value[start:end] || !strings.HasPrefix(word, prefix) && !completionFoldedPrefix(word, foldedPrefix) {
			return
		}
		if closest, seen := words[word]; seen {
			words[word] = min(closest, distance)
		} else if len(words) < completionItemLimit {
			words[word] = distance
		}
	}
	// Scan only nearby words. Neither the popup nor a pending refresh owns the
	// document string, so an edit cannot leave a full prior buffer alive.
	from, to := max(0, caret-radius), min(len(value), caret+radius)
	atWordBoundary := from == 0
	if from > 0 && utf8.RuneStart(value[from]) {
		previous, _ := utf8.DecodeLastRuneInString(value[:from])
		atWordBoundary = !completionWord(previous)
	}
	for from < to && !utf8.RuneStart(value[from]) {
		from++
	}
	wordStart := -1
	for offset, r := range value[from:to] {
		if completionWord(r) {
			if wordStart < 0 {
				wordStart = from + offset
			}
		} else if wordStart >= 0 {
			if wordStart > from || atWordBoundary {
				wordEnd := from + offset
				add(value[wordStart:wordEnd], max(caret-wordEnd, wordStart-caret, 0))
			}
			wordStart = -1
		}
	}
	if wordStart >= 0 && to == len(value) && (wordStart > from || atWordBoundary) {
		add(value[wordStart:to], max(wordStart-caret, 0))
	}
	if syntax := codeSyntaxByName[language]; syntax != nil {
		for word := range syntax.words {
			add(word, radius+1)
		}
	}
	if language == "JSON" {
		for _, word := range []string{"true", "false", "null"} {
			add(word, radius+1)
		}
	}
	type rankedWord struct {
		word     string
		distance int
		exact    bool
	}
	var firstWords [12]rankedWord
	ordered := firstWords[:0]
	if len(words) > len(firstWords) {
		ordered = make([]rankedWord, 0, len(words))
	}
	for word, distance := range words {
		ordered = append(ordered, rankedWord{word, distance, strings.HasPrefix(word, prefix)})
	}
	slices.SortFunc(ordered, func(a, b rankedWord) int {
		if a.exact != b.exact {
			if a.exact {
				return -1
			}
			return 1
		}
		if distance := cmp.Compare(a.distance, b.distance); distance != 0 {
			return distance
		}
		return strings.Compare(a.word, b.word)
	})
	items := make([]string, len(ordered))
	for i, candidate := range ordered {
		items[i] = strings.Clone(candidate.word)
	}
	return items
}

func (p *editorPane) completionSelectionEmpty() bool {
	selection := p.text.Selection()
	return selection.Start == selection.End && p.text.SelectionCount() == 1
}

func (p *editorPane) openCompletion() {
	if p.readOnly || p.text.CompositionActive() || !p.completionSelectionEmpty() {
		return
	}
	p.setCompletion(false)
	p.completion.justOpened = true
	p.completionTyping.readyAt = 0
	p.focus = true
}

func (p *editorPane) setCompletion(automatic bool) {
	value, caret := p.text.String(), p.text.ByteOffset(p.text.Caret())
	start, end := completionWordRange(value, caret)
	radius, selected := manualCompletionRadius, 0
	if automatic {
		radius, selected = automaticCompletionRadius, -1
	}
	var items []string
	if start >= 0 {
		items = completionCandidates(value, p.view.Snapshot.Language, start, caret, end, radius)
	}
	if automatic && len(items) == 0 {
		p.completion = nil
		return
	}
	p.completion = &editorCompletion{editor: p.text, revision: p.text.TrackEdits(),
		start: start, caret: caret, end: end, selected: selected, items: items, automatic: automatic}
}

func (p *editorPane) completionCurrent(s *editorCompletion) bool {
	return p.text == s.editor && p.text.TrackEdits() == s.revision && p.completionSelectionEmpty() &&
		p.text.ByteOffset(p.text.Caret()) == s.caret && !p.text.CompositionActive()
}

func (p *editorPane) acceptCompletion(s *editorCompletion, index int) bool {
	if p.completion != s || index < 0 || index >= len(s.items) || !p.completionCurrent(s) {
		return false
	}
	p.text.BreakUndoGroup()
	p.text.ReplaceRange(text.Range{Start: p.text.RuneIndex(s.start), End: p.text.RuneIndex(s.end)}, s.items[index])
	p.text.BreakUndoGroup()
	p.completion = nil
	p.completionTyping.revision = p.text.TrackEdits()
	p.completionTyping.readyAt = 0
	p.focus = true
	return true
}

func (p *editorPane) completionKey(event gfx.KeyEvent) gfx.EventResult {
	if event.Release || event.Modifiers != 0 {
		return gfx.EventResult{}
	}
	if event.Key == gfx.KeyEscape {
		p.completionTyping.readyAt = 0
	}
	s := p.completion
	if s == nil {
		return gfx.EventResult{}
	}
	if !p.completionCurrent(s) {
		p.completion = nil
		return gfx.EventResult{}
	}
	switch event.Key {
	case gfx.KeyEscape:
		p.completion = nil
	case gfx.KeyUp, gfx.KeyDown, gfx.KeyPageUp, gfx.KeyPageDown:
		if len(s.items) == 0 {
			return gfx.EventResult{}
		}
		switch event.Key {
		case gfx.KeyPageUp:
			s.selected = max(0, s.selected-completionVisibleRows)
		case gfx.KeyPageDown:
			s.selected = min(len(s.items)-1, max(0, s.selected)+completionVisibleRows)
		case gfx.KeyDown:
			s.selected = (s.selected + 1) % len(s.items)
		case gfx.KeyUp:
			s.selected = (max(0, s.selected) + len(s.items) - 1) % len(s.items)
		}
	case gfx.KeyEnter, gfx.KeyTab:
		if event.Key == gfx.KeyEnter && s.selected < 0 || !p.acceptCompletion(s, max(0, s.selected)) {
			p.completion = nil
			return gfx.EventResult{}
		}
	default:
		return gfx.EventResult{}
	}
	return gfx.EventResult{Handled: true, Repaint: true}
}

func (p *editorPane) updateTypingCompletion(canvas *gfx.Canvas) {
	tracker := &p.completionTyping
	editor, revision := p.text, p.text.TrackEdits()
	if tracker.editor != editor {
		*tracker = completionTyping{editor: editor, revision: revision}
		return
	}
	if editor.CompositionActive() {
		tracker.readyAt, tracker.composing = 0, true
		return
	}
	previous, composed := tracker.revision, tracker.composing
	tracker.revision, tracker.composing = revision, false
	if !p.completionSelectionEmpty() || p.completion != nil && !p.completion.automatic {
		tracker.readyAt = 0
		return
	}
	if previous != revision {
		tracker.readyAt = 0
		if revision-previous != 1 && !composed {
			return
		}
		var last [1]text.Edit
		edits, _, ok := editor.EditsSince(revision-1, last[:0])
		if !ok || len(edits) != 1 {
			return
		}
		edit := edits[0]
		caret := editor.ByteOffset(editor.Caret())
		popup := p.completion
		refresh := popup != nil && popup.automatic && popup.editor == editor && popup.revision == previous && !composed
		if (edit.Inserted == 0 && (!refresh || edit.Removed == 0)) || edit.Inserted > completionWordLimit ||
			edit.Removed > completionWordLimit || edit.Start+edit.Inserted != caret {
			return
		}
		value := editor.String()
		start, _ := completionWordRange(value, caret)
		if start < 0 || edit.Start < start || utf8.RuneCountInString(value[start:caret]) < 2 {
			return
		}
		if refresh && start == popup.start {
			p.setCompletion(true)
			if p.completion != nil && popup.selected >= 0 {
				p.completion.selected = slices.Index(p.completion.items, popup.items[popup.selected])
			}
			return
		}
		tracker.readyAt = canvas.Now() + automaticCompletionDelay
		tracker.caret = editor.Caret()
	}
	if tracker.readyAt == 0 {
		return
	}
	if editor.Caret() != tracker.caret {
		tracker.readyAt = 0
		return
	}
	if remaining := tracker.readyAt - canvas.Now(); remaining > 0 {
		canvas.RequestFrameIn(remaining)
		return
	}
	tracker.readyAt = 0
	p.setCompletion(true)
}

func (p *editorPane) completionView(c ui.C, editorID gfx.ID, available bool) {
	canvas := c.Canvas()
	if !available || p.readOnly || canvas == nil {
		p.completion, p.completionTyping = nil, completionTyping{}
		return
	}
	focused := canvas.Focused() == editorID
	if focused && p.automaticCompletion && p.input.Editor != nil && canvas.HasClock() {
		p.updateTypingCompletion(canvas)
	} else {
		p.completionTyping = completionTyping{}
	}
	s := p.completion
	if s == nil {
		return
	}
	buttonFocused := !canvas.Focused().IsZero() && slices.Contains(s.buttons[:], canvas.Focused())
	if p.text.CompositionActive() || !p.completionSelectionEmpty() ||
		!focused && !buttonFocused && !s.justOpened || s.automatic && !p.automaticCompletion {
		p.completion = nil
		return
	}
	s.justOpened = false
	if !p.completionCurrent(s) {
		p.completion = nil
		if s.automatic || s.editor != p.text || s.start < 0 {
			return
		}
		start, _ := completionWordRange(p.text.String(), p.text.ByteOffset(p.text.Caret()))
		if start != s.start {
			return
		}
		p.setCompletion(false)
		s = p.completion
	}
	position, ok := p.view.Code.CaretBounds()
	if !ok {
		return
	}
	height := float32(max(1, min(completionVisibleRows, len(s.items)))*27 + 8)
	viewport := c.Bounds()
	bounds := gfx.Rect{X: position.X, Y: position.Y + position.Height, Width: min(280, viewport.Width), Height: height}
	bounds.X = max(viewport.X, min(bounds.X, viewport.X+viewport.Width-bounds.Width))
	if bounds.Y+height > viewport.Y+viewport.Height {
		bounds.Y = max(viewport.Y, position.Y-height)
	}
	ui.Overlay(c, ui.OverlaySpec{Band: ui.LayerPaneChrome, Bounds: bounds}).Children(func() {
		ui.Column(c).Fill().Pad(4).Radius(6).Background(c.Theme().Popover).Border(1, c.Theme().Border).Clip().Children(func() {
			if len(s.items) == 0 {
				ui.Text(c, uiText("没有匹配的文档词或关键字")).FontSize(12).Muted()
				return
			}
			clear(s.buttons[:])
			start := max(0, s.selected-completionVisibleRows+1)
			for i := start; i < min(start+completionVisibleRows, len(s.items)); i++ {
				item := s.items[i]
				row := ui.Row(c).FillWidth().Height(27).PadX(8).Gap(8).Items(ui.Center).Radius(4).
					HitWith(ui.ID(c.KeyInt(i)), item, gfx.HitOptions{Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand, SkipTab: true}).
					Hover(ui.Look{}.Background(c.Theme().Hover))
				s.buttons[i-start] = row.N().ID
				if i == s.selected {
					row.Background(c.Theme().Secondary)
				}
				row.Children(func() {
					ui.Text(c, item).Grow(1).MinWidth(0).FontSize(12).Ellipsis()
					if s.automatic && i == max(0, s.selected) {
						ui.Text(c, "Tab").FontSize(11).Muted()
					}
				})
				if row.Clicked() {
					index := i
					c.After(func() { p.acceptCompletion(s, index) })
				}
			}
		})
	})
}
