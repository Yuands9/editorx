package editor

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"

	"github.com/vivaui/viva/text"
)

func findLargeEdit(ctx context.Context, r *largeEditRevision, query string, from int64, direction int) (int64, error) {
	result := int64(-1)
	from = max(0, min(r.size, from))
	if len(query) == 0 {
		return result, nil
	}
	needle := []byte(query)
	if direction >= 0 {
		err := walkLargeEditLiteral(ctx, r, from, needle, nil, false, func(span text.Span) bool {
			result = int64(span.Start)
			return false
		})
		return result, err
	}
	buffer := make([]byte, largeTextPageBytes+len(query)-1)
	for end := from; end > 0; {
		if err := ctx.Err(); err != nil {
			return -1, err
		}
		start := max(0, end-int64(len(buffer)))
		data := buffer[:end-start]
		if _, err := r.ReadAt(data, start); err != nil {
			return -1, err
		}
		if offset := bytes.LastIndex(data, needle); offset >= 0 {
			return start + int64(offset), nil
		}
		end -= largeTextPageBytes
	}
	return -1, nil
}

func collectLargeEditResults(ctx context.Context, r *largeEditRevision, query largeEditQuery, limit int) (largeSearchResults, error) {
	result := largeSearchResults{revision: r}
	var readErr error
	var cachedLine int
	var cachedStart, previousMatch int64
	var column int
	countBuffer := make([]byte, 32<<10)
	err := query.walk(ctx, r, 0, func(span text.Span, _ []int) bool {
		if len(result.hits) >= limit {
			result.truncated = true
			return false
		}
		if err := ctx.Err(); err != nil {
			readErr = err
			return false
		}
		start, end := int64(span.Start), int64(span.End)
		line, err := r.lineAt(start)
		if err != nil {
			readErr = err
			return false
		}
		match := len(result.hits)
		firstOnLine := match == 0 || cachedLine != line
		if firstOnLine {
			cachedStart, err = r.lineStart(line)
			if err != nil {
				readErr = err
				return false
			}
			cachedLine, previousMatch, column = line, cachedStart, 1
		}
		for previousMatch < start {
			data := countBuffer[:min(int64(len(countBuffer)), start-previousMatch)]
			if _, err := r.ReadAt(data, previousMatch); err != nil {
				readErr = err
				return false
			}
			for _, b := range data {
				if utf8.RuneStart(b) {
					column++
				}
			}
			previousMatch += int64(len(data))
		}
		if firstOnLine {
			// Read once per displayed line, including when a large line has many hits.
			left := cachedStart
			if start-left >= searchResultExcerptBytes {
				left = start - 120
			}
			right := min(r.size, start+searchResultExcerptBytes+utf8.UTFMax)
			for !r.boundary(left) {
				left++
			}
			for !r.boundary(right) {
				right--
			}
			value, err := r.read(left, right)
			if err != nil {
				readErr = err
				return false
			}
			result.lines = append(result.lines, newSearchResultLine(line+1, column, match, start, value[:start-left], value[start-left:], left > cachedStart))
		}
		result.lines[len(result.lines)-1].addHit(match, start, end)
		result.hits = append(result.hits, largeSearchHit{span: span, line: line + 1, column: column})
		return true
	})
	return result, errors.Join(err, readErr)
}

type largeEditReplacement struct {
	revision           *largeEditRevision
	count              int
	first              int64
	advance, skipEmpty bool
}

func replaceAllLargeEdit(ctx context.Context, r *largeEditRevision, query largeEditQuery, replacement string) (largeEditReplacement, error) {
	if query.expression == nil && query.query.Pattern() == replacement {
		return largeEditReplacement{revision: r}, nil
	}
	var changes []largeEditChange
	const sparseLimit = 16 << 10
	err := walkLargeEditReplacements(ctx, r, query, replacement, func(change largeEditChange) bool {
		changes = append(changes, change)
		return len(changes) <= sparseLimit
	})
	if err != nil || len(changes) == 0 {
		return largeEditReplacement{revision: r}, err
	}
	if len(changes) <= sparseLimit {
		next, err := r.apply(ctx, changes)
		return largeEditReplacement{revision: next, count: len(changes), first: changes[0].Start}, err
	}
	// With dense matches, a piece per match would use more memory than the
	// source. Build one contiguous region in the same scratch file instead.
	var writeErr error
	var offset int64
	count := 0
	next := &largeEditRevision{store: r.store}
	input := bufio.NewReaderSize(io.NewSectionReader(r, 0, r.size), largeTextPageBytes)
	writer := bufio.NewWriterSize(largeEditAppendWriter{next}, largeTextPageBytes)
	copyBytes := func(n int64) error {
		for n > 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
			chunk := int(min(n, largeTextPageBytes))
			data, err := input.Peek(chunk)
			if err != nil {
				return err
			}
			if _, err := writer.Write(data); err != nil {
				return err
			}
			_, _ = input.Discard(chunk)
			n -= int64(chunk)
		}
		return nil
	}
	err = walkLargeEditReplacements(ctx, r, query, replacement, func(change largeEditChange) bool {
		if writeErr = copyBytes(change.Start - offset); writeErr != nil {
			return false
		}
		if _, writeErr = input.Discard(int(change.End - change.Start)); writeErr != nil {
			return false
		}
		if _, writeErr = writer.WriteString(change.Text); writeErr != nil {
			return false
		}
		count++
		offset = change.End
		return true
	})
	if err = errors.Join(err, writeErr); err == nil {
		err = copyBytes(r.size - offset)
	}
	if err == nil {
		err = writer.Flush()
	}
	return largeEditReplacement{revision: next, count: count, first: changes[0].Start}, err
}

func walkLargeEditReplacements(ctx context.Context, r *largeEditRevision, query largeEditQuery, template string, visit func(largeEditChange) bool) error {
	var replaceErr error
	err := query.walk(ctx, r, 0, func(span text.Span, groups []int) bool {
		value, err := query.replacement(r, span, groups, template)
		if err != nil {
			replaceErr = err
			return false
		}
		start, end := int64(span.Start), int64(span.End)
		if query.expression != nil && len(value) == span.End-span.Start {
			old, err := r.read(start, end)
			if err != nil {
				replaceErr = err
				return false
			}
			if old == value {
				return true
			}
		}
		return visit(largeEditChange{Start: start, End: end, Text: value})
	})
	return errors.Join(err, replaceErr)
}

type largeEditAppendWriter struct {
	revision *largeEditRevision
}

func (w largeEditAppendWriter) Write(value []byte) (int, error) {
	piece, err := w.revision.store.append(value)
	if err != nil {
		return 0, err
	}
	w.revision.addPiece(piece)
	return len(value), nil
}

func (t *editorTab) reportLargeEditReplacement(result largeEditReplacement, err error) {
	s := &t.search
	if err != nil {
		s.message = err.Error()
		return
	}
	if result.count > 0 {
		t.noteLargeEdit(result.revision, largeEditPosition{result.first, result.first}, false)
	}
	if result.advance {
		t.refreshLargeEditSearch()
		if s.compiled != nil && !s.compiled.Empty() {
			t.startLargeEditFind(result.first, 1, result.skipEmpty)
		}
		return
	}
	if result.count == 0 {
		s.message = "没有需要替换的匹配"
		return
	}
	s.message = fmt.Sprintf("已替换 %d 处", result.count)
}
