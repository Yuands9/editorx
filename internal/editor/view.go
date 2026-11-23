package editor

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

const workspaceInset = 8

func (m *editorApp) workspacePanel(c ui.C) ui.Box {
	return ui.Column(c).FillHeight().MinWidth(0).Radius(m.theme.Metrics.RadiusSurface).
		Background(m.theme.Background).Clip()
}

func (m *editorApp) page(c ui.C) {
	m.backupProgress(c.Part("backup"))
	m.updateProgress(c.Part("update"))
	m.browserUpdate(c)
	m.aiUpdate(c)
	m.updateProjectGit(c)
	m.updateLocalShell()
	blocked := m.interactionBlocked()
	if !blocked && m.appSettings.shortcutEdit.action == shortcutNone && m.shortcutPressed(c, shortcutOpenSettings) {
		c.After(m.toggleApplicationSettings)
	}
	ui.Row(c.Part("workspace")).Fill().Background(m.theme.Card).Children(func() {
		if m.appSettings.open {
			m.settingsPage(c.Part("settings"))
		} else {
			m.editorPage(c.Part("editor"), blocked)
		}
		m.rightRail(c.Part("right-rail"), blocked)
	})
	if !m.appSettings.open {
		if m.active != nil && !blocked {
			m.searchDialog(c, m.active)
		}
		m.historyDialog(c)
		m.renameDialog(c)
		m.aiConversationDialog(c)
		m.aiReviewDialog(c)
	}
	m.backupSelectionDialog(c.Part("backup-selection"))
	m.aiDiscardDraftDialog(c.Part("ai-discard-draft"))
	m.updateDialog(c.Part("update-dialog"))
	m.dialogs(c)
}

func (m *editorApp) editorPage(c ui.C, blocked bool) {
	t := m.active
	if t != nil {
		isJSON := t.format() == "JSON"
		t.applyDefaultWrap(m.editorPrefs)
		if isJSON {
			t.updateResult()
		}
	}
	if !blocked {
		m.editorShortcuts(c)
		if t != nil && t.search.open && ui.Shortcut(c, 0, "escape") {
			c.After(t.search.close)
		}
	}

	page := ui.Column(c).Grow(1).MinWidth(0).FillHeight()
	if t != nil {
		page.DrawOver(func(d *ui.DrawContext) { t.syncViews(d.Canvas) })
	}
	page.Children(func() {
		upper := ui.Row(c).FillWidth().MinHeight(0).PadY(workspaceInset).PadLeft(workspaceInset)
		if m.tools.active != "" {
			upper.Height(m.tools.split.Pos).Shrink(0)
		} else {
			upper.Grow(1).Basis(0)
		}
		upper.Children(func() {
			if m.showFiles {
				m.workspacePanel(c.Part("sidebar")).Width(m.filesSplit.Pos).Shrink(0).PadBottom(6).
					Children(func() { m.sidebar(c, t, blocked) })
				ui.Splitter(c, &m.filesSplit).Mins(210, 580).SplitterThickness(workspaceInset)
			}
			ui.Row(c.Part("editing-area")).Grow(1).MinWidth(0).FillHeight().Clip().Children(func() {
				workbench := ui.Row(c.Part("workbench")).MinWidth(0).FillHeight().PadRight(workspaceInset).Clip()
				if m.ai.open {
					workbench.Width(m.aiEditorSplit.Pos).Shrink(0).PadRight(0)
				} else {
					workbench.Grow(1)
				}
				workbench.Children(func() {
					m.workspacePanel(c.Part("documents")).Grow(1).PadX(4).PadBottom(6).Children(func() {
						m.tabBar(c, blocked)
						if t == nil {
							ui.Stack(c.Part("opening")).FillWidth().Grow(1).MinHeight(0).Children(func() {
								m.openingPage(c, blocked)
							})
							return
						}
						m.documentHeader(c, t, blocked)
						m.searchView(c, t, blocked)
						m.documentPanes(c.Part("document-panes"), t, blocked)
					})
				})
				if m.ai.open {
					ui.Splitter(c, &m.aiEditorSplit).Mins(232, 280).SplitterThickness(12).
						Label("调整编辑区与 AI 助手宽度").DrawOver(func(d *ui.DrawContext) {
						m.drawAIResizeGrip(d, &m.aiEditorSplit, true)
					})
					m.aiPanel(c.Part("assistant"), blocked)
				}
			})
		})
		if m.tools.active != "" {
			ui.Splitter(c.Part("tools-split"), &m.tools.split).Mins(150, 150).SplitterThickness(7).Label("调整底部面板高度")
			m.projectToolsPanel(c.Part("tools"), blocked)
		}
		if t != nil {
			m.statusBar(c, t, blocked)
		}
	})
}

