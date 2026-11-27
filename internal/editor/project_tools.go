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
	gitOpen, terminalOpen bool
	split, gitSplit       ui.SplitterState
	git                   gitPanelState
	repositories          projectGitRepositories
	terminal              *localShell
	terminals             []*localShell
}

type gitPanelState struct {
	directory, remote, message, issue                                              string
	snapshot                                                                       gitStatus
	request                                                                        *ui.EffectState[gitResult, int]
	commit, address                                                                ui.Field
	commitAI                                                                       gitCommitAIState
	commitProblem                                                                  string
	commitAttempt                                                                  *gitCommitAttempt
	focusCommit                                                                    bool
	tree                                                                           ui.TreeState
	branches                                                                       gitBranchDialogs
	fileHashes                                                                     map[string][32]byte
	review                                                                         *gitReviewState
	watch                                                                          gitWatchState
	conflict                                                                       *editorTab
	rows                                                                           []gitChangeRow
	rowsEnglish                                                                    bool
	collapsed                                                                      map[string]bool
	selectedFile                                                                   string
	commitSelection                                                                map[string]bool
	moreCommitMenu                                                                 bool
	requestAction                                                                  gitAction
	projectScope                                                                   projectGitScope
	flatChanges, groupRepository, groupModule, showIgnored, collapseAll, groupMenu bool
	ignored, commitMessages, stashes                                               []string
	amend, messageMenu, stashConfirm                                               bool
	stashPage                                                                      string
	stashList                                                                      ui.ListState
	stashMessage                                                                   ui.Field
	pushAfterCommit, pushBranch                                                    string
	pushingCommit                                                                  bool
}

func (m *editorApp) gitProjectDirectory() string {
	paths := m.gitProjects()
	if len(paths) == 0 {
		return ""
	}
	if slices.Contains(paths, m.tools.git.directory) {
		return m.tools.git.directory
	}
	if m.tools.repositories.scan != nil && m.tools.repositories.byProject == nil {
		return ""
	}
	if m.active != nil {
		if repository := m.gitRepositoryAt(m.active.doc.path); repository != "" {
			return repository
		}
		project := m.projectRoot(m.active.doc.path)
		for _, path := range paths {
			if m.projectRoot(path) == project {
				return path
			}
		}
	}
	return paths[0]
}

func (m *editorApp) toolDirectories() []string {
	paths := m.projectDirectories()
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
	returningFromVault := m.leavePasswordVault()
	if name == "Git" {
		if m.tools.gitOpen && !returningFromVault {
			m.tools.gitOpen = false
			m.focusActiveSource()
			return
		}
		m.openProjectGit(m.gitProjectDirectory())
		return
	}
	if name == "终端" {
		if m.tools.terminalOpen && !returningFromVault {
			m.tools.terminalOpen = false
			m.focusActiveSource()
			return
		}
		m.tools.terminalOpen = true
		if m.tools.terminal == nil {
			m.openLocalShell(m.toolDirectory())
		} else {
			m.tools.terminal.wantFocus = true
		}
	}
}

func (m *editorApp) showProjectGitPanel() {
	m.leavePasswordVault()
	if m.ai.open {
		m.toggleAIPanel()
	}
	m.translation, m.templatePanel = nil, nil
	if !m.tools.gitOpen {
		m.tools.gitOpen = true
		if m.tools.gitSplit.Ratio == 0 {
			m.tools.gitSplit = m.newAssetSplit()
		}
	}
}

func (m *editorApp) openProjectGit(path string) {
	git := &m.tools.git
	m.showProjectGitPanel()
	if git.request != nil {
		return
	}
	if git.directory != path {
		git.watch.close()
		*git = gitPanelState{directory: path}
	}
	if path != "" {
		m.startGit(gitRefresh, "", nil)
	}
}

