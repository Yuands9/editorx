package editor

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

type fileNode struct {
	id                          int
	path                        string
	name                        string
	directory                   bool
	compactable, compactChecked bool
	expanded                    bool
	loaded                      bool
	children                    []*fileNode
	menuOpen                    bool
	loadError                   string
}

type fileRow struct {
	start, node *fileNode
	label       string
	depth       int
}

type folderTree struct {
	roots      []*fileNode
	nextID     int
	rows       []fileRow
	state      ui.TreeState
	activePath string
	blocked    bool
}

func newFolderTree() *folderTree {
	tree := &folderTree{}
	tree.state.RowHeight = 30
	tree.state.Indent = 14
	tree.state.Key = func(i int) int { return tree.rows[i].start.id }
	tree.state.Node = func(i int) ui.TreeNode {
		row := tree.rows[i]
		node := ui.TreeNode{Depth: row.depth, Branch: row.node.directory, Expanded: row.node.expanded,
			Selected: strings.EqualFold(row.node.path, tree.activePath), Disabled: tree.blocked}
		if row.depth == 0 {
			node.Height = 34
		}
		return node
	}
	tree.state.Label = func(i int) string { return tree.rows[i].label }
	return tree
}

func (tree *folderTree) project(path string) *fileNode {
	for _, root := range tree.roots {
		if strings.EqualFold(root.path, path) {
			return root
		}
	}
	return nil
}

func (tree *folderTree) newProject(path string) *fileNode {
	tree.nextID++
	name := filepath.Base(path)
	if filepath.Dir(path) == path {
		name = path
	}
	return &fileNode{id: tree.nextID, path: path, name: name, directory: true}
}

func (tree *folderTree) readChildren(node *fileNode) error {
	if node.loaded {
		return nil
	}
	entries, err := os.ReadDir(node.path)
	if err != nil {
		node.loadError = err.Error()
		return err
	}
	for _, entry := range entries {
		node.children = append(node.children, tree.newFileNode(node, entry))
	}
	sort.SliceStable(node.children, func(i, j int) bool {
		a, b := node.children[i], node.children[j]
		if a.directory != b.directory {
			return a.directory
		}
		return strings.ToLower(a.name) < strings.ToLower(b.name)
	})
	node.loaded, node.loadError = true, ""
	return nil
}

func (tree *folderTree) newFileNode(parent *fileNode, entry os.DirEntry) *fileNode {
	tree.nextID++
	return &fileNode{id: tree.nextID, name: entry.Name(), path: filepath.Join(parent.path, entry.Name()),
		directory: entry.IsDir(), compactable: entry.Type() == os.ModeDir}
}

func (tree *folderTree) compactChild(node *fileNode) *fileNode {
	if !node.compactable {
		return nil
	}
	if !node.loaded && !node.compactChecked {
		node.compactChecked = true
		// Discover single-directory chains without loading wide, collapsed branches.
		if directory, err := os.Open(node.path); err == nil {
			entries, err := directory.ReadDir(2)
			directory.Close()
			if (err == nil || errors.Is(err, io.EOF)) && len(entries) == 1 && entries[0].Type() == os.ModeDir {
				node.children = []*fileNode{tree.newFileNode(node, entries[0])}
				node.loaded = true
			}
		}
	}
	if len(node.children) == 1 && node.children[0].compactable {
		return node.children[0]
	}
	return nil
}

func (tree *folderTree) toggle(node *fileNode) error {
	if !node.directory {
		return nil
	}
	if err := tree.readChildren(node); err != nil {
		return err
	}
	node.expanded = !node.expanded
	tree.flatten()
	return nil
}

func (tree *folderTree) mergeProjects(source *folderTree) {
	var rekey func(*fileNode)
	rekey = func(node *fileNode) {
		tree.nextID++
		node.id = tree.nextID
		for _, child := range node.children {
			rekey(child)
		}
	}
	for _, root := range source.roots {
		if tree.project(root.path) != nil {
			continue
		}
		rekey(root)
		tree.roots = append(tree.roots, root)
	}
	tree.flatten()
}

func (tree *folderTree) flatten() {
	clear(tree.rows)
	tree.rows = tree.rows[:0]
	var visit func(*fileNode, int)
	visit = func(node *fileNode, depth int) {
		start, label := node, node.name
		if depth > 0 {
			for child := tree.compactChild(node); child != nil; child = tree.compactChild(node) {
				label += "." + child.name
				node = child
			}
		}
		tree.rows = append(tree.rows, fileRow{start: start, node: node, label: label, depth: depth})
		if node.expanded {
			for _, child := range node.children {
				visit(child, depth+1)
			}
		}
	}
	for _, root := range tree.roots {
		visit(root, 0)
	}
}

func expandedProjectFolders(root *fileNode) []string {
	var folders []string
	var collect func(*fileNode)
	collect = func(node *fileNode) {
		if node.expanded {
			folders = append(folders, node.path)
		}
		for _, child := range node.children {
			if child.directory {
				collect(child)
			}
		}
	}
	collect(root)
	return folders
}