func (m *editorApp) documentPanes(c ui.C, t *editorTab, blocked bool) {
	ui.Row(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
		if t.format() == "Markdown" && m.showOutline && !m.ai.open {
			ui.Column(c.Part("outline")).Width(m.outlineSplit.Pos).Shrink(0).FillHeight().Clip().
				Children(func() { m.outlinePanel(c, t) })
			ui.Splitter(c, &m.outlineSplit).Mins(160, 420).SplitterThickness(workspaceInset).Label("调整大纲宽度")
		}
		ui.Column(c.Part("document-content")).Grow(1).MinWidth(0).FillHeight().Children(func() {
			body := ui.Column(c.Part("editor-body")).FillWidth().MinHeight(0)
			if m.results.open {
				body.Height(m.results.split.Pos)
			} else {
				body.Grow(1)
			}
			body.Children(func() { m.documentView(c, t, blocked) })
			if m.results.open {
				ui.Splitter(c, &m.results.split).Mins(120, 100).SplitterThickness(6)
				m.searchResultsView(c)
			}
		})
	})
}

func (m *editorApp) rightRail(c ui.C, blocked bool) {
	ui.Column(c).Width(44).Shrink(0).FillHeight().PadX(6).PadY(workspaceInset + 4).Gap(6).Children(func() {
		t := m.active
		if t != nil && !m.appSettings.open {
			save := ui.Button(c, "保存文件").Icon(saveIcon).IconOnly().Ghost().Width(32).Height(34).Radius(11).Disabled(t.readOnlyPreview() || blocked).Tooltip(m.shortcutHint("保存文件", shortcutSave))
			if t.modified() {
				save.Secondary().ButtonRecipe(m.pinButtonRecipe)
			}
			if save.Clicked() {
				c.After(func() { m.saveTab(t, false) })
			}
			searchHint := m.shortcutHint("查找", shortcutFind) + " / " + m.shortcutHint("替换", shortcutReplace)
			if t.office != nil {
				searchHint = "Office 文档请使用文档内的查找功能"
			}
			if ui.Button(c, "查找内容").Icon(searchIcon).IconOnly().Ghost().Width(32).Height(34).Radius(11).Disabled(t.readOnlyPreview() || t.office != nil || blocked).Tooltip(searchHint).Clicked() {
				c.After(t.openSearch)
			}
			ui.Slot(c).FillWidth().Height(1).MarginY(3).Background(m.theme.Border)
			wrap := ui.Button(c, "自动折行").Icon(wrapIcon).IconOnly().Ghost().Width(30).Height(34).Disabled(t.readOnlyPreview() || t.office != nil || blocked).Tooltip(m.shortcutHint("自动折行", shortcutWrap))
			if !t.readOnlyPreview() && t.source.view.Snapshot.Wrap {
				wrap.Secondary()
			}
			if wrap.Clicked() {
				t.toggleWrap()
			}
			assistant := ui.Button(c, "AI 助手").Icon(aiIcon).IconOnly().Ghost().Width(30).Height(34).Tooltip("AI 助手").TooltipOn(ui.TooltipLeft).Disabled(blocked)
			if m.ai.open || m.ai.hasPendingRequest() {
				assistant.Secondary()
			}
			if assistant.Clicked() {
				c.After(m.toggleAIPanel)
			}
			for _, tool := range []struct {
				name string
				icon gfx.Icon
			}{{"Git", gitBranchIcon}, {"终端", shellIcon}} {
				entry := ui.Button(c.Key(tool.name), tool.name).Icon(tool.icon).IconOnly().Ghost().Width(30).Height(34).Tooltip(tool.name).Disabled(blocked)
				if m.tools.active == tool.name {
					entry.Secondary()
				}
				if entry.Clicked() {
					c.After(func() { m.useTool(tool.name) })
				}
			}
		}
		ui.Slot(c).Grow(1)
		if !m.appSettings.open {
			m.themeButton(c.Part("theme-toggle"), blocked)
		}
		label := "设置"
		if m.appSettings.open {
			label = "关闭设置"
		}
		gear := ui.Button(c.Part("settings-toggle"), label).Icon(settingsIcon).IconOnly().Ghost().Size(32, 34).
			Disabled(blocked).Tooltip(m.shortcutHint(label, shortcutOpenSettings))
		if m.appSettings.open {
			gear.Secondary()
		}
		if gear.Clicked() {
			c.After(m.toggleApplicationSettings)
		}
	})
}

