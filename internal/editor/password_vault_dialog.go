package editor

import (
	"fmt"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

func (m *editorApp) passwordVaultModal(c ui.C) {
	v := m.passwordVault
	if !m.vaultActive() || v.dialog == nil {
		return
	}
	d := v.dialog
	if d.mode == passwordArchiveResetMode || d.mode == passwordArchiveRestoreMode {
		m.passwordArchiveModal(c.Part("archive"), v, d)
		return
	}
	if v.store == nil {
		return
	}
	if d.confirmDelete || d.mode == passwordDeleteEntryMode || d.mode == passwordDeleteGroupMode {
		m.passwordDeleteConfirmation(c.Part("delete"), v, d)
		return
	}
	title, action, width := "新建条目", "保存条目", float32(570)
	switch d.mode {
	case "entry":
		if d.id != 0 {
			title = "编辑条目"
		}

	case "group":
		title, action, width = "新建分组", "保存分组", 420
		if d.id != 0 {
			title = "重命名分组"
		} else if d.parent != 0 {
			title = "新建子分组"
		}
	case "settings":
		title, action, width = "密码本设置", "完成", 490
	case passwordBackupExportMode:
		title, action, width = "设置备份密码", "导出备份…", 445
	case "import":
		title, action, width = "从文本识别", "保存条目", 810
	}
	closeDialog := func() {
		v.dialog = d.returnToSettings
		m.vaultTouch()
	}
	submitted := false
	dismissed := ui.ModalWith(c, &d.open, title, ui.ModalSpec{Width: width}, func() {
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			switch d.mode {
			case "entry":
				submitted = m.passwordEntryForm(c.Part("entry-form"), v, d)
			case "group":
				if d.parent > 0 {
					ui.Wrap(c, "上级分组 · "+v.store.data.groupPath(d.parent)).FillWidth().Muted().FontSize(12)
				}
				name := ui.TextField(c.Part("group-name"), &d.name, "分组名称", "例如个人账号").AutoFocus().FillWidth().Height(35)
				submitted = name.Submitted()
				if name.Changed() {
					m.vaultTouch()
				}
			case "settings":
				submitted = m.passwordSettingsForm(c.Part("vault-settings"), v, d)
				if len(v.archives) > 0 && ui.Button(c, "恢复旧密码本…").Ghost().FontSize(12).Clicked() {
					c.After(m.openPasswordArchiveRestore)
				}
			case passwordBackupExportMode:
				ui.Wrap(c, fmt.Sprintf("恢复备份时需要此密码，至少 %d 个字符。", passwordVaultMinMasterLength)).FillWidth().Muted().FontSize(12)
				submitted = m.passwordPair(c.Part("backup-password"), &d.master, &d.masterConfirmation, "备份密码", false)
			case "import":
				m.passwordImportForm(c.Part("import-form"), v, d)
			}
			if d.problem != "" {
				ui.Wrap(c, d.problem).FillWidth().Color(m.theme.Danger).FontSize(12)
			}
		})
	}, func() {
		if d.mode == "entry" && d.id != 0 && ui.Button(c, "删除条目").Ghost().Clicked() {
			c.After(func() { d.confirmDelete = true; d.secret.Secure = true; m.vaultTouch() })
		}
		if d.mode == "settings" && ui.Button(c, "导出加密备份…").Icon(exportIcon).Ghost().Clicked() {
			c.After(m.requestPasswordVaultExport)
		}
		if ui.Button(c, "取消").Ghost().Clicked() {
			c.After(closeDialog)
		}
		count := 0
		invalid := false
		if d.mode == "import" {
			for i := range d.importRows {
				row := &d.importRows[i]
				if row.included {
					count++
					invalid = invalid || validatePasswordEntry(row.entry(d.group)) != nil
				}
			}
			if count > 0 {
				action = fmt.Sprintf("保存 %d 条", count)
			}
		}
		if ui.Button(c, action).Disabled(d.mode == "import" && (count == 0 || invalid)).Clicked() || submitted {
			c.After(func() {
				switch d.mode {
				case "entry":
					m.savePasswordEntry()
				case "group":
					m.savePasswordGroup()
				case "settings":
					m.savePasswordVaultSettings()
				case passwordBackupExportMode:
					m.exportPasswordVault()
				case "import":
					m.savePasswordImport()
				}
			})
		}
	})
	if dismissed {
		closeDialog()
	}
}

