package editor

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	uimarkdown "github.com/vivaui/viva/ui/markdown"
)

var newTextFormats = [...]struct{ label, extension string }{
	{"纯文本", ".txt"}, {"Markdown", ".md"}, {"JSON", ".json"}, {"HTML", ".html"},
	{"YAML", ".yaml"}, {"XML", ".xml"}, {"CSV", ".csv"},
}

type templateChoice struct {
	template documentTemplate
	scope    string
}

type templatePreviewInput struct {
	choice             int
	filename           string
	inserting, english bool
	values             [templateFieldLimit]string
}

type templateDialogState struct {
	split                                 ui.SplitterState
	inSidebar, details, focusSearch       bool
	open, saving, inserting, projectScope bool
	variableOpen, templatesOnly           bool
	directory, project, problem, warning  string
	target                                textEditTarget
	choices                               []templateChoice
	options                               []int
	selected                              int
	list                                  ui.ListState
	search                                ui.Field
	filter, filterScope                   string
	name, filename, body, fieldName       ui.Field
	original                              *documentTemplate
	fields                                []templateField
	values                                []ui.Field
	eol                                   string
	bom                                   bool
	preview                               uimarkdown.State
	previewInput                          templatePreviewInput
	previewContent, previewIssue          string
	previewReady                          bool
}

func (m *editorApp) newFileOptions(c ui.C, menu *ui.MenuItems) {
	for _, format := range newTextFormats {
		if menu.Item(m.menuText(format.label) + " · " + format.extension).Clicked() {
			c.After(func() {
				m.createTextDraft(m.untitledName(format.extension), "", document{eol: "CRLF"})
			})
		}
	}
	menu.Separator()
	if menu.Item(m.menuText("从模板新建…")).Clicked() {
		c.After(func() { m.openTemplatePicker("", false) })
	}
}

func (m *editorApp) templateSaveActions(c ui.C, menu *ui.MenuItems, tab *editorTab) {
	if !aiTextTab(tab) || tab.comparingJSON() {
		return
	}
	if menu.Item(m.menuText("将文档存为模板…")).Clicked() {
		c.After(func() { m.openTemplateSave(tab, false) })
	}
	span := tab.source.text.Selection()
	if menu.Item(m.menuText("将选区存为模板…")).Disabled(!editableTextSource(tab) || span.Start == span.End).Clicked() {
		c.After(func() { m.openTemplateSave(tab, true) })
	}
}

func (m *editorApp) openTemplateSave(tab *editorTab, selection bool) {
	if !aiTextTab(tab) || tab.comparingJSON() {
		return
	}
	if _, composing := tab.source.text.Composition(); composing {
		m.fail(errors.New(uiText("请先完成中文选词")))
		return
	}
	body := tab.source.text.CommittedString()
	if selection {
		target, err := captureTextTarget(tab, true, templateBodyLimit)
		if err != nil {
			m.fail(err)
			return
		}
		body = target.selected
	}
	if body == "" || len(body) > templateBodyLimit {
		m.fail(errors.New(uiText("模板正文须为 128 KiB 以内的非空文本")))
		return
	}
	filename := tab.title()
	if filepath.Ext(filename) == "" {
		switch tab.format() {
		case "JSON":
			filename += ".json"
		case "Markdown":
			filename += ".md"
		case "HTML":
			filename += ".html"
		}
	}
	state := &templateDialogState{
		open: true, saving: true, project: m.activeTextProject(tab), eol: tab.doc.eol, bom: tab.doc.bom,
	}
	state.name.SetText(strings.TrimSuffix(filename, filepath.Ext(filename)))
	state.filename.SetText(filename)
	state.body.SetText(body)
	m.templateDialog = state
}

func (m *editorApp) openTemplatePicker(directory string, insert bool) {
	state := &templateDialogState{open: true, inserting: insert, directory: directory, project: m.activeTextProject(m.active)}
	if insert {
		target, err := captureTextTarget(m.active, false, templateBodyLimit)
		if err != nil {
			m.fail(err)
			return
		}
		state.target = target
	}
	m.loadTemplatePicker(state, "", "")
}

