package editor

import (
	"fmt"
	"slices"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

const gitShelfPrefix = "[EditorX Shelf] "

var gitExpandIcon = gfx.Icon{ViewBox: 24, Stroke: 1.7, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(7, 9), gfx.LineTo(12, 4), gfx.LineTo(17, 9), gfx.MoveTo(7, 15), gfx.LineTo(12, 20), gfx.LineTo(17, 15),
}}

var gitCollapseIcon = gfx.Icon{ViewBox: 24, Stroke: 1.7, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(7, 5), gfx.LineTo(12, 10), gfx.LineTo(17, 5), gfx.MoveTo(7, 19), gfx.LineTo(12, 14), gfx.LineTo(17, 19),
}}

func (m *editorApp) gitSidebar(c ui.C, blocked bool) {
	m.workspacePanel(c).Fill().MinWidth(0).MinHeight(0).Children(func() {
		ui.Row(c.Part("header")).FillWidth().Height(44).PadX(8).Gap(4).Items(ui.Center).Children(func() {
			ui.Icon(c, gitBranchIcon).Size(16, 16).Color(m.theme.MutedForeground)
			m.gitCommitTabs(c.Part("pages"), blocked || m.tools.git.request != nil || m.tools.git.snapshot.root == "")
			ui.Slot(c).Grow(1)
			if ui.Button(c, uiText("关闭 Git 侧栏")).Icon(closeIcon).IconOnly().Ghost().Size(27, 28).
				Tooltip(uiText("关闭 Git 侧栏")).Disabled(blocked).Clicked() {
				c.After(func() {
					m.tools.gitOpen = false
					m.focusActiveSource()
				})
			}
		})
		ui.Slot(c).FillWidth().Height(1).Background(m.theme.Border)
		m.gitPanel(c.Part("panel"), blocked)
	})
}

func (m *editorApp) gitCommitTabs(c ui.C, busy bool) {
	git := &m.tools.git
	for _, option := range []struct {
		id, label string
	}{{"", "提交变更"}, {"shelf", "搁置"}, {"stash", "贮藏"}} {
		button := ui.Button(c.Key(option.id), uiText(option.label)).Ghost().Height(30).PadX(8).FontSize(13).Disabled(busy)
		if git.stashPage == option.id {
			button.Secondary()
		}
		if button.Clicked() {
			c.After(func() {
				git.stashPage = option.id
				if git.stashPage != "" {
					m.startGit(gitListStashes, "", nil)
				}
			})
		}
	}
}

func (m *editorApp) gitCommitTools(c ui.C, busy bool) {
	git := &m.tools.git
	if git.stashPage != "" {
		return
	}
	ui.Row(c.Part("change-tools")).FillWidth().Gap(2).Items(ui.Center).Children(func() {
		selected, selectable := 0, 0
		for _, change := range git.snapshot.changes {
			if !change.conflicted() && change.status != "!!" {
				selectable++
				if git.commitSelected(change) {
					selected++
				}
			}
		}
		allSelected := selected == selectable
		caption := uiText("全选变更")
		if allSelected && selectable > 0 {
			caption = uiText("取消全选")
		}
		if ui.Button(c, caption).Icon(gitCommitIcon).Ghost().Height(29).FontSize(12).
			Disabled(busy || selectable == 0).Clicked() {
			c.After(func() {
				if git.commitSelection == nil {
					git.commitSelection = make(map[string]bool)
				}
				for _, change := range git.snapshot.changes {
					if !change.conflicted() && change.status != "!!" {
						git.commitSelection[change.path] = !allSelected
					}
				}
			})
		}
		count := fmt.Sprintf(uiText("已选 %d/%d"), selected, selectable)
		ui.Text(c.Part("selected-count"), count).FontSize(11).Muted().Grow(1).MinWidth(0).Ellipsis()
		for _, option := range []struct {
			label    string
			icon     gfx.Icon
			collapse bool
		}{{"展开全部变更", gitExpandIcon, false}, {"折叠全部变更", gitCollapseIcon, true}} {
			label := uiText(option.label)
			if ui.Button(c.Key(option.label), label).Icon(option.icon).IconOnly().Ghost().Size(26, 27).
				Tooltip(label).Disabled(busy || len(git.changeRows()) == 0).Clicked() {
				c.After(func() {
					git.collapseAll, git.collapsed, git.rows = option.collapse, nil, nil
					git.tree = ui.TreeState{}
				})
			}
		}
		grouping := ui.Button(c, uiText("分组与显示")).Icon(settingsIcon).IconOnly().Ghost().Size(27, 27).
			Tooltip(uiText("分组与显示")).Disabled(busy)
		if grouping.Clicked() {
			git.groupMenu = !git.groupMenu
		}
		ui.Menu(c, grouping, &git.groupMenu, func(items *ui.MenuItems) {
			for _, option := range []struct {
				label   string
				value   *bool
				checked bool
			}{{"按目录分组", &git.flatChanges, !git.flatChanges}, {"按仓库分组", &git.groupRepository, git.groupRepository},
				{"按模块分组", &git.groupModule, git.groupModule}, {"显示忽略文件", &git.showIgnored, git.showIgnored}} {
				item := items.Item(uiText(option.label)).Icon(blankMenuIcon)
				if option.checked {
					item.Icon(passwordCopiedIcon)
				}
				if item.Clicked() {
					c.After(func() {
						*option.value = !*option.value
						git.rows, git.collapsed = nil, nil
						if git.showIgnored && option.value == &git.showIgnored {
							m.startGit(gitListIgnored, "", nil)
						}
					})
				}
			}
		})
	})
}