// Read only the saved branches and any collapsed ancestors of those branches.
func (tree *folderTree) restoreExpanded(root *fileNode, folders []string) error {
	wanted := make(map[string]bool, len(folders))
	for _, folder := range folders {
		relative, err := filepath.Rel(root.path, folder)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			continue
		}
		folder = filepath.Join(root.path, relative)
		wanted[strings.ToLower(folder)] = true
		for !strings.EqualFold(folder, root.path) {
			folder = filepath.Dir(folder)
			key := strings.ToLower(folder)
			if _, known := wanted[key]; known {
				break
			}
			wanted[key] = false
		}
	}
	var failures []error
	var restore func(*fileNode)
	restore = func(node *fileNode) {
		expanded, needed := wanted[strings.ToLower(node.path)]
		if !needed {
			return
		}
		if err := tree.readChildren(node); err != nil {
			failures = append(failures, err)
			return
		}
		node.expanded = expanded
		for _, child := range node.children {
			if child.directory {
				restore(child)
			}
		}
	}
	restore(root)
	return errors.Join(failures...)
}

func (m *editorApp) addProjects(paths ...string) {
	tree := m.workspace
	if tree == nil {
		tree = newFolderTree()
	}
	var failures []error
	var firstAdded, firstExisting *fileNode
	added := 0
	lastName := ""
	for _, selected := range paths {
		path, err := filepath.Abs(selected)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		root := tree.project(path)
		if root == nil {
			root = tree.newProject(path)
			if err := tree.readChildren(root); err != nil {
				failures = append(failures, err)
				continue
			}
			root.expanded = true
			tree.roots = append(tree.roots, root)
			added++
			lastName = root.name
			if firstAdded == nil {
				firstAdded = root
			}
		} else if firstExisting == nil {
			firstExisting = root
		}
		m.rememberPath(root.path, true)
	}
	reveal := firstAdded
	if reveal == nil {
		reveal = firstExisting
	}
	if reveal != nil {
		m.workspace, m.showFiles = tree, true
		if added > 0 {
			tree.flatten()
		}
		row := slices.IndexFunc(tree.rows, func(row fileRow) bool { return row.node == reveal })
		if row >= 0 {
			tree.state.ScrollIntoView(row)
		}
		switch added {
		case 0:
			m.status = "项目已在列表中"
		case 1:
			m.status = "已添加项目 " + lastName
		default:
			m.status = fmt.Sprintf("已添加 %d 个项目", added)
		}
	}
	if err := errors.Join(failures...); err != nil {
		m.fail(fmt.Errorf("以下项目添加失败。\n%w", err))
	}
}

func (m *editorApp) refreshProject(root *fileNode, reveal string) {
	tree := m.workspace
	if tree == nil || !slices.Contains(tree.roots, root) {
		return
	}
	refreshed := &fileNode{id: root.id, path: root.path, name: root.name, directory: true}
	if err := tree.readChildren(refreshed); err != nil {
		m.fail(err)
		return
	}
	folders := expandedProjectFolders(root)
	if reveal != "" {
		for directory := filepath.Dir(reveal); ; directory = filepath.Dir(directory) {
			folders = append(folders, directory)
			if strings.EqualFold(directory, root.path) {
				break
			}
		}
	}
	err := tree.restoreExpanded(refreshed, folders)
	*root = *refreshed
	tree.flatten()
	if reveal != "" {
		row := slices.IndexFunc(tree.rows, func(row fileRow) bool { return strings.EqualFold(row.node.path, reveal) })
		if row >= 0 {
			tree.state.ScrollIntoView(row)
		}
	}
	m.status = "已刷新项目 " + root.name
	if err != nil {
		m.fail(fmt.Errorf("项目已刷新，部分子目录暂时无法读取。\n%w", err))
	}
}

func (m *editorApp) removeProject(root *fileNode) {
	tree := m.workspace
	if tree == nil {
		return
	}
	index := slices.Index(tree.roots, root)
	if index < 0 {
		return
	}
	tree.roots = slices.Delete(tree.roots, index, index+1)
	tree.flatten()
	if len(tree.roots) == 0 {
		m.workspace = nil
	}
	m.status = "已移除项目 " + root.name
}

var projectActionsIcon = gfx.Icon{ViewBox: 24, Stroke: 2.4, Cap: gfx.CapRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(5, 12), gfx.LineTo(5.25, 12), gfx.MoveTo(11.8, 12), gfx.LineTo(12.05, 12), gfx.MoveTo(18.55, 12), gfx.LineTo(18.8, 12),
}}