func (m *editorApp) mainMenu(c ui.C, t *editorTab, blocked bool) {
	button := ui.Button(c, "主菜单").Icon(menuIcon).IconOnly().Ghost().Width(32).Height(32).Tooltip("主菜单").Disabled(blocked)
	if button.Clicked() {
		m.fileMenu = !m.fileMenu
	}
	ui.Menu(c, button, &m.fileMenu, func(menu *ui.MenuItems) {
		menu.WidthRange(264, 300)
		menu.RowHeight(32)
		menu.Label(applicationName)
		if m.shortcutMenu(menu.Item("新建").Icon(plusIcon), shortcutNewFile).Clicked() {
			c.After(m.newDocument)
		}
		if m.shortcutMenu(menu.Item("新建窗口"), shortcutNewWindow).Disabled(m.newWindow == nil).Clicked() {
			c.After(m.newWindow)
		}
		if m.shortcutMenu(menu.Item("打开…").Icon(fileIcon), shortcutOpenFile).Clicked() {
			c.After(m.open)
		}
		if m.shortcutMenu(menu.Item("添加项目…").Icon(folderIcon), shortcutOpenProject).Clicked() {
			c.After(m.openFolder)
		}
		if menu.Item("最近打开…").Disabled(m.store == nil || len(m.store.history) == 0).Clicked() {
			m.showHistory = true
		}
		menu.Separator()
		if t != nil {
			if m.shortcutMenu(menu.Item("保存").Disabled(t.readOnlyPreview()), shortcutSave).Clicked() {
				c.After(func() { m.saveTab(t, false) })
			}
			if m.shortcutMenu(menu.Item("另存为…").Disabled(t.readOnlyPreview()), shortcutSaveAs).Clicked() {
				c.After(func() { m.saveTab(t, true) })
			}
			if m.shortcutMenu(menu.Item("关闭当前标签"), shortcutCloseTab).Disabled(t.isPinned()).Clicked() {
				c.After(func() { m.closeTab(t) })
			}
			menu.Separator()
			if menu.Item("搜索结果").Disabled(m.results.query == "").Clicked() {
				m.results.open = true
			}
			if m.shortcutMenu(menu.Item("搜索内容").Disabled(!t.canSearchText()), shortcutFind).Clicked() {
				c.After(t.openSearch)
			}
			if m.shortcutMenu(menu.Item("替换内容").Disabled(!t.canSearchText()), shortcutReplace).Clicked() {
				c.After(t.openReplace)
			}
		} else if menu.Item("AI 助手").Icon(aiIcon).Checked(m.ai.open).Clicked() {
			c.After(m.toggleAIPanel)
		}
		menu.Separator()
		if m.shortcutMenu(menu.Item("Git").Icon(gitBranchIcon), shortcutGit).Checked(m.tools.active == "Git").Clicked() {
			c.After(func() { m.useTool("Git") })
		}
		if m.shortcutMenu(menu.Item("本地终端").Icon(shellIcon), shortcutTerminal).Checked(m.tools.active == "终端").Clicked() {
			c.After(func() { m.useTool("终端") })
		}
		if t != nil && m.shortcutMenu(menu.Item("自动折行").Disabled(t.readOnlyPreview() || t.office != nil).Checked(t.source.view.Snapshot.Wrap), shortcutWrap).Clicked() {
			t.toggleWrap()
		}
		menu.Separator()
		if m.shortcutMenu(menu.Item("设置").Icon(settingsIcon), shortcutOpenSettings).Clicked() {
			c.After(m.openApplicationSettings)
		}
		if m.desktop != nil && menu.Item("退出 EditorX").Clicked() {
			c.After(m.desktop.quit)
		}
	})
}

