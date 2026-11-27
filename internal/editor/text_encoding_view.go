package editor

import (
	"fmt"

	"github.com/vivaui/viva/ui"
)

func (m *editorApp) encodingPicker(c ui.C, tab *editorTab, blocked bool) {
	if blocked {
		m.encodingMenu = false
	}
	_, composing := tab.source.text.Composition()
	button := ui.Button(c.Part("file-encoding"), tab.doc.encoding.label(tab.doc.bom)).Icon(chevronDownIcon).Ghost().Height(24).PadX(7).FontSize(12).Tooltip(m.menuText("文件编码")).Disabled(blocked || composing || tab.savingText != nil)
	if button.Clicked() {
		m.encodingMenu = !m.encodingMenu
	}
	ui.Menu(c, button, &m.encodingMenu, func(menu *ui.MenuItems) {
		menu.Above()
		menu.WidthRange(200, 250)
		menu.RowHeight(27)
		menu.Sub(m.menuText("转换编码（保存后生效）"), func(items *ui.MenuItems) {
			for _, enc := range textEncodings {
				bom := enc == utf16LEEncoding || enc == utf16BEEncoding
				if items.Item(enc.label(bom)).Checked(tab.doc.encoding == enc && tab.doc.bom == bom).Clicked() {
					c.After(func() { m.convertTextEncoding(tab, enc, bom) })
				}
			}
			if items.Item("UTF-8 BOM").Checked(tab.doc.encoding == utf8Encoding && tab.doc.bom).Clicked() {
				c.After(func() { m.convertTextEncoding(tab, utf8Encoding, true) })
			}
		}).Disabled(!canConvertText(tab))
		menu.Sub(m.menuText("按编码重新打开"), func(items *ui.MenuItems) {
			if items.Item(m.menuText("自动识别")).Clicked() {
				c.After(func() { m.reopenTextEncoding(tab, "") })
			}
			for _, enc := range textEncodings {
				if items.Item(string(enc)).Checked(enc == tab.doc.encoding).Clicked() {
					c.After(func() { m.reopenTextEncoding(tab, enc) })
				}
			}
		}).Disabled(tab.doc.path == "" || tab.modified() || tab.hasResultDraft() || tab.largeText != nil && tab.largeText.busyEditing())
	})
}

func (m *editorApp) reopenTextEncoding(tab *editorTab, enc textEncoding) {
	if tab.doc.path == "" {
		return
	}
	if _, composing := tab.source.text.Composition(); composing || tab.modified() || tab.hasResultDraft() {
		m.fail(fmt.Errorf(uiText("请先保存当前修改，再按其他编码打开 %s"), tab.title()))
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
			m.fail(fmt.Errorf(uiText("重新打开 %s 失败\n%w"), tab.title(), err))
			return
		}
		state.found, state.search, state.selectedResult = -1, nil, -1
		tab.applyLargeTextPage(page)
	} else {
		doc, err := readDocumentAs(tab.doc.path, enc)
		if err != nil {
			m.fail(fmt.Errorf(uiText("重新打开 %s 失败\n%w"), tab.title(), err))
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
	m.status = uiText("已按 ") + tab.doc.encoding.label(tab.doc.bom) + uiText(" 重新打开")
	m.focusActiveSource()
}
