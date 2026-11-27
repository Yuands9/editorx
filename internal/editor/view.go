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
const toolRailWidth = 44

func skipButtonTabStop(button ui.Box, id gfx.ID) {
	button.DrawOver(func(d *ui.DrawContext) {
		hits := d.Canvas.Hits()
		for i := len(hits) - 1; i >= 0; i-- {
			if hits[i].ID == id && hits[i].Role == gfx.RoleButton && !hits[i].IsLabel {
				hits[i].SkipTab = true
				return
			}
		}
	})
}

func (m *editorApp) workspacePanel(c ui.C) ui.Box {
	return ui.Column(c).FillHeight().MinWidth(0).Radius(m.theme.Metrics.RadiusSurface).
		Background(m.theme.Background).Clip()
}

func (m *editorApp) page(c ui.C) {
	m.observeNavigation()
	m.updateProjectWatches(c.Part("project-watches"))
	m.updateProjectDirectories(c.Part("project-directories"))
	m.updateTextIO(c.Part("document-io"))
	m.updateFileWatches(c.Part("file-watch"))
	m.checkMacroRecording()
	m.backupProgress(c.Part("backup"))
	m.updateProgress()
	m.browserUpdate(c)
	m.aiUpdate(c)
	m.updateSelectionTranslation(c.Part("selection-translation"))
	m.updateTranslationDictionary(c.Part("dictionary-download"))
	m.updateOfflineTranslation(c.Part("offline-translation"))
	m.updateProjectGit(c)
	m.updateLocalShell()
	m.updateLinkedFile(c.Part("link-download"))
	m.updateImageOCR(c.Part("image-ocr-job"))
	m.updateImageAI(c.Part("image-ai-job"))
	var afterLargeSaves []func()
	for _, tab := range m.tabs {
		if tab.projectPreview && (tab.modified() || tab.pinned || tab.group != nil || tab.hasResultDraft()) {
			tab.projectPreview = false
		}
		if tab == m.active && tab.format() == "JSON" {
			tab.updateResult()
		}
		tab.updateJSONPreview(c.Part("json-preview").KeyInt(tab.id))
		jobs := c.Part("large-file").KeyInt(tab.id)
		if after := m.updateLargeTextEdit(jobs, tab); after != nil {
			afterLargeSaves = append(afterLargeSaves, after)
		}
		m.updateLargeFindAll(jobs.Part("find-all"), tab)
		m.updateDocumentTranslation(c.Part("document-translation-job").KeyInt(tab.id), tab)
	}
	for _, after := range afterLargeSaves {
		if m.post != nil {
			m.post(after)
		} else {
			after()
		}
	}
	m.updateSearchBatch(c.Part("search-batch"))
	m.updateHistoryWindow(c.Part("history-window"))
	blocked := m.interactionBlocked()
	if !blocked && m.appSettings.shortcutEdit.action == shortcutNone && m.shortcutPressed(c, shortcutOpenSettings) {
		c.After(m.toggleApplicationSettings)
	}
	ui.Row(c.Part("workspace")).Fill().Background(m.theme.Card).Children(func() {
		if m.sidebarPrefs.Position == "left" {
			m.toolRail(c.Part("tool-rail"), blocked)
		}
		if m.appSettings.open {
			m.settingsPage(c.Part("settings"))
		} else {
			m.editorPage(c.Part("editor"), blocked)
		}
		if m.sidebarPrefs.Position != "left" {
			m.toolRail(c.Part("tool-rail"), blocked)
		}
	})
	if !m.appSettings.open {
		if m.active != nil && !blocked {
			m.searchDialog(c, m.active)
		}
		m.historyDialog(c)
		m.renameDialog(c)
		m.gotoLineDialog(c.Part("goto-line"))
		m.aiConversationDialog(c)
		m.aiReviewDialog(c)
	}
	m.backupSelectionDialog(c.Part("backup-selection"))
	m.aiDiscardDraftDialog(c.Part("ai-discard-draft"))
	m.updateDialog(c.Part("update-dialog"))
	m.linkedFileDialog(c.Part("link-dialog"))
	m.imageOCRView(c.Part("image-ocr-dialog"))
	m.imageAIView(c.Part("image-ai-dialog"))
	m.showTemplateDialog(c.Part("template-dialog"))
	m.showProjectFolderDialog(c.Part("project-folder-dialog"))
	m.showProjectRenameDialog(c.Part("project-rename-dialog"))
	m.showProjectDeleteDialog(c.Part("project-delete-dialog"))
	m.gitBranchDialog(c.Part("git-branch-dialog"))
	m.gitStashDialog(c.Part("git-stash-dialog"))
	m.gitReviewDialog(c.Part("git-review-dialog"))
	m.passwordVaultModal(c.Part("password-vault-dialog"))
	m.fileFormatDialog(c.Part("file-format-dialog"))
	m.exitConfirmationDialog(c.Part("exit-confirmation"))
	m.navigationPaletteDialog(c.Part("navigation-palette"))
	m.directorySearchDialog(c.Part("directory-search"))
	m.textCompareDialog(c.Part("compare-picker"))
	m.workflowDialogView(c.Part("workflow"))
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
		if ui.Shortcut(c, 0, "escape") {
			if t != nil && t.search.open {
				c.After(t.search.close)
			}
			if m.vaultActive() {
				c.After(func() {
					if v := m.passwordVault; v != nil {
						v.revealed = false
						v.password.Secure, v.confirmation.Secure = true, true
					}
				})
			}
		}
	}

	page := ui.Column(c).Grow(1).MinWidth(0).FillHeight()
	if t != nil {
		page.DrawOver(func(d *ui.DrawContext) { t.syncViews(d.Canvas) })
	}
	page.Children(func() {
		upper := ui.Row(c).FillWidth().MinHeight(0).PadY(workspaceInset).PadLeft(workspaceInset)
		if m.tools.terminalOpen {
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
				split := m.activeAssetSplit()
				if m.ai.open {
					split = &m.aiEditorSplit
				}
				left := m.sidebarPrefs.Position == "left"
				if split != nil && left {
					ui.Column(c.Part("tool-panel")).Width(split.Pos).Shrink(0).FillHeight().MinWidth(0).Children(func() { m.sideToolPanel(c, blocked) })
					m.toolPanelDivider(c.Part("tool-split"), split, true)
				}
				workbench := ui.Row(c.Part("workbench")).MinWidth(0).FillHeight().PadRight(workspaceInset).Clip()
				if split != nil && !left {
					workbench.Width(split.Pos).Shrink(0).PadRight(0)
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
						m.fileChangeBar(c.Part("disk-change"), t, blocked)
						m.macroRecordingBar(c.Part("macro-recording"), t)
						m.documentHeader(c, t, blocked)
						m.searchView(c, t, blocked)
						m.documentPanes(c.Part("document-panes"), t, blocked)
					})
				})
				if split != nil && !left {
					m.toolPanelDivider(c.Part("tool-split"), split, false)
					ui.Column(c.Part("tool-panel")).Grow(1).MinWidth(0).FillHeight().Children(func() { m.sideToolPanel(c, blocked) })
				}
			})
		})
		if m.tools.terminalOpen {
			ui.Splitter(c.Part("tools-split"), &m.tools.split).Mins(150, 150).SplitterThickness(7).Label(uiText("调整底部面板高度"))
			m.terminalPanel(c.Part("terminal"), blocked)
		}
		if !m.tools.terminalOpen {
			m.directoryResultsPanel(c.Part("directory-results"), blocked)
		}
		if t != nil {
			m.statusBar(c, t, blocked)
		}
	})
}

