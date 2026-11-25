package editor

import "github.com/vivaui/viva/ui"

func (m *editorApp) toggleTemplates() {
	if m.leavePasswordVault() && m.templatePanel != nil {
		return
	}
	if m.templatePanel != nil {
		m.templatePanel = nil
		m.focusActiveSource()
		return
	}
	m.openTemplatePicker("", false)
}

func (m *editorApp) templatesPanel(c ui.C, blocked bool) {
	state := m.templatePanel
	m.workspacePanel(c).Grow(1).MarginRight(workspaceInset).Pad(12).Gap(10).Children(func() {
		ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
			if state.details {
				if ui.Button(c, "返回模板列表").Icon(chevronLeftIcon).IconOnly().Ghost().Size(26, 28).Disabled(blocked).Clicked() {
					state.details = false
				}
			} else {
				ui.Icon(c, templateIcon).Size(19, 19).Color(m.theme.MutedForeground)
			}
			ui.Text(c, "模板库").Bold().FontSize(15).Grow(1)
			if ui.Button(c, "关闭模板库").Icon(closeIcon).IconOnly().Ghost().Size(28, 28).Disabled(blocked).Clicked() {
				c.After(m.toggleTemplates)
			}
		})
		if state.warning != "" {
			ui.Wrap(c, state.warning).FillWidth().Color(m.theme.Danger).FontSize(12)
		}
		if state.problem != "" {
			ui.Wrap(c, state.problem).FillWidth().Color(m.theme.Danger).FontSize(12)
		}
		if !state.details || state.selected < 0 {
			ui.TextField(c.Part("search"), &state.search, "搜索模板", "搜索模板").FillWidth().Disabled(blocked)
			state.filterTemplates()
			m.templateList(c.Part("list"), state, blocked)
			if len(state.options) == 0 {
				ui.Text(c, "没有匹配的模板").Muted().FontSize(12)
			}
			if ui.Button(c, "新建模板…").Icon(plusIcon).Ghost().FillWidth().Disabled(blocked).Clicked() {
				c.After(func() {
					form := &templateDialogState{open: true, saving: true, project: m.activeTextProject(m.active), eol: "CRLF"}
					form.filename.SetText("新建文件.txt")
					m.templateDialog = form
				})
			}
			return
		}
		ui.Scroll(c.Part("details")).FillWidth().Grow(1).MinHeight(0).Scrollbars().Children(func() {
			ui.Column(c).FillWidth().PadRight(6).Gap(8).Children(func() { m.templatePreview(c, state, blocked) })
		})
		ui.Row(c).FillWidth().Gap(6).Children(func() {
			if ui.Button(c, "创建草稿").Grow(1).FontSize(12).Disabled(blocked).Clicked() {
				c.After(func() {
					state.problem = ""
					state.inserting = false
					if err := m.useTemplate(state); err != nil {
						state.problem = err.Error()
					}
				})
			}
			if ui.Button(c, "插入当前文档").Outline().FontSize(12).Tooltip("有选区时替换选区，否则插入光标位置").Disabled(blocked || !editableTextSource(m.active)).Clicked() {
				c.After(func() {
					state.problem = ""
					target, err := captureTextTarget(m.active, false, templateBodyLimit)
					if err != nil {
						state.problem = err.Error()
						return
					}
					state.inserting, state.target = true, target
					if err := m.useTemplate(state); err != nil {
						state.problem = err.Error()
					}
					state.inserting = false
					m.focusActiveSource()
				})
			}
		})
	})
}
