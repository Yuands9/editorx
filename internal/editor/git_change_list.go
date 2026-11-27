package editor

import (
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/vivaui/viva/ui"
)

type gitChangeGroup struct {
	id, label string
	action    gitAction
	changes   []gitChange
}

func gitChangeGroups(changes []gitChange, ignored []string) []gitChangeGroup {
	groups := []gitChangeGroup{
		{id: "conflicts", label: "冲突"}, {id: "modified", label: "更改", action: gitStage},
		{id: "untracked", label: "未加入 Git", action: gitStage}, {id: "ignored", label: "已忽略"},
	}
	for _, change := range changes {
		if change.conflicted() {
			groups[0].changes = append(groups[0].changes, change)
			continue
		}
		group := 1
		if change.status == "??" {
			group = 2
		}
		groups[group].changes = append(groups[group].changes, change)
	}
	for _, name := range ignored {
		groups[3].changes = append(groups[3].changes, gitChange{status: "!!", path: name})
	}
	return slices.DeleteFunc(groups, func(group gitChangeGroup) bool { return len(group.changes) == 0 })
}

func gitChangeLabel(change gitChange) string {
	switch {
	case change.conflicted():
		return uiText("冲突")
	case change.status == "??":
		return uiText("未跟踪")
	case change.status == "!!":
		return uiText("已忽略")
	case strings.ContainsRune(change.status, 'D'):
		return uiText("已删除")
	case strings.ContainsRune(change.status, 'R'):
		return uiText("已重命名")
	case strings.ContainsRune(change.status, 'A'):
		return uiText("已添加")
	default:
		return uiText("已修改")
	}
}

func (git *gitPanelState) commitSelected(change gitChange) bool {
	if change.conflicted() || change.status == "!!" {
		return false
	}
	if selected, exists := git.commitSelection[change.path]; exists {
		return selected
	}
	return false
}

func (git *gitPanelState) selectedCommitPaths() []string {
	var paths []string
	for _, change := range git.snapshot.changes {
		if git.commitSelected(change) {
			paths = append(paths, change.paths()...)
		}
	}
	slices.Sort(paths)
	return slices.Compact(paths)
}

type gitChangeNode struct {
	key, label         string
	action             gitAction
	change             gitChange
	files              []gitChange
	children           []*gitChangeNode
	repository, module bool
}

type gitChangeRow struct {
	node  *gitChangeNode
	depth int
	open  bool
}

func (git *gitPanelState) changeRows() []gitChangeRow {
	if git.rows != nil && git.rowsEnglish == englishInterface.Load() {
		return git.rows
	}
	var ignored []string
	if git.showIgnored {
		ignored = git.ignored
	}
	rows := make([]gitChangeRow, 0)
	var visit func(*gitChangeNode, int)
	visit = func(node *gitChangeNode, depth int) {
		// Compact single-directory chains without hiding category or module boundaries.
		for depth > 0 && node.change.path == "" && !node.repository && !node.module && len(node.children) == 1 {
			child := node.children[0]
			if child.change.path != "" || child.repository || child.module {
				break
			}
			node.label += "/" + child.label
			node.key, node.children = child.key, child.children
		}
		closed, chosen := git.collapsed[node.key]
		open := len(node.children) > 0 && (chosen && !closed || !chosen && !git.collapseAll)
		rows = append(rows, gitChangeRow{node, depth, open})
		if open {
			for _, child := range node.children {
				visit(child, depth+1)
			}
		}
	}
	for _, group := range gitChangeGroups(git.snapshot.changes, ignored) {
		root := &gitChangeNode{key: group.id, label: uiText(group.label), action: group.action, files: group.changes}
		parent := root
		if git.groupRepository {
			label := filepath.Base(git.snapshot.root)
			if git.snapshot.branch != "" {
				label += " · " + git.snapshot.branch
			}
			parent = &gitChangeNode{key: group.id + "/repo", label: label, action: group.action, files: group.changes, repository: true}
			root.children = append(root.children, parent)
		}
		folders := map[string]*gitChangeNode{}
		for _, change := range group.changes {
			dir, file := path.Split(change.path)
			node := parent
			label := change.path
			parts := strings.FieldsFunc(dir, func(r rune) bool { return r == '/' })
			if git.flatChanges && git.groupModule && len(parts) > 1 {
				parts = parts[:1]
			}
			if git.flatChanges && !git.groupModule {
				parts = nil
			}
			prefix := parent.key + "\x00folder"
			for i, folder := range parts {
				prefix += "/" + folder
				key := prefix
				module := git.groupModule && i == 0
				if module {
					key += "\x00module"
				}
				child := folders[key]
				if child == nil {
					child = &gitChangeNode{key: key, label: folder, action: group.action, module: module}
					folders[key] = child
					node.children = append(node.children, child)
				}
				child.files = append(child.files, change)
				node = child
			}
			if len(parts) > 0 && file != "" {
				label = strings.TrimPrefix(change.path, strings.Join(parts, "/")+"/")
			}
			node.children = append(node.children, &gitChangeNode{key: group.id + "\x00file/" + change.path, label: label,
				action: group.action, change: change, files: []gitChange{change}})
		}
		visit(root, 0)
	}
	git.rows, git.rowsEnglish = rows, englishInterface.Load()
	return rows
}

