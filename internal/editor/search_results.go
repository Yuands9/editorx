package editor

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/text/find"
	"github.com/vivaui/viva/ui"
)

type searchResultGroup struct {
	tab       *editorTab
	pane      *editorPane
	source    string
	matches   []text.Span
	stream    *largeSearchResults
	lines     []searchResultLine
	collapsed bool
	truncated bool
}

func (g searchResultGroup) count() int {
	if g.stream != nil {
		return len(g.stream.hits)
	}
	return len(g.matches)
}

func (g searchResultGroup) name() string {
	if g.tab.doc.path != "" {
		return g.tab.doc.path
	}
	return g.tab.title() + "（未保存）"
}

// Offsets are in the decoded text. Large-file hits keep their original navigation
// offsets separately so multiple hits on one displayed line can still be visited.
type searchResultLine struct {
	number, column            int
	first, end                int
	sourceStart, sourceEnd    int
	offset                    int64
	excerpt                   string
	cells                     int
	clippedLeft, clippedRight bool
	marks                     []text.Span
}

const searchResultExcerptBytes = 1024

func newSearchResultLine(number, column, first int, offset int64, prefix, suffix string, continued bool) searchResultLine {
	if end := strings.IndexByte(suffix[:min(len(suffix), searchResultExcerptBytes+1)], '\n'); end >= 0 {
		suffix = suffix[:end]
	}
	left := 0
	if len(prefix)+len(suffix) > searchResultExcerptBytes {
		left = max(0, len(prefix)-120)
		for left < len(prefix) && !utf8.RuneStart(prefix[left]) {
			left++
		}
	}
	prefix = prefix[left:]
	right := min(len(suffix), searchResultExcerptBytes-len(prefix))
	for right < len(suffix) && !utf8.RuneStart(suffix[right]) {
		right--
	}
	clipped := right < len(suffix)
	value := strings.TrimSuffix(prefix+suffix[:right], "\r")
	if prefix == "" || right == 0 {
		// A single slice could otherwise keep the whole source allocation alive.
		value = strings.Clone(value)
	}
	cells := 0
	for _, ch := range value {
		if ch == '\t' {
			cells += 4
		} else {
			cells += text.CellWidth(ch)
		}
	}
	return searchResultLine{number: number, column: column, first: first, end: first, offset: offset - int64(len(prefix)), excerpt: value, cells: cells,
		clippedLeft: continued || left > 0, clippedRight: clipped}
}

func (line *searchResultLine) addHit(index int, start, end int64) {
	line.end = index + 1
	from, to := start-line.offset, end-line.offset
	length := int64(len(line.excerpt))
	if from > length {
		// A match on the LF in CRLF can be one byte after the excerpt.
		if line.clippedRight || from > length+1 {
			return
		}
		from = length
	}
	if to < 0 {
		return
	}
	mark := text.Span{Start: int(max(0, from)), End: int(min(length, to))}
	if mark.Start == mark.End {
		line.cells++
	}
	line.marks = append(line.marks, mark)
}

func (g *searchResultGroup) collectLines() {
	line, start, end := 1, 0, 0
	for i, hit := range g.matches {
		if i == 0 || hit.Start > end {
			scanned := g.source[start:hit.Start]
			line += strings.Count(scanned, "\n")
			if last := strings.LastIndexByte(scanned, '\n'); last >= 0 {
				start += last + 1
			}
			end = len(g.source)
			if next := strings.IndexByte(g.source[hit.Start:], '\n'); next >= 0 {
				end = hit.Start + next
			}
			prefix := g.source[start:hit.Start]
			row := newSearchResultLine(line, utf8.RuneCountInString(prefix)+1, i, int64(hit.Start), prefix, g.source[hit.Start:end], false)
			row.sourceStart, row.sourceEnd = start, end
			g.lines = append(g.lines, row)
		}
		g.lines[len(g.lines)-1].addHit(i, int64(hit.Start), int64(hit.End))
	}
}

type searchResultRow struct {
	group, line int // group -1 for the query, line -1 for a file heading
}