func (m *editorApp) sideToolPanel(c ui.C, blocked bool) {
	if m.vaultActive() {
		m.passwordVaultPage(c.Part("vault"), blocked)
	} else if m.tools.gitOpen {
		m.gitSidebar(c.Part("git"), blocked)
	} else if m.ai.open {
		m.aiPanel(c.Part("assistant"), blocked)
	} else if m.translation != nil {
		m.translationPanel(c.Part("translation"), blocked)
	} else if m.templatePanel != nil {
		m.templatesPanel(c.Part("templates"), blocked)
	}
}

func (m *editorApp) toolPanelDivider(c ui.C, split *ui.SplitterState, left bool) {
	divider := ui.Splitter(c, split).SplitterThickness(workspaceInset).Label(uiText("调整侧栏宽度"))
	panelMin := float32(300)
	if m.ai.open && c.Bounds().Width >= 1040 {
		panelMin = 350
	}
	if left {
		divider.Mins(panelMin, 230)
	} else {
		divider.Mins(230, panelMin)
	}
	if m.ai.open {
		divider.SplitterThickness(12).Label(uiText("调整编辑区与 AI 助手宽度")).DrawOver(func(d *ui.DrawContext) { m.drawAIResizeGrip(d, split, true) })
	}
}

func (m *editorApp) documentPanes(c ui.C, t *editorTab, blocked bool) {
	ui.Row(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
		if t.format() == "Markdown" && m.showOutline && !t.showingDocumentTranslation() && !m.ai.open && m.activeAssetSplit() == nil {
			ui.Column(c.Part("outline")).Width(m.outlineSplit.Pos).Shrink(0).FillHeight().Clip().
				Children(func() { m.outlinePanel(c, t) })
			ui.Splitter(c, &m.outlineSplit).Mins(160, 420).SplitterThickness(workspaceInset).Label(uiText("调整大纲宽度"))
		}
		ui.Column(c.Part("document-content")).Grow(1).MinWidth(0).FillHeight().Children(func() {
			body := ui.Column(c.Part("editor-body")).FillWidth().MinHeight(0)
			if m.results.open {
				body.Height(m.results.split.Pos)
			} else {
				body.Grow(1)
			}
			body.Children(func() { m.splitDocumentView(c, t, blocked) })
			if m.results.open {
				ui.Splitter(c, &m.results.split).Mins(120, 100).SplitterThickness(6)
				m.searchResultsView(c)
			}
		})
	})
}