func (m *editorApp) loadTemplatePicker(state *templateDialogState, preferred, scope string) {
	directory := state.directory
	if directory != "" {
		state.project = m.projectRoot(directory)
		state.choices = append(state.choices, templateChoice{template: documentTemplate{
			Name: "自定义", Filename: m.untitledName(""), EOL: "CRLF",
		}})
		for _, format := range newTextFormats {
			state.choices = append(state.choices, templateChoice{template: documentTemplate{
				Name: format.label, Filename: m.untitledName(format.extension), EOL: "CRLF",
			}})
		}
	}
	if state.project != "" {
		m.addTemplateChoices(state, state.project, "项目")
	}
	m.addTemplateChoices(state, "", "个人")
	for _, template := range builtInTemplates() {
		state.choices = append(state.choices, templateChoice{template, "内置"})
	}
	state.options = make([]int, len(state.choices))
	for i := range state.options {
		state.options[i] = i
	}
	selected := 0
	for i, choice := range state.choices {
		if choice.scope == scope && choice.template.Name == preferred {
			selected = i
			break
		}
	}
	state.list.Key = func(row int) int { return state.options[row] }
	state.list.Label = func(row int) string { return state.choices[state.options[row]].label() }
	state.selectTemplate(selected)
	state.list.ScrollIntoView(selected)
	if state.directory != "" {
		state.updateTemplateOptions()
		state.filename.Select(0, state.filename.GetEditor().RuneCount())
		m.templateDialog = state
		return
	}
	if m.ai.open {
		m.toggleAIPanel()
	}
	m.leavePasswordVault()
	m.tools.gitOpen = false
	m.translation, m.templateDialog = nil, nil
	state.inSidebar, state.details, state.split = true, preferred != "", m.newAssetSplit()
	state.focusSearch = !state.details
	m.templatePanel = state
}

func (m *editorApp) addTemplateChoices(state *templateDialogState, project, scope string) {
	path, err := m.textAssetPath(project, templateLibraryFile)
	if err == nil {
		var templates []documentTemplate
		templates, err = loadTemplates(path)
		for _, template := range templates {
			state.choices = append(state.choices, templateChoice{template, scope})
		}
	}
	if err != nil {
		state.warning = strings.TrimSpace(state.warning + "\n" + fmt.Sprintf(uiText("无法读取%s模板：%v"), scope, err))
	}
}

func (state *templateDialogState) selectTemplate(index int) {
	state.selected = index
	template := state.choices[index].content()
	state.filename.SetText(template.Filename)
	state.values = make([]ui.Field, len(template.Fields))
	for i, field := range template.Fields {
		state.values[i].SetText(field.Default)
	}
	state.problem = ""
	state.preview.Offset = 0
	state.previewReady = false
}

func (state *templateDialogState) filterTemplates() {
	query := strings.ToLower(strings.TrimSpace(state.search.GetEditor().CommittedString()))
	if query == state.filter {
		return
	}
	state.filter = query
	state.updateTemplateOptions()
}

func (state *templateDialogState) updateTemplateOptions() {
	state.options = state.options[:0]
	for i, choice := range state.choices {
		if state.directory != "" && state.templatesOnly != (choice.scope != "") {
			continue
		}
		if state.filterScope != "" && state.filterScope != choice.scope {
			continue
		}
		search := choice.label() + " " + choice.template.Name + " " + choice.template.Filename
		if strings.Contains(strings.ToLower(search), state.filter) {
			state.options = append(state.options, i)
		}
	}
	state.list.ScrollIntoView(0)
	if row := slices.Index(state.options, state.selected); row >= 0 {
		state.list.Select(row)
	} else {
		state.selected = -1
		if len(state.options) > 0 {
			state.selectTemplate(state.options[0])
			state.list.Select(0)
		}
	}
}

func (choice templateChoice) label() string {
	if choice.scope == "" {
		if extension := filepath.Ext(choice.template.Filename); extension != "" {
			return choice.title() + " · " + extension
		}
		return choice.title()
	}
	return choice.title() + " · " + uiText(choice.scope)
}

