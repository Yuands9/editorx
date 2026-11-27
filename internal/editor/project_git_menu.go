package editor

import (
	"context"
	"path/filepath"
	"slices"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

type projectGitScope struct {
	path      string
	directory bool
	action    gitAction
}

const (
	gitSelectScope gitAction = "勾选提交"
	gitDiffScope   gitAction = "查看 Git 差异"
)

func (m *editorApp) projectGitActions(c ui.C, menu *ui.MenuItems, path string, directory bool) {
	project := m.projectRoot(path)
	if project == "" {
		return
	}
	location := m.gitRepositoryAt(path)
	menu.Sub("Git", func(items *ui.MenuItems) {
		if location != "" {
			m.projectGitRepositoryActions(c, items, location, path, directory)
			return
		}
		if directory {
			found := false
			for _, repository := range m.tools.repositories.byProject[project] {
				if gitPathWithin(path, repository) {
					found = true
					items.Sub(m.gitRepositoryLabel(repository), func(repoMenu *ui.MenuItems) {
						m.projectGitRepositoryActions(c, repoMenu, repository, repository, true)
					}).Icon(gitBranchIcon)
				}
			}
			if found {
				return
			}
			if m.tools.repositories.scan != nil {
				items.Label(uiText("正在查找项目中的 Git 仓库…"))
				return
			}
			m.projectGitRepositoryActions(c, items, path, path, true)
			return
		}
		m.projectGitRepositoryActions(c, items, filepath.Dir(path), path, false)
	}).Icon(gitBranchIcon)
}

func (m *editorApp) projectGitRepositoryActions(c ui.C, items *ui.MenuItems, location, path string, directory bool) {
	busy := m.tools.git.request != nil || m.tools.git.review != nil && m.tools.git.review.apply != nil
	if items.Item(uiText("提交记录…")).Icon(historyCompareIcon).Clicked() {
		c.After(func() { m.openGitLogAt(location, path, directory) })
	}
	if !directory && items.Item(uiText(string(gitDiffScope))).Icon(historyCompareIcon).Disabled(busy).Clicked() {
		c.After(func() { m.startProjectGitScope(location, projectGitScope{path, false, gitDiffScope}) })
	}
	items.Separator()
	for _, action := range []struct {
		kind  gitAction
		label string
		icon  gfx.Icon
	}{
		{gitSelectScope, "勾选提交", gitCommitIcon},
		{gitStage, "暂存变更", gitPullIcon},
		{gitUnstage, "取消暂存", exportIcon},
	} {
		if items.Item(uiText(action.label)).Icon(action.icon).Disabled(busy).Clicked() {
			c.After(func() { m.startProjectGitScope(location, projectGitScope{path, directory, action.kind}) })
		}
	}
	items.Separator()
	if items.Item(uiText("打开 Git 面板")).Icon(gitBranchIcon).Disabled(busy).Clicked() {
		c.After(func() { m.openProjectGit(location) })
	}
}

func (m *editorApp) startProjectGitScope(location string, scope projectGitScope) {
	git := &m.tools.git
	if git.request != nil || git.review != nil && git.review.apply != nil {
		return
	}
	m.showProjectGitPanel()
	if git.directory == "" {
		git.directory = location
	}
	git.issue, git.message = "", ""
	git.commitAI = gitCommitAIState{}
	git.projectScope, git.requestAction = scope, gitRefresh
	git.request = ui.NewEffect[gitResult, int](func(ctx context.Context) (gitResult, error) {
		return runProjectGit(ctx, location, gitRefresh, "", nil, nil), nil
	})
}

func gitChangeInScope(root, path string, directory bool, change gitChange) bool {
	for _, name := range change.paths() {
		absolute := filepath.Join(root, name)
		if directory && gitPathWithin(path, absolute) || !directory && strings.EqualFold(filepath.Clean(path), absolute) {
			return true
		}
	}
	return false
}

func (m *editorApp) applyProjectGitScope(scope projectGitScope) {
	git := &m.tools.git
	if git.snapshot.root == "" {
		git.issue = uiText("此目录还没有 Git 仓库")
		return
	}
	var paths []string
	for _, change := range git.snapshot.changes {
		if !gitChangeInScope(git.snapshot.root, scope.path, scope.directory, change) {
			continue
		}
		if scope.action == gitDiffScope {
			m.openGitChange(&gitChangeNode{change: change})
			return
		}
		if change.conflicted() {
			continue
		}
		switch scope.action {
		case gitSelectScope:
			if git.commitSelection == nil {
				git.commitSelection = make(map[string]bool)
			}
			git.commitSelection[change.path] = true
			paths = append(paths, change.path)
		case gitStage:
			if change.unstaged() {
				paths = append(paths, change.paths()...)
			}
		case gitUnstage:
			if change.staged() {
				paths = append(paths, change.paths()...)
			}
		}
	}
	if len(paths) == 0 {
		git.message = uiText("所选范围没有可操作的 Git 变更")
		return
	}
	if scope.action == gitSelectScope {
		git.stashPage, git.focusCommit = "", true
		return
	}
	slices.Sort(paths)
	m.startGit(scope.action, "", slices.Compact(paths))
}