func (m *editorApp) filePanel(c ui.C, blocked bool) {
	ui.Row(c).FillWidth().Height(30).PadX(18).Items(ui.Center).Children(func() {
		ui.Text(c, "项目").FontSize(12).Bold().Muted()
	})
	tree := m.workspace
	if tree == nil {
		ui.Text(c, "用「添加项目」打开文件夹").PadX(18).PadTop(7).FontSize(12).Muted()
		return
	}
	tree.activePath, tree.blocked = "", blocked
	if m.active != nil {
		tree.activePath = m.active.doc.path
	}
	ui.Tree(c, &tree.state, len(tree.rows), tree.state.Label, func(i int) {
		row := tree.rows[i]
		node := row.node
		if row.depth == 0 {
			m.projectRow(c.KeyInt(node.id), tree.state.RowID(i), node, blocked)
			return
		}
		place := c.KeyInt(row.start.id)
		entry := ui.Row(place).Fill().Gap(8).Items(ui.Center).PadRight(8)
		if !node.directory {
			// Tree reserves this space for the chevron on directory rows.
			entry.PadLeft(24)
		}
		entry.Children(func() {
			icon, color := m.fileAppearance(node.name)
			if node.directory {
				icon, color = folderIcon, m.theme.MutedForeground
			}
			ui.Icon(place, icon).Size(16, 16).Color(color)
			name := ui.Text(place, row.label).FontSize(13).Grow(1).MinWidth(0).Ellipsis().LabelFor(tree.state.RowID(i))
			if node != row.start {
				name.Tooltip(node.path).TooltipOn(ui.TooltipRight)
			}
			if !blocked {
				ui.ContextMenu(place, name, func(menu *ui.MenuItems) {
					if node.directory {
						m.projectCreateActions(place, menu, node.path)
						if node != row.start {
							menu.Sub("上级目录", func(ancestors *ui.MenuItems) {
								for path := filepath.Dir(node.path); ; path = filepath.Dir(path) {
									ancestors.Sub(filepath.Base(path), func(items *ui.MenuItems) {
										m.projectCreateActions(place, items, path)
									})
									if path == row.start.path {
										break
									}
								}
							})
						}
						menu.Separator()
					}
					if (node.directory || aiTextPath(node.path)) && menu.Item("在 AI 助手中打开").Icon(aiIcon).Clicked() {
						place.After(func() { m.openAIAt(nil, node.path, node.directory) })
					}
					if menu.Item("打开所在文件夹").Icon(folderOpenIcon).Clicked() {
						path := node.path
						if !node.directory {
							path = filepath.Dir(path)
						}
						place.After(func() { m.openExplorerFolder(path) })
					}
				})
			}
		})
	}).FillWidth().Grow(1).MinHeight(0).MarginX(8)
	if blocked {
		return
	}
	if index, ok := tree.state.Toggled(); ok {
		node := tree.rows[index].node
		c.After(func() {
			if err := tree.toggle(node); err != nil {
				m.fail(err)
			}
		})
	}
	if index, ok := tree.state.Chosen(); ok {
		node := tree.rows[index].node
		c.After(func() {
			if node.directory {
				if err := tree.toggle(node); err != nil {
					m.fail(err)
				}
			} else {
				m.load(node.path)
			}
		})
	}
}

func (m *editorApp) projectRow(c ui.C, rowID gfx.ID, root *fileNode, blocked bool) {
	color := m.theme.Ring
	if root.loadError != "" {
		color = m.theme.Danger
	}
	ui.Row(c).Fill().Items(ui.Center).PadRight(4).Gap(8).Children(func() {
		ui.Icon(c, folderIcon).Size(16, 16).Shrink(0).Color(color)
		description := root.path
		if root.loadError != "" {
			description += "\n" + root.loadError
		}
		name := ui.Text(c, root.name).FontSize(13).Grow(1).MinWidth(0).Ellipsis().
			LabelFor(rowID).Tooltip(description).TooltipOn(ui.TooltipRight)
		actions := ui.Button(c, "项目操作 "+root.path).Icon(projectActionsIcon).IconOnly().Ghost().Size(24, 24).Shrink(0).
			Tooltip("项目操作").Disabled(blocked)
		if actions.Clicked() {
			root.menuOpen = !root.menuOpen
		}
		if !blocked {
			ui.Menu(c, actions, &root.menuOpen, func(menu *ui.MenuItems) { m.projectActions(c, menu, root) })
			ui.ContextMenu(c, name, func(menu *ui.MenuItems) { m.projectActions(c, menu, root) })
		}
	})
}

func (m *editorApp) projectActions(c ui.C, menu *ui.MenuItems, root *fileNode) {
	menu.WidthRange(180, 290)
	menu.Label(root.name)
	m.projectCreateActions(c, menu, root.path)
	menu.Separator()
	if menu.Item("在 AI 助手中打开").Icon(aiIcon).Clicked() {
		c.After(func() { m.openAIAt(nil, root.path, true) })
	}
	if menu.Item("打开项目文件夹").Icon(folderOpenIcon).Clicked() {
		c.After(func() { m.openExplorerFolder(root.path) })
	}
	if menu.Item("Git").Icon(gitBranchIcon).Clicked() {
		c.After(func() { m.openProjectGit(root.path) })
	}
	if menu.Item("在终端中打开").Icon(shellIcon).Clicked() {
		c.After(func() { m.openLocalShell(root.path) })
	}
	if menu.Item("刷新项目").Icon(refreshIcon).Clicked() {
		c.After(func() { m.refreshProject(root, "") })
	}
	if menu.Item("移除项目").Icon(closeIcon).Clicked() {
		c.After(func() { m.removeProject(root) })
	}
}