func (m *editorApp) toolRail(c ui.C, blocked bool) {
	preferences := m.sidebarPrefs
	if preferences.HideSave && preferences.HideSearch && preferences.HideWrap && preferences.HideAI &&
		preferences.HideGit && preferences.HideTerminal && preferences.HideTheme && preferences.HideSettings &&
		!preferences.PasswordVault && !preferences.Templates && !m.translationEnabled && !m.editorUpdates().available() {
		return
	}
	ui.Column(c).Width(toolRailWidth).Shrink(0).FillHeight().PadX(6).PadY(workspaceInset + 4).Gap(6).Children(func() {
		t := m.active
		vaultActive := m.vaultActive()
		if !preferences.HideSave {
			save := ui.Button(c, m.menuText("保存文件")).Icon(saveIcon).IconOnly().Ghost().Width(32).Height(34).Radius(11).Disabled(t == nil || t.readOnlyPreview() || blocked).Tooltip(m.shortcutHint(uiText("保存文件"), shortcutSave))
			if t != nil && t.modified() {
				save.Secondary().ButtonRecipe(m.pinButtonRecipe)
			}
			if save.Clicked() {
				c.After(func() {
					if m.leavePasswordVault() {
						m.focusActiveSource()
					}
					m.saveTab(t, false)
				})
			}
		}
		if !preferences.HideSearch {
			searchHint := m.shortcutHint(uiText("查找"), shortcutFind) + " / " + m.shortcutHint(uiText("替换"), shortcutReplace)
			if t != nil && t.office != nil {
				searchHint = m.menuText("Office 文档请使用文档内的查找功能")
			}
			if ui.Button(c, m.menuText("查找内容")).Icon(searchIcon).IconOnly().Ghost().Width(32).Height(34).Radius(11).Disabled(t == nil || !t.canFind() || blocked).Tooltip(searchHint).Clicked() {
				c.After(func() {
					m.closeSettings()
					m.leavePasswordVault()
					t.openSearch()
				})
			}
		}
		if !preferences.HideWrap {
			wrap := ui.Button(c, m.menuText("自动折行")).Icon(wrapIcon).IconOnly().Ghost().Width(32).Height(34).Disabled(t == nil || t.readOnlyPreview() || t.office != nil || blocked).Tooltip(m.shortcutHint(uiText("自动折行"), shortcutWrap))
			if t != nil && !t.readOnlyPreview() && t.source.view.Snapshot.Wrap {
				wrap.Secondary()
			}
			if wrap.Clicked() {
				c.After(func() {
					if m.leavePasswordVault() {
						m.focusActiveSource()
					}
					t.toggleWrap()
				})
			}
		}
		if !preferences.HideAI {
			label := m.menuText("AI 助手")
			assistant := ui.Button(c, label).Icon(aiIcon).IconOnly().Ghost().Width(32).Height(34).Tooltip(label).Disabled(blocked)
			if !vaultActive && (m.ai.open || m.ai.hasPendingRequest()) {
				assistant.Secondary()
			}
			if assistant.Clicked() {
				c.After(func() { m.closeSettings(); m.toggleAIPanel() })
			}
		}
		for _, tool := range []struct {
			name string
			icon gfx.Icon
		}{{"Git", gitBranchIcon}, {"终端", shellIcon}} {
			if tool.name == "Git" && preferences.HideGit || tool.name == "终端" && preferences.HideTerminal {
				continue
			}
			label := m.menuText(tool.name)
			entry := ui.Button(c.Key(tool.name), label).Icon(tool.icon).IconOnly().Ghost().Width(32).Height(34).Tooltip(label).Disabled(blocked)
			if tool.name == "Git" && m.tools.gitOpen || tool.name == "终端" && m.tools.terminalOpen {
				entry.Secondary()
			}
			if entry.Clicked() {
				c.After(func() { m.closeSettings(); m.useTool(tool.name) })
			}
		}
		if m.translationEnabled {
			label := m.menuText("翻译")
			button := ui.Button(c.Part("translation"), label).Icon(translationIcon).IconOnly().Ghost().Size(32, 34).
				Tooltip(label).Disabled(blocked)
			if !vaultActive && m.translation != nil {
				button.Secondary()
			}
			if button.Clicked() {
				c.After(func() { m.closeSettings(); m.toggleTranslation() })
			}
		}
		if preferences.PasswordVault {
			hint := uiText("密码本")
			if vaultActive {
				hint = uiText("关闭密码本")
			}
			button := ui.Button(c.Part("password-vault"), uiText("密码本")).Icon(passwordVaultIcon).IconOnly().Ghost().Size(32, 34).Tooltip(hint).Disabled(blocked)
			if vaultActive {
				button.Secondary()
			}
			if button.Clicked() {
				c.After(m.togglePasswordVault)
			}
		}
		if preferences.Templates {
			label := m.menuText("模板库")
			button := ui.Button(c.Part("templates"), label).Icon(templateIcon).IconOnly().Ghost().Size(32, 34).Tooltip(label).Disabled(blocked)
			if !vaultActive && m.templatePanel != nil {
				button.Secondary()
			}
			if button.Clicked() {
				c.After(func() { m.closeSettings(); m.toggleTemplates() })
			}
		}
		ui.Slot(c).Grow(1)
		m.updateButton(c.Part("update-available"), blocked)
		if !preferences.HideTheme {
			m.themeButton(c.Part("theme-toggle"), blocked)
		}
		if preferences.HideSettings {
			return
		}
		label := "设置"
		if m.appSettings.open {
			label = "关闭设置"
		}
		gear := ui.Button(c.Part("settings-toggle"), m.menuText(label)).Icon(settingsIcon).IconOnly().Ghost().Size(32, 34).
			Disabled(blocked).Tooltip(m.shortcutHint(label, shortcutOpenSettings))
		if m.appSettings.open {
			gear.Secondary()
		}
		if gear.Clicked() {
			c.After(m.toggleApplicationSettings)
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
			if ui.Button(c, m.menuText("展开文件栏")).Icon(leftPanelIcon).IconOnly().Ghost().Width(32).Height(32).Tooltip(m.shortcutHint(uiText("显示左侧主菜单"), shortcutFiles)).Disabled(blocked).Clicked() {
				c.After(m.toggleFiles)
			}
			for _, action := range []struct {
				label string
				icon  gfx.Icon
				run   func()
			}{
				{"打开文件", fileIcon, m.open}, {"添加项目", folderIcon, m.openFolder},
			} {
				label := m.menuText(action.label)
				if ui.Button(c, label).Icon(action.icon).IconOnly().Ghost().Width(30).Height(32).Tooltip(label).Disabled(m.confirm || m.showError).Clicked() {
					c.After(action.run)
				}
			}
		}
		scroll := ui.Scroll(c).Sideways().Scrollbars().Grow(1).MinWidth(0).FillHeight()
		m.tabStrip(c, scroll, blocked)
		if ui.Button(c, m.menuText("新建标签")).Icon(plusIcon).IconOnly().Ghost().Size(30, 30).MarginX(5).
			Shrink(0).Radius(10).Tooltip(m.shortcutHint(uiText("新建标签"), shortcutNewFile)).Disabled(blocked).Clicked() {
			c.After(m.newDocument)
		}
		options := ui.Button(c.Part("new-options"), m.menuText("新建选项")).Icon(chevronDownIcon).IconOnly().Ghost().Size(24, 30).
			Shrink(0).Tooltip(m.menuText("新建格式与模板")).Disabled(blocked)
		if options.Clicked() {
			m.newFileMenu = !m.newFileMenu
		}
		ui.Menu(c, options, &m.newFileMenu, func(menu *ui.MenuItems) { m.newFileOptions(c, menu) })
	})
}

