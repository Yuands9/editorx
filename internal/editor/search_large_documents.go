package editor

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/text/find"
	"github.com/vivaui/viva/ui"
)

type searchBatchFile struct {
	group              searchResultGroup
	title, replacement string
	revision           *largeEditRevision
	normalizeCRLF      bool
	ordinaryEdits      []find.Edit
	largeEdit          largeEditReplacement
}

type documentSearchBatch struct {
	origin             *editorTab
	query              string
	options            searchOptions
	allOpen, replacing bool
	effect             *ui.EffectState[[]searchBatchFile, int]
}

func (m *editorApp) startLargeDocumentSearch(t *editorTab, allOpen, replacing bool) bool {
	if !t.hasLargeEdit() && !(allOpen && slices.ContainsFunc(m.tabs, (*editorTab).hasLargeEdit)) {
		return false
	}
	s := &t.search
	if s.composing() {
		return true
	}
	pattern, options := s.query.CommittedString(), s.options
	if pattern == "" {
		return true
	}
	if len(pattern) > largeTextQueryLimit {
		s.message = uiText("包含大文件时，查找内容过长")
		return true
	}
	if m.searchBatch != nil && m.searchBatch.replacing {
		s.message = uiText("请等待当前替换完成")
		return true
	}
	query, err := options.compile(pattern)
	if err != nil {
		s.errorText = uiText("正则表达式无效：") + err.Error()
		return true
	}
	stream, err := newLargeEditQuery(query)
	if err != nil {
		s.errorText = uiText("正则表达式无效：") + err.Error()
		return true
	}
	var replacement string
	if replacing {
		s.compiled = query
		replacement = s.replacementText()
	}
	tabs := []*editorTab{t}
	if allOpen {
		tabs = m.tabs
	}
	files := make([]searchBatchFile, 0, len(tabs))
	for _, tab := range tabs {
		large := tab.hasLargeEdit()
		if !large && !tab.canSearchText() {
			continue
		}
		pane := tab.source
		if !large && !allOpen {
			pane = s.target
		}
		if replacing && pane.readOnly {
			continue
		}
		if _, composing := pane.text.Composition(); composing || large && tab.largeText.busyEditing() {
			s.message = uiText("请等文档编辑完成后重试")
			return true
		}
		file := searchBatchFile{group: searchResultGroup{tab: tab, pane: pane}, title: tab.title(), replacement: replacement}
		if large {
			file.revision = tab.largeText.editing.revision
			file.normalizeCRLF = tab.doc.eol == "CRLF"
			file.replacement = tab.largeEditInputValue(replacement)
			if replacing && strings.ContainsRune(file.replacement, 0) {
				s.message = uiText("请输入有效的替换内容")
				return true
			}
		} else {
			file.group.source = pane.text.String()
		}
		files = append(files, file)
	}
	if current := m.searchBatch; current != nil {
		current.origin.search.message = ""
	}
	s.message = uiText("正在查找…")
	if replacing {
		s.message = uiText("正在替换…")
	}
	m.searchBatch = &documentSearchBatch{origin: t, query: pattern, options: options, allOpen: allOpen, replacing: replacing}
	m.searchBatch.effect = ui.NewEffect[[]searchBatchFile, int](func(ctx context.Context) ([]searchBatchFile, error) {
		left := find.DefaultLimit
		for i := range files {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			file := &files[i]
			stream.normalizeCRLF = file.normalizeCRLF
			if replacing {
				if file.revision == nil {
					file.ordinaryEdits = query.Replacements(nil, file.group.source, file.replacement)
					continue
				}
				result, err := replaceAllLargeEdit(ctx, file.revision, stream, file.replacement)
				if err != nil {
					return nil, fmt.Errorf(uiText("无法替换 %s：%w"), file.title, err)
				}
				file.largeEdit = result
			} else {
				group := &file.group
				if file.revision == nil {
					matches, truncated := query.Matches(nil, group.source, max(1, left))
					group.matches = matches[:min(left, len(matches))]
					group.truncated = truncated || left == 0 && len(matches) > 0
					group.collectLines()
				} else {
					result, err := collectLargeEditResults(ctx, file.revision, stream, left)
					if err != nil {
						return nil, fmt.Errorf(uiText("无法查找 %s：%w"), file.title, err)
					}
					group.stream, group.lines, group.truncated = &result, result.lines, result.truncated
				}
				left -= group.count()
			}
		}
		return files, nil
	})
	return true
}

func (m *editorApp) updateSearchBatch(c ui.C) {
	job := m.searchBatch
	if job == nil {
		return
	}
	ui.Effect(c, job.effect, 0)
	files, err, ready := job.effect.Take()
	if !ready {
		return
	}
	m.searchBatch = nil
	if !slices.Contains(m.tabs, job.origin) {
		return
	}
	s := &job.origin.search
	message := ""
	defer func() {
		if s.open {
			s.message = message
		} else if message != "" {
			m.status = message
		}
	}()
	if err != nil {
		message = err.Error()
		return
	}
	for _, file := range files {
		tab, pane := file.group.tab, file.group.pane
		if !slices.Contains(m.tabs, tab) {
			message = "文档已关闭，请重试"
			return
		}
		_, composing := pane.text.Composition()
		if composing || job.replacing && pane.readOnly || file.revision != nil && (!tab.hasLargeEdit() || tab.largeText.editing.revision != file.revision || job.replacing && tab.largeText.busyEditing()) ||
			file.revision == nil && (!tab.canSearchText() || file.group.source != pane.text.String()) {
			message = "文档内容已变化，请重试"
			return
		}
	}
	if job.replacing {
		message = m.applySearchBatchReplacements(files)
		return
	}
	r := &m.results
	r.allOpen, r.origin, r.pane, r.query, r.options = job.allOpen, job.origin, s.target, job.query, job.options
	r.fixed, r.dirty, r.truncated, r.total = true, false, false, 0
	r.groups = make([]searchResultGroup, 0, len(files))
	for _, file := range files {
		group := file.group
		r.total += group.count()
		r.truncated = r.truncated || group.truncated
		r.groups = append(r.groups, group)
	}
	r.begin()
	r.buildRows()
	if s.open && !s.replacing && s.query.CommittedString() == job.query && s.options == job.options {
		s.close()
	}
}

func (m *editorApp) applySearchBatchReplacements(files []searchBatchFile) string {
	edits := make([][]text.Replacement, len(files))
	for i, file := range files {
		if file.revision != nil || len(file.ordinaryEdits) == 0 {
			continue
		}
		editor := file.group.pane.text
		edits[i] = searchReplacementRanges(editor, file.ordinaryEdits)
		if err := editor.ValidateReplaceRanges(edits[i], text.ReplaceRangesOptions{}); err != nil {
			return fmt.Sprintf(uiText("无法替换 %s：%v"), file.title, err)
		}
	}
	var count, changed int
	for i, file := range files {
		if result := file.largeEdit; result.count > 0 {
			first := result.first
			file.group.tab.noteLargeEdit(result.revision, largeEditPosition{first, first}, false)
			count += result.count
			changed++
		} else if len(edits[i]) > 0 {
			if err := file.group.pane.text.ReplaceRanges(edits[i]); err != nil {
				return fmt.Sprintf(uiText("无法替换 %s：%v"), file.title, err)
			}
			count += len(edits[i])
			changed++
		}
	}
	if count == 0 {
		return "没有需要替换的内容"
	}
	return fmt.Sprintf(uiText("已替换 %d 处 · %d 个文件"), count, changed)
}
