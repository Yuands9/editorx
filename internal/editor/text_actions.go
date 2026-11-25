package editor

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

type gotoLineState struct {
	open    bool
	field   ui.Field
	problem string
}

func (m *editorApp) openGotoLine() {
	tab := m.active
	if tab == nil || !tab.canFind() {
		return
	}
	if tab.largeText != nil && tab.largeText.edit != nil {
		return
	}
	m.gotoLine = gotoLineState{open: true}
}

func (m *editorApp) gotoLineDialog(c ui.C) {
	state := &m.gotoLine
	tab := m.active
	if !state.open || tab == nil {
		return
	}
	total, hint := 0, "正在读取行号…"
	if large := tab.largeText; large == nil {
		total = text.LineCount(tab.source.text.String())
	} else if large.editing != nil {
		total = large.editing.revision.breaks + 1
	} else if len(large.lines.breaks) > 0 {
		total = large.lines.breaks[len(large.lines.breaks)-1] + 1
	} else if large.indexError != "" {
		hint = large.indexError
	}
	if total > 0 {
		hint = fmt.Sprintf("共 %d 行", total)
	}
	submitted := false
	ui.ModalWith(c, &state.open, "转到行", ui.ModalSpec{Width: 340}, func() {
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			field := ui.TextField(c, &state.field, "行号", "输入行号").FillWidth().AutoFocus()
			submitted = field.Submitted()
			ui.Text(c, hint).Muted().FontSize(12)
			if state.problem != "" {
				ui.Text(c, state.problem).Color(m.theme.Danger).FontSize(12)
			}
		})
	}, func() {
		if ui.Button(c, "取消").Ghost().Clicked() {
			state.open = false
		}
		button := ui.Button(c, "跳转").Disabled(total == 0)
		if total == 0 || !(button.Clicked() || submitted) {
			return
		}
		line, err := strconv.Atoi(strings.TrimSpace(state.field.Text()))
		if err != nil || line < 1 || line > total {
			state.problem = "请输入文件范围内的行号"
			return
		}
		c.After(func() {
			if err := m.revealTextLine(tab, line); err != nil {
				state.problem = err.Error()
				return
			}
			state.open = false
		})
	})
}

func (m *editorApp) revealTextLine(tab *editorTab, line int) error {
	pane := tab.source
	offset := 0
	if state := tab.largeText; state != nil {
		if edit := state.editing; edit != nil {
			if state.busyEditing() {
				return fmt.Errorf("请等待当前大文件操作完成")
			}
			if _, composing := pane.text.Composition(); composing {
				return fmt.Errorf("请先完成中文选词")
			}
			if line < 1 || line > edit.revision.breaks+1 {
				return fmt.Errorf("行号超出文件范围")
			}
			caret, err := edit.revision.lineStart(line - 1)
			if err != nil {
				return err
			}
			state.found, state.search, state.selectedResult = -1, nil, -1
			tab.moveLargeEditCaret(caret, false)
			pane.focus = true
			m.status = fmt.Sprintf("已跳到第 %d 行", line)
			return nil
		}
		page, position, err := state.lines.goToLine(tab.doc.path, tab.doc.encoding, line)
		if err != nil {
			return err
		}
		state.found, state.search, state.selectedResult = -1, nil, -1
		tab.applyLargeTextPage(page)
		offset = position
	} else {
		value := pane.text.String()
		if line < 1 || line > text.LineCount(value) {
			return fmt.Errorf("行号超出文件范围")
		}
		offset = text.OffsetAt(value, text.Position{Line: line - 1})
		if tab.format() == "Markdown" && tab.markdownMode == "预览" {
			tab.markdownMode = "分屏"
		}
		if tab.format() == "HTML" && tab.htmlMode == "预览" {
			tab.htmlMode = "分屏"
		}
	}
	pane.text.MoveTo(pane.text.RuneIndex(offset), false)
	pane.view.Code.Reveal(offset)
	pane.focus = true
	m.status = fmt.Sprintf("已跳到第 %d 行", line)
	return nil
}

type textRewrite uint8

const (
	textUppercase textRewrite = iota
	textLowercase
	textSortLines
	textRemoveBlankLines
)

