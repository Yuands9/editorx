package editor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/text/find"
)

type largeEditQuery struct {
	query                 *find.Query
	expression, continued *regexp.Regexp
	foldedLiteral         []byte
	normalizeCRLF         bool
}

func newLargeEditQuery(query *find.Query) (largeEditQuery, error) {
	result := largeEditQuery{query: query}
	options := query.Options()
	pattern := query.Pattern()
	if query.Empty() || options.CaseSensitive && !options.Regexp && !strings.ContainsRune(pattern, '\r') {
		return result, nil
	}
	if !options.Regexp {
		if !options.CaseSensitive && strings.IndexFunc(pattern, func(ch rune) bool { return ch > unicode.MaxASCII || ch == '\r' }) < 0 {
			result.foldedLiteral = []byte(strings.ToLower(pattern))
		}
		pattern = regexp.QuoteMeta(pattern)
	}
	if !options.CaseSensitive {
		pattern = "(?i)" + pattern
	}
	pattern = "(?m)" + pattern
	var err error
	result.expression, err = regexp.Compile(pattern)
	if err != nil {
		return result, err
	}
	if len(result.foldedLiteral) > 0 {
		return result, nil
	}
	// Supply the preceding rune when resuming so ^ and word boundaries see
	// the real context. The first capture is the match after that prefix.
	result.continued, err = regexp.Compile("\\A(?s:..*?)(" + pattern + ")")
	return result, err
}

