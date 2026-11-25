package editor

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

var passwordVaultIcon = gfx.Icon{ViewBox: 24, Stroke: 1.7, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(14, 13), gfx.QuadTo(18, 15, 21, 12), gfx.QuadTo(24, 8, 20, 4), gfx.QuadTo(16, 1, 12, 4), gfx.QuadTo(9, 7, 11, 11),
	gfx.LineTo(3, 19), gfx.LineTo(3, 22), gfx.LineTo(6, 22), gfx.LineTo(8, 20), gfx.LineTo(8, 18), gfx.LineTo(10, 18), gfx.LineTo(13, 15), gfx.LineTo(14, 13),
	gfx.MoveTo(17, 7), gfx.LineTo(17.3, 7.3),
}}

var passwordLockIcon = gfx.Icon{ViewBox: 24, Stroke: 1.7, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(5, 10), gfx.LineTo(19, 10), gfx.LineTo(19, 21), gfx.LineTo(5, 21), gfx.ClosePath(),
	gfx.MoveTo(8, 10), gfx.LineTo(8, 7), gfx.QuadTo(8, 3, 12, 3), gfx.QuadTo(16, 3, 16, 7), gfx.LineTo(16, 10), gfx.MoveTo(12, 14), gfx.LineTo(12, 17),
}}

func passwordKindLabel(kind string) string {
	switch kind {
	case vaultKey:
		return "密钥"
	case vaultNote:
		return "安全备注"
	default:
		return "网站登录"
	}
}

func (m *editorApp) passwordVaultPage(c ui.C, blocked bool) {
	v := m.passwordVault
	v.expire(time.Now())
	ui.Column(c).Grow(1).FillHeight().MinWidth(0).Pad(workspaceInset).Children(func() {
		m.workspacePanel(c.Part("vault-workspace")).Grow(1).PadX(4).PadBottom(6).Children(func() {
			m.tabBar(c, blocked)
			if v.store == nil {
				m.passwordGate(c.Part("gate"), v, blocked)
				return
			}
			m.passwordVaultContent(c.Part("contents"), v, blocked)
		})
	})
}

