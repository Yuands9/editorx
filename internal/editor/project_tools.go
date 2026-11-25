package editor

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

var gitBranchIcon = gfx.Icon{ViewBox: 24, Stroke: 1.8, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(6, 7), gfx.LineTo(6, 16), gfx.MoveTo(6, 13), gfx.CubicTo(15, 13, 17, 11, 17, 7),
	gfx.MoveTo(8, 5), gfx.QuadTo(6, 1, 4, 5), gfx.QuadTo(6, 9, 8, 5),
	gfx.MoveTo(8, 18), gfx.QuadTo(6, 14, 4, 18), gfx.QuadTo(6, 22, 8, 18),
	gfx.MoveTo(19, 5), gfx.QuadTo(17, 1, 15, 5), gfx.QuadTo(17, 9, 19, 5),
}}

var gitFetchIcon = gfx.Icon{ViewBox: 24, Stroke: 1.7, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(5, 4), gfx.LineTo(19, 4),
	gfx.MoveTo(12, 8), gfx.LineTo(12, 19), gfx.MoveTo(7, 14), gfx.LineTo(12, 19), gfx.LineTo(17, 14),
}}

var gitPullIcon = gfx.Icon{ViewBox: 24, Stroke: 1.7, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(12, 3), gfx.LineTo(12, 14), gfx.MoveTo(8, 10), gfx.LineTo(12, 14), gfx.LineTo(16, 10),
	gfx.MoveTo(4, 15), gfx.LineTo(4, 20), gfx.LineTo(20, 20), gfx.LineTo(20, 15),
}}

var gitCommitIcon = gfx.Icon{ViewBox: 24, Stroke: 1.7, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(3, 12), gfx.LineTo(8, 12), gfx.MoveTo(16, 12), gfx.LineTo(21, 12),
	gfx.MoveTo(16, 12), gfx.CubicTo(16, 17.3, 8, 17.3, 8, 12), gfx.CubicTo(8, 6.7, 16, 6.7, 16, 12),
}}

var shellIcon = gfx.Icon{ViewBox: 24, Stroke: 1.8, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(3, 5), gfx.LineTo(21, 5), gfx.LineTo(21, 19), gfx.LineTo(3, 19), gfx.ClosePath(),
	gfx.MoveTo(6, 9), gfx.LineTo(9, 12), gfx.LineTo(6, 15), gfx.MoveTo(12, 15), gfx.LineTo(17, 15),
}}

type projectTools struct {
	active    string
	split     ui.SplitterState
	git       gitPanelState
	terminal  *localShell
	terminals []*localShell
}

type gitPanelState struct {
	directory, remote, message, issue string
	snapshot                          gitStatus
	request                           *ui.EffectState[gitResult, int]
	commit, address                   ui.Field
	list                              ui.ListState
	branches                          gitBranchDialogs
	fileHashes                        map[string][32]byte
}

func (m *editorApp) toolDirectories() []string {
	var paths []string
	if m.workspace != nil {
		for _, root := range m.workspace.roots {
			paths = append(paths, root.path)
		}
	}
	if active := m.active; active != nil && active.doc.path != "" && !slices.ContainsFunc(paths, func(path string) bool {
		return gitPathWithin(path, active.doc.path)
	}) {
		paths = append(paths, filepath.Dir(active.doc.path))
	}
	return paths
}

func (m *editorApp) toolDirectory() string {
	paths := m.toolDirectories()
	if len(paths) == 0 {
		return ""
	}
	if m.active != nil {
		best := ""
		for _, path := range paths {
			if len(path) > len(best) && gitPathWithin(path, m.active.doc.path) {
				best = path
			}
		}
		if best != "" {
			return best
		}
	}
	return paths[0]
}

func (m *editorApp) useTool(name string) {
	if m.tools.active == name {
		m.tools.active = ""
		m.focusActiveSource()
		return
	}
	m.tools.active = name
	if name == "Git" && m.tools.git.request == nil {
		dir := m.tools.git.directory
		if dir == "" {
			dir = m.toolDirectory()
		}
		m.openProjectGit(dir)
	}
	if name == "终端" {
		if m.tools.terminal == nil {
			m.openLocalShell(m.toolDirectory())
		} else {
			m.tools.terminal.wantFocus = true
		}
	}
}