func (m *editorApp) passwordDeleteConfirmation(c ui.C, v *passwordVaultView, d *passwordVaultDialog) {
	data := v.store.data
	groupDelete := d.mode == passwordDeleteGroupMode
	title, message, detail := "删除条目", "", ""
	if groupDelete {
		title = "删除分组"
		message = "删除「" + data.groupPath(d.id) + "」？"
		removed, destination := data.groupRemoval(d.id)
		if len(removed) > 1 {
			detail = fmt.Sprintf("同时删除 %d 个子分组。", len(removed)-1)
		}
		count := 0
		for _, entry := range data.Entries {
			if removed[entry.Group] {
				count++
			}
		}
		if count > 0 {
			detail += fmt.Sprintf("%d 个条目将移到「%s」。", count, data.groupPath(destination))
		}
	} else {
		entry, _ := data.entry(d.id)
		message = "删除「" + entry.Name + "」？"
		detail = data.groupPath(entry.Group)
		if entry.Account != "" {
			detail += " · " + entry.Account
		}
	}
	cancel := func() {
		if d.confirmDelete {
			d.open, d.confirmDelete, d.problem = true, false, ""
		} else {
			v.dialog = nil
		}
		m.vaultTouch()
	}
	if ui.ModalWith(c, &d.open, title, ui.ModalSpec{Width: 420}, func() {
		ui.Column(c).FillWidth().Gap(6).Children(func() {
			ui.Wrap(c, message).FillWidth()
			if detail != "" {
				ui.Wrap(c, detail).FillWidth().Muted().FontSize(12)
			}
			if d.problem != "" {
				ui.Wrap(c, d.problem).FillWidth().Color(m.theme.Danger).FontSize(12)
			}
		})
	}, func() {
		if ui.Button(c, "取消").Ghost().Clicked() {
			c.After(cancel)
		}
		if ui.Button(c, "确认删除").Destructive().Clicked() {
			if groupDelete {
				c.After(m.deletePasswordGroup)
			} else {
				c.After(m.deletePasswordEntry)
			}
		}
	}) {
		cancel()
	}
}

func (m *editorApp) passwordGroupSelect(c ui.C, data passwordVaultData, group *int, label string) {
	options := make([]int, 0, len(data.Groups)+1)
	if data.DefaultGroup == 0 || *group == 0 {
		options = append(options, passwordVaultNoGroup)
	}
	for _, item := range data.Groups {
		options = append(options, item.ID)
	}
	if ui.Select(c, group, options, data.groupPath).Label(label).FillWidth().Height(34).Changed() {
		m.vaultTouch()
	}
}