func (m *editorApp) startGit(action gitAction, input string, paths []string) {
	git := &m.tools.git
	if git.request != nil || git.directory == "" || git.review != nil && git.review.apply != nil {
		return
	}
	if action != gitRefresh {
		git.commitAI.request, git.commitAI.paths, git.commitAI.original = nil, nil, ""
	}
	directory := git.directory
	var openFiles []string
	if action.updatesOpenFiles() {
		git.fileHashes = make(map[string][32]byte)
		for _, tab := range m.tabs {
			owner := m.gitRepositoryAt(tab.doc.path)
			if owner != "" && !strings.EqualFold(owner, git.snapshot.root) {
				continue
			}
			if aiTextTab(tab) && !tab.modified() && !tab.hasResultDraft() && tab.doc.path != "" {
				openFiles = append(openFiles, tab.doc.path)
				git.fileHashes[tab.doc.path] = tab.doc.diskHash
			}
		}
	}
	git.requestAction = action
	if action == gitListMessages {
		git.commitProblem = ""
	} else {
		git.issue, git.message = "", ""
		if action != gitRefresh {
			git.message = fmt.Sprintf(uiText("正在执行 %s…"), uiText(string(action)))
		}
	}
	git.request = ui.NewEffect[gitResult, int](func(ctx context.Context) (gitResult, error) {
		return runProjectGit(ctx, directory, action, input, paths, openFiles), nil
	})
}

func (m *editorApp) updateProjectGit(c ui.C) {
	git := &m.tools.git
	m.updateGitCommitAI(c.Part("git-commit-ai"))
	m.updateGitWatch(c.Part("git-watch"))
	if git.request == nil {
		return
	}
	ui.Effect(c, git.request, 0)
	result, err, done := git.request.Take()
	if !done {
		return
	}
	git.request = nil
	if attempt := git.commitAttempt; attempt != nil && !attempt.open {
		if result.commitWarning != "" {
			attempt.warning, attempt.open = result.commitWarning, true
			return
		}
		git.commitAttempt = nil
		if result.commitProblem != "" {
			git.commitProblem = result.commitProblem
			m.checkFiles = true
			m.invalidator.Invalidate()
			return
		}
		if result.committed && attempt.pushRemote != "" {
			git.pushAfterCommit, git.pushBranch = attempt.pushRemote, attempt.branch
		}
	}
	scope := git.projectScope
	git.projectScope = projectGitScope{}
	fileHashes := git.fileHashes
	git.fileHashes = nil
	if err != nil {
		git.issue = err.Error()
		return
	}
	if result.action == gitListMessages {
		git.commitProblem = result.fail
		git.commitMessages = result.items
		if git.amend && strings.TrimSpace(git.commit.Text()) == "" && len(git.commitMessages) > 0 {
			git.commit.SetText(strings.TrimSpace(git.commitMessages[0]))
			git.focusCommit = true
		}
		return
	}
	if scope.path != "" && result.statusValid && result.status.root == "" {
		git.issue = uiText("此目录还没有 Git 仓库")
		return
	}
	if scope.path != "" && result.statusValid && result.status.root != "" {
		project := m.projectRoot(scope.path)
		if project == "" {
			return
		}
		directory := project
		if gitPathWithin(project, result.status.root) {
			directory = result.status.root
		}
		if !strings.EqualFold(git.directory, directory) || !strings.EqualFold(git.snapshot.root, result.status.root) {
			git.watch.close()
			*git = gitPanelState{directory: directory}
		}
	}
	git.issue, git.message = result.fail, ""
	switch result.action {
	case gitListIgnored:
		git.ignored, git.rows = result.items, nil
		return
	case gitListStashes:
		git.stashes = result.items
		git.stashList = ui.ListState{}
		return
	}
	if result.action == gitListBranches {
		git.branches.available = result.branches
		git.branches.list = ui.ListState{}
		return
	}
	if result.statusValid {
		git.snapshot, git.rows = result.status, nil
		if project := m.projectRoot(git.directory); project != "" && gitPathWithin(project, result.status.root) && gitPathWithin(result.status.root, git.directory) {
			git.directory = result.status.root
		}
	}
	if result.statusValid && len(git.commitSelection) > 0 {
		present := make(map[string]bool, len(git.snapshot.changes))
		for _, change := range git.snapshot.changes {
			present[change.path] = true
		}
		for path := range git.commitSelection {
			if !present[path] {
				delete(git.commitSelection, path)
			}
		}
	}
	m.invalidator.Invalidate()
	if !slices.Contains(git.snapshot.remotes, git.remote) {
		git.remote = ""
		if len(git.snapshot.remotes) > 0 {
			git.remote = git.snapshot.remotes[0]
		}
		if slices.Contains(git.snapshot.remotes, "origin") {
			git.remote = "origin"
		}
	}
	if result.action == gitPush && git.pushingCommit {
		git.pushingCommit = false
		if result.fail != "" {
			git.issue = uiText("已提交，但推送失败：") + result.fail
		}
	}
	if result.fail != "" {
		if result.action == gitCommit || result.action == gitAmend || result.action == gitCommitSelected || result.action == gitAmendSelected {
			git.pushAfterCommit, git.pushBranch = "", ""
			if result.committed {
				git.commit.SetText("")
				git.amend, git.commitMessages = false, nil
			}
		}
		return
	}
	if result.action == gitCommit || result.action == gitAmend || result.action == gitCommitSelected || result.action == gitAmendSelected {
		git.commit.SetText("")
		git.amend, git.commitMessages = false, nil
		remote, branch := git.pushAfterCommit, git.pushBranch
		git.pushAfterCommit, git.pushBranch = "", ""
		if remote != "" {
			if branch == result.status.branch && slices.Contains(result.status.remotes, remote) {
				git.pushingCommit = true
				m.startGit(gitPush, remote, nil)
				return
			}
			git.issue = uiText("已提交。分支或远程仓库已更改，请确认后手动推送。")
		}
	}
	if scope.path != "" {
		m.applyProjectGitScope(scope)
		return
	}
	if result.action == gitAddRemote {
		git.address.SetText("")
	}
	if result.action == gitInit {
		m.tools.repositories.refresh = true
	}
	if result.action == gitCreateBranch {
		git.commitSelection = nil
		git.branches.creating = false
	}
	if result.action == gitSwitchBranch {
		git.commitSelection = nil
		git.branches.switching = false
	}
	if result.action.updatesOpenFiles() {
		m.refreshGitFiles(result, fileHashes)
	}
	if result.action != gitRefresh {
		git.message = fmt.Sprintf(uiText("%s已完成"), uiText(string(result.action)))
	}
	if git.stashPage != "" && (result.action == gitStashSave || result.action == gitStashApply || result.action == gitStashPop) {
		git.stashMessage.SetText("")
		m.startGit(gitListStashes, "", nil)
	} else if git.showIgnored && (result.action == gitRefresh || result.action == gitStage || result.action == gitStageAll || result.action == gitUnstage) {
		m.startGit(gitListIgnored, "", nil)
	}
}