func (m *editorApp) gitChangesView(c ui.C, busy bool) {
	git := &m.tools.git
	rows := git.changeRows()
	if len(rows) == 0 {
		ui.Text(c, uiText("工作区无变更")).Muted().FontSize(12).PadY(8)
		ui.Slot(c).Grow(1)
		return
	}
	tree := &git.tree
	tree.RowHeight, tree.Indent = 44, 14
	tree.DisplayDepth = func(depth int) int { return min(depth, 5) }
	tree.Node = func(index int) ui.TreeNode {
		row := rows[index]
		return ui.TreeNode{Depth: row.depth, Branch: len(row.node.children) > 0, Expanded: row.open,
			Selected: row.node.change.path != "" && row.node.change.path == git.selectedFile, Disabled: busy}
	}
	ui.Tree(c, tree, len(rows), func(index int) string { return rows[index].node.label }, func(i int) {
		row := rows[i]
		node := row.node
		place := c.Part("change").Key(node.key)
		ui.Row(place).Fill().Items(ui.Center).Gap(6).PadRight(6).Children(func() {
			if node.action != "" {
				selected := 0
				for _, change := range node.files {
					if git.commitSelected(change) {
						selected++
					}
				}
				checked := selected == len(node.files)
				label := uiText("选择提交 · ") + node.label
				if node.change.path != "" {
					label = uiText("选择提交 · ") + node.change.path
				}
				if ui.Checkbox(place.Part("select"), &checked, "").Label(label).Mixed(selected > 0 && !checked).Disabled(busy).Changed() {
					place.After(func() {
						if git.commitSelection == nil {
							git.commitSelection = make(map[string]bool)
						}
						for _, change := range node.files {
							git.commitSelection[change.path] = checked
						}
					})
				}
			} else {
				ui.Slot(place).Width(18).Shrink(0)
			}
			if node.change.path == "" {
				if row.depth > 0 {
					icon, color := folderIcon, m.theme.MutedForeground
					if node.repository {
						icon, color = gitBranchIcon, m.theme.Ring
					} else if node.module {
						icon, color = codeIcon, m.theme.Ring
					}
					ui.Icon(place, icon).Size(15, 15).Color(color).Shrink(0)
				}
				ui.Text(place, node.label).FontSize(13).Bold().MinWidth(0).MaxWidthPercent(75).Ellipsis().LabelFor(tree.RowID(i))
				ui.Text(place, fmt.Sprintf(uiText("%d 个文件"), len(node.files))).Muted().FontSize(11).Shrink(0)
				ui.Slot(place).Grow(1)
				return
			}
			icon, color := m.fileAppearance(node.change.path)
			ui.Icon(place, icon).Size(15, 15).Color(color).Shrink(0)
			hint := filepath.Join(git.snapshot.root, node.change.path)
			if node.change.original != "" {
				hint = node.change.original + " → " + hint
			}
			statusColor := m.theme.Ring
			if node.change.conflicted() || strings.ContainsRune(node.change.status, 'D') {
				statusColor = m.theme.Danger
			} else if node.change.status == "??" || strings.ContainsRune(node.change.status, 'A') {
				statusColor = m.theme.Success
			} else if node.change.status == "!!" {
				statusColor = m.theme.MutedForeground
			}
			name := ui.Column(place).Grow(1).MinWidth(0).Gap(2).LabelFor(tree.RowID(i)).Tooltip(hint)
			name.Children(func() {
				ui.Text(place, path.Base(node.change.path)).FontSize(13).Color(statusColor).FillWidth().Ellipsis()
				directory := path.Dir(node.change.path)
				if directory == "." {
					directory = uiText("仓库根目录")
				}
				ui.Text(place, directory).FontSize(11).Muted().FillWidth().Ellipsis()
			})
			ui.Text(place, gitChangeLabel(node.change)).Color(statusColor).FontSize(11).Shrink(0)
			if !busy {
				ui.ContextMenu(place, name, func(menu *ui.MenuItems) {
					if node.change.status != "!!" && menu.Item(uiText("查看差异")).Icon(historyCompareIcon).Clicked() {
						place.After(func() { m.openGitChange(node) })
					}
					if node.change.staged() && !node.change.conflicted() && menu.Item(uiText("查看暂存区差异")).Icon(historyCompareIcon).Clicked() {
						place.After(func() { m.openGitReview(node.change, true) })
					}
					if node.change.unstaged() && !node.change.conflicted() && node.change.status != "??" && menu.Item(uiText("查看未暂存差异")).Icon(historyCompareIcon).Clicked() {
						place.After(func() { m.openGitReview(node.change, false) })
					}
					if !strings.Contains(node.change.status, "D") && menu.Item(uiText("打开文件")).Icon(fileIcon).Clicked() {
						place.After(func() { m.load(filepath.Join(git.snapshot.root, node.change.path)) })
					}
					if menu.Item(uiText("在项目中定位")).Icon(revealFileIcon).Clicked() {
						place.After(func() { m.revealProjectFile(filepath.Join(git.snapshot.root, node.change.path)) })
					}
				})
			}
		})
	}).FillWidth().Grow(1).MinHeight(0).Label(uiText("Git 变更"))
	if index, ok := tree.Toggled(); ok && !busy {
		row := rows[index]
		c.After(func() {
			if git.collapsed == nil {
				git.collapsed = map[string]bool{}
			}
			git.collapsed[row.node.key] = row.open
			git.rows = nil
		})
	}
	if index, ok := tree.Chosen(); ok && !busy {
		node := rows[index].node
		if node.change.path != "" && node.change.status != "!!" {
			c.After(func() { m.openGitChange(node) })
		}
	}
}

func (m *editorApp) openGitChange(node *gitChangeNode) {
	m.tools.git.selectedFile = node.change.path
	if node.change.conflicted() {
		m.openGitConflict(node.change)
		return
	}
	m.openGitWorkingReview(node.change)
}
