package editor

import (
	"fmt"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/text/find"
)

func (m *editorApp) windowShortcut(event gfx.KeyEvent) gfx.EventResult {
	if m.active != nil && !m.appSettings.open && !m.interactionBlocked() {
		s := &m.active.search
		if s.open && !event.Target.IsZero() && !event.Release && event.Key == gfx.KeyEnter {
			queryFocused := event.Target == s.field.ID
			replacementFocused := s.replacing && event.Target == s.replaceField.ID
			if queryFocused || replacementFocused {
				editor := s.query
				if replacementFocused {
					editor = s.replacement
				}
				if _, composing := editor.Composition(); composing {
					return gfx.EventResult{}
				}
				if event.Modifiers == gfx.ModifierControl {
					editor.Insert("\n")
					return gfx.EventResult{Handled: true, Repaint: true}
				}
				if queryFocused && (event.Modifiers == 0 || event.Modifiers == gfx.ModifierShift) {
					return m.active.searchKey(event, nil)
				}
			}
		}
	}
	return m.aiPromptShortcut(event)
}

func (s *documentSearch) composing() bool {
	_, query := s.query.Composition()
	_, replacement := s.replacement.Composition()
	return query || s.replacing && replacement
}

func (s *documentSearch) replacementText() string {
	value := strings.ReplaceAll(s.replacement.String(), "\r\n", "\n")
	if s.options.mode != searchText {
		value = searchEscapeCharacters.Replace(value)
	}
	// The literal multiline query uses an internal regexp for CRLF matching.
	if s.options.mode != searchRegexp && s.compiled.Options().Regexp {
		value = strings.ReplaceAll(value, "$", "$$")
	}
	return value
}

func (s *documentSearch) replaceCurrent() {
	if s.composing() || s.target.readOnly {
		return
	}
	editor := s.target.text
	if _, composing := editor.Composition(); composing {
		return
	}
	s.refresh()
	if s.compiled == nil || s.current < 0 {
		return
	}
	selection := editor.Selection()
	current := find.Index(s.matches, editor.ByteOffset(selection.Start), editor.ByteOffset(selection.End))
	if current < 0 {
		s.reveal()
		return
	}
	match := s.matches[current]
	source := editor.String()
	value := s.compiled.Replacement(source, match, s.replacementText())
	if source[match.Start:match.End] == value {
		s.step(1)
		return
	}
	editor.ReplaceRange(selection, value)
	caret := editor.ByteOffset(editor.Caret())
	s.refresh()
	if s.current >= 0 {
		if s.wrap || s.matches[s.current].Start >= caret {
			s.reveal()
		} else {
			s.current = -1
			s.target.view.Snapshot.CurrentMark = text.Span{}
		}
	}
	s.message = "已替换 1 处"
}

func (m *editorApp) replaceAll(t *editorTab, allOpen bool) {
	s := &t.search
	if s.composing() {
		return
	}
	s.refresh()
	if s.compiled == nil || s.compiled.Empty() {
		return
	}
	template := s.replacementText()
	tabs := []*editorTab{t}
	if allOpen {
		tabs = m.tabs
	}
	type fileReplacement struct {
		editor *text.Editor
		edits  []text.Replacement
	}
	var files []fileReplacement
	var count int
	var matches []find.Edit
	for _, tab := range tabs {
		if !tab.canSearchText() || tab.source.readOnly {
			continue
		}
		editor := tab.source.text
		if _, composing := editor.Composition(); composing {
			s.message = "请先完成中文输入，再替换"
			return
		}
		source := editor.String()
		matches = s.compiled.Replacements(matches[:0], source, template)
		if len(matches) == 0 {
			continue
		}
		edits := make([]text.Replacement, 0, len(matches))
		for _, match := range matches {
			if source[match.Start:match.End] == match.Text {
				continue
			}
			edits = append(edits, text.Replacement{
				Range: text.Range{Start: editor.RuneIndex(match.Start), End: editor.RuneIndex(match.End)},
				Text:  match.Text,
			})
		}
		if len(edits) == 0 {
			continue
		}
		if err := editor.ValidateReplaceRanges(edits, text.ReplaceRangesOptions{}); err != nil {
			m.fail(fmt.Errorf("无法替换 %s：%w", tab.title(), err))
			return
		}
		files = append(files, fileReplacement{editor: editor, edits: edits})
		count += len(edits)
	}
	for _, file := range files {
		// Each editor stays on the UI thread between validation and application.
		if err := file.editor.ReplaceRanges(file.edits); err != nil {
			m.fail(err)
			return
		}
	}
	if count == 0 {
		s.message = "没有需要替换的内容"
		return
	}
	s.refresh()
	s.message = fmt.Sprintf("已替换 %d 处 · %d 个文件", count, len(files))
}