func (m *editorApp) terminalPanel(c ui.C, blocked bool) {
	ui.Column(c).FillWidth().Grow(1).MinHeight(0).MarginX(workspaceInset).MarginBottom(workspaceInset).
		Background(m.theme.Background).Radius(12).Clip().Children(func() {
		ui.Row(c).FillWidth().Height(34).PadX(8).Gap(4).Items(ui.Center).Children(func() {
			ui.Text(c, uiText("终端")).Bold().FontSize(12)
			if m.tools.terminal != nil {
				m.shellActions(c.Part("shell-controls"), blocked)
			} else {
				ui.Slot(c).Grow(1)
			}
			if ui.Button(c, uiText("收起底部面板")).Icon(closeIcon).IconOnly().Ghost().Size(28, 28).Disabled(blocked).Clicked() {
				c.After(func() { m.tools.terminalOpen = false; m.focusActiveSource() })
			}
		})
		ui.Slot(c).FillWidth().Height(1).Background(m.theme.Border)
		m.shellPanel(c.Part("terminal"), blocked)
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
		paths := m.gitProjects()
		if git.directory == "" {
			ui.Row(c).Gap(8).Items(ui.Center).Children(func() {
				label := uiText("添加项目后可查看 Git 状态")
				if m.tools.repositories.scan != nil {
					label = uiText("正在查找项目中的 Git 仓库…")
				}
				if git.issue != "" {
					label = git.issue
				}
				ui.Text(c, label).Muted().FontSize(12)
				if ui.Button(c, uiText("添加项目")).Outline().Height(28).Disabled(busy).Clicked() {
					c.After(m.openFolder)
				}
			})
			return
		}
		ui.Row(c).FillWidth().Items(ui.Center).Gap(5).Children(func() {
			selected := git.directory
			if !slices.Contains(paths, selected) {
				paths = append(paths, selected)
			}
			if ui.Select(c, &selected, paths, m.gitRepositoryLabel).Label(uiText("Git 项目")).Grow(1).MinWidth(0).Height(28).Disabled(busy).Changed() {
				c.After(func() { m.openProjectGit(selected) })
			}
			if ui.Button(c, uiText("刷新 Git 状态")).Icon(refreshIcon).IconOnly().Ghost().Size(28, 28).Disabled(busy).Clicked() {
				c.After(func() {
					m.tools.repositories.refresh = true
					m.startGit(gitRefresh, "", nil)
				})
			}
			if ui.Button(c, uiText("提交记录")).Icon(historyCompareIcon).Ghost().Height(28).PadX(6).FontSize(12).Tooltip(uiText("浏览 Git 提交记录")).Disabled(busy || snapshot.root == "").Clicked() {
				c.After(m.openGitLog)
			}
		})
		if git.issue != "" {
			ui.Wrap(c, git.issue).FillWidth().MaxHeight(52).Clip().Color(m.theme.Danger).FontSize(12)
		} else if git.message != "" {
			ui.Text(c, git.message).Muted().FontSize(11).Ellipsis()
		}
		if snapshot.root == "" {
			if git.request == nil {
				git.focusCommit = false
			}
			if git.request == nil && git.issue == "" {
				ui.Row(c).Gap(8).Items(ui.Center).Children(func() {
					ui.Text(c, uiText("此项目尚未初始化 Git")).Muted().FontSize(12)
					if ui.Button(c, uiText("初始化仓库")).Outline().Height(28).Disabled(busy).Clicked() {
						c.After(func() { m.startGit(gitInit, "", nil) })
					}
				})
			}
			return
		}
		m.gitRemoteControls(c.Part("remotes"), busy)
		m.gitConflictToolbar(c.Part("conflicts"), busy)
		m.gitCommitTools(c.Part("changes-toolbar"), busy)
		if git.stashPage != "" {
			m.gitStashesView(c.Part("stashes"), busy)
			return
		}
		m.gitChangesView(c.Part("changes"), blocked || git.projectScope.path != "" || git.request != nil && git.requestAction != gitRefresh)
		m.gitCommitComposer(c.Part("commit"), busy)
	})
}