func (choice templateChoice) title() string {
	if choice.scope == "" || choice.scope == "内置" {
		return uiText(choice.template.Name)
	}
	return choice.template.Name
}

func (choice templateChoice) content() documentTemplate {
	template := choice.template
	if choice.scope != "内置" || !englishInterface.Load() {
		return template
	}
	template.Name, template.Body = uiText(template.Name), uiText(template.Body)
	template.Fields = slices.Clone(template.Fields)
	for i := range template.Fields {
		field := &template.Fields[i]
		field.Name, field.Default = uiText(field.Name), uiText(field.Default)
	}
	return template
}

func (m *editorApp) closeTemplateDialog() {
	m.templateDialog = nil
	m.focusActiveSource()
}

func (m *editorApp) showTemplateDialog(c ui.C) {
	state := m.templateDialog
	if state == nil {
		return
	}
	title, action, width := uiText("模板库"), "创建草稿", float32(840)
	filenameSubmitted := false
	switch {
	case state.saving:
		title, action, width = uiText("存为模板"), "保存", 620
		if state.original != nil {
			title = uiText("编辑模板")
		}
	case state.inserting:
		title, action = uiText("插入模板"), "应用到文档"
	case state.directory != "":
		title, action = uiText("新建文件"), "创建文件"
		if !state.templatesOnly {
			width = 500
		}
	}
	dismissed := ui.ModalWith(c, &state.open, uiText(title), ui.ModalSpec{Width: width}, func() {
		if state.saving {
			m.templateSaveForm(c, state)
			return
		}
		m.templatePickerForm(c, state)
		if state.directory != "" && !state.templatesOnly {
			filenameSubmitted = m.projectFileFormatForm(c, state)
		}
		if state.problem != "" {
			ui.Wrap(c, state.problem).FillWidth().FontSize(12).Color(m.theme.Danger)
		}
	}, func() {
		if ui.Button(c, uiText("取消")).Ghost().Clicked() {
			c.After(m.closeTemplateDialog)
		}
		if state.saving && ui.Button(c, uiText("保存并预览")).Secondary().Clicked() {
			c.After(func() {
				if err := m.saveTemplate(state, true); err != nil {
					state.problem = err.Error()
				}
			})
		}
		if !state.saving && !state.inserting && state.directory == "" && editableTextSource(m.active) &&
			ui.Button(c, uiText("插入当前文档…")).Outline().Disabled(state.selected < 0).Clicked() {
			c.After(func() {
				target, err := captureTextTarget(m.active, false, templateBodyLimit)
				if err != nil {
					state.problem = err.Error()
					return
				}
				state.inserting, state.target, state.problem = true, target, ""
			})
		}
		if state.inserting && ui.Button(c, uiText("改为创建草稿")).Ghost().Clicked() {
			state.inserting, state.target, state.problem = false, textEditTarget{}, ""
		}
		ready := state.saving || state.selected >= 0
		if state.directory != "" && !state.templatesOnly {
			ready = ready && strings.TrimSpace(state.filename.Text()) != ""
		}
		if ui.Button(c, uiText(action)).Disabled(!ready).Clicked() || ready && filenameSubmitted {
			c.After(func() {
				if state.saving {
					if err := m.saveTemplate(state, false); err != nil {
						state.problem = err.Error()
					}
					return
				}
				if err := m.useTemplate(state); err != nil {
					state.problem = err.Error()
					return
				}
				m.closeTemplateDialog()
			})
		}
	})
	if dismissed {
		m.closeTemplateDialog()
	}
}

