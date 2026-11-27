package editor

import (
	"github.com/vivaui/viva/ui"
)

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
				if ui.Button(c, uiText("返回模板列表")).Icon(chevronLeftIcon).IconOnly().Ghost().Size(26, 28).Disabled(blocked).Clicked() {
					state.details, state.focusSearch = false, true
				}
			} else {
				ui.Icon(c, templateIcon).Size(19, 19).Color(m.theme.MutedForeground)
			}
			ui.Text(c, uiText("模板库")).Bold().FontSize(15).Grow(1)
			if !state.details && ui.Button(c.Part("create-template"), uiText("新建模板…")).Icon(plusIcon).IconOnly().Ghost().
				Size(28, 28).Tooltip(uiText("新建模板…")).Disabled(blocked).Clicked() {
				c.After(func() {
					form := &templateDialogState{open: true, saving: true, project: m.activeTextProject(m.active), eol: "CRLF"}
					form.filename.SetText(uiText("新建文件.txt"))
					m.templateDialog = form
				})
			}
			if ui.Button(c, uiText("关闭模板库")).Icon(closeIcon).IconOnly().Ghost().Size(28, 28).Disabled(blocked).Clicked() {
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
			m.templateSearch(c.Part("search"), state, blocked)
			ui.Flow(c.Part("sources")).FillWidth().Gap(5).LineGap(5).Children(func() {
				for _, scope := range []string{"", "个人", "项目", "内置"} {
					if scope == "项目" && state.project == "" {
						continue
					}
					label := scope
					if scope == "" {
						label = "全部"
					}
					button := ui.Button(c.Key(scope), uiText(label)).Ghost().Height(27).PadX(8).FontSize(12).Disabled(blocked)
					if state.filterScope == scope {
						button.Secondary()
					}
					if button.Clicked() {
						c.After(func() { state.filterScope = scope; state.updateTemplateOptions() })
					}
				}
			})
			if len(state.options) == 0 {
				ui.Text(c, uiText("没有匹配的模板")).Muted().FontSize(12).PadY(12)
				return
			}
			m.templateList(c.Part("list"), state, blocked)
			return
		}
		ui.Scroll(c.Part("details")).FillWidth().Grow(1).MinHeight(0).Scrollbars().Children(func() {
			ui.Column(c).FillWidth().PadRight(6).Gap(8).Children(func() { m.templatePreview(c, state, blocked) })
		})
		ui.Row(c).FillWidth().Gap(6).PadTop(6).Children(func() {
			if ui.Button(c, uiText("创建草稿")).Grow(1).FontSize(12).Height(34).Disabled(blocked).Clicked() {
				c.After(func() {
					state.problem = ""
					state.inserting = false
					if err := m.useTemplate(state); err != nil {
						state.problem = err.Error()
					}
				})
			}
			if ui.Button(c, uiText("插入当前文档")).Outline().FontSize(12).Tooltip(uiText("有选区时替换选区，否则插入光标位置")).Disabled(blocked || !editableTextSource(m.active)).Clicked() {
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
