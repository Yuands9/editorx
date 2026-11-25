package editor

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

type largeEditSearchOutcome struct {
	match   text.Span
	count   int
	counted bool
}

func (t *editorTab) hasLargeEdit() bool {
	return t.largeText != nil && t.largeText.editing != nil
}

func (t *editorTab) refreshLargeEditSearch() {
	s, edit := &t.search, t.largeText.editing
	if s.composing() {
		return
	}
	pattern := s.query.CommittedString()
	if s.ready && s.pattern == pattern && s.options == s.lastOptions {
		return
	}
	s.pattern, s.lastOptions, s.ready = pattern, s.options, true
	s.message, s.errorText, s.compiled = "", "", nil
	edit.searchQuery, edit.searchJob, edit.searchSelected = largeEditQuery{}, nil, false
	t.largeText.selectedResult = -1
	if len(pattern) > largeTextQueryLimit {
		s.errorText = "查找内容过长"
	} else {
		query, err := s.options.compile(pattern)
		if err == nil {
			edit.searchQuery, err = newLargeEditQuery(query)
			edit.searchQuery.normalizeCRLF = t.doc.eol == "CRLF"
		}
		if err != nil {
			s.errorText = "正则表达式无效：" + err.Error()
		} else {
			s.compiled = query
		}
	}
	t.source.view.Snapshot.CurrentMark = text.Span{}
	t.markLargeTextMatches()
}

func (t *editorTab) largeEditSearchReady() bool {
	if t.largeText.busyEditing() || t.search.composing() {
		return false
	}
	if _, composing := t.source.text.Composition(); composing {
		return false
	}
	t.refreshLargeEditSearch()
	return t.search.compiled != nil && !t.search.compiled.Empty()
}

func (t *editorTab) findInLargeEdit(direction int) {
	s, edit := &t.search, t.largeText.editing
	if !t.largeEditSearchReady() {
		if s.query.CommittedString() == "" && !s.open {
			t.openSearch()
		}
		return
	}
	start, end := edit.position.rangeOffsets()
	from := end
	if direction < 0 {
		from = start
	}
	skipEmpty := edit.searchSelected && edit.searchMatch == (text.Span{Start: int(from), End: int(from)})
	t.startLargeEditFind(from, direction, skipEmpty)
}

func (t *editorTab) startLargeEditFind(from int64, direction int, skipEmpty bool) {
	s, edit := &t.search, t.largeText.editing
	query, revision, wrap := edit.searchQuery, edit.revision, s.wrap
	t.largeText.selectedResult, s.message = -1, "正在查找…"
	edit.searchJob = ui.NewEffect[largeEditSearchOutcome, int](func(ctx context.Context) (largeEditSearchOutcome, error) {
		match, err := searchLargeEdit(ctx, revision, query, from, direction, wrap, skipEmpty)
		return largeEditSearchOutcome{match: match}, err
	})
}

func searchLargeEdit(ctx context.Context, revision *largeEditRevision, query largeEditQuery, from int64, direction int, wrap, skipEmpty bool) (text.Span, error) {
	none := text.Span{Start: -1}
	search := func(at int64, skip bool) (text.Span, error) {
		if direction < 0 && at == 0 {
			return none, nil
		}
		if query.expression == nil && !query.query.Options().WholeWord {
			pattern := query.query.Pattern()
			start, err := findLargeEdit(ctx, revision, pattern, at, direction)
			if err != nil || start < 0 {
				return none, err
			}
			return text.Span{Start: int(start), End: int(start) + len(pattern)}, nil
		}
		match := none
		scanFrom := at
		if direction < 0 {
			scanFrom = 0
		}
		err := query.walk(ctx, revision, scanFrom, func(span text.Span, _ []int) bool {
			if direction < 0 {
				if int64(span.Start) >= at || int64(span.End) > at {
					return false
				}
				match = span
				return true
			}
			if skip && int64(span.Start) == at && span.Start == span.End {
				return true
			}
			match = span
			return false
		})
		return match, err
	}
	match, err := search(from, skipEmpty)
	if err != nil || match.Start >= 0 || !wrap {
		return match, err
	}
	if direction < 0 && from <= revision.size {
		return search(revision.size+1, false)
	}
	if direction > 0 && (from > 0 || skipEmpty) {
		return search(0, false)
	}
	return match, nil
}