func (m *editorApp) templateSaveForm(c ui.C, state *templateDialogState) {
	ui.Column(c).FillWidth().Gap(12).Children(func() {
		ui.Row(c).FillWidth().Gap(12).Children(func() {
			ui.Column(c.Part("name")).Grow(1).Basis(0).MinWidth(0).Gap(5).Children(func() {
				ui.Text(c, uiText("模板名称")).FontSize(12)
				ui.TextField(c, &state.name, uiText("模板名称"), "").FillWidth().Height(32)
			})
			ui.Column(c.Part("filename")).Grow(1).Basis(0).MinWidth(0).Gap(5).Children(func() {
				ui.Text(c, uiText("默认文件名")).FontSize(12)
				ui.TextField(c, &state.filename, uiText("默认文件名"), "").FillWidth().Height(32)
			})
		})
		ui.Column(c.Part("content")).FillWidth().Gap(5).Children(func() {
			ui.Row(c.Part("content-actions")).FillWidth().Gap(8).Items(ui.Center).Children(func() {
				ui.Text(c, uiText("正文")).Grow(1).FontSize(12)
				button := ui.Button(c.Part("mark"), uiText("设为变量")).Icon(plusIcon).Ghost().Height(26).FontSize(12).
					Tooltip(uiText("选中正文中的文字，再设为变量"))
				if state.variableOpen {
					button.Secondary()
				}
				if button.Clicked() {
					state.problem = ""
					selection := state.body.GetEditor().Selection()
					if !state.variableOpen && selection.Start == selection.End {
						state.problem = uiText("请先在模板正文中选中文字")
					} else {
						state.variableOpen = !state.variableOpen
					}
				}
			})
			ui.TextArea(c.Part("body"), &state.body, uiText("模板正文"), uiText("输入模板正文")).FillWidth().Height(148)
			if state.variableOpen {
				ui.Row(c.Part("variable")).FillWidth().Gap(8).Items(ui.Center).Children(func() {
					name := ui.TextField(c.Part("field"), &state.fieldName, uiText("变量名"), uiText("变量名")).Grow(1).MinWidth(0).Height(32).AutoFocus()
					if ui.Button(c.Part("add"), uiText("添加变量")).Secondary().Height(32).FontSize(12).Clicked() || name.Submitted() {
						c.After(func() {
							if err := state.markTemplateField(); err != nil {
								state.problem = err.Error()
								return
							}
							state.variableOpen, state.problem = false, ""
						})
					}
				})
			}
		})
		if state.original == nil && state.project != "" {
			ui.Row(c.Part("destination")).FillWidth().Gap(8).Items(ui.Center).Children(func() {
				ui.Text(c, uiText("保存到")).FontSize(12)
				ui.Select(c, &state.projectScope, []bool{false, true}, func(project bool) string {
					if project {
						return uiText("项目 · ") + filepath.Base(state.project)
					}
					return uiText("个人模板库")
				}).Label(uiText("保存到")).Width(208).Height(32)
			})
		}
		if state.problem != "" {
			ui.Wrap(c, state.problem).FillWidth().FontSize(12).Color(m.theme.Danger)
		}
	})
}

func (state *templateDialogState) markTemplateField() error {
	editor := state.body.GetEditor()
	if _, composing := editor.Composition(); composing {
		return errors.New(uiText("请先完成中文选词"))
	}
	name := strings.TrimSpace(state.fieldName.Text())
	if !placeholderName(name) || len([]rune(name)) > 24 {
		return errors.New(uiText("变量名须为 24 个字符以内的文字、数字、点、下划线或连字符"))
	}
	span := editor.Selection()
	if span.Start == span.End {
		return errors.New(uiText("请先在模板正文中选中文字"))
	}
	body := editor.CommittedString()
	marker := "{{" + name + "}}"
	if !slicesContainsField(state.fields, name) {
		if len(state.fields) >= templateFieldLimit {
			return fmt.Errorf(uiText("每个模板最多支持 %d 个变量"), templateFieldLimit)
		}
		if strings.Contains(body, marker) {
			return errors.New(uiText("该标记已出现在正文中，请换一个变量名"))
		}
		value := body[editor.ByteOffset(span.Start):editor.ByteOffset(span.End)]
		state.fields = append(state.fields, templateField{Name: name, Default: strings.Clone(value)})
	}
	editor.ReplaceRange(span, marker)
	state.fieldName.SetText("")
	return nil
}