type searchResults struct {
	open, allOpen            bool
	fixed                    bool
	origin                   *editorTab
	pane                     *editorPane
	query                    string
	options                  searchOptions
	groups                   []searchResultGroup
	rows                     []searchResultRow
	total                    int
	truncated                bool
	dirty                    bool
	collapsed                bool
	files                    int
	lineDigits, previewCells int
	cellWidth                float32
	resetHorizontal          bool
	list                     ui.ListState
	split                    ui.SplitterState
}

func (r *searchResults) begin() {
	r.open, r.collapsed, r.resetHorizontal = true, false, true
	if r.split.Ratio == 0 {
		r.split.Ratio = 0.60
	}
	r.list = ui.ListState{Name: r.rowLabel, NoTypeahead: true}
	r.list.ScrollTo(0, ui.ListStart)
}

func (m *editorApp) findAll(t *editorTab, allOpen bool) {
	if m.startLargeDocumentSearch(t, allOpen, false) {
		return
	}
	s := &t.search
	if _, composing := s.query.Composition(); composing {
		return
	}
	s.refresh()
	if s.query.String() == "" || s.errorText != "" {
		return
	}
	r := &m.results
	r.allOpen, r.origin, r.pane, r.query, r.options = allOpen, t, s.target, s.query.String(), s.options
	if m.searchBatch != nil && !m.searchBatch.replacing {
		m.searchBatch.origin.search.message = ""
		m.searchBatch = nil
	}
	r.groups = nil
	r.fixed, r.dirty = false, true
	r.begin()
	r.refresh(m)
	s.close()
}

func (r *searchResults) refresh(m *editorApp) {
	if r.fixed {
		return
	}
	tabs := m.tabs
	if slices.ContainsFunc(tabs, func(tab *editorTab) bool { return !tab.canSearchText() }) {
		tabs = nil
		for _, tab := range m.tabs {
			if tab.canSearchText() {
				tabs = append(tabs, tab)
			}
		}
	}
	if !r.allOpen {
		tabs = nil
		if slices.Contains(m.tabs, r.origin) && r.origin.searchPaneVisible(r.pane) {
			tabs = []*editorTab{r.origin}
		}
	}
	changed := r.dirty || len(tabs) != len(r.groups)
	for i, t := range tabs {
		p := t.source
		if !r.allOpen {
			p = r.pane
		}
		if _, composing := p.text.Composition(); composing {
			return
		}
		if i >= len(r.groups) || r.groups[i].tab != t || r.groups[i].pane != p || r.groups[i].source != p.text.String() {
			changed = true
		}
	}
	if !changed {
		return
	}
	query, err := r.options.compile(r.query)
	if err != nil {
		return
	}
	groups := make([]searchResultGroup, 0, len(tabs))
	r.total, r.truncated = 0, false
	for _, t := range tabs {
		p := t.source
		if !r.allOpen {
			p = r.pane
		}
		g := searchResultGroup{tab: t, pane: p, source: p.text.String()}
		limit := find.DefaultLimit - r.total
		matches, truncated := query.Matches(nil, g.source, max(1, limit))
		g.truncated = truncated || limit == 0 && len(matches) > 0
		if limit > 0 {
			g.matches = matches
			r.total += len(matches)
		}
		r.truncated = r.truncated || g.truncated
		for _, old := range r.groups {
			if old.tab == t {
				g.collapsed = old.collapsed
				break
			}
		}
		g.collectLines()
		groups = append(groups, g)
	}
	r.groups, r.dirty = groups, false
	r.buildRows()
}

func (r *searchResults) buildRows() {
	r.rows = r.rows[:0]
	r.files, r.lineDigits, r.previewCells = 0, 1, 0
	if r.total == 0 {
		return
	}
	r.rows = append(r.rows, searchResultRow{group: -1, line: -1})
	for i, g := range r.groups {
		if len(g.lines) == 0 {
			continue
		}
		r.files++
		if r.collapsed {
			continue
		}
		r.rows = append(r.rows, searchResultRow{group: i, line: -1})
		if g.collapsed {
			continue
		}
		r.lineDigits = max(r.lineDigits, len(strconv.Itoa(g.lines[len(g.lines)-1].number)))
		for j, line := range g.lines {
			r.rows = append(r.rows, searchResultRow{group: i, line: j})
			cells := line.cells
			if line.clippedLeft {
				cells++
			}
			if line.clippedRight {
				cells++
			}
			if count := line.end - line.first; count > len(line.marks) {
				cells += 12
			}
			r.previewCells = max(r.previewCells, cells)
		}
	}
}

