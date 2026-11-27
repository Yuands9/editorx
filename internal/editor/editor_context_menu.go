package editor

import (
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
	"github.com/vivaui/viva/window"
)

func (m *editorApp) sourceEditor(c ui.C, tab *editorTab, blocked bool) ui.Box {
	tab.source.onFocused = nil
	box := tab.source.declare(c, "代码编辑区", !blocked)
	if !blocked {
		m.editorContextMenu(c, box, tab, tab.source)
	}
	return box
}

func (m *editorApp) editorContextMenu(c ui.C, box ui.Box, tab *editorTab, pane *editorPane) {
	redoModifiers, redoKey := ui.Ctrl, "y"
	if !pane.readOnly && m.keyBindings.chord(shortcutDeleteLine) == controlY {
		redoModifiers, redoKey = ui.Ctrl|ui.Shift, "z"
		if box.Shortcut(controlY.Modifiers, controlY.Name()) {
			c.After(func() { m.runLineCommand(tab, pane, components.CodeEditorDeleteLines) })
		}
	}
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
			if menu.Item(m.menuText("撤销")).Icon(blankMenuIcon).Keys(ui.Ctrl, "z").Disabled(composing || !canUndo).Clicked() {
				c.After(func() {
					m.applyEditorMenuEvent(tab, pane, window.TextEvent{Kind: window.TextCommandEvent, Command: window.TextUndo})
				})
			}
			if menu.Item(m.menuText("重做")).Icon(blankMenuIcon).Keys(redoModifiers, redoKey).Disabled(composing || !canRedo).Clicked() {
				c.After(func() {
					m.applyEditorMenuEvent(tab, pane, window.TextEvent{Kind: window.TextCommandEvent, Command: window.TextRedo})
				})
			}
			menu.Separator()
			if menu.Item(m.menuText("剪切")).Icon(blankMenuIcon).Keys(ui.Ctrl, "x").Disabled(composing || !selected).Clicked() {
				c.After(func() {
					if m.copyEditorSelection(tab, pane) {
						m.applyEditorMenuEvent(tab, pane, removeSelection)
					}
				})
			}
		}
		if menu.Item(m.menuText("复制")).Icon(copyIcon).Keys(ui.Ctrl, "c").Disabled(composing || !selected).Clicked() {
			c.After(func() {
				if m.copyEditorSelection(tab, pane) {
					pane.focus = true
				}
			})
		}
		if !pane.readOnly {
			if menu.Item(m.menuText("粘贴")).Icon(blankMenuIcon).Keys(ui.Ctrl, "v").Disabled(composing).Clicked() {
				c.After(func() { m.pasteEditorClipboard(tab, pane) })
			}
			if menu.Item(m.menuText("删除选区")).Icon(aiDeleteIcon).Disabled(composing || !selected).Clicked() {
				c.After(func() { m.applyEditorMenuEvent(tab, pane, removeSelection) })
			}
		}
		if menu.Item(m.menuText(selectAll)).Icon(blankMenuIcon).Keys(ui.Ctrl, "a").Disabled(composing || !hasText).Clicked() {
			c.After(func() {
				m.applyEditorMenuEvent(tab, pane, window.TextEvent{Kind: window.TextCommandEvent, Command: window.TextSelectAll})
			})
		}

		if (source && tab.canFind()) || tab.searchPaneVisible(pane) {
			menu.Separator()
			if m.shortcutMenu(menu.Item(m.menuText("查找…")).Icon(searchIcon), shortcutFind).Clicked() {
				c.After(func() { tab.openSearchIn(pane) })
			}
			if source && !pane.readOnly && tab.canReplace() &&
				m.shortcutMenu(menu.Item(m.menuText("替换…")).Icon(blankMenuIcon), shortcutReplace).Clicked() {
				c.After(tab.openReplace)
			}
			if source && m.shortcutMenu(menu.Item(m.menuText("转到行…")).Icon(blankMenuIcon), shortcutGotoLine).Clicked() {
				c.After(m.openGotoLine)
			}
		}
		if !pane.readOnly && tab.largeText == nil {
			if m.shortcutMenu(menu.Item(m.menuText("补全文档词 / 关键字")).Icon(blankMenuIcon), shortcutComplete).Clicked() {
				c.After(pane.openCompletion)
			}
			if foldableLanguage(tab.format()) {
				if m.shortcutMenu(menu.Item(m.menuText("折叠全部")).Icon(collapseIcon), shortcutFoldAll).Clicked() {
					c.After(func() { pane.foldAll(true) })
				}
				if m.shortcutMenu(menu.Item(m.menuText("展开全部")).Icon(expandIcon), shortcutUnfoldAll).Clicked() {
					c.After(func() { pane.foldAll(false) })
				}
			}
			menu.Sub(m.menuText("多光标与列编辑"), func(items *ui.MenuItems) {
				if m.shortcutMenu(items.Item(m.menuText("选择下一个相同内容")).Icon(blankMenuIcon), shortcutSelectNext).Clicked() {
					c.After(func() { pane.selectNextOccurrence(false); pane.focus = true })
				}
				if m.shortcutMenu(items.Item(m.menuText("选择全部相同内容")).Icon(blankMenuIcon), shortcutSelectAllMatches).Clicked() {
					c.After(func() { pane.selectNextOccurrence(true); pane.focus = true })
				}
				if m.shortcutMenu(items.Item(m.menuText("拆分为多行选区")).Icon(blankMenuIcon), shortcutSplitSelection).Clicked() {
					c.After(func() { pane.splitSelectionLines(); pane.focus = true })
				}
				items.Separator()
				items.Label(m.menuText("Alt+单击添加光标 · Alt+拖动列选择"))
			}).Icon(blankMenuIcon)
		}
		if !source {
			return
		}
		if historyEditableTab(tab) && menu.Item(m.menuText("与其他文本对比…")).Icon(historyCompareIcon).Clicked() {
			c.After(m.openTextComparePicker)
		}
		if tab.doc.path != "" && menu.Item(m.menuText("比对历史…")).Icon(historyCompareIcon).Clicked() {
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
			if translateDocument && menu.Item(m.menuText("整篇翻译")).Icon(translationIcon).Clicked() {
				c.After(func() { m.openDocumentTranslation(tab) })
			}
			if importSelection && menu.Item(m.menuText("识别到密码本…")).Icon(passwordVaultIcon).Clicked() {
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