func (m *editorApp) openProjectGit(path string) {
	git := &m.tools.git
	m.tools.active = "Git"
	if git.request != nil {
		return
	}
	if git.directory != path {
		*git = gitPanelState{directory: path}
	}
	if path != "" {
		m.startGit(gitRefresh, "", nil)
	}
}

func (m *editorApp) startGit(action gitAction, input string, paths []string) {
	git := &m.tools.git
	if git.request != nil || git.directory == "" {
		return
	}
	directory := git.directory
	var openFiles []string
	if action.updatesOpenFiles() {
		git.fileHashes = make(map[string][32]byte)
		for _, tab := range m.tabs {
			if aiTextTab(tab) && !tab.modified() && !tab.hasResultDraft() && tab.doc.path != "" {
				openFiles = append(openFiles, tab.doc.path)
				git.fileHashes[tab.doc.path] = tab.doc.diskHash
			}
		}
	}
	git.issue, git.message = "", "正在"+string(action)+"…"
	git.request = ui.NewEffect[gitResult, int](func(ctx context.Context) (gitResult, error) {
		return runProjectGit(ctx, directory, action, input, paths, openFiles), nil
	})
}

func (m *editorApp) updateProjectGit(c ui.C) {
	git := &m.tools.git
	if git.request == nil {
		return
	}
	ui.Effect(c, git.request, 0)
	result, err, done := git.request.Take()
	if !done {
		return
	}
	git.request = nil
	fileHashes := git.fileHashes
	git.fileHashes = nil
	if err != nil {
		git.issue = err.Error()
		return
	}
	git.issue, git.message = result.fail, ""
	if result.action == gitListBranches {
		git.branches.available = result.branches
		git.branches.list = ui.ListState{}
		return
	}
	git.snapshot = result.status
	if !slices.Contains(git.snapshot.remotes, git.remote) {
		git.remote = ""
		if len(git.snapshot.remotes) > 0 {
			git.remote = git.snapshot.remotes[0]
		}
		if slices.Contains(git.snapshot.remotes, "origin") {
			git.remote = "origin"
		}
	}
	if result.fail != "" {
		return
	}
	if result.action == gitCommit {
		git.commit.SetText("")
	}
	if result.action == gitAddRemote {
		git.address.SetText("")
	}
	if result.action == gitCreateBranch {
		git.branches.creating = false
	}
	if result.action == gitSwitchBranch {
		git.branches.switching = false
	}
	if result.action.updatesOpenFiles() {
		m.refreshGitFiles(result, fileHashes)
	}
	if result.action != gitRefresh {
		git.message = string(result.action) + "完成"
	}
}

func (m *editorApp) projectToolsPanel(c ui.C, blocked bool) {
	ui.Column(c).FillWidth().Grow(1).MinHeight(0).MarginX(workspaceInset).MarginBottom(workspaceInset).
		Background(m.theme.Background).Radius(12).Clip().Children(func() {
		ui.Row(c).FillWidth().Height(34).PadX(8).Gap(4).Items(ui.Center).Children(func() {
			for _, mode := range []string{"Git", "终端"} {
				button := ui.Button(c.Key(mode), mode).Ghost().Height(28).FontSize(12).Disabled(blocked)
				if m.tools.active == mode {
					button.Secondary()
				}
				if button.Clicked() && m.tools.active != mode {
					c.After(func() { m.useTool(mode) })
				}
			}
			if m.tools.active == "终端" && m.tools.terminal != nil {
				m.shellActions(c.Part("shell-controls"), blocked)
			} else {
				ui.Slot(c).Grow(1)
			}
			if ui.Button(c, "收起底部面板").Icon(closeIcon).IconOnly().Ghost().Size(28, 28).Disabled(blocked).Clicked() {
				c.After(func() { m.tools.active = ""; m.focusActiveSource() })
			}
		})
		ui.Slot(c).FillWidth().Height(1).Background(m.theme.Border)
		if m.tools.active == "Git" {
			m.gitPanel(c.Part("git"), blocked)
		} else {
			m.shellPanel(c.Part("terminal"), blocked)
		}
	})
}

