package editor

import (
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/ui"
)

type tabRenameState struct {
	open  bool
	tab   *editorTab
	group *tabGroup
	field ui.Field
}

func (r *tabRenameState) begin(tab *editorTab, group *tabGroup) {
	*r = tabRenameState{open: true, tab: tab, group: group}
	if group != nil {
		r.field.SetText(group.Name)
	} else {
		r.field.SetText(tab.tabTitle())
	}
	r.field.Select(0, r.field.GetEditor().RuneCount())
}

func (m *editorApp) renameDialog(c ui.C) {
	rename := &m.rename
	if !rename.open {
		return
	}
	title, label, saveLabel := uiText("重命名标签"), "标签名称", "保存标签名称"
	if rename.group != nil {
		title, label, saveLabel = uiText("重命名标签分组"), "分组名称", "保存分组名称"
	}
	closeDialog := func() {
		m.rename = tabRenameState{}
		m.focusActiveSource()
	}
	submitted := false
	dismissed := ui.ModalWith(c, &rename.open, title, ui.ModalSpec{Width: 400}, func() {
		field := ui.TextField(c, &rename.field, label, uiText("输入")+label).FillWidth()
		submitted = field.Submitted()
	}, func() {
		if ui.Button(c, uiText("取消重命名")).Outline().Clicked() {
			c.After(closeDialog)
		}
		name := strings.TrimSpace(rename.field.Text())
		_, composing := rename.field.GetEditor().Composition()
		valid := !composing && name != "" && (rename.group == nil || utf8.RuneCountInString(name) <= 32)
		save := ui.Button(c, saveLabel).Disabled(!valid).Tooltip(uiText("Enter 确认"))
		if valid && (save.Clicked() || submitted) {
			c.After(func() {
				if rename.group != nil {
					rename.group.Name = name
				} else if rename.tab.doc.path == "" {
					rename.tab.name = name
				} else if name == rename.tab.title() {
					rename.tab.displayName = ""
				} else {
					rename.tab.displayName = name
				}
				m.tabReveal = true
				closeDialog()
			})
		}
	})
	if dismissed {
		closeDialog()
	}
}
