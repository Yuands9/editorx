package editor

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	uimarkdown "github.com/vivaui/viva/ui/markdown"
)

var newTextFormats = [...]struct{ label, extension string }{
	{"纯文本", ".txt"}, {"Markdown", ".md"}, {"JSON", ".json"}, {"HTML", ".html"},
}

type templateChoice struct {
	template documentTemplate
	scope    string
}

type templateDialogState struct {
	split                                 ui.SplitterState
	inSidebar, details                    bool
	open, saving, inserting, projectScope bool
	variableOpen                          bool
	directory, project, problem, warning  string
	target                                textEditTarget
	choices                               []templateChoice
	options                               []int
	selected                              int
	list                                  ui.ListState
	search                                ui.Field
	filter                                string
	name, filename, body, fieldName       ui.Field
	original                              *documentTemplate
	fields                                []templateField
	values                                []ui.Field
	eol                                   string
	bom                                   bool
	preview                               uimarkdown.State
}

func (m *editorApp) newFileOptions(c ui.C, menu *ui.MenuItems) {
	for _, format := range newTextFormats {
		if menu.Item(format.label + " · " + format.extension).Clicked() {
			c.After(func() {
				m.createTextDraft(m.untitledName(format.extension), "", document{eol: "CRLF"})
			})
		}
	}
	menu.Separator()
	if menu.Item("从模板新建…").Clicked() {
		c.After(func() { m.openTemplatePicker("", false) })
	}
}

func (m *editorApp) templateSaveActions(c ui.C, menu *ui.MenuItems, tab *editorTab) {
	if !aiTextTab(tab) || tab.jsonCompare {
		return
	}
	if menu.Item("将文档存为模板…").Clicked() {
		c.After(func() { m.openTemplateSave(tab, false) })
	}
	span := tab.source.text.Selection()
	if menu.Item("将选区存为模板…").Disabled(!editableTextSource(tab) || span.Start == span.End).Clicked() {
		c.After(func() { m.openTemplateSave(tab, true) })
	}
}

func (m *editorApp) sourceEditor(c ui.C, tab *editorTab, blocked bool) ui.Box {
	box := tab.source.declare(c, "代码编辑区", !blocked)
	if blocked || !m.translationEnabled {
		return box
	}
	span := tab.source.text.Selection()
	if span.Start == span.End {
		return box
	}
	ui.ContextMenu(c, box, func(menu *ui.MenuItems) {
		if menu.Item("翻译选区…").Icon(translationIcon).Clicked() {
			c.After(func() { m.openTranslation(true) })
		}
	})
	return box
}

func (m *editorApp) openTemplateSave(tab *editorTab, selection bool) {
	if !aiTextTab(tab) || tab.jsonCompare {
		return
	}
	if _, composing := tab.source.text.Composition(); composing {
		m.fail(errors.New("请先完成中文选词"))
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
		m.fail(errors.New("模板正文须为 128 KiB 以内的非空文本"))
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
		state.choices = append(state.choices, templateChoice{template: documentTemplate{Filename: m.untitledName(".txt"), EOL: "CRLF"}})
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
		m.templateDialog = state
		return
	}
	if m.ai.open {
		m.toggleAIPanel()
	}
	m.translation, m.templateDialog = nil, nil
	state.inSidebar, state.details, state.split = true, preferred != "", m.newAssetSplit()
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
		state.warning = strings.TrimSpace(state.warning + "\n" + fmt.Sprintf("无法读取%s模板：%v", scope, err))
	}
}

func (state *templateDialogState) selectTemplate(index int) {
	state.selected = index
	template := state.choices[index].template
	state.filename.SetText(template.Filename)
	state.values = make([]ui.Field, len(template.Fields))
	for i, field := range template.Fields {
		state.values[i].SetText(field.Default)
	}
	state.problem = ""
	state.preview.Offset = 0
}

func (state *templateDialogState) filterTemplates() {
	query := strings.ToLower(strings.TrimSpace(state.search.Text()))
	if query == state.filter {
		return
	}
	state.filter = query
	state.options = state.options[:0]
	for i, choice := range state.choices {
		if strings.Contains(strings.ToLower(choice.label()+" "+choice.template.Filename), query) {
			state.options = append(state.options, i)
		}
	}
	state.list.ScrollIntoView(0)
	if !slices.Contains(state.options, state.selected) {
		state.selected = -1
		if len(state.options) > 0 {
			state.selectTemplate(state.options[0])
		}
	}
}