func (m *editorApp) refreshGitFiles(result gitResult, fileHashes map[string][32]byte) {
	unchangedTab := func(path string) *editorTab {
		tab := m.tabAtPath(path)
		hash, tracked := fileHashes[path]
		if !tracked || !aiTextTab(tab) || tab.doc.diskHash != hash || tab.modified() || tab.hasResultDraft() {
			return nil
		}
		if _, composing := tab.source.text.Composition(); composing {
			return nil
		}
		return tab
	}
	for path, doc := range result.updated {
		tab := unchangedTab(path)
		if tab == nil || doc.diskHash == tab.doc.diskHash {
			continue
		}
		caret := tab.source.text.Caret()
		tab.doc = doc
		tab.source.replace(doc.saved)
		tab.source.text.MoveTo(min(caret, tab.source.text.RuneCount()), false)
		tab.search.refresh()
		m.results.dirty = true
	}
	for _, path := range result.missing {
		if tab := unchangedTab(path); tab != nil {
			tab.name = tab.title()
			tab.doc.path, tab.doc.saved, tab.doc.diskHash = "", "", [32]byte{}
			m.results.dirty = true
		}
	}
	if m.workspace != nil {
		for _, root := range m.workspace.roots {
			if root.loaded && (gitPathWithin(result.status.root, root.path) || gitPathWithin(root.path, result.status.root)) {
				m.refreshProject(root, "")
			}
		}
	}
}

func (m *editorApp) gitPanel(c ui.C, blocked bool) {
	git := &m.tools.git
	busy := blocked || git.request != nil
	snapshot := git.snapshot
	ui.Column(c).FillWidth().Grow(1).MinHeight(0).PadX(11).PadY(5).Gap(4).Children(func() {
		paths := m.toolDirectories()
		if git.directory != "" && !slices.Contains(paths, git.directory) {
			paths = append(paths, git.directory)
		}
		if git.directory == "" {
			if len(paths) != 0 && !busy {
				c.After(func() { m.openProjectGit(m.toolDirectory()) })
			}
			ui.Row(c).Gap(8).Items(ui.Center).Children(func() {
				ui.Text(c, "添加项目后可查看 Git 状态").Muted().FontSize(12)
				if ui.Button(c, "添加项目").Outline().Height(28).Disabled(busy).Clicked() {
					c.After(m.openFolder)
				}
			})
			return
		}
		ui.Row(c).FillWidth().Items(ui.Center).Gap(8).Children(func() {
			selected := git.directory
			if ui.Select(c, &selected, paths, filepath.Base).Label("Git 项目").Width(175).Height(28).Disabled(busy).Changed() {
				c.After(func() { m.openProjectGit(selected) })
			}
			if snapshot.root != "" {
				m.gitBranchControls(c.Part("branches"), busy)
			}
			ui.Text(c, snapshot.tracking).FontSize(11).Muted().Grow(1).MinWidth(0).Ellipsis()
			if ui.Button(c, "刷新 Git 状态").Icon(refreshIcon).IconOnly().Ghost().Size(28, 28).Disabled(busy).Clicked() {
				c.After(func() { m.startGit(gitRefresh, "", nil) })
			}
		})
		if git.issue != "" {
			ui.Wrap(c, git.issue).FillWidth().MaxHeight(52).Clip().Color(m.theme.Danger).FontSize(12)
		} else if git.message != "" {
			ui.Text(c, git.message).Muted().FontSize(11).Ellipsis()
		}
		if snapshot.root == "" {
			if git.request == nil && git.issue == "" {
				ui.Row(c).Gap(8).Items(ui.Center).Children(func() {
					ui.Text(c, "此项目尚未初始化 Git").Muted().FontSize(12)
					if ui.Button(c, "初始化仓库").Outline().Height(28).Disabled(busy).Clicked() {
						c.After(func() { m.startGit(gitInit, "", nil) })
					}
				})
			}
			return
		}
		m.gitToolbar(c.Part("commands"), busy)
		if len(snapshot.changes) == 0 {
			ui.Text(c, "工作区无变更").Muted().FontSize(12).PadY(7)
			return
		}
		ui.List(c, &git.list, len(snapshot.changes), func(row ui.ListRow) {
			change := snapshot.changes[row.Index]
			place := row.C
			row.Element.Height(29).PadX(4).Gap(8).Items(ui.Center).Children(func() {
				code := change.status
				if code == "??" {
					code = "未跟踪"
				}
				ui.Text(place, code).Width(48).FontSize(12).Color(m.theme.Ring)
				name := ui.Row(place.Part("open-file")).Grow(1).MinWidth(0).Height(25).Items(ui.Center).PadX(6).Radius(6)
				name.HitWith(ui.ID(place.Part("open-file")), change.path, gfx.HitOptions{Role: gfx.RoleButton, Disabled: blocked}).
					Hover(ui.Look{}.Background(m.theme.Hover)).Tooltip(change.path)
				name.Children(func() { ui.Text(place, change.path).FontSize(12).Grow(1).MinWidth(0).Ellipsis() })
				if name.Clicked() {
					c.After(func() { m.load(filepath.Join(snapshot.root, change.path)) })
				}
				for _, action := range []gitAction{gitStage, gitUnstage} {
					if action == gitStage && change.unstaged() || action == gitUnstage && change.staged() {
						if ui.Button(place.Key(string(action)), string(action)).Ghost().Height(25).FontSize(11).Disabled(busy).Clicked() {
							c.After(func() { m.startGit(action, "", change.paths()) })
						}
					}
				}
			})
		}).FillWidth().Grow(1).MinHeight(0).Label("Git 文件变更")
	})
}

