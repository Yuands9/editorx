package editor

import "github.com/vivaui/viva/ui"

func (m *editorApp) mainMenu(c ui.C, t *editorTab, blocked bool) {
	button := ui.Button(c, "主菜单").Icon(menuIcon).IconOnly().Ghost().Size(32, 32).Tooltip("主菜单").Disabled(blocked)
	if button.Clicked() {
		m.fileMenu = !m.fileMenu
	}
	ui.Menu(c, button, &m.fileMenu, func(menu *ui.MenuItems) {
		menu.WidthRange(250, 300)
		menu.RowHeight(28)
		if m.shortcutMenu(menu.Item("新建").Icon(plusIcon), shortcutNewFile).Clicked() {
			c.After(m.newDocument)
		}
		if m.shortcutMenu(menu.Item("打开…").Icon(fileIcon), shortcutOpenFile).Clicked() {
			c.After(m.open)
		}
		if m.shortcutMenu(menu.Item("快速打开文件…").Icon(searchIcon), shortcutQuickOpen).Clicked() {
			c.After(func() { m.openNavigationPalette(false) })
		}
		menu.Separator()
		if m.shortcutMenu(menu.Item("保存").Icon(saveIcon).Disabled(t == nil || t.readOnlyPreview() || m.vaultActive()), shortcutSave).Clicked() {
			c.After(func() { m.saveTab(t, false) })
		}
		if m.shortcutMenu(menu.Item("全部保存").Icon(blankMenuIcon).Disabled(!m.navigationCommandEnabled(shortcutSaveAll)), shortcutSaveAll).Clicked() {
			c.After(m.saveAllTabs)
		}
		menu.Sub("文件与标签", func(items *ui.MenuItems) {
			items.RowHeight(28)
			items.Sub("新建格式与模板", func(options *ui.MenuItems) { m.newFileOptions(c, options) }).Icon(plusIcon)
			if m.shortcutMenu(items.Item("新建窗口").Icon(blankMenuIcon), shortcutNewWindow).Disabled(m.newWindow == nil).Clicked() {
				c.After(m.newWindow)
			}
			items.Sub("按编码打开", func(options *ui.MenuItems) {
				for _, enc := range textEncodings {
					if options.Item(string(enc)).Icon(blankMenuIcon).Clicked() {
						c.After(func() { m.openWithEncoding(enc) })
					}
				}
			}).Icon(blankMenuIcon)
			if m.shortcutMenu(items.Item("打开链接文件…").Icon(linkIcon), shortcutOpenLink).Clicked() {
				c.After(m.openLinkedFileDialog)
			}
			if m.shortcutMenu(items.Item("添加项目…").Icon(folderIcon), shortcutOpenProject).Clicked() {
				c.After(m.openFolder)
			}
			if m.shortcutMenu(items.Item("最近打开…").Icon(blankMenuIcon).Disabled(m.store == nil), shortcutRecentFiles).Clicked() {
				c.After(func() { m.runEditorShortcut(shortcutRecentFiles) })
			}
			items.Separator()
			if m.shortcutMenu(items.Item("另存为…").Icon(blankMenuIcon).Disabled(t == nil || t.readOnlyPreview() || m.vaultActive()), shortcutSaveAs).Clicked() {
				c.After(func() { m.saveTab(t, true) })
			}
			if m.shortcutMenu(items.Item("关闭当前标签").Icon(closeIcon).Disabled(t == nil && !m.vaultActive()), shortcutCloseTab).Clicked() {
				c.After(func() { m.runEditorShortcut(shortcutCloseTab) })
			}
			if m.shortcutMenu(items.Item("恢复关闭标签").Icon(blankMenuIcon).Disabled(len(m.closedTabs) == 0), shortcutReopenTab).Clicked() {
				c.After(m.reopenClosedTab)
			}
		}).Icon(folderIcon)
		menu.Sub("编辑与查找", func(items *ui.MenuItems) {
			items.RowHeight(28)
			for _, action := range []editorShortcut{shortcutDirectorySearch, shortcutDirectoryReplace, shortcutFind, shortcutReplace, shortcutComplete, shortcutSelectNext, shortcutSelectAllMatches, shortcutSplitSelection, shortcutCaretAbove, shortcutCaretBelow, shortcutGotoLine, shortcutDuplicateLine, shortcutDeleteLine, shortcutWrap} {
				if m.shortcutMenu(items.Item(editorShortcutSpecs[action].label).Icon(blankMenuIcon).Disabled(!m.navigationCommandEnabled(action)), action).Clicked() {
					c.After(func() { m.runEditorShortcut(action) })
				}
			}
			if items.Item("搜索结果").Icon(blankMenuIcon).Disabled(m.results.query == "").Clicked() {
				m.results.open = true
			}
			if t != nil && !m.vaultActive() {
				items.Sub("文本操作", func(options *ui.MenuItems) { m.textActions(c, options, t) }).Icon(blankMenuIcon)
			}
		}).Icon(searchIcon)
		menu.Sub("工具与模板", func(items *ui.MenuItems) {
			items.RowHeight(28)
			if items.Item("密码本").Icon(passwordVaultIcon).Clicked() {
				c.After(m.openPasswordVault)
			}
			if m.vaultActive() && items.Item("关闭密码本").Icon(closeIcon).Clicked() {
				c.After(m.closePasswordVault)
			}
			if items.Item("AI 助手").Icon(aiIcon).Checked(m.ai.open).Clicked() {
				c.After(m.toggleAIPanel)
			}
			if m.translationEnabled && !m.vaultActive() {
				if items.Item("翻译").Icon(translationIcon).Checked(m.translation != nil).Clicked() {
					c.After(m.toggleTranslation)
				}
				if items.Item("翻译选区…").Icon(blankMenuIcon).Disabled(!editableTextSource(t)).Clicked() {
					c.After(func() { m.openTranslation(true) })
				}
			}
			if m.shortcutMenu(items.Item("Git").Icon(gitBranchIcon), shortcutGit).Clicked() {
				c.After(func() { m.useTool("Git") })
			}
			if m.shortcutMenu(items.Item("本地终端").Icon(shellIcon), shortcutTerminal).Clicked() {
				c.After(func() { m.useTool("终端") })
			}
			if t != nil && !m.vaultActive() {
				items.Separator()
				if items.Item("插入模板…").Icon(blankMenuIcon).Disabled(!editableTextSource(t)).Clicked() {
					c.After(func() { m.openTemplatePicker("", true) })
				}
				m.templateSaveActions(c, items, t)
			}
		}).Icon(aiIcon)
		menu.Sub("工作流", func(items *ui.MenuItems) {
			items.RowHeight(28)
			for _, action := range workflowActions {
				if items.Item(action.label).Icon(blankMenuIcon).Disabled(!m.workflowEnabled(action.id)).Clicked() {
					c.After(func() { m.runWorkflow(action.id) })
				}
			}
		}).Icon(historyCompareIcon)
		menu.Separator()
		if m.shortcutMenu(menu.Item("命令面板…").Icon(blankMenuIcon), shortcutCommandPalette).Clicked() {
			c.After(func() { m.openNavigationPalette(true) })
		}
		if m.shortcutMenu(menu.Item("设置").Icon(settingsIcon), shortcutOpenSettings).Clicked() {
			c.After(m.openApplicationSettings)
		}
		if m.desktop != nil && menu.Item("退出 EditorX").Icon(blankMenuIcon).Clicked() {
			c.After(m.desktop.quit)
		}
	})
}
