package editor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/text/find"
)

type largeLineIndex struct {
	file os.FileInfo
	// Values count the line breaks before each page. The last entry is for EOF.
	breaks []int
}

func sameTextFile(a, b os.FileInfo) bool {
	return a != nil && b != nil && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime()) && os.SameFile(a, b)
}

func indexLargeText(ctx context.Context, path string, enc textEncoding) (largeLineIndex, error) {
	file, err := openSharedTextFile(path)
	if err != nil {
		return largeLineIndex{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return largeLineIndex{}, err
	}
	size := info.Size()
	enc, _, err = inspectTextEncoding(file, size, enc)
	if err != nil {
		return largeLineIndex{}, err
	}
	count := largeTextPageCount(size)
	index := largeLineIndex{file: info, breaks: make([]int, count+1)}
	buffer := make([]byte, largeTextPageBytes+4096)
	order := enc.utf16Order()
	start := int64(0)
	for page := int64(0); page < count; page++ {
		if err := ctx.Err(); err != nil {
			return largeLineIndex{}, err
		}
		end, err := largeTextBoundary(file, (page+1)*largeTextPageBytes, size, enc)
		if err != nil {
			return largeLineIndex{}, err
		}
		window := buffer[:end-start]
		if len(window) > 0 {
			if _, err := file.ReadAt(window, start); err != nil {
				return largeLineIndex{}, err
			}
		}
		lines := 0
		if order == nil {
			lines = bytes.Count(window, []byte{'\n'})
		} else {
			for offset := 0; offset+1 < len(window); offset += 2 {
				if order.Uint16(window[offset:]) == '\n' {
					lines++
				}
			}
		}
		index.breaks[page+1] = index.breaks[page] + lines
		start = end
	}
	current, err := file.Stat()
	if err != nil {
		return largeLineIndex{}, err
	}
	if !sameTextFile(info, current) {
		return largeLineIndex{}, errors.New(uiText("文件已变化，请重新读取"))
	}
	return index, nil
}

func (index largeLineIndex) goToLine(path string, enc textEncoding, line int) (largeTextPage, int, error) {
	wanted := line - 1
	last := len(index.breaks) - 1
	if wanted < 0 || last < 1 || wanted > index.breaks[last] {
		return largeTextPage{}, 0, errors.New(uiText("行号超出文件范围"))
	}
	pageIndex := sort.Search(last, func(i int) bool { return index.breaks[i+1] >= wanted })
	page, err := readLargeTextPage(path, int64(pageIndex), -1, "", enc)
	if err != nil {
		return largeTextPage{}, 0, err
	}
	if !sameTextFile(index.file, page.file) {
		return largeTextPage{}, 0, errors.New(uiText("文件已变化，请重新读取"))
	}
	offset := text.OffsetAt(page.value, text.Position{Line: wanted - index.breaks[pageIndex]})
	if offset == len(page.value) && pageIndex+1 < last {
		page, err = readLargeTextPage(path, int64(pageIndex+1), -1, "", enc)
		if err == nil && !sameTextFile(index.file, page.file) {
			err = errors.New(uiText("文件已变化，请重新读取"))
		}
		offset = 0
	}
	return page, offset, err
}

type largeSearchHit struct {
	page         int64
	span         text.Span
	line, column int
}

type largeSearchResults struct {
	file      os.FileInfo
	revision  *largeEditRevision
	hits      []largeSearchHit
	lines     []searchResultLine
	truncated bool
}

func collectLargeSearchResults(ctx context.Context, path, query string, requested textEncoding) (largeSearchResults, error) {
	file, err := os.Open(path)
	if err != nil {
		return largeSearchResults{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return largeSearchResults{}, err
	}
	enc, bom, err := inspectTextEncoding(file, info.Size(), requested)
	if err != nil {
		return largeSearchResults{}, err
	}
	size := info.Size()
	result := largeSearchResults{file: info}
	if len(query) == 0 || len(query) > largeTextQueryLimit {
		return result, nil
	}
	page, err := readLargeTextChunk(file, size, 0, -1, "", enc, bom)
	if err != nil {
		return largeSearchResults{}, err
	}
	line, previousColumn, skip := 1, 0, 0
	var decodedStart int64
pages:
	for i, count := int64(0), largeTextPageCount(size); i < count; i++ {
		if err := ctx.Err(); err != nil {
			return largeSearchResults{}, err
		}
		var next largeTextPage
		var edge string
		edgeStart := max(0, len(page.value)-len(query)+1)
		if i+1 < count {
			next, err = readLargeTextChunk(file, size, i+1, -1, "", enc, bom)
			if err != nil {
				return largeSearchResults{}, err
			}
			if len(query) > 1 {
				tailEnd := min(len(query)-1, len(next.value))
				for tailEnd < len(next.value) && !utf8.RuneStart(next.value[tailEnd]) {
					tailEnd++
				}
				edge = page.value[edgeStart:] + next.value[:tailEnd]
			}
		}
		column, consumed, lineStart := previousColumn, 0, 0
		start := skip
		skip = 0
		for start < len(page.value) {
			found := strings.Index(page.value[start:], query)
			if found < 0 {
				if edge == "" {
					break
				}
				start = max(start, edgeStart)
				found = strings.Index(edge[start-edgeStart:], query)
				if found < 0 || start+found >= len(page.value) {
					break
				}
			}
			from, to := start+found, start+found+len(query)
			if len(result.hits) >= find.DefaultLimit {
				result.truncated = true
				break pages
			}
			prefix := page.value[consumed:from]
			if breaks := strings.Count(prefix, "\n"); breaks > 0 {
				line += breaks
				lineStart = consumed + strings.LastIndexByte(prefix, '\n') + 1
				column = utf8.RuneCountInString(page.value[lineStart:from])
			} else {
				column += utf8.RuneCountInString(prefix)
			}
			match := len(result.hits)
			offset := decodedStart + int64(from)
			if match == 0 || result.lines[len(result.lines)-1].number != line {
				suffix := page.value[from:]
				if len(suffix) < searchResultExcerptBytes && !strings.ContainsRune(suffix, '\n') {
					suffix += next.value[:min(len(next.value), searchResultExcerptBytes+utf8.UTFMax)]
				}
				result.lines = append(result.lines, newSearchResultLine(line, column+1, match, offset, page.value[lineStart:from], suffix, lineStart == 0 && previousColumn > 0))
			}
			result.lines[len(result.lines)-1].addHit(match, offset, decodedStart+int64(to))
			result.hits = append(result.hits, largeSearchHit{line: line, column: column + 1, page: i, span: text.Span{Start: from, End: to}})
			consumed, start = from, to
			skip = max(0, to-len(page.value))
		}
		rest := page.value[consumed:]
		if breaks := strings.Count(rest, "\n"); breaks > 0 {
			line += breaks
			previousColumn = utf8.RuneCountInString(rest[strings.LastIndexByte(rest, '\n')+1:])
		} else {
			previousColumn = column + utf8.RuneCountInString(rest)
		}
		decodedStart += int64(len(page.value))
		page = next
	}
	current, err := file.Stat()
	if err != nil {
		return largeSearchResults{}, err
	}
	if !sameTextFile(info, current) {
		return largeSearchResults{}, errors.New(uiText("文件已变化，请重新查找"))
	}
	return result, nil
}

func (m *editorApp) showLargeSearchResults(tab *editorTab, query string, result largeSearchResults) {
	r := &m.results
	r.allOpen, r.origin, r.pane, r.query, r.options = false, tab, tab.source, query, searchOptions{CaseSensitive: true}
	r.fixed, r.dirty = true, false
	r.groups = []searchResultGroup{{tab: tab, stream: &result, lines: result.lines, truncated: result.truncated}}
	r.total, r.truncated = len(result.hits), result.truncated
	r.begin()
	r.buildRows()
}

func (m *editorApp) stepLargeSearch(tab *editorTab, direction int) {
	r, state := &m.results, tab.largeText
	if state.busyEditing() {
		return
	}
	large := tab.hasLargeEdit()
	query, options := state.query.Text(), searchOptions{CaseSensitive: true}
	if large {
		query, options = tab.search.query.CommittedString(), tab.search.options
	}
	if state.selectedResult >= 0 && r.origin == tab && len(r.groups) == 1 && r.groups[0].stream != nil && r.query == query && r.options == options {
		group := &r.groups[0]
		next := state.selectedResult + direction
		if next < 0 || next >= group.count() {
			if large {
				tab.findInLargeEdit(direction)
			} else {
				state.problem = uiText("此方向没有更多匹配")
			}
			return
		}
		if large {
			start, end := state.editing.position.rangeOffsets()
			span := group.stream.hits[state.selectedResult].span
			if start != int64(span.Start) || end != int64(span.End) {
				tab.findInLargeEdit(direction)
				return
			}
		}
		m.chooseLargeSearchResult(group, next)
		if r.open && !r.collapsed && !group.collapsed && state.selectedResult == next {
			line := sort.Search(len(group.lines), func(i int) bool { return group.lines[i].end > next })
			// A streamed search has one file, after the query heading.
			r.list.Select(line + 2)
			r.list.ScrollIntoView(line + 2)
		}
		return
	}
	if large {
		tab.findInLargeEdit(direction)
	} else {
		tab.findInLargeText(direction)
	}
}

func (m *editorApp) chooseLargeSearchResult(group *searchResultGroup, index int) {
	tab := group.tab
	hit := group.stream.hits[index]
	query := m.results.query
	if tab.largeText == nil || tab.largeText.busyEditing() {
		return
	}
	if edit := tab.largeText.editing; edit != nil {
		if edit.revision != group.stream.revision {
			tab.largeText.problem = uiText("内容已变化，请重新查找")
			return
		}
		if _, composing := tab.source.text.Composition(); composing {
			return
		}
		m.activate(tab)
		state := tab.largeText
		s := &tab.search
		if s.query.CommittedString() != query {
			s.field.SetText(query)
		}
		s.options = m.results.options
		tab.refreshLargeEditSearch()
		edit.searchJob = nil
		if !tab.selectLargeEditMatch(hit.span) {
			return
		}
		state.selectedResult = index
		tab.markLargeTextMatches()
		tab.source.focus = true
		m.status = fmt.Sprintf(uiText("已定位 %s · 行 %d，列 %d"), tab.title(), hit.line, hit.column)
		return
	}
	page, err := readLargeTextPage(tab.doc.path, hit.page, -1, "", tab.doc.encoding)
	if err == nil && !sameTextFile(group.stream.file, page.file) {
		err = errors.New(uiText("文件已变化，请重新查找"))
	}
	if err == nil && hit.span.End > len(page.value) {
		next, nextErr := readLargeTextPage(tab.doc.path, hit.page+1, -1, "", tab.doc.encoding)
		if nextErr != nil || !sameTextFile(page.file, next.file) || hit.span.End-len(page.value) > len(next.value) {
			err = errors.New(uiText("文件已变化，请重新查找"))
		} else {
			page.value += next.value[:hit.span.End-len(page.value)]
		}
	}
	if err == nil && (hit.span.Start > len(page.value) || hit.span.End > len(page.value) || page.value[hit.span.Start:hit.span.End] != query) {
		err = errors.New(uiText("文件已变化，请重新查找"))
	}
	if err != nil {
		m.status = err.Error()
		return
	}
	m.activate(tab)
	tab.largeText.search, tab.largeText.found = nil, -1
	page.selectionStart = utf8.RuneCountInString(page.value[:hit.span.Start])
	page.selectionEnd = page.selectionStart + utf8.RuneCountInString(query)
	tab.largeText.highlight = query
	if tab.largeText.query.Text() != query {
		tab.largeText.query.SetText(query)
	}
	tab.largeText.selectedResult = index
	tab.applyLargeTextPage(page)
	tab.source.focus = true
	m.status = fmt.Sprintf(uiText("已定位 %s · 行 %d，列 %d"), tab.title(), hit.line, hit.column)
}