func slicesContainsField(fields []templateField, name string) bool {
	for _, field := range fields {
		if field.Name == name {
			return true
		}
	}
	return false
}

func (m *editorApp) saveTemplate(state *templateDialogState, useNow bool) error {
	for _, field := range []*ui.Field{&state.name, &state.filename, &state.body} {
		if _, composing := field.GetEditor().Composition(); composing {
			return errors.New(uiText("请先完成中文选词"))
		}
	}
	body := state.body.Text()
	template := documentTemplate{
		Name: strings.TrimSpace(state.name.Text()), Filename: strings.TrimSpace(state.filename.Text()),
		Body: body, EOL: state.eol, BOM: state.bom,
	}
	for _, field := range state.fields {
		if strings.Contains(body, "{{"+field.Name+"}}") {
			template.Fields = append(template.Fields, field)
		}
	}
	project := ""
	if state.projectScope {
		project = state.project
	}
	path, err := m.textAssetPath(project, templateLibraryFile)
	if err != nil {
		return err
	}
	if err := writeTemplate(path, template, state.original); err != nil {
		return fmt.Errorf(uiText("无法保存模板：%w"), err)
	}
	m.status = uiText("已保存模板 · ") + template.Name
	if useNow || m.templatePanel != nil {
		scope := "个人"
		if state.projectScope {
			scope = "项目"
		}
		m.loadTemplatePicker(&templateDialogState{open: true, project: state.project}, template.Name, scope)
	} else {
		m.closeTemplateDialog()
	}
	return nil
}

func (m *editorApp) templatePickerForm(c ui.C, state *templateDialogState) {
	if state.directory != "" {
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			ui.Text(c, state.directory).FillWidth().Ellipsis().Muted().FontSize(12).
				Hit(ui.ID(c.Part("project-location")), state.directory).Tooltip(state.directory)
			ui.Row(c.Part("creation-mode")).Gap(4).Pad(3).Radius(10).Background(m.theme.Background).Children(func() {
				for index, label := range []string{"常见格式", "模板"} {
					templates := index == 1
					button := ui.Button(c.KeyInt(index), uiText(label)).Ghost().MinWidth(95).Height(30)
					if state.templatesOnly == templates {
						button.Secondary()
					}
					if button.Clicked() && state.templatesOnly != templates {
						c.After(func() {
							state.templatesOnly = templates
							state.search.SetText("")
							state.filter = ""
							state.updateTemplateOptions()
						})
					}
				}
			})
		})
		if !state.templatesOnly {
			return
		}
	}
	if state.warning != "" {
		ui.Wrap(c, state.warning).FillWidth().FontSize(12).Color(m.theme.Danger)
	}
	if state.inserting {
		location := "光标位置"
		if state.target.span.Start != state.target.span.End {
			location = "当前选区（替换）"
		}
		ui.Wrap(c, uiText("应用位置 · ")+state.target.tab.tabTitle()+" · "+uiText(location)).FillWidth().Muted().FontSize(12)
	}
	ui.Row(c).FillWidth().Gap(12).Children(func() {
		ui.Column(c.Part("catalog")).Width(230).Shrink(0).Gap(6).Children(func() {
			m.templateSearch(c.Part("search"), state, false)
			m.templateList(c.Part("list"), state, false)
			if state.directory == "" && ui.Button(c, uiText("新建模板…")).Icon(plusIcon).Ghost().FillWidth().Clicked() {
				c.After(func() {
					form := &templateDialogState{open: true, saving: true, project: state.project, eol: "CRLF"}
					form.filename.SetText(uiText("新建文件.txt"))
					m.templateDialog = form
				})
			}
		})
		ui.Column(c.Part("details")).Grow(1).MinWidth(0).Pad(12).Radius(12).Background(m.theme.Muted).Gap(7).Children(func() {
			if state.selected < 0 {
				ui.Wrap(c, uiText("没有找到匹配的名称或后缀。")).FillWidth().Muted()
				return
			}
			m.templatePreview(c, state, false)
		})
	})
}