func (m *editorApp) passwordGate(c ui.C, v *passwordVaultView, blocked bool) {
	creating := !v.exists && v.backupSource == ""
	title, hint, submit := "密码本已锁定", "解锁后查看本机密码库。", "解锁"
	if creating {
		title, submit = "创建本机密码本", "创建密码本"
		if v.useMaster {
			hint = fmt.Sprintf("请选择至少 %d 个字符的主密码。忘记主密码将无法解锁。", passwordVaultMinMasterLength)
		} else {
			hint = "由当前 Windows 账户保护，打开时直接进入。"
		}
	} else if v.backupSource != "" {
		title, hint, submit = "恢复加密备份", filepath.Base(v.backupSource), "恢复密码本"
	} else if v.windowsProtected {
		hint, submit = "", "打开密码本"
	}
	ui.Column(c).FillWidth().Grow(1).Center().Pad(14).Children(func() {
		m.assetCard(c.Part("unlock")).Width(420).Gap(11).Children(func() {
			m.assetHeading(c, passwordLockIcon, title, hint)
			if creating {
				enabled := v.useMaster
				if settingsSwitchRow(c.Part("master-switch"), "使用主密码", &enabled) && !blocked {
					c.After(func() {
						v.useMaster, v.problem = enabled, ""
						v.password, v.confirmation = ui.Field{Secure: true}, ui.Field{Secure: true}
					})
				}
			}
			submitted := false
			switch {
			case creating && v.useMaster:
				submitted = m.passwordPair(c.Part("create-master"), &v.password, &v.confirmation, "主密码", blocked)
			case v.backupSource != "":
				submitted = m.passwordInput(c.Part("restore-password"), &v.password, "备份密码", "输入备份密码", 36, blocked).AutoFocus().Submitted()
			case v.exists && !v.windowsProtected:
				submitted = m.passwordInput(c.Part("unlock-password"), &v.password, "主密码", "输入主密码", 36, blocked).AutoFocus().Submitted()
			}
			if v.backupSource == "" && (v.exists && !v.windowsProtected || len(v.archives) > 0) {
				ui.Row(c.Part("vault-recovery")).FillWidth().Gap(6).Children(func() {
					if v.exists && !v.windowsProtected && ui.Button(c, "忘记主密码？创建新密码本").Ghost().
						Height(26).FontSize(12).Disabled(blocked).Clicked() {
						c.After(func() { v.dialog = &passwordVaultDialog{open: true, mode: passwordArchiveResetMode} })
					}
					if len(v.archives) > 0 && ui.Button(c, "恢复旧密码本…").Ghost().
						Height(26).FontSize(12).Disabled(blocked).Clicked() {
						c.After(m.openPasswordArchiveRestore)
					}
				})
			}
			if v.pendingSource != "" {
				ui.Text(c, "打开后继续识别选区").Muted().FontSize(12)
			}
			if v.problem != "" {
				ui.Wrap(c, v.problem).FillWidth().FontSize(12).Color(m.theme.Danger)
			}
			ui.Row(c).FillWidth().Gap(6).Items(ui.Center).Children(func() {
				if creating && ui.Button(c, "从加密备份恢复…").Ghost().Disabled(blocked).Clicked() {
					c.After(m.choosePasswordVaultBackup)
				}
				if v.backupSource != "" && ui.Button(c, "返回创建").Ghost().Disabled(blocked).Clicked() {
					c.After(func() {
						v.backupSource, v.problem = "", ""
						v.password, v.confirmation = ui.Field{Secure: true}, ui.Field{Secure: true}
					})
				}
				ui.Slot(c).Grow(1)
				if ui.Button(c, submit).Disabled(blocked).Clicked() || submitted && !blocked {
					c.After(m.submitPasswordVault)
				}
			})
		})
	})
}

func (m *editorApp) passwordPair(c ui.C, field, confirmation *ui.Field, label string, blocked bool) bool {
	first := m.passwordInput(c.Part("password"), field, label, "输入"+label, 36, blocked).AutoFocus()
	second := m.passwordInput(c.Part("confirmation"), confirmation, "确认"+label, "再次输入"+label, 36, blocked)
	return first.Submitted() || second.Submitted()
}

func (m *editorApp) passwordInput(c ui.C, field *ui.Field, label, placeholder string, height float32, blocked bool) ui.Box {
	icon, caption := showSecretIcon, "显示"+label
	if !field.Secure {
		icon, caption = hideSecretIcon, "隐藏"+label
	}
	input := ui.TextField(c, field, label, placeholder).FillWidth().MinWidth(0).Height(height).
		Disabled(blocked).Action(icon, caption)
	skipButtonTabStop(input, input.ActionID())
	if input.ActionClicked() {
		c.After(func() {
			field.Secure = !field.Secure
			m.vaultTouch()
		})
	}
	return input
}