func (r *searchResults) rowLabel(index int) string {
	row := r.rows[index]
	if row.group < 0 {
		return "结果搜索词 " + r.query
	}
	group := r.groups[row.group]
	if row.line < 0 {
		return "结果分组 " + group.tab.title()
	}
	line := group.lines[row.line]
	return fmt.Sprintf("结果 %s 行 %d 列 %d", group.tab.title(), line.number, line.column)
}

func (m *editorApp) chooseSearchResult(index int) {
	r := &m.results
	if index < 0 || index >= len(r.rows) {
		return
	}
	row := r.rows[index]
	if row.group < 0 {
		r.collapsed = !r.collapsed
		r.buildRows()
		return
	}
	group := &r.groups[row.group]
	if row.line < 0 {
		group.collapsed = !group.collapsed
		r.buildRows()
		return
	}
	line := group.lines[row.line]
	if group.stream != nil {
		m.chooseLargeSearchResult(group, line.first)
		return
	}
	if !slices.Contains(m.tabs, group.tab) || group.source != group.pane.text.String() {
		r.refresh(m)
		if r.fixed {
			m.status = "内容已变化，请重新查找"
		} else {
			m.status = "搜索内容已更新，请重新选择结果"
		}
		return
	}
	tab := group.tab
	m.activate(tab)
	if group.pane == tab.comparison {
		tab.jsonCompare = true
	} else if group.pane == tab.result {
		tab.jsonCompare = false
	}
	if tab.format() == "Markdown" && tab.markdownMode == "预览" {
		tab.markdownMode = "分屏"
	}
	s := &tab.search
	s.query.SetSelection(text.Range{Start: 0, End: s.query.RuneCount()})
	s.query.Insert(r.query)
	s.target, s.options, s.matches, s.current, s.ready = group.pane, r.options, group.matches, line.first, false
	s.replacing = s.replacing && group.pane == tab.source
	s.reveal()
	group.pane.view.Snapshot.Marks = group.matches
	group.pane.focus = true
	tab.source.focus = group.pane == tab.source
	m.status = fmt.Sprintf("已定位 %s · 行 %d，列 %d", tab.title(), line.number, line.column)
}

func (m *editorApp) copySearchResults() {
	r := &m.results
	var out strings.Builder
	fmt.Fprintf(&out, "搜索 %q · %d 个匹配\n", r.query, r.total)
	for _, g := range r.groups {
		if g.count() == 0 {
			continue
		}
		fmt.Fprintf(&out, "%s (%d 个匹配)\n", g.name(), g.count())
		for _, line := range g.lines {
			fmt.Fprintf(&out, "  行 %d: ", line.number)
			if g.stream == nil {
				out.WriteString(strings.TrimSuffix(g.source[line.sourceStart:line.sourceEnd], "\r"))
			} else {
				if line.clippedLeft {
					out.WriteString("…")
				}
				out.WriteString(line.excerpt)
				if line.clippedRight {
					out.WriteString("…")
				}
			}
			out.WriteByte('\n')
		}
	}
	if r.truncated {
		out.WriteString("仅包含前 10,000 个匹配。\n")
	}
	if err := app.SetClipboardText(out.String()); err != nil {
		m.fail(err)
		return
	}
	m.status = "已复制搜索结果"
}

// 结果面板即使隐藏，也不能继续引用已关闭文档和其大段原文。
func (r *searchResults) removeTab(tab *editorTab) {
	if r.origin == tab && len(r.groups) == 1 && r.groups[0].stream != nil {
		*r = searchResults{split: r.split}
		return
	}
	if r.origin == tab {
		r.origin, r.pane = nil, nil
	}
	previous := len(r.groups)
	r.groups = slices.DeleteFunc(r.groups, func(group searchResultGroup) bool { return group.tab == tab })
	if len(r.groups) == previous {
		return
	}
	r.total, r.dirty, r.truncated = 0, true, false
	for _, group := range r.groups {
		r.total += group.count()
		r.truncated = r.truncated || group.truncated
	}
	r.buildRows()
}
