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

var passwordCopiedIcon = gfx.Icon{ViewBox: 24, Stroke: 1.8, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(5, 12), gfx.LineTo(10, 17), gfx.LineTo(19, 7),
}}

var passwordFavoriteIcon = gfx.Icon{ViewBox: 24, Stroke: 1.6, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(12, 3), gfx.LineTo(14.9, 8.9), gfx.LineTo(21.4, 9.8), gfx.LineTo(16.7, 14.4),
	gfx.LineTo(17.8, 20.9), gfx.LineTo(12, 17.8), gfx.LineTo(6.2, 20.9), gfx.LineTo(7.3, 14.4),
	gfx.LineTo(2.6, 9.8), gfx.LineTo(9.1, 8.9), gfx.ClosePath(),
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
	m.workspacePanel(c).Fill().MinWidth(0).MinHeight(0).Children(func() {
		ui.Row(c.Part("heading")).FillWidth().Height(36).PadX(10).Gap(6).Items(ui.Center).Children(func() {
			ui.Icon(c, passwordVaultIcon).Size(16, 16).Color(m.theme.MutedForeground)
			ui.Text(c, uiText("密码本")).Bold().FontSize(13).Grow(1)
			if v.store != nil && ui.Button(c, uiText("锁定密码本")).Icon(passwordLockIcon).IconOnly().Ghost().Size(27, 28).
				Tooltip(uiText("锁定密码本")).Disabled(blocked).Clicked() {
				c.After(v.lock)
			}
			if ui.Button(c, uiText("关闭密码本")).Icon(closeIcon).IconOnly().Ghost().Size(27, 28).
				Tooltip(uiText("关闭密码本")).Disabled(blocked).Clicked() {
				c.After(m.closePasswordVault)
			}
		})
		ui.Slot(c).FillWidth().Height(1).Background(m.theme.Border)
		if v.store == nil {
			m.passwordGate(c.Part("gate"), v, blocked)
			return
		}
		m.passwordVaultContent(c.Part("contents"), v, blocked)
	})
}

