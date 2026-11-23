package editor

import (
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

var gitNewBranchIcon = gfx.Icon{ViewBox: 24, Stroke: 1.7, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(6, 7), gfx.LineTo(6, 16), gfx.MoveTo(6, 13), gfx.CubicTo(11, 13, 13, 11, 14, 9),
	gfx.MoveTo(8, 5), gfx.QuadTo(6, 1, 4, 5), gfx.QuadTo(6, 9, 8, 5),
	gfx.MoveTo(8, 18), gfx.QuadTo(6, 14, 4, 18), gfx.QuadTo(6, 22, 8, 18),
	gfx.MoveTo(18, 3), gfx.LineTo(18, 10), gfx.MoveTo(14.5, 6.5), gfx.LineTo(21.5, 6.5),
}}

type gitBranchDialogs struct {
	creating, switching bool
	focusName           bool
	name                ui.Field
	available           []string
	list                ui.ListState
}

func (m *editorApp) gitBranchControls(c ui.C, busy bool) {
	git := &m.tools.git
	label := git.snapshot.branch
	if label == "" {
		label = "分支"
	}
	context := c.Part("switch")
	control := ui.Row(context).MaxWidth(194).Height(28).PadX(7).Gap(5).Radius(7).Items(ui.Center)
	control.HitWith(ui.ID(context), "切换分支", gfx.HitOptions{Role: gfx.RoleButton, Value: label, Disabled: busy}).
		Hover(ui.Look{}.Background(m.theme.Hover)).Focus(ui.Look{}.Background(m.theme.Hover)).Tooltip("切换分支")
	control.Children(func() {
		ui.Icon(c, gitBranchIcon).Size(14, 14).Shrink(0).Color(m.theme.Ring)
		ui.Text(c, label).FontSize(12).MinWidth(0).Ellipsis()
		ui.Icon(c, chevronDownIcon).Size(10, 10).Shrink(0).Color(m.theme.MutedForeground)
	})
	if control.Clicked() {
		c.After(func() {
			git.branches.switching = true
			git.branches.available = nil
			m.startGit(gitListBranches, "", nil)
		})
	}
	if ui.Button(c.Part("create"), "新建分支").Icon(gitNewBranchIcon).IconOnly().
		Ghost().Size(28, 28).Shrink(0).Tooltip("新建分支").Disabled(busy).Clicked() {
		c.After(func() {
			git.branches.creating, git.branches.focusName = true, true
			git.branches.name.SetText("")
			git.issue = ""
		})
	}
}

func (m *editorApp) gitBranchDialog(c ui.C) {
	git := &m.tools.git
	if git.branches.creating {
		m.gitCreateBranchDialog(c.Part("create"))
	}
	if git.branches.switching {
		m.gitSwitchBranchDialog(c.Part("switch"))
	}
}

func (m *editorApp) gitCreateBranchDialog(c ui.C) {
	git := &m.tools.git
	state := &git.branches
	busy := git.request != nil
	submitted := false
	ui.ModalWith(c, &state.creating, "新建分支", ui.ModalSpec{Width: 382}, func() {
		field := ui.TextField(c, &state.name, "分支名称", "输入分支名称").FillWidth().Disabled(busy)
		if state.focusName {
			field.AutoFocus()
			state.focusName = false
		}
		submitted = field.Submitted()
		if git.issue != "" {
			ui.Wrap(c, git.issue).FillWidth().Color(m.theme.Danger).FontSize(12)
		}
	}, func() {
		if ui.Button(c, "取消").Ghost().Clicked() {
			c.After(func() { state.creating = false })
		}
		name := strings.TrimSpace(state.name.Text())
		_, composing := state.name.GetEditor().Composition()
		ready := !busy && !composing && name != ""
		if ui.Button(c, "创建并切换").Disabled(!ready).Clicked() || ready && submitted {
			c.After(func() { m.startGit(gitCreateBranch, name, nil) })
		}
	})
}

func (m *editorApp) gitSwitchBranchDialog(c ui.C) {
	git := &m.tools.git
	state := &git.branches
	busy := git.request != nil
	ui.ModalWith(c, &state.switching, "切换分支", ui.ModalSpec{Width: 382}, func() {
		if git.issue != "" {
			ui.Wrap(c, git.issue).FillWidth().Color(m.theme.Danger).FontSize(12)
		}
		if len(state.available) == 0 {
			if busy {
				ui.Text(c, "正在读取本地分支…").Muted()
			} else if git.issue == "" {
				ui.Text(c, "暂无可切换的本地分支").Muted()
			}
			return
		}
		height := min(float32(270), float32(len(state.available))*34)
		ui.List(c, &state.list, len(state.available), func(row ui.ListRow) {
			name := state.available[row.Index]
			selected := name == git.snapshot.branch
			item := row.Element.Height(34).PadX(8).Gap(6).Items(ui.Center).Radius(7).
				HitWith(ui.ID(row.C), name, gfx.HitOptions{Role: gfx.RoleButton, Disabled: busy || selected}).
				Hover(ui.Look{}.Background(m.theme.Hover)).Focus(ui.Look{}.Background(m.theme.Hover))
			if selected {
				item.Background(m.theme.Selection)
			}
			item.Children(func() {
				ui.Icon(row.C, gitBranchIcon).Size(14, 14).Color(m.theme.MutedForeground)
				ui.Text(row.C, name).Grow(1).MinWidth(0).Ellipsis()
				if selected {
					ui.Text(row.C, "当前分支").FontSize(11).Muted().Shrink(0)
				}
			})
			if item.Clicked() {
				c.After(func() { m.startGit(gitSwitchBranch, name, nil) })
			}
		}).FillWidth().Height(height).Label("本地分支")
	}, nil)
}