func (choice templateChoice) label() string {
	if choice.scope == "" {
		return "空白文件"
	}
	return choice.template.Name + " · " + choice.scope
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
	title, action, width := "模板库", "创建草稿", float32(840)
	switch {
	case state.saving:
		title, action, width = "存为模板", "保存", 620
		if state.original != nil {
			title = "编辑模板"
		}
	case state.inserting:
		title, action = "插入模板", "应用到文档"
	case state.directory != "":
		title, action = "在此文件夹新建", "创建文件"
	}
	dismissed := ui.ModalWith(c, &state.open, title, ui.ModalSpec{Width: width}, func() {
		if state.saving {
			m.templateSaveForm(c, state)
			return
		}
		m.templatePickerForm(c, state)
		if state.problem != "" {
			ui.Wrap(c, state.problem).FillWidth().FontSize(12).Color(m.theme.Danger)
		}
	}, func() {
		if ui.Button(c, "取消").Ghost().Clicked() {
			c.After(m.closeTemplateDialog)
		}
		if state.saving && ui.Button(c, "保存并预览").Secondary().Clicked() {
			c.After(func() {
				if err := m.saveTemplate(state, true); err != nil {
					state.problem = err.Error()
				}
			})
		}
		if !state.saving && !state.inserting && state.directory == "" && editableTextSource(m.active) &&
			ui.Button(c, "插入当前文档…").Outline().Disabled(state.selected < 0).Clicked() {
			c.After(func() {
				target, err := captureTextTarget(m.active, false, templateBodyLimit)
				if err != nil {
					state.problem = err.Error()
					return
				}
				state.inserting, state.target, state.problem = true, target, ""
			})
		}
		if state.inserting && ui.Button(c, "改为创建草稿").Ghost().Clicked() {
			state.inserting, state.target, state.problem = false, textEditTarget{}, ""
		}
		if ui.Button(c, action).Disabled(!state.saving && state.selected < 0).Clicked() {
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
				ui.Text(c, "模板名称").FontSize(12)
				ui.TextField(c, &state.name, "模板名称", "").FillWidth().Height(32)
			})
			ui.Column(c.Part("filename")).Grow(1).Basis(0).MinWidth(0).Gap(5).Children(func() {
				ui.Text(c, "默认文件名").FontSize(12)
				ui.TextField(c, &state.filename, "默认文件名", "").FillWidth().Height(32)
			})
		})
		ui.Column(c.Part("content")).FillWidth().Gap(5).Children(func() {
			ui.Row(c.Part("content-actions")).FillWidth().Gap(8).Items(ui.Center).Children(func() {
				ui.Text(c, "正文").Grow(1).FontSize(12)
				button := ui.Button(c.Part("mark"), "设为变量").Icon(plusIcon).Ghost().Height(26).FontSize(12).
					Tooltip("选中正文中的文字，再设为变量")
				if state.variableOpen {
					button.Secondary()
				}
				if button.Clicked() {
					state.problem = ""
					selection := state.body.GetEditor().Selection()
					if !state.variableOpen && selection.Start == selection.End {
						state.problem = "请先在模板正文中选中文字"
					} else {
						state.variableOpen = !state.variableOpen
					}
				}
			})
			ui.TextArea(c.Part("body"), &state.body, "模板正文", "输入模板正文").FillWidth().Height(148)
			if state.variableOpen {
				ui.Row(c.Part("variable")).FillWidth().Gap(8).Items(ui.Center).Children(func() {
					name := ui.TextField(c.Part("field"), &state.fieldName, "变量名", "变量名").Grow(1).MinWidth(0).Height(32).AutoFocus()
					if ui.Button(c.Part("add"), "添加变量").Secondary().Height(32).FontSize(12).Clicked() || name.Submitted() {
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
				ui.Text(c, "保存到").FontSize(12)
				ui.Select(c, &state.projectScope, []bool{false, true}, func(project bool) string {
					if project {
						return "项目 · " + filepath.Base(state.project)
					}
					return "个人模板库"
				}).Label("保存到").Width(208).Height(32)
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
		return errors.New("请先完成中文选词")
	}
	name := strings.TrimSpace(state.fieldName.Text())
	if !placeholderName(name) || len([]rune(name)) > 24 {
		return errors.New("变量名须为 24 个字符以内的文字、数字、点、下划线或连字符")
	}
	span := editor.Selection()
	if span.Start == span.End {
		return errors.New("请先在模板正文中选中文字")
	}
	body := editor.CommittedString()
	marker := "{{" + name + "}}"
	if !slicesContainsField(state.fields, name) {
		if len(state.fields) >= templateFieldLimit {
			return fmt.Errorf("每个模板最多支持 %d 个变量", templateFieldLimit)
		}
		if strings.Contains(body, marker) {
			return errors.New("该标记已出现在正文中，请换一个变量名")
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
			return errors.New("请先完成中文选词")
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
		return fmt.Errorf("无法保存模板：%w", err)
	}
	m.status = "已保存模板 · " + template.Name
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
	m.assetHeading(c, templateIcon, "从熟悉的内容开始", "① 选择模板   →   ② 填写变量、查看预览   →   ③ 新建文档或插入选区")
	if state.directory != "" {
		ui.Wrap(c, state.directory).FillWidth().Muted().FontSize(12)
	}
	if state.warning != "" {
		ui.Wrap(c, state.warning).FillWidth().FontSize(12).Color(m.theme.Danger)
	}
	if state.inserting {
		location := "光标位置"
		if state.target.span.Start != state.target.span.End {
			location = "当前选区（替换）"
		}
		ui.Wrap(c, "应用位置 · "+state.target.tab.tabTitle()+" · "+location).FillWidth().Muted().FontSize(12)
	}
	ui.Row(c).FillWidth().Gap(12).Children(func() {
		ui.Column(c.Part("catalog")).Width(230).Shrink(0).Gap(6).Children(func() {
			ui.TextField(c.Part("search"), &state.search, "搜索模板", "搜索名称或后缀").FillWidth()
			state.filterTemplates()
			m.templateList(c.Part("list"), state, false)
			if state.directory == "" && ui.Button(c, "新建模板…").Icon(plusIcon).Ghost().FillWidth().Clicked() {
				c.After(func() {
					form := &templateDialogState{open: true, saving: true, project: state.project, eol: "CRLF"}
					form.filename.SetText("新建文件.txt")
					m.templateDialog = form
				})
			}
		})
		ui.Column(c.Part("details")).Grow(1).MinWidth(0).Pad(12).Radius(12).Background(m.theme.Muted).Gap(7).Children(func() {
			if state.selected < 0 {
				ui.Wrap(c, "没有找到匹配的模板，可以试试其他名称或后缀。").FillWidth().Muted()
				return
			}
			m.templatePreview(c, state, false)
		})
	})
}

func (m *editorApp) templateList(c ui.C, state *templateDialogState, blocked bool) {
	list := ui.List(c, &state.list, len(state.options), func(row ui.ListRow) {
		index := state.options[row.Index]
		choice := state.choices[index]
		item := row.Element.Height(64).PadX(9).Gap(9).Items(ui.Center).Radius(9).
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
			ui.Row(row.C).Size(32, 36).Shrink(0).Radius(7).Background(fill).Center().Children(func() {
				ui.Icon(row.C, icon).Size(21, 21).Color(ink)
			})
			name := choice.template.Name
			if choice.scope == "" {
				name = "空白文件"
			}
			ui.Column(row.C).Grow(1).MinWidth(0).Gap(4).Children(func() {
				ui.Text(row.C, name).FillWidth().Ellipsis().FontSize(13).Bold()
				ui.Text(row.C, choice.scope+" · "+documentFormat(choice.template.Filename)).Muted().FontSize(11)
			})
		})
		if item.Clicked() && !blocked {
			c.After(func() { state.selectTemplate(index); state.details = true })
		}
	}).FillWidth().Label("模板列表")
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
	template := choice.template
	ui.Row(c).FillWidth().Items(ui.Center).Gap(6).Children(func() {
		ui.Text(c, choice.label()).Bold().FontSize(14).Grow(1).MinWidth(0).Ellipsis()
		if (choice.scope == "个人" || choice.scope == "项目") &&
			ui.Button(c, "编辑模板…").Ghost().FontSize(12).Disabled(blocked).Clicked() {
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
		ui.TextField(c.Part("filename"), &state.filename, "文件名", "新文档的文件名").FillWidth().Disabled(blocked)
	}
	if template.Name == "" {
		ui.Wrap(c, "从空白开始。创建后直接输入内容，Ctrl+N 也能随时新建空白标签。").FillWidth().Muted().FontSize(12)
		return
	}
	ui.Scroll(c.Part("preview-area")).FillWidth().Height(235).Scrollbars().Children(func() {
		ui.Column(c).FillWidth().Gap(7).PadRight(8).Children(func() {
			if len(template.Fields) > 0 {
				ui.Text(c, "填写变量").Muted().FontSize(12)
			}
			for i, field := range template.Fields {
				ui.Row(c.Key(field.Name)).FillWidth().Gap(8).Items(ui.Center).Children(func() {
					ui.Text(c, field.Name).Width(70).FontSize(12).Muted()
					ui.TextField(c.Key(field.Name+"/value"), &state.values[i], field.Name, "填写"+field.Name).Grow(1).MinWidth(0).Disabled(blocked)
				})
			}
			preview, issue := renderTemplate(template, state.previewFilename(), state.fieldValues(), state.inserting)
			ui.Row(c).FillWidth().Items(ui.Center).Gap(8).Children(func() {
				ui.Icon(c, fileIcon).Size(15, 15).Color(m.theme.MutedForeground)
				ui.Text(c, "内容预览").Muted().FontSize(12)
			})
			previewRunes := []rune(preview)
			if len(previewRunes) > 2048 {
				preview = string(previewRunes[:2048]) + "\n…"
			}
			ui.Column(c).FillWidth().Pad(12).Radius(8).Background(m.theme.Background).Children(func() {
				if documentFormat(template.Filename) == "Markdown" {
					state.preview.Source = preview
					state.preview.Style = uimarkdown.Style{Size: 13, LineHeight: 22}
					uimarkdown.View(c, "模板内容预览", &state.preview).FillWidth().Height(max(180, state.preview.Height()))
				} else {
					ui.Wrap(c, preview).FillWidth().FontSize(13)
				}
			})
			if issue != nil {
				ui.Wrap(c, issue.Error()).FillWidth().Muted().FontSize(12)
			}
		})
	})
	if !state.inserting {
		encoding := "UTF-8"
		if template.BOM {
			encoding += " BOM"
		}
		ui.Text(c, documentFormat(state.filename.Text())+" · "+encoding+" · "+template.EOL).Muted().FontSize(12)
	}
}

func (state *templateDialogState) previewFilename() string {
	if state.inserting {
		return state.choices[state.selected].template.Filename
	}
	return state.filename.Text()
}

func (state *templateDialogState) fieldValues() []string {
	values := make([]string, len(state.values))
	for i := range state.values {
		values[i] = state.values[i].Text()
	}
	return values
}

func (m *editorApp) useTemplate(state *templateDialogState) error {
	template := state.choices[state.selected].template
	filename := strings.TrimSpace(state.previewFilename())
	if !state.inserting {
		if _, composing := state.filename.GetEditor().Composition(); composing {
			return errors.New("请先完成中文选词")
		}
		if err := validateTextFilename(filename); err != nil {
			return err
		}
	}
	for i := range state.values {
		if _, composing := state.values[i].GetEditor().Composition(); composing {
			return errors.New("请先完成中文选词")
		}
	}
	body, err := renderTemplate(template, filename, state.fieldValues(), state.inserting)
	if err != nil && template.Name != "" {
		return err
	}
	if state.inserting {
		value, start, end, err := state.target.current(m.active)
		if err != nil {
			return err
		}
		if m.active.format() == "JSON" && !json.Valid([]byte(value[:start]+body+value[end:])) {
			return errors.New("插入后无法解析为 JSON")
		}
		m.active.source.text.ReplaceRange(state.target.span, body)
		m.status = "已插入模板 · " + template.Name
		return nil
	}
	doc := document{bom: template.BOM, eol: template.EOL}
	if state.directory != "" {
		return m.createTextFile(state.directory, filename, body, doc)
	}
	m.createTextDraft(filename, body, doc)
	return nil
}
