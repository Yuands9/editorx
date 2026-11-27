package editor

import "github.com/vivaui/viva/ui"

func canConvertText(tab *editorTab) bool {
	if tab == nil || tab.office != nil || tab.readOnlyPreview() || tab.largeText != nil || tab.savingText != nil {
		return false
	}
	_, composing := tab.source.text.Composition()
	return !composing
}

func (m *editorApp) convertTextEncoding(tab *editorTab, enc textEncoding, bom bool) {
	if !canConvertText(tab) {
		return
	}
	if tab.doc.encoding == enc && tab.doc.bom == bom {
		return
	}
	tab.doc.encoding, tab.doc.bom, tab.doc.formatDirty = enc, bom, true
	m.status = uiText("保存时转换为 ") + enc.label(bom)
}

func (m *editorApp) convertTextEOL(tab *editorTab, eol string) {
	if !canConvertText(tab) || tab.doc.eol == eol {
		return
	}
	if eol != "LF" && eol != "CRLF" && eol != "CR" {
		return
	}
	tab.doc.eol, tab.doc.formatDirty = eol, true
	m.status = uiText("保存时统一换行为 ") + eol
}

func (m *editorApp) eolPicker(c ui.C, tab *editorTab, blocked bool) {
	label := tab.doc.eol
	if label == "" {
		label = "LF"
	}
	button := ui.Button(c.Part("file-eol"), label).Icon(chevronDownIcon).Ghost().Height(24).PadX(7).FontSize(12).Tooltip(m.menuText("转换换行格式")).Disabled(blocked || !canConvertText(tab))
	if button.Clicked() {
		m.eolMenu = !m.eolMenu
	}
	if blocked {
		m.eolMenu = false
	}
	ui.Menu(c, button, &m.eolMenu, func(menu *ui.MenuItems) {
		menu.Above()
		menu.Label(m.menuText("转换换行格式（保存后生效）"))
		for _, eol := range []string{"LF", "CRLF", "CR"} {
			if menu.Item(eol).Checked(tab.doc.eol == eol).Clicked() {
				c.After(func() { m.convertTextEOL(tab, eol) })
			}
		}
	})
}