func (m *editorApp) passwordEntryForm(c ui.C, v *passwordVaultView, d *passwordVaultDialog) bool {
	submitted := false
	ui.Row(c.Part("name-type")).FillWidth().Gap(8).Items(ui.Center).Children(func() {
		name := ui.TextField(c.Part("entry-name"), &d.name, "名称", "条目名称").Grow(1).MinWidth(0).Height(34).AutoFocus()
		if name.Changed() {
			d.problem = ""
			m.vaultTouch()
		}
		submitted = name.Submitted()
		if ui.Select(c.Part("entry-type"), &d.kind, []string{vaultLogin, vaultKey, vaultNote}, passwordKindLabel).Label("类型").Width(135).Height(34).Changed() {
			d.secret.Secure = true
			m.vaultTouch()
		}
	})
	ui.Row(c.Part("entry-group-row")).FillWidth().Gap(8).Items(ui.Center).Children(func() {
		ui.Text(c, "所属分组").Muted().FontSize(12).Width(65)
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			m.passwordGroupSelect(c.Part("entry-group"), v.store.data, &d.group, "所属分组")
		})
	})
	if d.kind != vaultNote {
		if ui.TextField(c.Part("entry-site"), &d.site, "网站", "网站地址（可选）").FillWidth().Height(34).Changed() {
			m.vaultTouch()
		}
		if d.kind == vaultLogin && ui.TextField(c.Part("entry-account"), &d.account, "用户名", "输入用户名").FillWidth().Height(34).Changed() {
			d.problem = ""
			m.vaultTouch()
		}
		label := "密码"
		if d.kind == vaultKey {
			label = "密钥"
		}
		ui.Row(c.Part("entry-secret")).FillWidth().Gap(5).Items(ui.Center).Children(func() {
			m.passwordSecretInput(c.Part("input"), d, &d.secret, label).Grow(1).Basis(0)
			if d.kind == vaultLogin && ui.Button(c, "生成密码").Ghost().Height(32).Clicked() {
				c.After(func() {
					value, err := generatePassword()
					if err != nil {
						d.problem = "无法生成随机密码"
						return
					}
					d.secret.SetText(value)
					d.problem = ""
					m.vaultTouch()
				})
			}
		})
	}
	if ui.TextField(c.Part("entry-tags"), &d.tags, "标签", "用逗号分隔标签（可选）").FillWidth().Height(34).Changed() {
		m.vaultTouch()
	}
	if ui.TextArea(c.Part("entry-note"), &d.note, "备注", "输入备注").FillWidth().Height(69).Changed() {
		d.problem = ""
		m.vaultTouch()
	}
	return submitted
}

func (m *editorApp) passwordSecretInput(c ui.C, d *passwordVaultDialog, field *ui.Field, label string) ui.Box {
	input := m.passwordInput(c, field, label, "输入"+label, 34, false)
	if input.Changed() {
		d.problem = ""
		m.vaultTouch()
	}
	return input
}

func (m *editorApp) passwordSettingsForm(c ui.C, v *passwordVaultView, d *passwordVaultDialog) bool {
	if settingsSwitchRow(c.Part("master-switch"), "使用主密码", &d.useMaster) {
		d.master, d.masterConfirmation = ui.Field{Secure: true}, ui.Field{Secure: true}
		d.problem = ""
		m.vaultTouch()
	}
	submitted := false
	if d.useMaster && v.windowsProtected {
		ui.Wrap(c, fmt.Sprintf("设置至少 %d 个字符的主密码。", passwordVaultMinMasterLength)).FillWidth().Muted().FontSize(12)
		submitted = m.passwordPair(c.Part("new-master"), &d.master, &d.masterConfirmation, "主密码", false)
	}
	idleSubmitted := m.passwordDurationSetting(c.Part("idle"), d, "闲置自动锁定", "闲置锁定时间", "分钟", &d.idleLock, &d.idleMinutes)
	clipboardSubmitted := m.passwordDurationSetting(c.Part("clipboard"), d, "复制后清理剪贴板", "剪贴板清理时间", "秒", &d.clearClipboard, &d.clipboardSeconds)

	return submitted || idleSubmitted || clipboardSubmitted
}

func (m *editorApp) passwordDurationSetting(c ui.C, d *passwordVaultDialog, label, fieldLabel, unit string, enabled *bool, amount *ui.Field) bool {
	submitted := false
	ui.Row(c).FillWidth().Height(36).PadX(4).Gap(8).Items(ui.Center).Children(func() {
		labelText := ui.Text(c, label).FontSize(13).Grow(1).MinWidth(0).Ellipsis()
		changed := false
		if *enabled {
			field := ui.TextField(c.Part("amount"), amount, fieldLabel, "").Width(88).Height(32)
			ui.Text(c, unit).Width(26).FontSize(12).Muted()
			submitted, changed = field.Submitted(), field.Changed()
		}
		toggle := ui.Switch(c.Part("enabled"), enabled, "").Label(label).Shrink(0)
		labelText.LabelFor(toggle.N().ID)
		if changed || toggle.Changed() {
			d.problem = ""
			m.vaultTouch()
		}
	})
	return submitted
}