func (m *editorApp) passwordVaultContent(c ui.C, v *passwordVaultView, blocked bool) {
	ui.Column(c).Fill().MinHeight(0).PadX(12).Gap(8).Children(func() {
		ui.Row(c.Part("heading")).FillWidth().Height(62).PadY(5).Gap(4).Items(ui.Center).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Children(func() { m.assetHeading(c, passwordVaultIcon, "密码本", "本机加密库") })
			if ui.Button(c, "锁定密码本").Icon(passwordLockIcon).Ghost().Height(32).Disabled(blocked).Clicked() {
				c.After(v.lock)
			}
			if ui.Button(c, "从文本识别").Icon(searchIcon).Secondary().Height(32).Disabled(blocked).Clicked() {
				c.After(func() { m.openPasswordImport("") })
			}
			if ui.Button(c, "新建条目").Icon(plusIcon).Height(32).Disabled(blocked).Clicked() {
				c.After(func() { m.openPasswordEntry(passwordEntry{Group: v.entryGroup(), Kind: vaultLogin}) })
			}
			if ui.Button(c, "密码本设置").Icon(settingsIcon).IconOnly().Ghost().Size(32, 32).Tooltip("密码本设置").Disabled(blocked).Clicked() {
				c.After(func() {
					data := v.store.data
					d := &passwordVaultDialog{open: true, mode: "settings", useMaster: !v.windowsProtected,
						idleLock: !data.DisableIdleLock, clearClipboard: !data.DisableClipboardCleanup,
						master: ui.Field{Secure: true}, masterConfirmation: ui.Field{Secure: true}}
					d.idleMinutes.SetText(fmt.Sprint(data.IdleMinutes))
					d.clipboardSeconds.SetText(fmt.Sprint(data.ClipboardSeconds))
					v.dialog = d
					v.revealed = false
					m.vaultTouch()
				})
			}
		})
		ui.Slot(c).FillWidth().Height(1).Background(m.theme.Border)
		ui.Row(c.Part("columns")).FillWidth().Grow(1).MinHeight(0).Gap(10).Children(func() {
			m.passwordGroupList(c.Part("groups"), v, blocked)
			ui.Slot(c).Width(1).FillHeight().Background(m.theme.Border)
			m.passwordEntryList(c.Part("entries"), v, blocked)
			ui.Slot(c).Width(1).FillHeight().Background(m.theme.Border)
			m.passwordDetail(c.Part("detail"), v, blocked)
		})
		ui.Slot(c).FillWidth().Height(1).Background(m.theme.Border)
		if v.problem != "" {
			ui.Wrap(c, v.problem).FillWidth().Color(m.theme.Danger).FontSize(12)
		} else if v.message != "" {
			ui.Wrap(c, v.message).FillWidth().Muted().FontSize(12)
		} else {
			ui.Text(c, "Ctrl+F 搜索").Muted().FontSize(12)
		}
	})
}