func (m *editorApp) documentHeader(c ui.C, t *editorTab, blocked bool) {
	if m.editorSplit != nil {
		return
	}
	format := t.format()
	showTranslation := t.showingDocumentTranslation()
	if format != "Markdown" && format != "HTML" {
		return
	}
	ui.Row(c).FillWidth().Height(34).PadX(10).Gap(4).Items(ui.Center).Children(func() {
		if format == "Markdown" && !showTranslation && !m.ai.open && m.activeAssetSplit() == nil {
			label := "展开大纲"
			if m.showOutline {
				label = "收起大纲"
			}
			toggle := ui.Button(c.Part("outline-toggle"), label).Icon(leftPanelIcon).Ghost().Height(28).FontSize(12).
				Disabled(blocked || m.ai.open || m.translation != nil || m.templatePanel != nil).Tooltip(m.shortcutHint(label, shortcutOutline))
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
					if t.markdownMode == mode && !showTranslation {
						button.Secondary()
					}
					if button.Clicked() {
						c.After(func() {
							if showTranslation {
								m.closeDocumentTranslation(t)
							}
							t.markdownMode, t.source.focus = mode, mode != "预览"
						})
					}
				}
			})
		}
		if !showTranslation && (format == "JSON" || format == "Markdown") {
			button := ui.Button(c, uiText("同步滚动")).Ghost().Height(28).FontSize(12).Tooltip(uiText("左右面板同步滚动；点击切换"))
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
	if t.largeText == nil {
		m.updateBookmarkMarkers(t)
	}
	if t.largeText != nil {
		ui.Column(c.KeyInt(t.id)).FillWidth().Grow(1).MinHeight(0).Children(func() {
			m.largeTextView(c, t, blocked)
		})
		return
	}
	format := t.format()
	t.source.view.Snapshot.Language = format
	t.source.syntaxDisabled = t.languageOverride == "纯文本"
	if format != "JSON" {
		t.source.view.Snapshot.MarkLayers = nil
	}
	ui.Column(c.KeyInt(t.id)).FillWidth().Grow(1).MinHeight(0).Children(func() {
		if t.showingDocumentTranslation() {
			m.documentTranslationView(c.Part("translation"), t, blocked)
			return
		}
		switch format {
		case "XLSX", "DOCX":
			m.officeView(c, t, blocked)
		case "HTML":
			m.htmlView(c, t, blocked)
		case "图片":
			m.imageView(c, t, blocked)
		case "PDF", "XMind", "Word":
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
						ui.Text(c, uiText("源文件")).Muted().FontSize(12).Height(36).PadX(14).PadY(9)
						m.sourceEditor(c, t, blocked).FillWidth().Grow(1)
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
			t.source.placeholder = ""
			if t.doc.path != "" {
				t.source.placeholder = "文件为空，输入开始编辑"
			}
			ui.Stack(c).FillWidth().Grow(1).MinHeight(0).Clip().Children(func() {
				m.sourceEditor(c, t, blocked).Fill()
				if t.doc.path == "" && t.source.text.RuneCount() == 0 && !t.source.text.CompositionActive() {
					m.openingPage(c.Part("blank-home"), blocked)
				}
			})
		}
	})
}