func (m *editorApp) gitToolbar(c ui.C, busy bool) {
	git := &m.tools.git
	status := git.snapshot
	ui.Row(c).FillWidth().Gap(5).Items(ui.Center).Children(func() {
		if ui.Button(c, "全部暂存").Icon(plusIcon).IconOnly().Ghost().Size(30, 29).Shrink(0).
			Tooltip("全部暂存").Disabled(busy || len(status.changes) == 0).Clicked() {
			c.After(func() { m.startGit(gitStageAll, "", nil) })
		}
		message := ui.TextField(c, &git.commit, "提交说明", "提交说明").Height(27).Grow(1).MinWidth(85).Disabled(busy)
		canCommit := !busy && status.hasStaged() && strings.TrimSpace(git.commit.Text()) != ""
		submit := ui.Button(c, "提交").Icon(gitCommitIcon).IconOnly().Size(30, 29).Shrink(0).
			Tooltip("提交已暂存的变更").Disabled(!canCommit)
		if canCommit && (submit.Clicked() || message.Submitted()) {
			c.After(func() { m.startGit(gitCommit, git.commit.Text(), nil) })
		}
		ui.Text(c, fmt.Sprintf("%d 个变更", len(status.changes))).Muted().FontSize(11)
		if len(status.remotes) > 1 {
			ui.Select(c, &git.remote, status.remotes, func(s string) string { return s }).Label("Git 远程").Width(106).Height(29).Disabled(busy)
		}
		for _, command := range []struct {
			action   gitAction
			icon     gfx.Icon
			hint     string
			disabled bool
		}{
			{gitFetch, gitFetchIcon, "获取远程更新", len(status.remotes) == 0},
			{gitPull, gitPullIcon, "拉取并快进合并", status.tracking == ""},
			{gitPush, exportIcon, "推送当前分支", git.remote == "" || status.branch == "" || strings.HasPrefix(status.branch, "HEAD ")},
		} {
			label := string(command.action)
			if ui.Button(c.Key(label), label).Icon(command.icon).IconOnly().Ghost().Size(30, 29).Shrink(0).
				Tooltip(command.hint).Disabled(busy || command.disabled).Clicked() {
				c.After(func() { m.startGit(command.action, git.remote, nil) })
			}
		}
	})
	if len(status.remotes) == 0 {
		ui.Row(c.Part("add-remote")).FillWidth().Gap(7).Items(ui.Center).Children(func() {
			link := ui.TextField(c, &git.address, "远程仓库地址", "远程仓库地址").Height(27).Grow(1).MinWidth(70).Disabled(busy)
			canAdd := !busy && strings.TrimSpace(git.address.Text()) != ""
			button := ui.Button(c, "添加远程").Outline().Height(27).FontSize(12).Disabled(!canAdd)
			if canAdd && (button.Clicked() || link.Submitted()) {
				c.After(func() { m.startGit(gitAddRemote, git.address.Text(), nil) })
			}
		})
	}
}