func (m *editorApp) projectFileFormatForm(c ui.C, state *templateDialogState) bool {
	submitted := false
	ui.Column(c.Part("project-file-format")).FillWidth().Gap(8).PadTop(8).Children(func() {
		ui.Text(c, uiText("文件名")).FontSize(12).Bold()
		filename := ui.TextField(c.Part("filename"), &state.filename, uiText("文件名"), uiText("输入文件名，可自定义后缀")).
			FillWidth().Height(36).AutoFocus()
		submitted = filename.Submitted()

		const perRow = 3
		for start := 0; start < len(state.options); start += perRow {
			ui.Row(c.KeyInt(start)).FillWidth().Gap(6).Children(func() {
				for offset := 0; offset < perRow; offset++ {
					if start+offset >= len(state.options) {
						ui.Slot(c).Width(0).Grow(1)
						break
					}
					index := state.options[start+offset]
					choice := state.choices[index]
					button := ui.Button(c.KeyInt(index), choice.label()).Ghost().Height(30).Width(0).MinWidth(0).Grow(1).FontSize(12)
					if state.selected == index {
						button.Secondary()
					}
					if state.selected != index && button.Clicked() {
						c.After(func() {
							name := state.filename.Text()
							customName := name != state.choices[state.selected].template.Filename
							state.selectTemplate(index)
							if customName {
								state.filename.SetText(name)
							}
						})
					}
				}
			})
		}
	})
	return submitted
}

func (m *editorApp) templateSearch(c ui.C, state *templateDialogState, blocked bool) {
	ui.Row(c).FillWidth().Height(36).PadX(8).Gap(6).Items(ui.Center).
		Radius(9).Background(m.theme.Card).Children(func() {
		ui.Icon(c, searchIcon).Size(15, 15).Color(m.theme.MutedForeground).Shrink(0)
		search := ui.TextFieldBase(c.Part("query"), &state.search, uiText("搜索模板"), uiText("搜索名称或后缀")).
			Grow(1).MinWidth(0).Height(32).Disabled(blocked).OnKey(func(event gfx.KeyEvent) gfx.EventResult {
			if event.Release || event.Modifiers != 0 || state.search.GetEditor().CompositionActive() || len(state.options) == 0 {
				return gfx.EventResult{}
			}
			row := slices.Index(state.options, state.selected)
			switch event.Key {
			case gfx.KeyDown:
				row = min(row+1, len(state.options)-1)
			case gfx.KeyUp:
				row = max(0, row-1)
			case gfx.KeyEnter:
				if state.inSidebar && state.selected >= 0 {
					state.details = true
					return gfx.EventResult{Handled: true, Repaint: true}
				}
				return gfx.EventResult{}
			default:
				return gfx.EventResult{}
			}
			if index := state.options[row]; index != state.selected {
				state.selectTemplate(index)
			}
			state.list.Select(row)
			state.list.ScrollIntoView(row)
			return gfx.EventResult{Handled: true, Repaint: true}
		})
		if state.focusSearch && !blocked {
			search.AutoFocus()
			state.focusSearch = false
		}
		if state.search.Text() != "" && ui.Button(c.Part("clear"), uiText("清空搜索")).Icon(closeIcon).IconOnly().Ghost().
			Size(25, 25).Tooltip(uiText("清空搜索")).Disabled(blocked).Clicked() {
			c.After(func() { state.search.SetText(""); state.focusSearch = true })
		}
	})
	state.filterTemplates()
}