func (t *editorTab) countLargeEditMatches() {
	if !t.largeEditSearchReady() {
		return
	}
	edit := t.largeText.editing
	query, revision := edit.searchQuery, edit.revision
	t.search.message = "正在计数…"
	edit.searchJob = ui.NewEffect[largeEditSearchOutcome, int](func(ctx context.Context) (largeEditSearchOutcome, error) {
		count := 0
		err := query.walk(ctx, revision, 0, func(text.Span, []int) bool {
			count++
			return true
		})
		return largeEditSearchOutcome{count: count, counted: true}, err
	})
}

func (t *editorTab) updateLargeEditSearch(c ui.C) {
	s, edit := &t.search, t.largeText.editing
	if edit.searchJob == nil {
		return
	}
	ui.Effect(c, edit.searchJob, 0)
	result, err, ready := edit.searchJob.Take()
	if !ready {
		return
	}
	edit.searchJob = nil
	if s.pattern != s.query.CommittedString() || s.lastOptions != s.options {
		s.message = ""
		return
	}
	switch {
	case err != nil:
		s.message = err.Error()
	case result.counted:
		s.message = fmt.Sprintf("当前范围共有 %d 个匹配", result.count)
	case result.match.Start < 0:
		s.message = "此方向没有更多匹配"
	default:
		if _, composing := t.source.text.Composition(); composing {
			s.message = ""
			return
		}
		s.message = ""
		t.selectLargeEditMatch(result.match)
	}
	if !s.open {
		if s.message != "" {
			t.largeText.problem = s.message
		}
		t.source.focus = true
	}
}

func (t *editorTab) selectLargeEditMatch(match text.Span) bool {
	state, edit := t.largeText, t.largeText.editing
	start, end := int64(match.Start), int64(match.End)
	if start < edit.viewport.start || start >= edit.viewport.end && edit.viewport.end != edit.revision.size || end > edit.viewport.end {
		if err := t.loadLargeEditWindow(start); err != nil {
			state.problem = err.Error()
			return false
		}
	}
	edit.pendingTop = false
	t.moveLargeEdit(largeEditPosition{anchor: end, caret: start}, true)
	w := edit.viewport
	t.source.view.Snapshot.CurrentMark = text.Span{Start: w.local(start), End: w.local(end)}
	edit.searchMatch, edit.searchSelected, state.problem = match, true, ""
	return true
}

func (t *editorTab) replaceInLargeEdit(all bool) {
	if !t.largeEditSearchReady() {
		return
	}
	state, edit, s := t.largeText, t.largeText.editing, &t.search
	template := t.largeEditInputValue(s.replacementText())
	if !utf8.ValidString(template) || strings.ContainsRune(template, 0) {
		s.message = "请输入有效的替换内容"
		return
	}
	start, end := edit.position.rangeOffsets()
	if !all && start == end && (!edit.searchSelected || edit.searchMatch.Start != int(start) || edit.searchMatch.End != int(end)) {
		t.findInLargeEdit(1)
		return
	}
	revision, query := edit.revision, edit.searchQuery
	edit.searchJob, edit.lastType = nil, time.Time{}
	s.message = "正在替换…"
	state.replacing = ui.NewEffect[largeEditReplacement, int](func(ctx context.Context) (largeEditReplacement, error) {
		if all {
			return replaceAllLargeEdit(ctx, revision, query, template)
		}
		result := largeEditReplacement{revision: revision, first: start, advance: true}
		var replaceErr error
		err := query.walk(ctx, revision, start, func(span text.Span, groups []int) bool {
			if span.Start != int(start) || span.End != int(end) {
				result.first = int64(span.Start)
				return false
			}
			result.skipEmpty = start == end
			value, err := query.replacement(revision, span, groups, template)
			if err == nil {
				var old string
				if int64(len(value)) == end-start {
					old, err = revision.read(start, end)
				}
				if err == nil && (int64(len(value)) != end-start || value != old) {
					result.revision, err = revision.apply(ctx, []largeEditChange{{Start: start, End: end, Text: value}})
					if err == nil {
						result.count = 1
					}
				}
			}
			replaceErr, result.first = err, start+int64(len(value))
			return false
		})
		if err != nil {
			return result, err
		}
		return result, replaceErr
	})
}