func (m *editorApp) fileAppearance(name string) (gfx.Icon, gfx.Color) {
	if isImageFile(name) {
		return imageIcon, m.theme.Ring
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".pdf":
		return fileIcon, gfx.RGB(213, 121, 122)
	case ".xmind":
		return folderIcon, m.theme.Ring
	case ".xlsx":
		if m.light {
			return spreadsheetIcon, gfx.RGB(28, 119, 80)
		}
		return spreadsheetIcon, gfx.RGB(116, 196, 159)
	case ".docx":
		if m.light {
			return wordIcon, gfx.RGB(45, 98, 176)
		}
		return wordIcon, gfx.RGB(137, 181, 243)
	case ".json":
		if m.light {
			return jsonIcon, gfx.RGB(151, 84, 22)
		}
		return jsonIcon, gfx.RGB(218, 184, 126)
	case ".md", ".markdown":
		if m.light {
			return markdownIcon, gfx.RGB(34, 91, 164)
		}
		return markdownIcon, gfx.RGB(132, 181, 233)
	case ".xml", ".html", ".htm", ".svg":
		if m.light {
			return codeIcon, gfx.RGB(30, 112, 68)
		}
		return codeIcon, gfx.RGB(151, 194, 167)
	default:
		if m.light {
			return fileIcon, gfx.RGB(96, 105, 118)
		}
		return fileIcon, gfx.RGB(142, 155, 174)
	}
}

func (m *editorApp) tabBar(c ui.C, blocked bool) {
	if !blocked {
		blocked = m.handleGroupDoubleClick(c)
	}
	ui.Row(c).FillWidth().Height(tabStripHeight + 4).PadX(4).Items(ui.Center).Children(func() {
		if !m.showFiles {
			m.mainMenu(c, m.active, blocked)
			if ui.Button(c, "展开文件栏").Icon(leftPanelIcon).IconOnly().Ghost().Width(32).Height(32).Tooltip(m.shortcutHint("显示左侧主菜单", shortcutFiles)).Disabled(blocked).Clicked() {
				c.After(m.toggleFiles)
			}
			for _, action := range []struct {
				label string
				icon  gfx.Icon
				run   func()
			}{
				{"打开文件", fileIcon, m.open}, {"添加项目", folderIcon, m.openFolder},
			} {
				if ui.Button(c, action.label).Icon(action.icon).IconOnly().Ghost().Width(30).Height(32).Tooltip(action.label).Disabled(m.confirm || m.showError).Clicked() {
					c.After(action.run)
				}
			}
		}
		scroll := ui.Scroll(c).Sideways().Scrollbars().Grow(1).MinWidth(0).FillHeight()
		m.tabStrip(c, scroll, blocked)
		if ui.Button(c, "新建标签").Icon(plusIcon).IconOnly().Ghost().Size(30, 30).MarginX(5).
			Shrink(0).Radius(10).Tooltip(m.shortcutHint("新建标签", shortcutNewFile)).Disabled(blocked).Clicked() {
			c.After(m.newDocument)
		}
	})
}

func (m *editorApp) documentHeader(c ui.C, t *editorTab, blocked bool) {
	format := t.format()
	if format != "JSON" && format != "Markdown" && format != "HTML" {
		return
	}
	ui.Row(c).FillWidth().Height(38).PadX(16).Gap(6).Items(ui.Center).Children(func() {
		if format == "Markdown" {
			label := "展开大纲"
			if m.showOutline {
				label = "收起大纲"
			}
			toggle := ui.Button(c.Part("outline-toggle"), label).Icon(leftPanelIcon).Ghost().Height(28).FontSize(12).
				Disabled(blocked || m.ai.open).Tooltip(m.shortcutHint(label, shortcutOutline))
			if m.showOutline {
				toggle.Secondary()
			}
			if toggle.Clicked() {
				c.After(m.toggleMarkdownOutline)
			}
		}
		ui.Slot(c).Grow(1)
		if t.format() == "HTML" {
			ui.Row(c).Pad(3).Gap(2).Radius(14).Background(m.theme.Card).Children(func() {
				for _, mode := range []string{"编辑", "分屏", "预览"} {
					button := ui.Button(c, mode).Ghost().Height(26).FontSize(12)
					if t.htmlMode == mode {
						button.Secondary()
					}
					if button.Clicked() {
						c.After(func() {
							t.htmlMode = mode
							t.source.focus = mode != "预览"
							if mode != "编辑" && (t.html.view == nil || t.html.source != t.source.text.String() || t.html.path != t.doc.path) {
								m.refreshHTML(t)
							}
						})
					}
				}
			})
			label := "刷新预览"
			if t.html.view != nil && t.html.source != t.source.text.String() {
				label = "刷新预览（有修改）"
			}
			if ui.Button(c, label).Secondary().Height(28).FontSize(12).Disabled(!m.webReady || m.confirm || m.showError).Clicked() {
				c.After(func() { m.refreshHTML(t) })
			}
			label = "在浏览器打开"
			if t.doc.path == "" || t.modified() {
				label = "保存并在浏览器打开"
			}
			if ui.Button(c, label).Icon(browserIcon).Secondary().Height(28).FontSize(12).Disabled(m.browser != nil || m.confirm || m.showError).Clicked() {
				c.After(func() { m.openHTML(t) })
			}
		}
		if t.format() == "Markdown" {
			ui.Row(c).Pad(3).Gap(2).Radius(14).Background(m.theme.Card).Children(func() {
				for _, mode := range []string{"编辑", "分屏", "预览"} {
					button := ui.Button(c, mode).Ghost().Height(26).FontSize(12)
					if t.markdownMode == mode {
						button.Secondary()
					}
					if button.Clicked() {
						c.After(func() { t.markdownMode = mode; t.source.focus = mode != "预览" })
					}
				}
			})
		}
		if t.format() == "JSON" || t.format() == "Markdown" {
			button := ui.Button(c, "同步滚动").Ghost().Height(28).FontSize(12).Tooltip("左右面板同步滚动；点击切换")
			if t.scroll.enabled {
				button.Secondary()
			}
			if button.Clicked() {
				t.scroll.enabled = !t.scroll.enabled
				t.scroll.ready = false
			}
		}
	})
	ui.Slot(c).FillWidth().Height(1).Background(m.theme.Border)
}

