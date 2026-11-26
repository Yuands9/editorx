package editor

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/text/find"
	"github.com/vivaui/viva/ui"
)

type largeTextView struct {
	index, size, start int64
	file               os.FileInfo
	lines              largeLineIndex
	indexing           *ui.EffectState[largeLineIndex, int]
	indexError         string
	allMatches         *ui.EffectState[largeSearchResults, int]
	highlight          string
	found              int64
	selectedResult     int
	query              ui.Field
	lastQuery, problem string
	searchOpen, focus  bool
	selecting          bool
	search             *ui.EffectState[int64, int]
	edit               *ui.EffectState[largeEditOpening, int]
	loadStore          *largeEditStorage
	editing            *largeEditView
	editSession        *largeEditSession
	resumeEdit         bool
	saving             *ui.EffectState[document, int]
	afterSave          func()
	savePath           string
	replacing          *ui.EffectState[largeEditReplacement, int]
}

func (t *editorTab) startLargeText(page largeTextPage) {
	t.largeText = &largeTextView{found: -1, selectedResult: -1}
	pane := t.source
	pane.readOnly, pane.view.Snapshot.ReadOnly, pane.view.Snapshot.LineNumbers = true, true, false
	pane.view.Snapshot.Wrap, pane.view.Snapshot.MatchBrackets = false, false
	pane.view.Pointer = t.selectLargeText
	pane.view.Scroll = func(event gfx.ScrollEvent) gfx.EventResult {
		if event.DeltaY == 0 || event.DeltaX != 0 || event.Modifiers != 0 {
			return gfx.EventResult{}
		}
		direction := int64(1)
		if event.DeltaY < 0 {
			direction = -1
		}
		changed := t.stepLargeText(direction, true)
		return gfx.EventResult{Handled: changed, Repaint: changed}
	}
	t.applyLargeTextPage(page)
}

func (t *editorTab) applyLargeTextPage(page largeTextPage) {
	state := t.largeText
	state.index, state.size, state.start, state.problem = page.index, page.size, page.start, ""
	state.selecting = false
	if !sameTextFile(state.file, page.file) || page.encoding != t.doc.encoding {
		state.lines, state.indexing, state.indexError = largeLineIndex{}, nil, ""
	}
	state.file = page.file
	if page.encoding != "" {
		t.doc.encoding, t.doc.bom = page.encoding, page.bom
	}
	t.source.setResult(page.value)
	t.source.view.Snapshot.Marks = nil
	t.source.view.Snapshot.CurrentMark = text.Span{}
	t.markLargeTextMatches()
	if page.selectionEnd > page.selectionStart {
		t.source.text.SetSelection(text.Range{Start: page.selectionStart, End: page.selectionEnd})
		start, end := t.source.text.ByteOffset(page.selectionStart), t.source.text.ByteOffset(page.selectionEnd)
		t.source.view.Snapshot.CurrentMark = text.Span{Start: start, End: end}
		t.source.view.Code.Reveal(start)
	}
}

func (t *editorTab) moveLargeText(index int64, bottom bool) {
	state := t.largeText
	state.found, state.search, state.selectedResult = -1, nil, -1
	page, err := readLargeTextPage(t.doc.path, index, -1, "", t.doc.encoding)
	if err != nil {
		state.problem = err.Error()
		return
	}
	t.applyLargeTextPage(page)
	if bottom {
		t.source.text.MoveTo(t.source.text.RuneCount(), false)
		t.source.view.Code.Reveal(len(page.value))
	}
}

func (t *editorTab) stepLargeText(direction int64, fromEdge bool) bool {
	state := t.largeText
	index := state.index + direction
	if state.edit != nil || index < 0 || index >= largeTextPageCount(state.size) {
		return false
	}
	t.moveLargeText(index, fromEdge && direction < 0)
	return true
}