func (status gitStatus) hasConflicts() bool {
	return slices.ContainsFunc(status.changes, gitChange.conflicted)
}

func (m *editorApp) gitCommitComposer(c ui.C, busy bool) {
	git := &m.tools.git
	selected := 0
	for _, change := range git.snapshot.changes {
		if git.commitSelected(change) {
			selected++
		}
	}
	generating := git.commitAI.request != nil
	ui.Column(c).FillWidth().Gap(7).PadTop(6).PadBottom(4).Children(func() {
		ui.Slot(c).FillWidth().Height(1).Background(m.theme.Border)
		ui.Row(c.Part("compose-tools")).FillWidth().Items(ui.Center).Gap(3).Children(func() {
			if ui.Checkbox(c, &git.amend, uiText("修改上次提交")).Disabled(busy || generating || !git.snapshot.hasCommit).Changed() && git.amend {
				if strings.TrimSpace(git.commit.Text()) == "" {
					c.After(func() { m.startGit(gitListMessages, "", nil) })
				}
			}
			ui.Slot(c).Grow(1)
			m.gitCommitAIAction(c.Part("ai"), busy, selected)
			recent := ui.Button(c, uiText("复用提交说明")).Icon(chevronDownIcon).Ghost().Height(28).PadX(5).FontSize(11).
				Tooltip(uiText("选择以前的提交说明，填入输入框")).Disabled(busy || generating || !git.snapshot.hasCommit)
			if recent.Clicked() {
				git.messageMenu = !git.messageMenu
				if git.messageMenu {
					c.After(func() { m.startGit(gitListMessages, "", nil) })
				}
			}
			ui.Menu(c, recent, &git.messageMenu, func(items *ui.MenuItems) {
				if len(git.commitMessages) == 0 {
					items.Item(uiText("暂无提交说明")).Disabled(true)
				}
				for _, message := range git.commitMessages {
					first, _, _ := strings.Cut(strings.TrimSpace(message), "\n")
					if trimmed := []rune(first); len(trimmed) > 42 {
						first = string(trimmed[:41]) + "…"
					}
					if items.Item(first).Clicked() {
						c.After(func() {
							git.commit.SetText(strings.TrimSpace(message))
							git.focusCommit = true
						})
					}
				}
			})
		})
		if git.commitAI.problem != "" {
			ui.Wrap(c, git.commitAI.problem).FillWidth().FontSize(12).Color(m.theme.Danger)
		}
		placeholder := uiText("输入提交说明")
		if generating {
			placeholder = uiText("正在生成提交说明…")
		}
		field := ui.TextArea(c, &git.commit, uiText("提交说明"), placeholder).FillWidth().Height(108).Disabled(busy || generating)
		if git.focusCommit && !busy {
			field.DrawOver(func(d *ui.DrawContext) {
				d.Canvas.RequestFocus(git.commit.ID)
				git.focusCommit = false
			})
		}
		_, composing := git.commit.GetEditor().Composition()
		message := strings.TrimSpace(git.commit.Text())
		ready := !busy && !generating && !composing && message != "" && !git.snapshot.hasConflicts() && (selected > 0 || git.amend && git.snapshot.hasCommit)
		canPush := git.remote != "" && git.snapshot.branch != "" && !strings.HasPrefix(git.snapshot.branch, "HEAD ")
		ui.Row(c).FillWidth().Gap(6).Items(ui.Center).Children(func() {
			commit := ui.Button(c.Part("commit-button"), uiText("提交")).Icon(gitCommitIcon).Height(34).PadX(14).Disabled(!ready)
			push := ui.Button(c.Part("push-button"), uiText("提交并推送")).Outline().Height(34).PadX(14).Disabled(!ready || !canPush)
			commitClicked, pushClicked := commit.Clicked(), push.Clicked()
			more := ui.Button(c.Part("more-commit"), uiText("更多提交操作")).Icon(chevronDownIcon).IconOnly().Ghost().Size(28, 34).Tooltip(uiText("更多提交操作")).Disabled(busy)
			if more.Clicked() {
				git.moreCommitMenu = !git.moreCommitMenu
			}
			ui.Menu(c, more, &git.moreCommitMenu, func(items *ui.MenuItems) {
				stagedReady := !busy && !generating && !composing && message != "" && git.snapshot.hasStaged() && !git.snapshot.hasConflicts()
				if items.Item(uiText("仅提交暂存区")).Icon(gitCommitIcon).Disabled(!stagedReady).Clicked() {
					c.After(func() {
						action := gitCommit
						if git.amend {
							action = gitAmend
						}
						m.startGit(action, message, nil)
					})
				}
			})
			if ready && (commitClicked || pushClicked) {
				c.After(func() {
					if pushClicked {
						git.pushAfterCommit, git.pushBranch = git.remote, git.snapshot.branch
					}
					action := gitCommitSelected
					if git.amend {
						action = gitAmendSelected
					}
					m.startGit(action, message, git.selectedCommitPaths())
				})
			}
		})
	})
}