func (m *editorApp) documentView(c ui.C, t *editorTab, blocked bool) {
	t.source.view.Snapshot.Language = t.syntaxLanguage()
	t.source.syntaxDisabled = t.languageOverride == "纯文本"
	if t.format() != "JSON" {
		t.source.view.Snapshot.MarkLayers = nil
	}
	ui.Column(c.KeyInt(t.id)).FillWidth().Grow(1).MinHeight(0).Children(func() {
		switch t.format() {
		case "XLSX", "DOCX":
			m.officeView(c, t, blocked)
		case "HTML":
			m.htmlView(c, t, blocked)
		case "图片":
			m.imageView(c, t)
		case "PDF", "XMind":
			m.documentPreviewView(c, t, blocked)
		case "JSON":
			m.jsonToolbar(c, t, blocked)
			m.jsonView(c, t, blocked)
		case "Markdown":
			t.updateOutline()
			ui.Row(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
				if t.markdownMode != "预览" {
					column := ui.Column(c).FillHeight().MinWidth(0)
					if t.markdownMode == "分屏" {
						column.Width(t.split.Pos)
					} else {
						column.Grow(1)
					}
					column.Children(func() {
						ui.Text(c, "源文件").Muted().FontSize(12).Height(36).PadX(14).PadY(9)
						t.source.declare(c, "代码编辑区", !blocked).FillWidth().Grow(1)
					})
				}
				if t.markdownMode == "分屏" {
					ui.Splitter(c, &t.split).Mins(180, 180).SplitterThickness(1)
				}
				if t.markdownMode != "编辑" {
					m.markdownPreview(c, t)
				}
			})
		default:
			t.source.view.Snapshot.Placeholder = ""
			if t.doc.path != "" {
				t.source.view.Snapshot.Placeholder = "文件为空，输入开始编辑"
			}
			ui.Stack(c).FillWidth().Grow(1).MinHeight(0).Clip().Children(func() {
				t.source.declare(c, "代码编辑区", !blocked).Fill()
				_, composing := t.source.text.Composition()
				if t.doc.path == "" && t.source.text.RuneCount() == 0 && !composing {
					m.openingPage(c, blocked)
				}
			})
		}
	})
}