func (t *editorTab) selectLargeText(event gfx.PointerEvent) gfx.EventResult {
	pane, state := t.source, t.largeText
	switch event.Kind {
	case gfx.PointerDown:
		if event.Button != gfx.PointerButtonPrimary {
			return gfx.EventResult{}
		}
	case gfx.PointerMove:
		if !state.selecting || !event.Captured {
			return gfx.EventResult{}
		}
	case gfx.PointerUp, gfx.PointerExit:
		state.selecting = false
		return gfx.EventResult{}
	default:
		return gfx.EventResult{}
	}
	offset, ok := pane.view.Code.OffsetAt(gfx.Point{X: event.X, Y: event.Y})
	if !ok {
		return gfx.EventResult{}
	}
	caret := pane.text.RuneIndex(offset)
	if event.Kind == gfx.PointerDown {
		state.selecting = true
		state.selectedResult = -1
		switch {
		case event.Modifiers&ui.Shift != 0:
			pane.text.MoveTo(caret, true)
		case event.ClickCount >= 3:
			pane.text.SetSelection(text.LineRange(pane.text.String(), caret))
		case event.ClickCount == 2:
			pane.text.SetSelection(text.WordRange(pane.text.String(), caret))
		default:
			pane.text.MoveTo(caret, false)
		}
	} else {
		pane.text.MoveTo(caret, true)
	}
	return gfx.EventResult{Focus: event.Target, Handled: true, Repaint: true}
}

func (t *editorTab) largeTextKey(event gfx.KeyEvent) gfx.EventResult {
	if t.largeText.editing != nil {
		return t.largeEditKey(event)
	}
	if t.largeText.edit != nil {
		return gfx.EventResult{}
	}
	handled := true
	switch {
	case event.Modifiers == ui.Ctrl && event.Key == gfx.KeyHome:
		t.moveLargeText(0, false)
	case event.Modifiers == ui.Ctrl && event.Key == gfx.KeyEnd:
		t.moveLargeText(-1, true)
	case event.Modifiers == 0 && event.Key == gfx.KeyPageUp && t.source.view.Code.ScrollY() == 0:
		handled = t.stepLargeText(-1, true)
	case event.Modifiers == 0 && event.Key == gfx.KeyPageDown && t.source.view.Code.ScrollY() >= t.source.view.Code.MaxScrollY():
		handled = t.stepLargeText(1, true)
	case (event.Key == gfx.KeyLeft || event.Key == gfx.KeyRight) && event.Modifiers&^(ui.Ctrl|ui.Shift) == 0:
		editor := t.source.text
		extend := event.Modifiers&ui.Shift != 0
		if event.Modifiers&ui.Ctrl != 0 {
			editor.MoveWord(event.Key == gfx.KeyRight, extend)
		} else if event.Key == gfx.KeyRight {
			editor.MoveRight(extend)
		} else {
			editor.MoveLeft(extend)
		}
	default:
		handled = false
	}
	return gfx.EventResult{Handled: handled, Repaint: handled}
}

func (t *editorTab) findInLargeText(direction int) {
	state := t.largeText
	if state.busyEditing() {
		return
	}
	editor := state.query.GetEditor()
	if _, composing := editor.Composition(); composing {
		return
	}
	query := editor.CommittedString()
	if query == "" {
		t.openSearch()
		return
	}
	if len(query) > largeTextQueryLimit {
		state.problem = "查找内容过长"
		return
	}
	needle, err := encodeText(query, t.doc.encoding)
	if err != nil {
		state.problem = err.Error()
		return
	}
	from := state.start
	if query == state.lastQuery && state.found >= 0 {
		from = state.found
		if direction > 0 {
			from += int64(len(needle))
		}
	} else if direction < 0 && from == 0 {
		from = state.size
	}
	path, enc := t.doc.path, t.doc.encoding
	if query != state.lastQuery {
		state.found = -1
	}
	state.lastQuery, state.problem = query, ""
	state.allMatches, state.highlight = nil, query
	t.markLargeTextMatches()
	state.search = ui.NewEffect[int64, int](func(ctx context.Context) (int64, error) {
		return findLargeText(ctx, path, query, from, direction, enc)
	})
}

func (t *editorTab) markLargeTextMatches() {
	snapshot := &t.source.view.Snapshot
	snapshot.Marks = snapshot.Marks[:0]
	value := t.source.text.String()
	if t.hasLargeEdit() {
		s := &t.search
		// A viewport alone cannot establish document-wide regexp boundaries.
		if s.compiled != nil && s.options.mode != searchRegexp {
			snapshot.Marks, _ = s.compiled.Matches(snapshot.Marks, value, find.DefaultLimit)
		}
		return
	}
	query := t.largeText.highlight
	if query == "" {
		return
	}
	for position := 0; len(snapshot.Marks) < find.DefaultLimit; {
		next := strings.Index(value[position:], query)
		if next < 0 {
			return
		}
		start := position + next
		position = start + len(query)
		snapshot.Marks = append(snapshot.Marks, text.Span{Start: start, End: position})
	}
}