func (m *editorApp) passwordGroupList(c ui.C, v *passwordVaultView, blocked bool) {
	data := v.store.data
	counts := map[int]int{passwordVaultAllGroups: len(data.Entries)}
	for _, entry := range data.Entries {
		counts[entry.Group]++
	}
	children := data.groupsByParent()
	type groupRow struct {
		group passwordGroup
		depth int
	}
	rows := []groupRow{{group: passwordGroup{ID: passwordVaultAllGroups, Name: "全部条目"}}}
	var appendChildren func(int, int)
	appendChildren = func(parent, depth int) {
		for _, group := range children[parent] {
			rows = append(rows, groupRow{group, depth})
			if !v.closedGroups[group.ID] {
				appendChildren(group.ID, depth+1)
			}
		}
	}
	appendChildren(0, 0)
	if data.DefaultGroup == 0 || counts[passwordVaultNoGroup] > 0 {
		rows = append(rows, groupRow{group: data.group(passwordVaultNoGroup)})
	}
	tree := &v.groupTree
	tree.RowHeight, tree.Indent = 32, 13
	tree.DisplayDepth = func(depth int) int { return min(depth, 4) }
	tree.Key = func(i int) int { return rows[i].group.ID + 1 }
	tree.Node = func(i int) ui.TreeNode {
		row := rows[i]
		return ui.TreeNode{Depth: row.depth, Branch: row.group.ID > 0 && len(children[row.group.ID]) > 0,
			Expanded: !v.closedGroups[row.group.ID], Selected: row.group.ID == v.group, Disabled: blocked}
	}
	ui.Column(c).Width(164).Shrink(0).FillHeight().MinHeight(0).Gap(5).Children(func() {
		ui.Row(c).FillWidth().Items(ui.Center).Children(func() {
			ui.Text(c, "分组").Bold().Muted().Grow(1)
			if v.group > 0 && ui.Button(c, "重命名分组").Icon(modelEditIcon).IconOnly().Ghost().Size(27, 28).Tooltip("重命名分组").Disabled(blocked).Clicked() {
				c.After(func() { m.openPasswordGroup(v.group, 0) })
			}
			if ui.Button(c, "新建分组").Icon(plusIcon).IconOnly().Ghost().Size(27, 28).Tooltip("新建分组").Disabled(blocked).Clicked() {
				c.After(func() { m.openPasswordGroup(0, 0) })
			}
		})
		ui.Tree(c.Part("group-tree"), tree, len(rows), func(i int) string { return rows[i].group.Name }, func(i int) {
			group := rows[i].group
			place := c.Part("group").KeyInt(group.ID + 1)
			row := ui.Row(place).Fill().Justify(ui.Start).Items(ui.Center).Gap(6).PadRight(5)
			if group.ID <= 0 || len(children[group.ID]) == 0 {
				row.PadLeft(24)
			}
			row.Children(func() {
				ui.Icon(place, folderIcon).Size(14, 14).Color(m.theme.MutedForeground)
				name := ui.Text(place, group.Name).Grow(1).MinWidth(0).Ellipsis().FontSize(12).LabelFor(tree.RowID(i))
				if group.ID > 0 {
					caption := data.groupPath(group.ID)
					if group.Production {
						caption += " · 正式环境"
					}
					name.Tooltip(caption).TooltipOn(ui.TooltipRight)
					if !blocked {
						ui.ContextMenu(place, name, func(menu *ui.MenuItems) {
							if menu.Item("新建子分组…").Icon(folderIcon).Clicked() {
								place.After(func() { m.openPasswordGroup(0, group.ID) })
							}
							if menu.Item("新建条目…").Icon(plusIcon).Clicked() {
								place.After(func() { m.openPasswordEntry(passwordEntry{Group: group.ID, Kind: vaultLogin}) })
							}
							menu.Separator()
							if menu.Item("重命名").Icon(modelEditIcon).Clicked() {
								place.After(func() { m.openPasswordGroup(group.ID, 0) })
							}
							if menu.Item("删除分组…").Destructive().Clicked() {
								place.After(func() { m.confirmPasswordDelete(passwordDeleteGroupMode, group.ID) })
							}
						})
					}
				}
				ui.Text(place, fmt.Sprint(counts[group.ID])).FontSize(11).Muted().Shrink(0)
			})
		}).FillWidth().Grow(1).MinHeight(0)
		if blocked {
			return
		}
		if index, ok := tree.Toggled(); ok {
			id := rows[index].group.ID
			c.After(func() {
				if v.closedGroups[id] {
					delete(v.closedGroups, id)
				} else {
					v.closedGroups[id] = true
					for parent := data.group(v.group).Parent; parent != 0; parent = data.group(parent).Parent {
						if parent == id {
							v.selectGroup(id)
							break
						}
					}
				}
				m.vaultTouch()
			})
		}
		if index, ok := tree.Chosen(); ok {
			id := rows[index].group.ID
			c.After(func() { v.selectGroup(id); m.vaultTouch() })
		}
	})
}

type passwordSiteBucket struct {
	key, label string
	entries    []passwordEntry
}

func passwordSiteBuckets(entries []passwordEntry) []passwordSiteBucket {
	var buckets []passwordSiteBucket
	positions := make(map[string]int)
	for _, entry := range entries {
		key, label := passwordSite(entry)
		if index, ok := positions[key]; ok {
			buckets[index].entries = append(buckets[index].entries, entry)
			continue
		}
		if key != "" {
			positions[key] = len(buckets)
		}
		buckets = append(buckets, passwordSiteBucket{key, label, []passwordEntry{entry}})
	}
	return buckets
}