func (m *editorApp) templateList(c ui.C, state *templateDialogState, blocked bool) {
	list := ui.List(c, &state.list, len(state.options), func(row ui.ListRow) {
		index := state.options[row.Index]
		choice := state.choices[index]
		item := row.Element.Height(54).PadX(9).Gap(9).Items(ui.Center).Radius(9).
			HitWith(ui.ID(row.C), choice.label(), gfx.HitOptions{Role: gfx.RoleButton, Disabled: blocked}).
			Hover(ui.Look{}.Background(m.theme.Hover)).Focus(ui.Look{}.Background(m.theme.Hover))
		if state.selected == index {
			item.Background(m.theme.Selection)
		}
		item.Children(func() {
			ink, fill := m.assetAccent()
			icon := fileIcon
			switch documentFormat(choice.template.Filename) {
			case "Markdown":
				icon = markdownIcon
			case "JSON":
				icon = jsonIcon
			case "HTML":
				icon = codeIcon
			}
			ui.Row(row.C).Size(30, 32).Shrink(0).Radius(8).Background(fill).Center().Children(func() {
				ui.Icon(row.C, icon).Size(17, 17).Color(ink)
			})
			detail := filepath.Ext(choice.template.Filename)
			if choice.scope != "" {
				detail = uiText(choice.scope) + " · " + uiText(documentFormat(choice.template.Filename))
			} else if detail == "" {
				detail = "任意后缀，也可省略"
			}
			ui.Column(row.C).Grow(1).MinWidth(0).Gap(4).Children(func() {
				ui.Text(row.C, choice.title()).FillWidth().Ellipsis().FontSize(13).Bold()
				ui.Text(row.C, uiText(detail)).FillWidth().Ellipsis().Muted().FontSize(11)
			})
			if state.inSidebar {
				ui.Icon(row.C, chevronRightIcon).Size(12, 12).Color(m.theme.MutedForeground).Shrink(0)
			}
		})
		if item.Clicked() && !blocked {
			c.After(func() { state.selectTemplate(index); state.details = true })
		}
	}).FillWidth().Label(uiText("模板列表"))
	if state.inSidebar {
		list.Grow(1).MinHeight(0)
	} else {
		list.Height(256)
	}
	if row := state.list.Selected(); !blocked && state.list.Changed() && row >= 0 && row < len(state.options) {
		index := state.options[row]
		c.After(func() {
			if state.selected != index {
				state.selectTemplate(index)
			}
		})
	}
	if _, submitted := state.list.Submitted(); submitted && !blocked {
		state.details = true
	}
}

func (m *editorApp) templatePreview(c ui.C, state *templateDialogState, blocked bool) {
	choice := state.choices[state.selected]
	template := choice.content()
	ui.Row(c).FillWidth().Items(ui.Center).Gap(6).Children(func() {
		ui.Text(c, choice.label()).Bold().FontSize(14).Grow(1).MinWidth(0).Ellipsis()
		if (choice.scope == "个人" || choice.scope == "项目") &&
			ui.Button(c, uiText("编辑模板…")).Ghost().FontSize(12).Disabled(blocked).Clicked() {
			c.After(func() {
				form := &templateDialogState{
					open: true, saving: true, original: &template, project: state.project,
					projectScope: choice.scope == "项目", eol: template.EOL, bom: template.BOM,
					fields: slices.Clone(template.Fields),
				}
				form.name.SetText(template.Name)
				form.filename.SetText(template.Filename)
				form.body.SetText(template.Body)
				m.templateDialog = form
			})
		}
	})
	if !state.inserting {
		ui.TextField(c.Part("filename"), &state.filename, uiText("文件名"), uiText("新文档的文件名")).FillWidth().Disabled(blocked)
	}
	if choice.scope == "" {
		return
	}
	ui.Scroll(c.Part("preview-area")).FillWidth().Height(235).Scrollbars().Children(func() {
		ui.Column(c).FillWidth().Gap(7).PadRight(8).Children(func() {
			if len(template.Fields) > 0 {
				ui.Text(c, uiText("填写变量")).Muted().FontSize(12)
			}
			for i, field := range template.Fields {
				ui.Row(c.Key(field.Name)).FillWidth().Gap(8).Items(ui.Center).Children(func() {
					ui.Text(c, field.Name).Width(72).FontSize(12).Muted().Ellipsis()
					ui.TextField(c.KeyInt(i).Part("value"), &state.values[i], field.Name, field.Name).Grow(1).MinWidth(0).Disabled(blocked)
				})
			}
			preview, issue := state.templatePreviewContent(template)
			ui.Row(c).FillWidth().Items(ui.Center).Gap(8).Children(func() {
				ui.Icon(c, fileIcon).Size(15, 15).Color(m.theme.MutedForeground)
				ui.Text(c, uiText("内容预览")).Muted().FontSize(12)
			})
			ui.Column(c).FillWidth().Pad(12).Radius(8).Background(m.theme.Background).Children(func() {
				if documentFormat(template.Filename) == "Markdown" {
					state.preview.Source = preview
					state.preview.Style = uimarkdown.Style{Size: 13, LineHeight: 22}
					uimarkdown.View(c, "模板内容预览", &state.preview).FillWidth().Height(max(180, state.preview.Height()))
				} else {
					ui.Wrap(c, preview).FillWidth().FontSize(13)
				}
			})
			if issue != "" {
				ui.Wrap(c, issue).FillWidth().Muted().FontSize(12)
			}
		})
	})
	if !state.inserting {
		encoding := "UTF-8"
		if template.BOM {
			encoding += " BOM"
		}
		ui.Text(c, uiText(documentFormat(state.filename.Text()))+" · "+encoding+" · "+uiText(template.EOL)).Muted().FontSize(12)
	}
}