func (t *editorTab) findAllLargeText() {
	state := t.largeText
	if state.busyEditing() {
		return
	}
	editor := state.query.GetEditor()
	if _, composing := editor.Composition(); composing {
		return
	}
	query := editor.CommittedString()
	if len(query) == 0 || len(query) > largeTextQueryLimit {
		return
	}
	state.search, state.found, state.problem, state.highlight, state.selectedResult = nil, -1, "", query, -1
	t.markLargeTextMatches()
	path, enc := t.doc.path, t.doc.encoding
	state.allMatches = ui.NewEffect[largeSearchResults, int](func(ctx context.Context) (largeSearchResults, error) {
		return collectLargeSearchResults(ctx, path, query, enc)
	})
}

func (m *editorApp) updateLargeFindAll(c ui.C, tab *editorTab) {
	state := tab.largeText
	if state == nil || state.allMatches == nil {
		return
	}
	ui.Effect(c, state.allMatches, 0)
	result, err, ready := state.allMatches.Take()
	if !ready {
		return
	}
	state.allMatches = nil
	if err != nil {
		state.problem = err.Error()
		return
	}
	m.showLargeSearchResults(tab, state.highlight, result)
}

func (m *editorApp) largeTextView(c ui.C, tab *editorTab, blocked bool) {
	state := tab.largeText
	if state.editing == nil && state.edit == nil && state.file != nil && state.lines.file == nil && state.indexing == nil && state.indexError == "" {
		path, enc := tab.doc.path, tab.doc.encoding
		state.indexing = ui.NewEffect[largeLineIndex, int](func(ctx context.Context) (largeLineIndex, error) {
			return indexLargeText(ctx, path, enc)
		})
	}
	if state.indexing != nil {
		ui.Effect(c.Part("line-index"), state.indexing, 0)
		if index, err, ready := state.indexing.Take(); ready {
			state.indexing = nil
			if err != nil {
				state.indexError = err.Error()
			} else if sameTextFile(index.file, state.file) {
				state.lines = index
			} else {
				state.indexError = "文件已变化，请重新读取"
			}
		}
	}
	snapshot := &tab.source.view.Snapshot
	path := tab.doc.path
	if state.editing != nil && path == "" {
		path = state.editing.basePath
	}
	snapshot.Language = documentFormat(path)
	if state.editing == nil {
		snapshot.LineNumbers, snapshot.LineNumberOffset = state.index == 0, 0
		if int(state.index) < len(state.lines.breaks)-1 {
			snapshot.LineNumbers = true
			snapshot.LineNumberOffset = state.lines.breaks[state.index]
		}
	}

	if state.search != nil {
		ui.Effect(c.Part("find-file"), state.search, 0)
		if offset, err, ready := state.search.Take(); ready {
			state.search = nil
			switch {
			case err != nil:
				state.problem = err.Error()
			case offset < 0:
				state.problem = "此方向没有更多匹配"
			default:
				page, err := readLargeTextPage(tab.doc.path, state.index, offset, state.lastQuery, tab.doc.encoding)
				if err != nil {
					state.problem = err.Error()
				} else {
					tab.applyLargeTextPage(page)
					state.found = offset
				}
			}
		}
	}
	total := max(state.index+1, largeTextPageCount(state.size))
	disabled := blocked || state.busyEditing()
	if !disabled && state.searchOpen && ui.Shortcut(c.Part("close-find"), 0, "escape") {
		c.After(func() { tab.closeLargeTextSearch() })
	}
	if state.editing != nil {
		m.largeEditToolbar(c.Part("navigation"), tab, disabled)
	} else {
		ui.Row(c.Part("navigation")).FillWidth().Height(40).PadX(12).Gap(5).Items(ui.Center).Children(func() {
			ui.Text(c, "大文件浏览").Bold().FontSize(13)
			ui.Text(c, fmt.Sprintf("%d / %d 页", state.index+1, total)).Muted().FontSize(12).MarginX(5)
			if ui.Button(c, "上一页").Ghost().FontSize(12).Height(28).Disabled(disabled || state.index == 0).Clicked() {
				c.After(func() { tab.stepLargeText(-1, false) })
			}
			if ui.Button(c, "下一页").Ghost().FontSize(12).Height(28).Disabled(disabled || state.index+1 >= total).Clicked() {
				c.After(func() { tab.stepLargeText(1, false) })
			}
			if ui.Button(c, "到末尾").Ghost().FontSize(12).Height(28).Disabled(disabled).Tooltip("重新读取文件大小并跳到末尾").Clicked() {
				c.After(func() { tab.moveLargeText(-1, true) })
			}
			if ui.Button(c, "重新读取").Icon(refreshIcon).IconOnly().Ghost().Size(25, 26).Disabled(disabled).Tooltip("重新读取当前页").Clicked() {
				c.After(func() { tab.moveLargeText(state.index, false) })
			}
			ui.Slot(c).Grow(1)
			if ui.Button(c, "查找").Icon(searchIcon).Ghost().FontSize(12).Height(28).Disabled(disabled).Clicked() {
				c.After(tab.openSearch)
			}
			label := "打开完整编辑"
			if state.edit != nil {
				label = "正在打开…"
			}
			if ui.Button(c, label).Outline().FontSize(12).Height(28).Disabled(disabled).Clicked() {
				c.After(func() { m.startLargeTextEdit(tab, tab.doc.encoding) })
			}
			if state.editSession.hasChanges() &&
				ui.Button(c, "导出未保存改动…").Ghost().FontSize(12).Height(28).Disabled(disabled).Clicked() {
				c.After(func() { m.exportLargeEditDraft(tab) })
			}
		})
	}
	if state.searchOpen {
		ui.Row(c.Part("search")).FillWidth().PadX(12).Gap(5).Items(ui.Center).Children(func() {
			field := ui.TextField(c.Part("query"), &state.query, "在大文件中查找", "区分大小写 · 查找全文").
				Grow(1).MinWidth(0).Height(30).Disabled(disabled)
			if state.focus {
				field.AutoFocus()
				state.focus = false
			}
			if field.Submitted() && !disabled {
				c.After(func() { tab.findInLargeText(1) })
			}
			for _, direction := range []struct {
				label string
				value int
			}{{"上一个", -1}, {"下一个", 1}} {
				if ui.Button(c, direction.label).Ghost().Height(28).FontSize(12).Disabled(disabled).Clicked() {
					c.After(func() { m.stepLargeSearch(tab, direction.value) })
				}
			}
			if ui.Button(c, "查找全部").Secondary().Height(28).FontSize(12).Disabled(disabled || state.allMatches != nil || state.query.Text() == "").Clicked() {
				c.After(tab.findAllLargeText)
			}
			if ui.Button(c, "关闭查找").Icon(closeIcon).IconOnly().Ghost().Size(26, 28).Disabled(disabled).Clicked() {
				c.After(tab.closeLargeTextSearch)
			}
		})
	}
	if state.saving != nil {
		ui.Text(c, "正在保存…").Muted().FontSize(12).PadX(12)
	} else if state.replacing != nil {
		ui.Text(c, "正在替换…").Muted().FontSize(12).PadX(12)
	} else if state.search != nil || state.allMatches != nil {
		ui.Text(c, "正在查找…").Muted().FontSize(12).PadX(12)
	} else if state.problem != "" {
		ui.Wrap(c, state.problem).FillWidth().PadX(12).FontSize(12).Color(m.theme.Danger)
	}
	m.updateBookmarkMarkers(tab)
	if state.editing != nil {
		m.largeEditPane(c.Part("content"), tab, blocked)
		return
	}
	box := tab.source.declare(c.Part("page"), "大文件内容", !blocked).FillWidth().Grow(1).MinHeight(0).Clip()
	if blocked {
		return
	}
	if box.Shortcut(ui.Ctrl, "c") {
		c.After(func() { m.copyEditorSelection(tab, tab.source) })
	}
	if box.Shortcut(ui.Ctrl, "a") {
		c.After(func() { tab.source.text.SetSelection(text.Range{End: tab.source.text.RuneCount()}) })
	}
	if !state.busyEditing() {
		m.editorContextMenu(c.Part("page-menu"), box, tab, tab.source)
	}
}

func (t *editorTab) closeLargeTextSearch() {
	state := t.largeText
	state.searchOpen, state.search, state.allMatches, state.problem = false, nil, nil, ""
	t.source.focus = true
}
