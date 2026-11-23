package editor

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/text/find"
	"github.com/vivaui/viva/ui"
)

type searchResultGroup struct {
	tab       *editorTab
	pane      *editorPane
	source    string
	matches   []text.Span
	collapsed bool
}

type searchResultRow struct {
	group, match         int
	line, column         int
	before, value, after string
}

type searchResults struct {
	open, allOpen bool
	origin        *editorTab
	pane          *editorPane
	query         string
	options       find.Options
	groups        []searchResultGroup
	rows          []searchResultRow
	total         int
	truncated     bool
	dirty         bool
	list          ui.ListState
	split         ui.SplitterState
}

func (m *editorApp) findAll(t *editorTab, allOpen bool) {
	s := &t.search
	if _, composing := s.query.Composition(); composing {
		return
	}
	s.refresh()
	if s.query.String() == "" || s.errorText != "" {
		return
	}
	r := &m.results
	r.open, r.allOpen, r.origin, r.pane, r.query, r.options = true, allOpen, t, s.target, s.query.String(), s.options
	r.groups = nil
	if r.split.Ratio == 0 {
		r.split.Ratio = 0.60
	}
	r.list = ui.ListState{}
	r.list.Name = r.rowLabel
	r.list.NoTypeahead = true
	r.refresh(m)
	s.close()
}