func (m *editorApp) passwordImportForm(c ui.C, v *passwordVaultView, d *passwordVaultDialog) {
	ui.Row(c).FillWidth().Gap(13).Children(func() {
		ui.Column(c.Part("source")).Width(330).Shrink(0).Gap(7).Children(func() {
			ui.Text(c, "原文").Bold().FontSize(13)
			if ui.TextArea(c.Part("source-text"), &d.source, "待识别的原文", "粘贴账号密码文本或 Markdown 表格").FillWidth().Height(245).AutoFocus().Changed() {
				m.vaultTouch()
			}
			d.parsePasswordImport()
		})
		ui.Column(c.Part("review")).Grow(1).MinWidth(0).Gap(6).Children(func() {
			ui.Text(c, fmt.Sprintf("识别到 %d 条", len(d.importRows))).Bold().FontSize(13)
			if len(d.importRows) == 0 {
				ui.Wrap(c, "粘贴后自动回填。可识别带账号和密码标签的文本，以及 Markdown 表格。").FillWidth().Muted().FontSize(12)
				return
			}
			ui.Scroll(c.Part("matches")).FillWidth().Height(96).Scrollbars().Children(func() {
				ui.Column(c).FillWidth().Gap(3).Children(func() {
					for index := range d.importRows {
						row := &d.importRows[index]
						ui.Row(c.KeyInt(index)).FillWidth().Items(ui.Center).Gap(5).Children(func() {
							if ui.Checkbox(c.Part("include"), &row.included, fmt.Sprintf("选择第 %d 条", index+1)).FontSize(11).Width(88).Shrink(0).Changed() {
								m.vaultTouch()
							}
							label := row.name.Text()
							if label == "" {
								label = "未命名条目"
							}
							choice := m.passwordListRow(c.Part("select").KeyInt(index), label, gfx.Icon{}, 26, d.id == index, false).
								Grow(1).Basis(0).Tooltip(row.source)
							if choice.Clicked() {
								c.After(func() {
									d.importRows[d.id].secret.Secure = true
									d.id, d.problem = index, ""
									m.vaultTouch()
								})
							}
						})
					}
				})
			})
			row := &d.importRows[d.id]
			before := validatePasswordEntry(row.entry(d.group))
			ui.Row(c.Part("name-and-site")).FillWidth().Gap(7).Children(func() {
				m.passwordImportField(c.Part("import-name"), &row.name, "名称", "条目名称")
				m.passwordImportField(c.Part("import-url"), &row.site, "网站", "网站地址")
			})
			ui.Row(c.Part("account-and-secret")).FillWidth().Gap(7).Children(func() {
				m.passwordImportField(c.Part("import-account"), &row.account, "用户名", "补齐用户名")
				ui.Column(c.Part("secret-column")).Grow(1).Basis(0).MinWidth(0).Gap(3).Children(func() {
					ui.Text(c, "密码").Muted().FontSize(12)
					m.passwordSecretInput(c.Part("import-secret").KeyInt(d.id), d, &row.secret, "密码")
				})
			})
			if issue := validatePasswordEntry(row.entry(d.group)); issue != nil {
				ui.Wrap(c, issue.Error()).FillWidth().Muted().FontSize(12)
			} else if before != nil {
				row.included = true
			}
		})
	})
	ui.Row(c.Part("import-destination")).FillWidth().Gap(8).Items(ui.Center).Children(func() {
		ui.Text(c, "保存到").Muted().FontSize(12).Width(54)
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			m.passwordGroupSelect(c.Part("import-group"), v.store.data, &d.group, "保存到分组")
		})
	})
}

func (m *editorApp) passwordImportField(c ui.C, field *ui.Field, label, placeholder string) {
	ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(3).Children(func() {
		ui.Text(c, label).Muted().FontSize(12)
		if ui.TextField(c.Part("input"), field, label, placeholder).FillWidth().Height(34).Changed() {
			m.vaultTouch()
		}
	})
}