func (state *templateDialogState) templatePreviewContent(template documentTemplate) (string, string) {
	input := templatePreviewInput{choice: state.selected, filename: state.previewFilename(), inserting: state.inserting, english: englishInterface.Load()}
	for i := range state.values {
		input.values[i] = state.values[i].Text()
	}
	if !state.previewReady || input != state.previewInput {
		value, err := renderTemplate(template, input.filename, input.values[:len(state.values)], input.inserting)
		length, count := 0, 0
		for length < len(value) && count < 2048 {
			_, size := utf8.DecodeRuneInString(value[length:])
			length += size
			count++
		}
		if length < len(value) {
			value = value[:length] + "\n…"
		}
		state.previewContent, state.previewIssue = value, ""
		if err != nil {
			state.previewIssue = err.Error()
		}
		state.previewInput, state.previewReady = input, true
	}
	return state.previewContent, state.previewIssue
}

func (state *templateDialogState) previewFilename() string {
	if state.inserting {
		return state.choices[state.selected].template.Filename
	}
	name := strings.TrimSpace(state.filename.Text())
	if state.directory != "" && name != "" && filepath.Ext(name) == "" {
		name += filepath.Ext(state.choices[state.selected].template.Filename)
	}
	return name
}

func (state *templateDialogState) fieldValues() []string {
	values := make([]string, len(state.values))
	for i := range state.values {
		values[i] = state.values[i].Text()
	}
	return values
}

func (m *editorApp) useTemplate(state *templateDialogState) error {
	choice := state.choices[state.selected]
	template := choice.content()
	filename := state.previewFilename()
	if !state.inserting {
		if _, composing := state.filename.GetEditor().Composition(); composing {
			return errors.New(uiText("请先完成中文选词"))
		}
		if choice.scope != "" || state.directory == "" {
			if err := validateTextFilename(filename); err != nil {
				return err
			}
		}
	}
	for i := range state.values {
		if _, composing := state.values[i].GetEditor().Composition(); composing {
			return errors.New(uiText("请先完成中文选词"))
		}
	}
	var body string
	if choice.scope != "" {
		var err error
		body, err = renderTemplate(template, filename, state.fieldValues(), state.inserting)
		if err != nil {
			return err
		}
	}
	if state.inserting {
		value, start, end, err := state.target.current(m.active)
		if err != nil {
			return err
		}
		if m.active.format() == "JSON" && !json.Valid([]byte(value[:start]+body+value[end:])) {
			return errors.New(uiText("插入后无法解析为 JSON"))
		}
		m.active.source.text.ReplaceRange(state.target.span, body)
		m.status = uiText("已插入模板 · ") + template.Name
		return nil
	}
	doc := document{bom: template.BOM, eol: template.EOL}
	if state.directory != "" {
		return m.createTextFile(state.directory, filename, body, doc)
	}
	m.createTextDraft(filename, body, doc)
	return nil
}