func (q largeEditQuery) walk(ctx context.Context, revision *largeEditRevision, from int64, visit func(text.Span, []int) bool) error {
	if q.query.Empty() {
		return nil
	}
	options := q.query.Options()
	if q.expression == nil || len(q.foldedLiteral) > 0 {
		needle := q.foldedLiteral
		if q.expression == nil {
			needle = []byte(q.query.Pattern())
		}
		return walkLargeEditLiteral(ctx, revision, from, needle, q.expression, options.WholeWord, func(span text.Span) bool {
			return visit(span, nil)
		})
	}
	reader := largeEditSearchReader{ctx: ctx, revision: revision, buffer: make([]byte, largeTextPageBytes), normalizeCRLF: q.normalizeCRLF}
	previousEnd := -1
	for from <= revision.size {
		reader.position = from
		expression := q.expression
		if from > 0 {
			reader.position -= int64(reader.precedingRuneSize(from))
			expression = q.continued
		}
		base := int(reader.position)
		groups := expression.FindReaderSubmatchIndex(&reader)
		if reader.err != nil {
			return reader.err
		}
		if groups == nil {
			return nil
		}
		if from > 0 {
			groups = groups[2:]
		}
		for i, offset := range groups {
			if offset >= 0 {
				groups[i] = base + offset
			}
		}
		span := text.Span{Start: groups[0], End: groups[1]}
		empty := span.Start == span.End
		word := !options.WholeWord || empty || reader.wholeWord(span)
		if word && (!empty || span.Start != previousEnd) && !visit(span, groups) {
			return reader.err
		}
		previousEnd = span.End
		from = int64(span.End)
		if empty || !word && !options.Regexp {
			_, size := reader.runeAt(int64(span.Start))
			from = int64(span.Start) + int64(max(1, size))
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return reader.err
}

// Literal search uses byte blocks and retains enough bytes for matches and
// word boundaries across block edges. ASCII folding preserves byte offsets.
func walkLargeEditLiteral(ctx context.Context, revision *largeEditRevision, from int64, needle []byte, folded *regexp.Regexp, wholeWord bool, visit func(text.Span) bool) error {
	if len(needle) == 0 {
		return nil
	}
	foldKelvin := folded != nil && bytes.ContainsRune(needle, 'k')
	foldLongS := folded != nil && bytes.ContainsRune(needle, 's')
	width := len(needle)
	if foldKelvin || foldLongS {
		// Unicode simple folding also matches Kelvin sign to K and long S to S.
		width *= 3
	}
	contextBytes := 0
	if wholeWord {
		contextBytes = utf8.UTFMax
	}
	buffer := make([]byte, largeTextPageBytes+width-1+2*contextBytes)
	for from < revision.size {
		if err := ctx.Err(); err != nil {
			return err
		}
		base := max(0, from-int64(contextBytes))
		limit := min(revision.size, from+largeTextPageBytes)
		end := min(revision.size, limit+int64(width-1+contextBytes))
		block := buffer[:end-base]
		if _, err := revision.ReadAt(block, base); err != nil {
			return err
		}
		unicodeFold := foldKelvin && bytes.Contains(block, []byte("K")) || foldLongS && bytes.Contains(block, []byte("ſ"))
		if folded != nil && !unicodeFold {
			for i, ch := range block {
				if ch >= 'A' && ch <= 'Z' {
					block[i] = ch + ('a' - 'A')
				}
			}
		}
		cursor := int(from - base)
		for base+int64(cursor) < limit {
			var start, end int
			if unicodeFold {
				span := folded.FindIndex(block[cursor:])
				if span == nil {
					break
				}
				start, end = cursor+span[0], cursor+span[1]
			} else {
				index := bytes.Index(block[cursor:], needle)
				if index < 0 {
					break
				}
				start = cursor + index
				end = start + len(needle)
			}
			if base+int64(start) >= limit {
				break
			}
			if wholeWord {
				first, size := utf8.DecodeRune(block[start:end])
				before, _ := utf8.DecodeLastRune(block[:start])
				last, _ := utf8.DecodeLastRune(block[start:end])
				after, _ := utf8.DecodeRune(block[end:])
				if isSearchWordRune(first) && isSearchWordRune(before) || isSearchWordRune(last) && isSearchWordRune(after) {
					cursor = start + size
					continue
				}
			}
			if !visit(text.Span{Start: int(base) + start, End: int(base) + end}) {
				return nil
			}
			cursor = end
		}
		from = max(limit, base+int64(cursor))
	}
	return nil
}

type largeEditSearchReader struct {
	ctx                  context.Context
	revision             *largeEditRevision
	buffer               []byte
	start, end, position int64
	err                  error
	normalizeCRLF        bool
}

func (r *largeEditSearchReader) at(offset int64) []byte {
	if r.err != nil || offset >= r.revision.size {
		return nil
	}
	if offset < r.start || r.end-offset < min(utf8.UTFMax, r.revision.size-offset) {
		r.start = max(0, offset-utf8.UTFMax)
		r.end = min(r.revision.size, r.start+int64(len(r.buffer)))
		r.err = r.ctx.Err()
		if r.err == nil {
			_, r.err = r.revision.ReadAt(r.buffer[:r.end-r.start], r.start)
		}
		if r.err != nil {
			return nil
		}
	}
	return r.buffer[offset-r.start : r.end-r.start]
}

func (r *largeEditSearchReader) runeAt(offset int64) (rune, int) {
	value := r.at(offset)
	if r.normalizeCRLF && len(value) >= 2 && value[0] == '\r' && value[1] == '\n' {
		// Present the editor's newline while keeping the original byte span.
		return '\n', 2
	}
	return utf8.DecodeRune(value)
}

func (r *largeEditSearchReader) ReadRune() (rune, int, error) {
	ch, size := r.runeAt(r.position)
	if r.err != nil {
		return 0, 0, r.err
	}
	if size == 0 {
		return 0, 0, io.EOF
	}
	r.position += int64(size)
	return ch, size, nil
}

func (r *largeEditSearchReader) precedingRuneSize(offset int64) int {
	start := max(0, offset-utf8.UTFMax)
	value := r.at(start)
	if r.err != nil {
		return 0
	}
	prefix := value[:offset-start]
	if r.normalizeCRLF && len(prefix) >= 2 && prefix[len(prefix)-2] == '\r' && prefix[len(prefix)-1] == '\n' {
		return 2
	}
	_, size := utf8.DecodeLastRune(prefix)
	return size
}

func isSearchWordRune(ch rune) bool {
	return ch == '_' || unicode.IsLetter(ch) || unicode.IsDigit(ch)
}

func (r *largeEditSearchReader) wholeWord(span text.Span) bool {
	start, end := int64(span.Start), int64(span.End)
	first, _ := r.runeAt(start)
	if start > 0 && isSearchWordRune(first) {
		previous, _ := r.runeAt(start - int64(r.precedingRuneSize(start)))
		if isSearchWordRune(previous) {
			return false
		}
	}
	last, _ := r.runeAt(end - int64(r.precedingRuneSize(end)))
	next, _ := r.runeAt(end)
	return !isSearchWordRune(last) || !isSearchWordRune(next)
}

func (q largeEditQuery) replacement(revision *largeEditRevision, span text.Span, groups []int, template string) (string, error) {
	if q.expression == nil || !q.query.Options().Regexp || !strings.Contains(template, "$") {
		return template, nil
	}
	value, err := revision.read(int64(span.Start), int64(span.End))
	if err != nil {
		return "", fmt.Errorf("读取正则匹配内容失败：%w", err)
	}
	for i, offset := range groups {
		if offset >= 0 {
			groups[i] = offset - span.Start
		}
	}
	return string(q.expression.ExpandString(nil, template, value, groups)), nil
}