func (m *editorApp) statusBar(c ui.C, t *editorTab, blocked bool) {
	if t.largeText != nil && t.largeText.editing != nil {
		m.largeEditStatusBar(c, t, blocked)
		return
	}
	if t.largeText != nil {
		ui.Row(c).FillWidth().Height(28).PadX(14).Gap(18).Items(ui.Center).Background(m.theme.Card).Children(func() {
			ui.Text(c, m.status).Muted().FontSize(12).Grow(1).MinWidth(0).Ellipsis()
			state := t.largeText
			if int(state.index) < len(state.lines.breaks)-1 {
				value := t.source.text.String()
				offset := t.source.text.ByteOffset(t.source.text.Caret())
				line := state.lines.breaks[state.index] + strings.Count(value[:offset], "\n") + 1
				total := state.lines.breaks[len(state.lines.breaks)-1] + 1
				ui.Text(c, fmt.Sprintf(uiText("行 %d · 共 %d 行"), line, total)).Muted().FontSize(12)
			}
			ui.Text(c, fmt.Sprintf(uiText("只读 · %.1f MiB"), float64(state.size)/(1<<20))).Muted().FontSize(12)
			m.encodingPicker(c, t, blocked)
		})
		return
	}
	if t.office != nil {
		status := m.status
		if t.modified() {
			status = uiText("未保存")
		}
		if t.office.errorText != "" {
			status = t.office.errorText
		}
		ui.Row(c).FillWidth().Height(28).PadX(14).Gap(18).Items(ui.Center).Background(m.theme.Card).Children(func() {
			ui.Text(c, status).Muted().FontSize(12).Grow(1).MinWidth(0).Ellipsis()
			ui.Text(c, uiText("本地编辑 · ")+t.format()).Muted().FontSize(12)
		})
		return
	}
	if t.preview != nil {
		ui.Row(c).FillWidth().Height(28).PadX(14).Gap(18).Items(ui.Center).Background(m.theme.Card).Children(func() {
			ui.Text(c, m.status).Muted().FontSize(12).Grow(1).MinWidth(0).Ellipsis()
			ui.Text(c, t.preview.kind+uiText(" · 只读预览")).Muted().FontSize(12)
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

	status, color := m.status, m.theme.MutedForeground
	if t.modified() {
		status, color = uiText("未保存"), m.theme.Ring
	}
	if _, composing := t.source.text.Composition(); composing {
		status, color = uiText("正在输入"), m.theme.Ring
	}
	ui.Slot(c).FillWidth().Height(1).Background(m.theme.Border)
	ui.Row(c).FillWidth().Height(28).PadX(14).Gap(18).Items(ui.Center).Background(m.theme.Card).Children(func() {
		ui.Text(c, "●").FontSize(7).Color(color)
		ui.Text(c, status).Color(color).FontSize(12).Grow(1).MinWidth(0).Ellipsis()
		files := fmt.Sprintf(uiText("%d 个文件"), len(m.tabs))
		if len(m.tabs) == 1 && englishInterface.Load() {
			files = "1 file"
		}
		ui.Text(c, files).Muted().FontSize(12)
		ui.Text(c, fmt.Sprintf(uiText("行 %d，列 %d"), line, column)).Muted().FontSize(12)
		m.encodingPicker(c, t, blocked)
		m.eolPicker(c, t, blocked)
		m.languagePicker(c, t, blocked)
	})
}

func (m *editorApp) languagePicker(c ui.C, t *editorTab, blocked bool) {
	if blocked {
		m.languageMenu = false
	}
	button := ui.Button(c, m.menuText(t.format())).Icon(chevronDownIcon).Ghost().Height(24).FontSize(12).PadX(7).Tooltip(m.menuText("切换文件类型")).Disabled(blocked)
	if button.Clicked() {
		m.languageMenu = !m.languageMenu
	}
	ui.Menu(c, button, &m.languageMenu, func(menu *ui.MenuItems) {
		menu.Above()
		menu.WidthRange(160, 190)
		menu.RowHeight(28)
		menu.Label(m.menuText("文件类型"))
		if menu.Item(m.menuText("自动识别")).Checked(t.languageOverride == "").Clicked() {
			c.After(func() { m.selectLanguage(t, "") })
		}
		menu.Separator()
		addLanguage := func(items *ui.MenuItems, language string) {
			if items.Item(m.menuText(language)).Checked(t.languageOverride == language).Clicked() {
				c.After(func() { m.selectLanguage(t, language) })
			}
		}
		for _, language := range syntaxLanguages {
			addLanguage(menu, language)
		}
		menu.Separator()
		menu.Sub(m.menuText("代码与脚本"), func(items *ui.MenuItems) {
			for _, syntax := range codeSyntaxes {
				addLanguage(items, syntax.name)
			}
		})
		menu.Sub(m.menuText("数据与日志"), func(items *ui.MenuItems) {
			for _, language := range dataSyntaxLanguages {
				addLanguage(items, language)
			}
		})
	})
}

func (m *editorApp) dialogs(c ui.C) {
	description := ""
	if m.confirmTab != nil {
		description = m.confirmTab.title() + uiText(" 有未保存的修改。")
	}
	dismissed := ui.ModalWith(c, &m.confirm, uiText("保存当前修改？"), ui.ModalSpec{Description: description, Width: 480, NoClose: true}, func() {
		ui.Checkbox(c.Part("save-confirmation-choice"), &m.dontAskToSave, uiText("不再询问")).Height(28)
	}, func() {
		if ui.Button(c, uiText("取消")).Outline().Clicked() {
			c.After(m.cancelSaveConfirmation)
		}
		var state *largeTextView
		if m.confirmTab != nil {
			state = m.confirmTab.largeText
		}
		saving := state != nil && state.saving != nil
		if ui.Button(c, uiText("放弃修改")).Outline().Disabled(saving).Clicked() {
			c.After(m.finishSaveConfirmation)
		}
		save := ui.Button(c, uiText("保存并继续")).Disabled(saving || state != nil && state.editing == nil)
		if m.confirmFocus {
			save.AutoFocus()
			m.confirmFocus = false
		}
		if save.Clicked() && m.confirmTab != nil {
			tab := m.confirmTab
			c.After(func() {
				m.saveBeforeClosing(tab, func(saved bool) {
					if saved && m.confirmTab == tab {
						m.finishSaveConfirmation()
					}
				})
			})
		}
	})
	if dismissed {
		c.After(m.cancelSaveConfirmation)
	}
	ui.ModalWith(c, &m.showError, uiText("操作未完成"), ui.ModalSpec{Description: m.errorText, Width: 640, NoClose: true}, nil, func() {
		if ui.Button(c, uiText("确定")).Clicked() {
			m.showError = false
			m.focusActiveSource()
		}
	})
}