func (m *editorApp) passwordEntryList(c ui.C, v *passwordVaultView, blocked bool) {
	ui.Column(c).Width(272).Shrink(0).FillHeight().MinHeight(0).Gap(5).Children(func() {
		box := ui.TextField(c.Part("vault-search"), &v.search, "搜索密码本", "搜索名称、账号、网站或标签").FillWidth().Height(34).Disabled(blocked)
		if v.focusSearch && !blocked {
			box.DrawOver(func(d *ui.DrawContext) { d.Canvas.RequestFocus(v.search.ID); v.focusSearch = false })
		}
		if box.Changed() {
			v.revealed = false
			clear(v.collapsed)
			v.resultList.ScrollIntoView(0)
			m.vaultTouch()
		}
		query := strings.ToLower(strings.TrimSpace(v.search.GetEditor().CommittedString()))
		var visible []passwordEntry
		for _, entry := range v.store.data.Entries {
			if (v.group != passwordVaultAllGroups && v.group != entry.Group) || (v.favorites && !entry.Favorite) {
				continue
			}
			if query != "" && !strings.Contains(strings.ToLower(entry.Name+" "+entry.Account+" "+entry.Site+" "+strings.Join(entry.Tags, " ")), query) {
				continue
			}
			visible = append(visible, entry)
		}
		selected := false
		for _, entry := range visible {
			if entry.ID == v.selected {
				selected = true
				break
			}
		}
		if !selected {
			v.selected, v.revealed = 0, false
			if len(visible) > 0 {
				v.selected = visible[0].ID
			}
		}
		ui.Row(c.Part("filters")).FillWidth().Items(ui.Center).Gap(4).Children(func() {
			for _, option := range []struct {
				label    string
				favorite bool
			}{{"全部", false}, {"收藏", true}} {
				button := ui.Button(c.Key(option.label), option.label).Ghost().Height(27).FontSize(12).Disabled(blocked)
				if v.favorites == option.favorite {
					button.Secondary()
				}
				if button.Clicked() {
					c.After(func() {
						v.favorites, v.revealed = option.favorite, false
						v.resultList.ScrollIntoView(0)
						m.vaultTouch()
					})
				}
			}
			ui.Slot(c).Grow(1)
			ui.Text(c, fmt.Sprintf("%d 项", len(visible))).Muted().FontSize(12)
		})
		if len(visible) == 0 {
			ui.Wrap(c, "没有符合条件的条目。").FillWidth().Muted().FontSize(12)
			return
		}
		type catalogRow struct {
			entry  passwordEntry
			bucket passwordSiteBucket
			nested bool
		}
		var rows []catalogRow
		keys := make([]int, 0, len(visible))
		for _, bucket := range passwordSiteBuckets(visible) {
			nested := len(bucket.entries) > 1
			if nested {
				rows = append(rows, catalogRow{bucket: bucket})
				keys = append(keys, bucket.entries[0].ID*2-2)
				if v.collapsed[bucket.key] {
					continue
				}
			}
			for _, entry := range bucket.entries {
				rows = append(rows, catalogRow{entry: entry, nested: nested})
				keys = append(keys, entry.ID*2-1)
			}
		}
		v.resultList.Key = func(index int) int { return keys[index] }
		ui.List(c.Part("vault-results"), &v.resultList, len(rows), func(row ui.ListRow) {
			item := rows[row.Index]
			row.Element.Height(37).FillWidth().Items(ui.Center).Children(func() {
				if item.entry.ID > 0 {
					m.passwordEntryRow(row.C, v, item.entry, item.nested, blocked)
					return
				}
				bucket := item.bucket
				closed := v.collapsed[bucket.key]
				chevron := chevronDownIcon
				if closed {
					chevron = chevronRightIcon
				}
				label := fmt.Sprintf("%s · %d 个账号", bucket.label, len(bucket.entries))
				if v.group == passwordVaultAllGroups {
					label += " · " + v.store.data.groupPath(bucket.entries[0].Group)
				}
				if m.passwordListRow(row.C, label, chevron, 32, false, blocked).Background(m.theme.Secondary).Clicked() {
					row.C.After(func() { v.collapsed[bucket.key] = !closed; m.vaultTouch() })
				}
			})
		}).FillWidth().Grow(1).MinHeight(0).Label("密码本条目")
	})
}