func (r *searchResults) refresh(m *editorApp) {
	tabs := m.tabs
	if slices.ContainsFunc(tabs, func(tab *editorTab) bool { return tab.picture != nil || tab.office != nil }) {
		tabs = nil
		for _, tab := range m.tabs {
			if tab.picture == nil && tab.office == nil {
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
	query, err := find.Compile(r.query, r.options)
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
		if limit == 0 {
			if len(matches) > 0 {
				r.truncated = true
			}
		} else {
			g.matches = matches
			r.total += len(matches)
			r.truncated = r.truncated || truncated
		}
		for _, old := range r.groups {
			if old.tab == t {
				g.collapsed = old.collapsed
				break
			}
		}
		groups = append(groups, g)
	}
	r.groups, r.dirty = groups, false
	r.buildRows()
}

func (r *searchResults) buildRows() {
	r.rows = nil
	for i, g := range r.groups {
		if len(g.matches) == 0 {
			continue
		}
		r.rows = append(r.rows, searchResultRow{group: i, match: -1})
		if g.collapsed {
			continue
		}
		starts := []int{0}
		for at, ch := range g.source {
			if ch == '\n' {
				starts = append(starts, at+1)
			}
		}
		previousFrom, previousLine, column := 0, -1, 1
		for j, match := range g.matches {
			line := sort.SearchInts(starts, match.Start+1) - 1
			end := len(g.source)
			if line+1 < len(starts) {
				end = starts[line+1] - 1
			}
			from, to := match.Start, min(end, match.End)
			prefixStart := from
			for count := 0; prefixStart > starts[line] && count < 24; count++ {
				_, size := utf8.DecodeLastRuneInString(g.source[starts[line]:prefixStart])
				prefixStart -= size
			}
			before := g.source[prefixStart:from]
			if prefixStart > starts[line] {
				before = "…" + before
			}
			value := g.source[from:to]
			if value == "" {
				value = "│"
			}
			suffix := strings.TrimSuffix(g.source[to:end], "\r")
			suffixEnd := 0
			for count := 0; suffixEnd < len(suffix) && count < 100; count++ {
				_, size := utf8.DecodeRuneInString(suffix[suffixEnd:])
				suffixEnd += size
			}
			after := suffix[:suffixEnd]
			if suffixEnd < len(suffix) || match.End > end {
				after += "…"
			}
			if line != previousLine {
				previousFrom, column = starts[line], 1
			}
			column += utf8.RuneCountInString(g.source[previousFrom:from])
			previousFrom, previousLine = from, line
			r.rows = append(r.rows, searchResultRow{group: i, match: j, line: line + 1, column: column, before: strings.ReplaceAll(before, "\t", "    "), value: value, after: strings.ReplaceAll(after, "\t", "    ")})
		}
	}
}

func (r *searchResults) rowLabel(index int) string {
	row := r.rows[index]
	group := r.groups[row.group]
	if row.match < 0 {
		return "结果分组 " + group.tab.title()
	}
	return fmt.Sprintf("结果 %s 行 %d 列 %d", group.tab.title(), row.line, row.column)
}

func (m *editorApp) chooseSearchResult(index int) {
	r := &m.results
	if index < 0 || index >= len(r.rows) {
		return
	}
	row := r.rows[index]
	group := &r.groups[row.group]
	if row.match < 0 {
		group.collapsed = !group.collapsed
		r.buildRows()
		return
	}
	if !slices.Contains(m.tabs, group.tab) || group.source != group.pane.text.String() {
		r.refresh(m)
		m.status = "搜索内容已更新，请重新选择结果"
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
	s.target, s.options, s.matches, s.current, s.ready = group.pane, r.options, group.matches, row.match, false
	s.reveal()
	group.pane.view.Snapshot.Marks = group.matches
	group.pane.focus = true
	tab.source.focus = group.pane == tab.source
	m.status = fmt.Sprintf("已定位 %s · 行 %d，列 %d", tab.title(), row.line, row.column)
}

func (m *editorApp) copySearchResults() {
	r := &m.results
	var out strings.Builder
	fmt.Fprintf(&out, "搜索 %q · %d 个匹配\n", r.query, r.total)
	for _, g := range r.groups {
		if len(g.matches) == 0 {
			continue
		}
		name := g.tab.doc.path
		if name == "" {
			name = g.tab.title()
		}
		fmt.Fprintf(&out, "%s (%d 个匹配)\n", name, len(g.matches))
		for _, match := range g.matches {
			line := strings.Count(g.source[:match.Start], "\n") + 1
			start := strings.LastIndex(g.source[:match.Start], "\n") + 1
			end := len(g.source)
			if next := strings.IndexByte(g.source[match.Start:], '\n'); next >= 0 {
				end = match.Start + next
			}
			fmt.Fprintf(&out, "  行 %d: %s\n", line, g.source[start:end])
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

func (m *editorApp) searchResultsView(c ui.C) {
	r := &m.results
	r.refresh(m)
	files := 0
	for _, g := range r.groups {
		if len(g.matches) > 0 {
			files++
		}
	}
	ui.Column(c.Part("search-results")).FillWidth().Grow(1).MinHeight(0).Background(m.theme.Card).Children(func() {
		ui.Row(c).FillWidth().Height(35).PadX(12).Gap(8).Items(ui.Center).Children(func() {
			ui.Text(c, "搜索结果").Bold().FontSize(13)
			summary := fmt.Sprintf("“%s” · %d 个匹配 · %d 个文档", r.query, r.total, files)
			if r.truncated {
				summary += " · 已截断至 10,000 项"
			}
			ui.Text(c, summary).FontSize(12).Muted().Grow(1).MinWidth(0).Ellipsis()
			if ui.Button(c, "复制搜索结果").Icon(copyIcon).IconOnly().Ghost().Width(26).Height(26).Tooltip("复制搜索结果").Clicked() {
				c.After(m.copySearchResults)
			}
			if ui.Button(c, "清空搜索结果").Ghost().Height(26).FontSize(12).Clicked() {
				c.After(func() {
					r.query = ""
					r.origin, r.pane = nil, nil
					r.truncated, r.dirty = false, false
					r.groups = nil
					r.rows = nil
					r.total = 0
					r.open = false
					m.active.source.focus = true
				})
			}
			if ui.Button(c, "关闭搜索结果").Icon(closeIcon).IconOnly().Ghost().Width(26).Height(26).Tooltip("关闭结果面板").Clicked() {
				r.open = false
				m.active.source.focus = true
			}
		})
		if r.total == 0 {
			ui.Text(c, "没有找到匹配内容").FontSize(13).Muted().Pad(14)
			return
		}
		ui.List(c, &r.list, len(r.rows), func(item ui.ListRow) {
			row := r.rows[item.Index]
			group := r.groups[row.group]
			item.Element.Height(28).PadX(12).Gap(6).Items(ui.Center).Clip().Children(func() {
				if row.match < 0 {
					icon := chevronDownIcon
					if group.collapsed {
						icon = chevronRightIcon
					}
					ui.Icon(item.C, icon).Width(14).Height(14)
					name := group.tab.doc.path
					if name == "" {
						name = group.tab.title() + "（未保存）"
					}
					ui.Text(item.C, fmt.Sprintf("%s  (%d 个匹配)", name, len(group.matches))).FontSize(12).Bold().Grow(1).MinWidth(0).Ellipsis()
					return
				}
				ui.Text(item.C, fmt.Sprintf("行 %d:%d", row.line, row.column)).Width(82).FontSize(12).Color(m.theme.Success)
				ui.Row(item.C).Grow(1).MinWidth(0).Clip().Children(func() {
					ui.Text(item.C, row.before).FontSize(12).Mono()
					matchForeground := gfx.RGB(245, 214, 130)
					if m.light {
						matchForeground = gfx.RGB(106, 69, 9)
					}
					ui.Text(item.C, row.value).FontSize(12).Mono().Background(gfx.RGBA(190, 148, 38, 0.25)).Color(matchForeground)
					ui.Text(item.C, row.after).FontSize(12).Mono().Grow(1).MinWidth(0).Ellipsis()
				})
			})
		}).FillWidth().Grow(1).MinHeight(0).Label("搜索结果列表")
		index, submitted := r.list.Submitted()
		if !submitted && r.list.Changed() {
			index = r.list.Selected()
			submitted = true
		}
		if submitted {
			c.After(func() { m.chooseSearchResult(index) })
		}
	})
}

// 结果面板即使隐藏，也不能继续引用已关闭文档和其大段原文。
func (r *searchResults) removeTab(tab *editorTab) {
	if r.origin == tab {
		r.origin, r.pane = nil, nil
	}
	previous := len(r.groups)
	r.groups = slices.DeleteFunc(r.groups, func(group searchResultGroup) bool { return group.tab == tab })
	if len(r.groups) == previous {
		return
	}
	r.total, r.dirty = 0, true
	for _, group := range r.groups {
		r.total += len(group.matches)
	}
	r.buildRows()
}
