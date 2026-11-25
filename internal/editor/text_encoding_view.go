package editor

import (
	"fmt"

	"github.com/vivaui/viva/ui"
)

func (m *editorApp) encodingPicker(c ui.C, tab *editorTab, blocked bool) {
	label := tab.doc.encoding.label(tab.doc.bom)
	if tab.doc.path == "" {
		ui.Text(c, label).Muted().FontSize(12)
		return
	}
	if blocked {
		m.encodingMenu = false
	}
	composing := false
	if _, composing = tab.source.text.Composition(); composing {
		m.encodingMenu = false
	}
	disabled := blocked || composing || tab.modified() || tab.hasResultDraft() || tab.largeText != nil && tab.largeText.busyEditing()
	hint := "按编码重新打开"
	if !blocked && disabled && (tab.modified() || tab.hasResultDraft()) {
		hint = "保存当前修改后可重新选择编码"
	}
	button := ui.Button(c.Part("file-encoding"), label).Icon(chevronDownIcon).Ghost().Height(24).PadX(7).FontSize(12).Tooltip(hint).Disabled(disabled)
	if button.Clicked() {
		m.encodingMenu = !m.encodingMenu
	}
	ui.Menu(c, button, &m.encodingMenu, func(menu *ui.MenuItems) {
		menu.Above()
		menu.WidthRange(162, 192)
		menu.RowHeight(27)
		menu.Label("按编码重新打开")
		if menu.Item("自动识别").Clicked() {
			c.After(func() { m.reopenTextEncoding(tab, "") })
		}
		menu.Separator()
		for _, enc := range textEncodings {
			if menu.Item(string(enc)).Checked(enc == tab.doc.encoding).Clicked() {
				c.After(func() { m.reopenTextEncoding(tab, enc) })
			}
		}
	})
}

func (m *editorApp) reopenTextEncoding(tab *editorTab, enc textEncoding) {
	if tab.doc.path == "" {
		return
	}
	if _, composing := tab.source.text.Composition(); composing || tab.modified() || tab.hasResultDraft() {
		m.fail(fmt.Errorf("请先保存当前修改，再按其他编码打开 %s", tab.title()))
		return
	}
	if tab.largeText != nil && tab.largeText.busyEditing() {
		return
	}
	if state := tab.largeText; state != nil {
		if state.editing != nil {
			m.startLargeTextEdit(tab, enc)
			return
		}
		page, err := readLargeTextPage(tab.doc.path, state.index, -1, "", enc)
		if err != nil {
			m.fail(fmt.Errorf("重新打开 %s 失败\n%w", tab.title(), err))
			return
		}
		state.found, state.search, state.selectedResult = -1, nil, -1
		tab.applyLargeTextPage(page)
	} else {
		doc, err := readDocumentAs(tab.doc.path, enc)
		if err != nil {
			m.fail(fmt.Errorf("重新打开 %s 失败\n%w", tab.title(), err))
			return
		}
		tab.doc = doc
		tab.source.setResult(doc.saved)
		tab.source.foldSource, tab.outlineSource = "", ""
		tab.resultReady = false
		tab.search.ready = false
		tab.search.refresh()
		m.results.dirty = true
		if tab.html.view != nil {
			m.refreshHTML(tab)
		}
	}
	m.status = "已按 " + tab.doc.encoding.label(tab.doc.bom) + " 重新打开"
	m.focusActiveSource()
}