func (m *editorApp) passwordEntryRow(c ui.C, v *passwordVaultView, entry passwordEntry, nested, blocked bool) {
	row := ui.Row(c).FillWidth().MinWidth(0)
	if nested {
		row.PadLeft(13)
	}
	row.Children(func() {
		label := entry.Name
		if entry.Account != "" {
			label += " · " + entry.Account
		}
		if v.group == passwordVaultAllGroups && entry.Group != 0 {
			label += " · " + v.store.data.groupPath(entry.Group)
		}
		button := m.passwordListRow(c, label, passwordVaultIcon, 35, entry.ID == v.selected, blocked)
		if button.Clicked() {
			c.After(func() { v.selected, v.revealed, v.message = entry.ID, false, ""; m.vaultTouch() })
		}
		if !blocked {
			ui.ContextMenu(c, button, func(menu *ui.MenuItems) {
				if menu.Item("删除条目…").Destructive().Clicked() {
					c.After(func() { m.confirmPasswordDelete(passwordDeleteEntryMode, entry.ID) })
				}
			})
		}
	})
}

func (m *editorApp) passwordListRow(c ui.C, label string, icon gfx.Icon, height float32, selected, blocked bool) ui.Box {
	row := ui.Row(c).FillWidth().MinWidth(0).Height(height).PadX(9).Gap(7).
		Justify(ui.Start).Items(ui.Center).Radius(9)
	row.HitWith(ui.ID(c.Part("select-row")), label, gfx.HitOptions{Role: gfx.RoleButton, Disabled: blocked, Selected: selected}).
		Hover(ui.Look{}.Background(m.theme.Hover)).Focus(ui.Look{}.Ring(1, m.theme.Ring)).Tooltip(label)
	if selected {
		row.Background(m.theme.Secondary)
	}
	row.Children(func() {
		if len(icon.Commands) > 0 {
			ui.Icon(c, icon).Size(15, 15).Color(m.theme.Foreground).Shrink(0)
		}
		ui.Text(c, label).FontSize(12).Grow(1).MinWidth(0).Ellipsis()
	})
	return row
}