func (m *editorApp) gitRemoteControls(c ui.C, busy bool) {
	git := &m.tools.git
	status := git.snapshot
	ui.Row(c).FillWidth().Gap(5).Items(ui.Center).Children(func() {
		m.gitBranchControls(c.Part("branches"), busy)
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
			if ui.Button(c.Key(label), uiText(label)).Icon(command.icon).IconOnly().Ghost().Size(26, 29).Shrink(0).
				Tooltip(uiText(command.hint)).Disabled(busy || command.disabled).Clicked() {
				c.After(func() { m.startGit(command.action, git.remote, nil) })
			}
		}
	})
	if len(status.remotes) > 1 {
		ui.Select(c.Part("remote"), &git.remote, status.remotes, func(s string) string { return s }).Label(uiText("Git 远程")).FillWidth().Height(29).Disabled(busy)
	}
	if len(status.remotes) == 0 {
		ui.Row(c.Part("add-remote")).FillWidth().Gap(7).Items(ui.Center).Children(func() {
			link := ui.TextField(c, &git.address, uiText("远程仓库地址"), uiText("远程仓库地址")).Height(27).Grow(1).MinWidth(70).Disabled(busy)
			canAdd := !busy && strings.TrimSpace(git.address.Text()) != ""
			button := ui.Button(c, uiText("添加远程")).Outline().Height(27).FontSize(12).Disabled(!canAdd)
			if canAdd && (button.Clicked() || link.Submitted()) {
				c.After(func() { m.startGit(gitAddRemote, git.address.Text(), nil) })
			}
		})
	}
}