func rewriteTextSelection(tab *editorTab, action textRewrite) bool {
	if !editableTextSource(tab) {
		return false
	}
	editor := tab.source.text
	if _, composing := editor.Composition(); composing {
		return false
	}
	source, selection := editor.String(), editor.Selection()
	start, end := editor.ByteOffset(selection.Start), editor.ByteOffset(selection.End)
	var replacement string
	switch action {
	case textUppercase, textLowercase:
		if start == end {
			return false
		}
		if action == textUppercase {
			replacement = strings.ToUpper(source[start:end])
		} else {
			replacement = strings.ToLower(source[start:end])
		}
	case textSortLines, textRemoveBlankLines:
		if start == end {
			start, end = 0, len(source)
		} else {
			start = strings.LastIndexByte(source[:start], '\n') + 1
			if end == 0 || source[end-1] != '\n' {
				if next := strings.IndexByte(source[end:], '\n'); next >= 0 {
					end += next + 1
				} else {
					end = len(source)
				}
			}
		}
		content, terminated := strings.CutSuffix(source[start:end], "\n")
		lines := strings.Split(content, "\n")
		if action == textSortLines {
			slices.Sort(lines)
		} else {
			lines = slices.DeleteFunc(lines, func(line string) bool { return strings.TrimSpace(line) == "" })
		}
		replacement = strings.Join(lines, "\n")
		if terminated && len(lines) > 0 {
			replacement += "\n"
		}
	}
	if replacement == source[start:end] {
		return false
	}
	first := editor.RuneIndex(start)
	editor.BreakUndoGroup()
	editor.ReplaceRange(text.Range{Start: first, End: editor.RuneIndex(end)}, replacement)
	editor.BreakUndoGroup()
	if selection.Start != selection.End {
		editor.SetSelection(text.Range{Start: first, End: editor.RuneIndex(start + len(replacement))})
	}
	tab.source.focus = true
	return true
}

func (m *editorApp) textActions(c ui.C, menu *ui.MenuItems, tab *editorTab) {
	if m.shortcutMenu(menu.Item("转到行…").Disabled(!tab.canFind()), shortcutGotoLine).Clicked() {
		c.After(m.openGotoLine)
	}
	menu.Sub("行书签", func(items *ui.MenuItems) {
		for _, entry := range []struct {
			label    string
			shortcut editorShortcut
		}{{"切换当前行书签", shortcutToggleBookmark}, {"下一个书签", shortcutNextBookmark}, {"上一个书签", shortcutPreviousBookmark}} {
			if m.shortcutMenu(items.Item(entry.label), entry.shortcut).Disabled(!tab.canFind()).Clicked() {
				c.After(func() { m.runEditorShortcut(entry.shortcut) })
			}
		}
	})
	current := &tab.source.view.Snapshot
	if menu.Item("显示空白字符").Checked(current.ShowWhitespace).Disabled(!tab.canFind()).Clicked() {
		current.ShowWhitespace = !current.ShowWhitespace
	}
	if menu.Item("显示换行符").Checked(current.ShowEOL).Disabled(!tab.canFind()).Clicked() {
		current.ShowEOL = !current.ShowEOL
	}
	menu.Separator()
	editable := editableTextSource(tab)
	for _, item := range []struct {
		label    string
		command  components.CodeEditorCommand
		shortcut editorShortcut
	}{
		{"复制当前行", components.CodeEditorDuplicateLinesDown, shortcutDuplicateLine},
		{"上移选中行", components.CodeEditorMoveLinesUp, shortcutNone},
		{"下移选中行", components.CodeEditorMoveLinesDown, shortcutNone},
		{"删除选中行", components.CodeEditorDeleteLines, shortcutDeleteLine},
	} {
		largeEditable := tab.largeText != nil && tab.largeText.editing != nil && !tab.largeText.busyEditing()
		if m.shortcutMenu(menu.Item(item.label).Disabled(!editable && !largeEditable), item.shortcut).Clicked() {
			c.After(func() {
				if largeEditable {
					tab.largeEditCodeCommand(item.command)
					return
				}
				if _, composing := tab.source.text.Composition(); !composing {
					item.command.Apply(tab.source.text)
					tab.source.focus = true
				}
			})
		}
	}
	for _, item := range []struct {
		label  string
		action textRewrite
	}{
		{"转大写", textUppercase}, {"转小写", textLowercase}, {"行排序（升序）", textSortLines}, {"删除空白行", textRemoveBlankLines},
	} {
		selection := tab.source.text.Selection()
		disabled := !editable || item.action <= textLowercase && selection.Start == selection.End
		if menu.Item(item.label).Disabled(disabled).Clicked() {
			c.After(func() { rewriteTextSelection(tab, item.action) })
		}
	}
}