func (m *editorApp) passwordDetail(c ui.C, v *passwordVaultView, blocked bool) {
	entry, ok := v.store.data.entry(v.selected)
	if !ok {
		ui.Column(c).Grow(1).MinWidth(0).FillHeight().Center().Children(func() { ui.Text(c, "选择一个条目").Muted() })
		return
	}
	ui.Scroll(c).Grow(1).MinWidth(0).FillHeight().Scrollbars().Children(func() {
		ui.Column(c).FillWidth().MaxWidth(560).MinWidth(0).Pad(8).Gap(10).Children(func() {
			ui.Row(c.Part("item-heading")).FillWidth().Items(ui.Center).Gap(5).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Children(func() {
					m.assetHeading(c, passwordVaultIcon, entry.Name, passwordKindLabel(entry.Kind))
				})
				label := "收藏条目"
				if entry.Favorite {
					label = "取消收藏"
				}
				if ui.Button(c, label).Ghost().Height(28).FontSize(12).Disabled(blocked).Clicked() {
					c.After(func() {
						data := v.store.data.clone()
						for i, item := range data.Entries {
							if item.ID == entry.ID {
								data.Entries[i].Favorite = !entry.Favorite
								break
							}
						}
						m.savePasswordData(data)
					})
				}
				if ui.Button(c, "编辑条目").Icon(modelEditIcon).IconOnly().Ghost().Size(28, 28).Tooltip("编辑条目").Disabled(blocked).Clicked() {
					c.After(func() { m.openPasswordEntry(entry) })
				}
			})
			group := v.store.data.group(entry.Group)
			groupPath := v.store.data.groupPath(entry.Group)
			if group.Production {
				ink, fill := gfx.RGB(130, 68, 17), gfx.RGB(255, 240, 220)
				if !m.light {
					ink, fill = gfx.RGB(255, 208, 161), gfx.RGB(60, 41, 27)
				}
				ui.Text(c, "正式环境 · "+groupPath).FontSize(12).Bold().Color(ink).Background(fill).Radius(5).PadX(7).PadY(4)
			} else {
				m.assetBadge(c, groupPath)
			}
			if entry.Site != "" || entry.Account != "" || entry.Secret != "" {
				m.assetCard(c.Part("fields")).PadY(7).Gap(3).Children(func() {
					if entry.Site != "" {
						m.passwordDetailField(c.Part("site"), "网站", entry.Site, false, v, blocked)
					}
					if entry.Account != "" {
						m.passwordDetailField(c.Part("account"), "用户名", entry.Account, false, v, blocked)
					}
					if entry.Secret != "" {
						label := "密码"
						if entry.Kind == vaultKey {
							label = "密钥"
						}
						m.passwordDetailField(c.Part("secret"), label, entry.Secret, true, v, blocked)
					}
				})
			}
			if len(entry.Tags) > 0 || entry.Note != "" {
				m.assetCard(c.Part("metadata")).Gap(10).Children(func() {
					if len(entry.Tags) > 0 {
						ui.Column(c.Part("tags")).FillWidth().Gap(6).Children(func() {
							ui.Text(c, "标签").Bold().FontSize(12)
							ink, fill := m.assetAccent()
							ui.Flow(c).FillWidth().Gap(6).LineGap(6).Children(func() {
								for _, tag := range entry.Tags {
									ui.Wrap(c, tag).FontSize(12).Color(ink).Background(fill).Radius(6).PadX(8).PadY(4)
								}
							})
						})
					}
					if len(entry.Tags) > 0 && entry.Note != "" {
						ui.Slot(c).FillWidth().Height(1).Background(m.theme.Border)
					}
					if entry.Note != "" {
						ui.Column(c.Part("note")).FillWidth().Gap(5).Children(func() {
							ui.Text(c, "备注").Bold().FontSize(12)
							ui.Wrap(c, entry.Note).FillWidth().FontSize(13)
						})
					}
				})
			}
			if key, _ := passwordSite(entry); key != "" && ui.Button(c, "添加同站点账号").Icon(plusIcon).Outline().Height(30).FontSize(12).Disabled(blocked).Clicked() {
				c.After(func() { m.openPasswordEntry(passwordEntry{Group: entry.Group, Kind: vaultLogin, Site: entry.Site}) })
			}
		})
	})
}

func (m *editorApp) passwordDetailField(c ui.C, label, value string, secret bool, v *passwordVaultView, blocked bool) {
	ui.Row(c).FillWidth().MinWidth(0).Items(ui.Start).Gap(7).Children(func() {
		shown := value
		if secret && !v.revealed {
			shown = "••••••••••••"
		}
		ui.Text(c, label).Width(49).Shrink(0).PadTop(6).Muted().FontSize(12)
		ui.Wrap(c, shown).MinWidth(0).Shrink(1).PadY(5).FontSize(13)
		ui.Row(c.Part("actions")).Shrink(0).Gap(2).Children(func() {
			if secret {
				action, icon := "显示"+label, showSecretIcon
				if v.revealed {
					action, icon = "隐藏"+label, hideSecretIcon
				}
				eye := ui.Button(c, action).Icon(icon).IconOnly().Ghost().Size(27, 28).Tooltip(action).Disabled(blocked)
				skipButtonTabStop(eye, eye.N().ID)
				if eye.Clicked() {
					c.After(func() { v.revealed = !v.revealed; m.vaultTouch() })
				}
			}
			if ui.Button(c, "复制"+label).Icon(copyIcon).IconOnly().Ghost().Size(27, 28).Tooltip("复制" + label).Disabled(blocked).Clicked() {
				c.After(func() { m.copyPasswordField(value, label) })
			}
		})
	})
}
