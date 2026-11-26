package editor

import (
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
	"github.com/vivaui/viva/window"
)

func (m *editorApp) sourceEditor(c ui.C, tab *editorTab, blocked bool) ui.Box {
	box := tab.source.declare(c, "代码编辑区", !blocked)
	if !blocked {
		m.editorContextMenu(c, box, tab, tab.source)
	}
	return box
}

func (m *editorApp) editorContextMenu(c ui.C, box ui.Box, tab *editorTab, pane *editorPane) {
	ui.ContextMenu(c, box, func(menu *ui.MenuItems) {
		editor := pane.text
		source := pane == tab.source
		selected := false
		for _, selection := range editor.Selections() {
			if selection.Start != selection.End {
				selected = true
				break
			}
		}
		hasText, canUndo, canRedo := editor.RuneCount() != 0, editor.CanUndo(), editor.CanRedo()
		selectAll := "全选"
		if source && tab.largeText != nil {
			if edit := tab.largeText.editing; edit != nil {
				start, end := edit.position.rangeOffsets()
				selected, hasText = start != end, edit.revision.size != 0
				canUndo, canRedo = len(edit.undo) != 0, len(edit.redo) != 0
			} else {
				selectAll = "全选当前页"
			}
		}
		_, composing := editor.Composition()
		// An empty paste removes only the selected ranges and forms its own undo unit.
		removeSelection := window.TextEvent{Kind: window.TextPaste}
		menu.RowHeight(28)
		if !pane.readOnly {
			if menu.Item("撤销").Icon(blankMenuIcon).Keys(ui.Ctrl, "z").Disabled(composing || !canUndo).Clicked() {
				c.After(func() {
					m.applyEditorMenuEvent(tab, pane, window.TextEvent{Kind: window.TextCommandEvent, Command: window.TextUndo})
				})
			}
			if menu.Item("重做").Icon(blankMenuIcon).Keys(ui.Ctrl, "y").Disabled(composing || !canRedo).Clicked() {
				c.After(func() {
					m.applyEditorMenuEvent(tab, pane, window.TextEvent{Kind: window.TextCommandEvent, Command: window.TextRedo})
				})
			}
			menu.Separator()
			if menu.Item("剪切").Icon(blankMenuIcon).Keys(ui.Ctrl, "x").Disabled(composing || !selected).Clicked() {
				c.After(func() {
					if m.copyEditorSelection(tab, pane) {
						m.applyEditorMenuEvent(tab, pane, removeSelection)
					}
				})
			}
		}
		if menu.Item("复制").Icon(copyIcon).Keys(ui.Ctrl, "c").Disabled(composing || !selected).Clicked() {
			c.After(func() {
				if m.copyEditorSelection(tab, pane) {
					pane.focus = true
				}
			})
		}
		if !pane.readOnly {
			if menu.Item("粘贴").Icon(blankMenuIcon).Keys(ui.Ctrl, "v").Disabled(composing).Clicked() {
				c.After(func() { m.pasteEditorClipboard(tab, pane) })
			}
			if menu.Item("删除选区").Icon(aiDeleteIcon).Disabled(composing || !selected).Clicked() {
				c.After(func() { m.applyEditorMenuEvent(tab, pane, removeSelection) })
			}
		}
		if menu.Item(selectAll).Icon(blankMenuIcon).Keys(ui.Ctrl, "a").Disabled(composing || !hasText).Clicked() {
			c.After(func() {
				m.applyEditorMenuEvent(tab, pane, window.TextEvent{Kind: window.TextCommandEvent, Command: window.TextSelectAll})
			})
		}

		if (source && tab.canFind()) || tab.searchPaneVisible(pane) {
			menu.Separator()
			if m.shortcutMenu(menu.Item("查找…").Icon(searchIcon), shortcutFind).Clicked() {
				c.After(func() { tab.openSearchIn(pane) })
			}
			if source && !pane.readOnly && tab.canReplace() &&
				m.shortcutMenu(menu.Item("替换…").Icon(blankMenuIcon), shortcutReplace).Clicked() {
				c.After(tab.openReplace)
			}
			if source && m.shortcutMenu(menu.Item("转到行…").Icon(blankMenuIcon), shortcutGotoLine).Clicked() {
				c.After(m.openGotoLine)
			}
		}
		if !source {
			return
		}
		if tab.doc.path != "" && menu.Item("比对历史…").Icon(historyCompareIcon).Clicked() {
			c.After(func() { m.openHistoryComparison(tab.doc.path, localHistoryVersion{}) })
		}
		translateSelection := !composing && tab.hasTranslationSelection()
		translateDocument := tab.canTranslateDocument() && !tab.showingDocumentTranslation()
		importSelection := translateSelection && editor.SelectionCount() == 1 && editableTextSource(tab)
		if translateSelection || translateDocument {
			menu.Separator()
			if translateSelection {
				m.translationSelectionItem(c, menu)
			}
			if translateDocument && menu.Item("整篇翻译").Icon(translationIcon).Clicked() {
				c.After(func() { m.openDocumentTranslation(tab) })
			}
			if importSelection && menu.Item("识别到密码本…").Icon(passwordVaultIcon).Clicked() {
				c.After(func() { m.openPasswordSelection(tab) })
			}
		}
	})
}

func (m *editorApp) applyEditorMenuEvent(tab *editorTab, pane *editorPane, event window.TextEvent) {
	// Full large-file editing applies the event to the file revision beyond the viewport.
	if pane.input.Editor == nil && pane.input.OnEvent != nil {
		if _, err := pane.input.OnEvent(event); err != nil {
			m.editorMenuError(tab, err)
			return
		}
	} else {
		event.Apply(pane.text)
	}
	pane.focus = true
}

func (m *editorApp) copyEditorSelection(tab *editorTab, pane *editorPane) bool {
	var value string
	if pane.input.CopyOverride {
		if pane.input.CopyText == nil {
			return false
		}
		value = pane.input.CopyText()
	} else {
		editor := pane.text
		source := editor.String()
		parts := make([]string, 0, editor.SelectionCount())
		for _, selection := range editor.Selections() {
			if selection.Start != selection.End {
				parts = append(parts, source[editor.ByteOffset(selection.Start):editor.ByteOffset(selection.End)])
			}
		}
		value = strings.Join(parts, "\n")
	}
	if value == "" {
		return false
	}
	writeClipboard := app.SetClipboardText
	if pane.input.CopyRectangular {
		writeClipboard = app.SetRectangularClipboardText
	}
	if err := writeClipboard(value); err != nil {
		m.editorMenuError(tab, err)
		return false
	}
	return true
}

func (m *editorApp) pasteEditorClipboard(tab *editorTab, pane *editorPane) {
	value, rectangular, err := app.ClipboardTextWithFormat()
	if err != nil {
		m.editorMenuError(tab, err)
		return
	}
	if value == "" {
		return
	}
	if transform := pane.input.TransformPaste; transform != nil && !rectangular {
		value = transform(value)
	}
	m.applyEditorMenuEvent(tab, pane, window.TextEvent{Kind: window.TextPaste, Text: value, Rectangular: rectangular})
}

func (m *editorApp) editorMenuError(tab *editorTab, err error) {
	if tab.largeText != nil {
		tab.largeText.problem = err.Error()
		return
	}
	m.fail(err)
}