func (m *editorApp) gitStashesView(c ui.C, busy bool) {
	git := &m.tools.git
	shelf := git.stashPage == "shelf"
	label := uiText("贮藏变更")
	if shelf {
		label = uiText("搁置变更")
	}
	ui.Row(c.Part("stash-tools")).FillWidth().Gap(4).Items(ui.Center).Children(func() {
		if ui.Button(c, label).Icon(plusIcon).Outline().Height(28).
			Disabled(busy || !git.snapshot.hasCommit || len(git.snapshot.changes) == 0 || git.snapshot.hasConflicts()).Clicked() {
			c.After(func() { git.stashConfirm = true })
		}
		ui.Slot(c).Grow(1)
		if ui.Button(c, uiText("刷新列表")).Icon(refreshIcon).IconOnly().Ghost().Size(28, 28).
			Tooltip(uiText("刷新列表")).Disabled(busy).Clicked() {
			c.After(func() { m.startGit(gitListStashes, "", nil) })
		}
	})
	type stashEntry struct{ ref, oid, message string }
	var entries []stashEntry
	for i := 0; i+2 < len(git.stashes); i += 3 {
		ref, oid, message := git.stashes[i], git.stashes[i+1], git.stashes[i+2]
		_, shelfMessage, shelved := strings.Cut(message, ": "+strings.TrimSpace(gitShelfPrefix))
		if shelved != shelf {
			continue
		}
		if shelved {
			message = strings.TrimSpace(shelfMessage)
			if message == "" {
				message = uiText("工作区改动")
			}
		}
		entries = append(entries, stashEntry{ref, oid, message})
	}
	if len(entries) == 0 {
		ui.Text(c, uiText("暂无记录")).Muted().FontSize(12).PadY(8)
		return
	}
	ui.List(c, &git.stashList, len(entries), func(row ui.ListRow) {
		entry := entries[row.Index]
		row.Element.FillWidth().Height(52).PadX(5).Gap(5).Items(ui.Center).Children(func() {
			ui.Column(row.C).Grow(1).MinWidth(0).Gap(2).Children(func() {
				ui.Text(row.C, entry.message).Bold().FontSize(12).Ellipsis()
				ui.Text(row.C, entry.ref).Muted().FontSize(11)
			})
			if ui.Button(row.C, uiText("应用")).Ghost().Height(26).Disabled(busy).Clicked() {
				row.C.After(func() { m.startGit(gitStashApply, entry.ref+"\x00"+entry.oid, nil) })
			}
			if ui.Button(row.C, uiText("弹出")).Ghost().Height(26).Disabled(busy).Clicked() {
				row.C.After(func() { m.startGit(gitStashPop, entry.ref+"\x00"+entry.oid, nil) })
			}
		})
	}).FillWidth().Grow(1).MinHeight(0).Label(label)
}

func (m *editorApp) gitStashDialog(c ui.C) {
	git := &m.tools.git
	if !git.stashConfirm {
		return
	}
	shelf := git.stashPage == "shelf"
	label := uiText("贮藏变更")
	if shelf {
		label = uiText("搁置变更")
	}
	submitted := false
	ui.ModalWith(c, &git.stashConfirm, label, ui.ModalSpec{Width: 382}, func() {
		ui.Wrap(c, uiText("收起已写入磁盘的工作区改动，包括未加入 Git 的文件。之后可以从列表重新应用。")).FillWidth().Muted()
		submitted = ui.TextField(c, &git.stashMessage, uiText("说明"), uiText("填写说明（可选）")).FillWidth().AutoFocus().Submitted()
	}, func() {
		if ui.Button(c, uiText("取消")).Ghost().Clicked() {
			c.After(func() { git.stashConfirm = false })
		}
		ready := git.request == nil && !git.snapshot.hasConflicts()
		if button := ui.Button(c, label).Disabled(!ready); ready && (button.Clicked() || submitted) {
			c.After(func() {
				git.stashConfirm = false
				message := strings.TrimSpace(git.stashMessage.Text())
				if shelf {
					message = gitShelfPrefix + message
				}
				m.startGit(gitStashSave, message, nil)
			})
		}
	})
}