func (m *editorApp) passwordGate(c ui.C, v *passwordVaultView, blocked bool) {
	creating := !v.exists && v.backupSource == ""
	title, hint, submit := uiText("密码本已锁定"), uiText("解锁后查看本机密码库。"), uiText("解锁")
	if creating {
		title, submit = uiText("创建本机密码本"), uiText("创建密码本")
		if v.useMaster {
			hint = fmt.Sprintf(uiText("请选择至少 %d 个字符的主密码。忘记主密码将无法解锁。"), passwordVaultMinMasterLength)
		} else {
			hint = uiText("由当前 Windows 账户保护，打开时直接进入。")
		}
	} else if v.backupSource != "" {
		title, hint, submit = uiText("恢复加密备份"), filepath.Base(v.backupSource), uiText("恢复密码本")
	} else if v.windowsProtected {
		hint, submit = "", uiText("打开密码本")
	}
	ui.Scroll(c).FillWidth().Grow(1).MinHeight(0).Pad(9).Scrollbars().Children(func() {
		m.assetCard(c.Part("unlock")).FillWidth().MaxWidth(460).Gap(10).Children(func() {
			if v.backupSource == "" {
				hint = uiText(hint)
			}
			m.assetHeading(c, passwordLockIcon, uiText(title), hint)
			if creating {
				enabled := v.useMaster
				if settingsSwitchRow(c.Part("master-switch"), uiText("使用主密码"), &enabled) && !blocked {
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
				ui.Flow(c.Part("vault-recovery")).FillWidth().Gap(4).LineGap(4).Children(func() {
					if v.exists && !v.windowsProtected && ui.Button(c, uiText("忘记主密码？创建新密码本")).Ghost().
						Height(26).FontSize(12).Disabled(blocked).Clicked() {
						c.After(func() { v.dialog = &passwordVaultDialog{open: true, mode: passwordArchiveResetMode} })
					}
					if len(v.archives) > 0 && ui.Button(c, uiText("恢复旧密码本…")).Ghost().
						Height(26).FontSize(12).Disabled(blocked).Clicked() {
						c.After(m.openPasswordArchiveRestore)
					}
				})
			}
			if v.pendingSource != "" {
				ui.Text(c, uiText("打开后继续识别选区")).Muted().FontSize(12)
			}
			if v.problem != "" {
				ui.Wrap(c, v.problem).FillWidth().FontSize(12).Color(m.theme.Danger)
			}
			ui.Flow(c).FillWidth().Gap(6).LineGap(5).Children(func() {
				if creating && ui.Button(c, uiText("从加密备份恢复…")).Ghost().Disabled(blocked).Clicked() {
					c.After(m.choosePasswordVaultBackup)
				}
				if v.backupSource != "" && ui.Button(c, uiText("返回创建")).Ghost().Disabled(blocked).Clicked() {
					c.After(func() {
						v.backupSource, v.problem = "", ""
						v.password, v.confirmation = ui.Field{Secure: true}, ui.Field{Secure: true}
					})
				}
				if ui.Button(c, uiText(submit)).Disabled(blocked).Clicked() || submitted && !blocked {
					c.After(m.submitPasswordVault)
				}
			})
		})
	})
}

func (m *editorApp) passwordPair(c ui.C, field, confirmation *ui.Field, label string, blocked bool) bool {
	label = uiText(label)
	first := m.passwordInput(c.Part("password"), field, label, fmt.Sprintf(uiText("输入%s"), label), 36, blocked).AutoFocus()
	second := m.passwordInput(c.Part("confirmation"), confirmation, fmt.Sprintf(uiText("确认%s"), label), fmt.Sprintf(uiText("再次输入%s"), label), 36, blocked)
	return first.Submitted() || second.Submitted()
}

func (m *editorApp) passwordInput(c ui.C, field *ui.Field, label, placeholder string, height float32, blocked bool) ui.Box {
	icon, caption := showSecretIcon, fmt.Sprintf(uiText("显示%s"), uiText(label))
	if !field.Secure {
		icon, caption = hideSecretIcon, fmt.Sprintf(uiText("隐藏%s"), uiText(label))
	}
	input := ui.TextField(c, field, uiText(label), uiText(placeholder)).FillWidth().MinWidth(0).Height(height).
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
	ui.Column(c).Fill().MinHeight(0).Pad(8).Gap(6).Children(func() {
		ui.Row(c.Part("actions")).FillWidth().Gap(4).Items(ui.Center).Children(func() {
			if v.detailOpen {
				if ui.Button(c, uiText("返回条目列表")).Icon(chevronLeftIcon).Ghost().Height(30).FontSize(12).Disabled(blocked).Clicked() {
					c.After(func() {
						v.detailOpen, v.revealed = false, false
						m.vaultTouch()
					})
				}
				ui.Slot(c).Grow(1)
			} else {
				title := uiText("所有分组")
				if v.group != passwordVaultAllGroups {
					title = v.store.data.groupPath(v.group)
				}
				groups := ui.Button(c.Part("group-picker"), title).Icon(folderIcon).Ghost().Grow(1).MinWidth(0).
					Height(30).PadX(6).Justify(ui.Start).FontSize(12).Ellipsis().Tooltip(title).Disabled(blocked)
				if v.groupsOpen {
					groups.Secondary()
				}
				if groups.Clicked() {
					c.After(func() { v.groupsOpen = !v.groupsOpen; m.vaultTouch() })
				}
			}
			if ui.Button(c, uiText("从文本识别")).Icon(passwordRecognizeIcon).IconOnly().Ghost().Size(28, 30).
				Tooltip(uiText("从文本识别")).Disabled(blocked).Clicked() {
				c.After(func() { m.openPasswordImport("") })
			}
			if ui.Button(c, uiText("新建条目")).Icon(plusIcon).IconOnly().Size(28, 30).
				Tooltip(uiText("新建条目")).Disabled(blocked).Clicked() {
				c.After(func() { m.openPasswordEntry(passwordEntry{Group: v.entryGroup(), Kind: vaultLogin}) })
			}
			if ui.Button(c, uiText("密码本设置")).Icon(settingsIcon).IconOnly().Ghost().Size(28, 30).Tooltip(uiText("密码本设置")).Disabled(blocked).Clicked() {
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
		if v.detailOpen {
			m.passwordDetail(c.Part("detail"), v, blocked)
		} else {
			if v.groupsOpen {
				m.passwordGroupList(c.Part("groups"), v, blocked)
			}
			m.passwordEntryList(c.Part("entries"), v, blocked)
		}
		if v.problem != "" {
			ui.Wrap(c, v.problem).FillWidth().Color(m.theme.Danger).FontSize(12)
		} else if v.message != "" {
			ui.Wrap(c, v.message).FillWidth().Muted().FontSize(12)
		} else if !v.detailOpen {
			ui.Text(c, uiText("本机加密库")).Muted().FontSize(11).PadX(3)
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
	rows := []groupRow{{group: passwordGroup{ID: passwordVaultAllGroups, Name: uiText("全部条目")}}}
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
		group := data.group(passwordVaultNoGroup)
		group.Name = uiText(group.Name)
		rows = append(rows, groupRow{group: group})
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
	ui.Column(c).FillWidth().Height(min(202, 45+32*float32(len(rows)))).Shrink(0).MinHeight(0).Pad(7).Gap(3).
		Radius(10).Background(m.theme.Card).Children(func() {
		ui.Row(c).FillWidth().Items(ui.Center).Children(func() {
			ui.Text(c, uiText("分组")).Bold().Muted().Grow(1)
			if v.group > 0 && ui.Button(c, uiText("重命名分组")).Icon(modelEditIcon).IconOnly().Ghost().Size(27, 28).Tooltip(uiText("重命名分组")).Disabled(blocked).Clicked() {
				c.After(func() { m.openPasswordGroup(v.group, 0) })
			}
			if ui.Button(c, uiText("新建分组")).Icon(plusIcon).IconOnly().Ghost().Size(27, 28).Tooltip(uiText("新建分组")).Disabled(blocked).Clicked() {
				c.After(func() { m.openPasswordGroup(0, 0) })
			}
		})
		ui.Tree(c.Part("group-tree"), tree, len(rows), func(i int) string { return rows[i].group.displayName() }, func(i int) {
			group := rows[i].group
			place := c.Part("group").KeyInt(group.ID + 1)
			row := ui.Row(place).Fill().Justify(ui.Start).Items(ui.Center).Gap(6).PadRight(5)
			if group.ID <= 0 || len(children[group.ID]) == 0 {
				row.PadLeft(24)
			}
			row.Children(func() {
				ui.Icon(place, folderIcon).Size(14, 14).Color(m.theme.MutedForeground)
				name := ui.Text(place, group.displayName()).Grow(1).MinWidth(0).Ellipsis().FontSize(12).LabelFor(tree.RowID(i))
				if group.ID > 0 {
					caption := data.groupPath(group.ID)
					if group.Production {
						caption += " · " + uiText("正式环境")
					}
					name.Tooltip(caption).TooltipOn(ui.TooltipRight)
					if !blocked {
						ui.ContextMenu(place, name, func(menu *ui.MenuItems) {
							if menu.Item(m.menuText("新建子分组…")).Icon(folderIcon).Clicked() {
								place.After(func() { m.openPasswordGroup(0, group.ID) })
							}
							if menu.Item(m.menuText("新建条目…")).Icon(plusIcon).Clicked() {
								place.After(func() { m.openPasswordEntry(passwordEntry{Group: group.ID, Kind: vaultLogin}) })
							}
							menu.Separator()
							if menu.Item(m.menuText("重命名")).Icon(modelEditIcon).Clicked() {
								place.After(func() { m.openPasswordGroup(group.ID, 0) })
							}
							if menu.Item(m.menuText("删除分组…")).Destructive().Clicked() {
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
	ui.Column(c).FillWidth().Grow(1).MinHeight(0).Pad(7).Gap(6).
		Radius(10).Background(m.theme.Card).Children(func() {
		ui.Row(c.Part("search-bar")).FillWidth().Height(37).PadX(8).Gap(6).Items(ui.Center).
			Radius(8).Background(m.theme.Background).Children(func() {
			ui.Icon(c, searchIcon).Size(15, 15).Color(m.theme.MutedForeground).Shrink(0)
			box := ui.TextFieldBase(c.Part("vault-search"), &v.search, uiText("搜索密码本"), uiText("搜索名称、账号、网站或标签")).
				Grow(1).MinWidth(0).Height(33).Disabled(blocked)
			if v.focusSearch && !blocked {
				box.DrawOver(func(d *ui.DrawContext) { d.Canvas.RequestFocus(v.search.ID); v.focusSearch = false })
			}
			if box.Changed() {
				v.revealed = false
				clear(v.collapsed)
				v.resultList.ScrollIntoView(0)
				m.vaultTouch()
			}
			if v.search.Text() != "" && ui.Button(c.Part("clear"), uiText("清空搜索")).Icon(closeIcon).IconOnly().Ghost().
				Size(25, 25).Tooltip(uiText("清空搜索")).Disabled(blocked).Clicked() {
				c.After(func() {
					v.search.SetText("")
					v.focusSearch, v.revealed = true, false
					clear(v.collapsed)
					v.resultList.ScrollIntoView(0)
					m.vaultTouch()
				})
			}
		})
		query := strings.ToLower(strings.TrimSpace(v.search.GetEditor().CommittedString()))
		matches := v.matchingEntries(query)
		visible := make([]passwordEntry, 0, len(matches))
		for _, index := range matches {
			visible = append(visible, v.store.data.Entries[index])
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
				button := ui.Button(c.Key(option.label), uiText(option.label)).Ghost().Height(27).FontSize(12).Disabled(blocked)
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
			ui.Text(c, fmt.Sprintf(uiText("%d 项"), len(visible))).Muted().FontSize(12)
		})
		if len(visible) == 0 {
			ui.Wrap(c, uiText("没有符合条件的条目。")).FillWidth().Muted().FontSize(12)
			return
		}
		type catalogRow struct {
			entry  passwordEntry
			bucket passwordSiteBucket
			nested bool
		}
		var rows []catalogRow
		keys := make([]int, 0, len(visible))
		labels := make([]string, 0, len(visible))
		for _, bucket := range passwordSiteBuckets(visible) {
			nested := len(bucket.entries) > 1
			if nested {
				rows = append(rows, catalogRow{bucket: bucket})
				keys = append(keys, bucket.entries[0].ID*2-2)
				labels = append(labels, bucket.label)
				if v.collapsed[bucket.key] {
					continue
				}
			}
			for _, entry := range bucket.entries {
				rows = append(rows, catalogRow{entry: entry, nested: nested})
				keys = append(keys, entry.ID*2-1)
				labels = append(labels, entry.Name)
			}
		}
		v.resultList.Key = func(index int) int { return keys[index] }
		v.resultList.Label = func(index int) string { return labels[index] }
		ui.List(c.Part("vault-results"), &v.resultList, len(rows), func(row ui.ListRow) {
			item := rows[row.Index]
			height := float32(50)
			if item.entry.ID == 0 {
				height = 38
			}
			row.Element.Height(height).FillWidth().Items(ui.Center).Children(func() {
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
				label := fmt.Sprintf(uiText("%s · %d 个账号"), bucket.label, len(bucket.entries))
				if v.group == passwordVaultAllGroups {
					label += " · " + v.store.data.groupPath(bucket.entries[0].Group)
				}
				if m.passwordListRow(row.C, label, chevron, 32, false, blocked).Background(m.theme.Secondary).Clicked() {
					row.C.After(func() { v.collapsed[bucket.key] = !closed; m.vaultTouch() })
				}
			})
		}).FillWidth().Grow(1).MinHeight(0).Label(uiText("密码本条目"))
		if index, submitted := v.resultList.Submitted(); !blocked && submitted && index >= 0 && index < len(rows) {
			if entry := rows[index].entry; entry.ID != 0 {
				c.After(func() {
					v.selected, v.revealed, v.message, v.detailOpen = entry.ID, false, "", true
					m.vaultTouch()
				})
			}
		}
		if selected := v.resultList.Selected(); !blocked && v.resultList.Changed() && selected >= 0 && selected < len(rows) {
			if entry := rows[selected].entry; entry.ID > 0 && entry.ID != v.selected {
				c.After(func() { v.selected, v.revealed = entry.ID, false; m.vaultTouch() })
			}
		}
	})
}

func (m *editorApp) passwordEntryRow(c ui.C, v *passwordVaultView, entry passwordEntry, nested, blocked bool) {
	row := ui.Row(c).FillWidth().MinWidth(0)
	if nested {
		row.PadLeft(13)
	}
	row.Children(func() {
		detail := entry.Account
		if detail == "" {
			detail = entry.Site
		}
		if v.group == passwordVaultAllGroups && entry.Group != 0 {
			if detail != "" {
				detail += " · "
			}
			detail += v.store.data.groupPath(entry.Group)
		}
		label := entry.Name
		if detail != "" {
			label += " · " + detail
		}
		selected := entry.ID == v.selected
		button := ui.Row(c.Part("entry-card")).FillWidth().MinWidth(0).Height(46).PadX(8).Gap(8).Items(ui.Center).Radius(8).
			HitWith(ui.ID(c.Part("entry-select")), label, gfx.HitOptions{Role: gfx.RoleButton, Disabled: blocked, Selected: selected}).
			Hover(ui.Look{}.Background(m.theme.Hover)).Focus(ui.Look{}.Ring(1, m.theme.Ring)).Tooltip(label)
		if selected {
			button.Background(m.theme.Selection)
		}
		button.Children(func() {
			ink, fill := m.assetAccent()
			ui.Row(c).Size(27, 28).Shrink(0).Center().Radius(7).Background(fill).Children(func() {
				ui.Icon(c, passwordVaultIcon).Size(15, 15).Color(ink)
			})
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, entry.Name).FillWidth().Ellipsis().FontSize(12).Bold()
				if detail != "" {
					ui.Text(c, detail).FillWidth().Ellipsis().FontSize(11).Muted()
				}
			})
			if entry.Favorite {
				ui.Icon(c, passwordFavoriteIcon).Size(13, 13).Color(ink).Shrink(0)
			}
		})
		if button.Clicked() {
			c.After(func() { v.selected, v.revealed, v.message, v.detailOpen = entry.ID, false, "", true; m.vaultTouch() })
		}
		if !blocked {
			ui.ContextMenu(c, button, func(menu *ui.MenuItems) {
				if entry.Account != "" && menu.Item(uiText("复制用户名")).Icon(copyIcon).Clicked() {
					c.After(func() { v.selected, v.revealed = entry.ID, false; m.copyPasswordField(entry.Account, "用户名") })
				}
				if entry.Secret != "" {
					label := "密码"
					if entry.Kind == vaultKey {
						label = "密钥"
					}
					if menu.Item(fmt.Sprintf(uiText("复制%s"), uiText(label))).Icon(copyIcon).Clicked() {
						c.After(func() { v.selected, v.revealed = entry.ID, false; m.copyPasswordField(entry.Secret, label) })
					}
				}
				if menu.Item(uiText("编辑条目")).Icon(modelEditIcon).Clicked() {
					c.After(func() { m.openPasswordEntry(entry) })
				}
				menu.Separator()
				if menu.Item(m.menuText("删除条目…")).Destructive().Clicked() {
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
		ui.Column(c).Grow(1).MinWidth(0).FillHeight().Center().Children(func() { ui.Text(c, uiText("选择一个条目")).Muted() })
		return
	}
	ui.Scroll(c).FillWidth().Grow(1).MinWidth(0).MinHeight(0).Scrollbars().Children(func() {
		ui.Column(c).FillWidth().MaxWidth(560).MinWidth(0).Pad(8).Gap(10).Children(func() {
			ui.Row(c.Part("item-heading")).FillWidth().Items(ui.Center).Gap(5).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Children(func() {
					m.assetHeading(c, passwordVaultIcon, entry.Name, uiText(passwordKindLabel(entry.Kind)))
				})
				label := "收藏条目"
				if entry.Favorite {
					label = "取消收藏"
				}
				favorite := ui.Button(c, uiText(label)).Icon(passwordFavoriteIcon).IconOnly().Ghost().Size(28, 28).Tooltip(uiText(label)).Disabled(blocked)
				if entry.Favorite {
					favorite.Secondary()
				}
				if favorite.Clicked() {
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
				if ui.Button(c, uiText("编辑条目")).Icon(modelEditIcon).IconOnly().Ghost().Size(28, 28).Tooltip(uiText("编辑条目")).Disabled(blocked).Clicked() {
					c.After(func() { m.openPasswordEntry(entry) })
				}
				if key, _ := passwordSite(entry); key != "" && ui.Button(c, uiText("添加同站点账号")).Icon(plusIcon).IconOnly().Ghost().Size(28, 28).
					Tooltip(uiText("添加同站点账号")).Disabled(blocked).Clicked() {
					c.After(func() { m.openPasswordEntry(passwordEntry{Group: entry.Group, Kind: vaultLogin, Site: entry.Site}) })
				}
			})
			group := v.store.data.group(entry.Group)
			groupPath := v.store.data.groupPath(entry.Group)
			if group.Production {
				ink, fill := gfx.RGB(130, 68, 17), gfx.RGB(255, 240, 220)
				if !m.light {
					ink, fill = gfx.RGB(255, 208, 161), gfx.RGB(60, 41, 27)
				}
				ui.Text(c, uiText("正式环境 · ")+groupPath).FontSize(12).Bold().Color(ink).Background(fill).Radius(5).PadX(7).PadY(4)
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
							ui.Text(c, uiText("标签")).Bold().FontSize(12)
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
							ui.Text(c, uiText("备注")).Bold().FontSize(12)
							ui.Wrap(c, entry.Note).FillWidth().FontSize(13)
						})
					}
				})
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
		labelWidth := float32(52)
		if englishInterface.Load() {
			labelWidth = 70
		}
		ui.Text(c, uiText(label)).Width(labelWidth).Shrink(0).PadTop(6).Muted().FontSize(12)
		ui.Text(c, shown).MinWidth(0).Shrink(1).PadY(5).FontSize(13).Ellipsis()
		ui.Row(c.Part("actions")).Shrink(0).Gap(2).Children(func() {
			if secret {
				action, icon := fmt.Sprintf(uiText("显示%s"), uiText(label)), showSecretIcon
				if v.revealed {
					action, icon = fmt.Sprintf(uiText("隐藏%s"), uiText(label)), hideSecretIcon
				}
				eye := ui.Button(c, action).Icon(icon).IconOnly().Ghost().Size(27, 28).Tooltip(action).Disabled(blocked)
				skipButtonTabStop(eye, eye.N().ID)
				if eye.Clicked() {
					c.After(func() { v.revealed = !v.revealed; m.vaultTouch() })
				}
			}
			copyLabel := fmt.Sprintf(uiText("复制%s"), uiText(label))
			tooltip, icon := copyLabel, copyIcon
			if remaining := time.Until(v.copyFeedbackUntil); remaining > 0 && v.copiedEntry == v.selected && v.copiedField == label {
				tooltip, icon = uiText("已复制"), passwordCopiedIcon
				if canvas := c.Canvas(); canvas != nil {
					canvas.RequestFrameIn(remaining)
				}
			}
			if ui.Button(c, copyLabel).Icon(icon).IconOnly().Ghost().Size(27, 28).Tooltip(tooltip).Disabled(blocked).Clicked() {
				c.After(func() { m.copyPasswordField(value, label) })
			}
		})
	})
}