func (m *editorApp) statusBar(c ui.C, t *editorTab, blocked bool) {
	if t.office != nil {
		status := m.status
		if t.modified() {
			status = "未保存"
		}
		if t.office.errorText != "" {
			status = t.office.errorText
		}
		ui.Row(c).FillWidth().Height(28).PadX(14).Gap(18).Items(ui.Center).Background(m.theme.Card).Children(func() {
			ui.Text(c, status).Muted().FontSize(12).Grow(1).MinWidth(0).Ellipsis()
			ui.Text(c, "本地编辑 · "+t.format()).Muted().FontSize(12)
		})
		return
	}
	if t.preview != nil {
		ui.Row(c).FillWidth().Height(28).PadX(14).Gap(18).Items(ui.Center).Background(m.theme.Card).Children(func() {
			ui.Text(c, m.status).Muted().FontSize(12).Grow(1).MinWidth(0).Ellipsis()
			ui.Text(c, t.preview.kind+" · 只读预览").Muted().FontSize(12)
		})
		return
	}
	if t.picture != nil {
		m.imageStatus(c, t)
		return
	}
	value := t.source.text.String()
	caret := t.source.text.ByteOffset(t.source.text.Caret())
	line := strings.Count(value[:caret], "\n") + 1
	column := utf8.RuneCountInString(value[strings.LastIndex(value[:caret], "\n")+1:caret]) + 1
	encoding := "UTF-8"
	if t.doc.bom {
		encoding += " BOM"
	}
	status, color := m.status, m.theme.MutedForeground
	if t.modified() {
		status, color = "未保存", m.theme.Ring
	}
	if _, composing := t.source.text.Composition(); composing {
		status, color = "正在输入", m.theme.Ring
	}
	ui.Slot(c).FillWidth().Height(1).Background(m.theme.Border)
	ui.Row(c).FillWidth().Height(28).PadX(14).Gap(18).Items(ui.Center).Background(m.theme.Card).Children(func() {
		ui.Text(c, "●").FontSize(7).Color(color)
		ui.Text(c, status).Color(color).FontSize(12).Grow(1).MinWidth(0).Ellipsis()
		ui.Text(c, fmt.Sprintf("%d 个文件", len(m.tabs))).Muted().FontSize(12)
		ui.Text(c, fmt.Sprintf("行 %d，列 %d", line, column)).Muted().FontSize(12)
		ui.Text(c, encoding).Muted().FontSize(12)
		ui.Text(c, t.doc.eol).Muted().FontSize(12)
		m.languagePicker(c, t, blocked)
	})
}

func (m *editorApp) languagePicker(c ui.C, t *editorTab, blocked bool) {
	if blocked {
		m.languageMenu = false
	}
	button := ui.Button(c, t.syntaxLanguage()).Icon(chevronDownIcon).Ghost().Height(24).FontSize(12).PadX(7).Tooltip("切换语法高亮").Disabled(blocked)
	if button.Clicked() {
		m.languageMenu = !m.languageMenu
	}
	ui.Menu(c, button, &m.languageMenu, func(menu *ui.MenuItems) {
		menu.Above()
		menu.WidthRange(160, 190)
		menu.RowHeight(28)
		menu.Label("语法高亮")
		if menu.Item("自动识别").Checked(t.languageOverride == "").Clicked() {
			c.After(func() {
				t.languageOverride = ""
				m.focusActiveSource()
			})
		}
		menu.Separator()
		addLanguage := func(items *ui.MenuItems, language string) {
			if items.Item(language).Checked(t.languageOverride == language).Clicked() {
				c.After(func() {
					t.languageOverride = language
					m.focusActiveSource()
				})
			}
		}
		for _, language := range syntaxLanguages {
			addLanguage(menu, language)
		}
		menu.Separator()
		menu.Sub("代码与脚本", func(items *ui.MenuItems) {
			for _, syntax := range codeSyntaxes {
				addLanguage(items, syntax.name)
			}
		})
		menu.Sub("数据与日志", func(items *ui.MenuItems) {
			for _, language := range dataSyntaxLanguages {
				addLanguage(items, language)
			}
		})
	})
}

func (m *editorApp) dialogs(c ui.C) {
	description := ""
	if m.confirmTab != nil {
		description = m.confirmTab.title() + " 有未保存的修改。"
	}
	ui.ModalWith(c, &m.confirm, "保存当前修改？", ui.ModalSpec{Description: description, Width: 480, NoClose: true}, func() {
		ui.Checkbox(c.Part("save-confirmation-choice"), &m.dontAskToSave, "不再询问").Height(28)
	}, func() {
		if ui.Button(c, "取消").Outline().Clicked() {
			m.clearSaveConfirmation()
			m.focusActiveSource()
		}
		if ui.Button(c, "放弃修改").Outline().Clicked() {
			c.After(m.finishSaveConfirmation)
		}
		save := ui.Button(c, "保存并继续")
		if m.confirmFocus {
			save.AutoFocus()
			m.confirmFocus = false
		}
		if save.Clicked() && m.confirmTab != nil {
			tab := m.confirmTab
			c.After(func() {
				m.saveBeforeClosing(tab, func() {
					if m.confirmTab == tab {
						m.finishSaveConfirmation()
					}
				})
			})
		}
	})
	ui.ModalWith(c, &m.showError, "操作未完成", ui.ModalSpec{Description: m.errorText, Width: 640, NoClose: true}, nil, func() {
		if ui.Button(c, "确定").Clicked() {
			m.showError = false
			m.focusActiveSource()
		}
	})
}
