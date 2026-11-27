package editor

import (
	"context"
	"slices"
	"strings"

	"github.com/vivaui/viva/ui"
)

type projectDirectoryResult struct {
	root    *fileNode
	problem error
}

type projectDirectoryJob struct {
	node       *fileNode
	reveal     string
	notice     string
	newProject bool
	expandAll  bool
	request    *ui.EffectState[projectDirectoryResult, int]
}

// Only detached nodes are read or compacted in the worker. The live tree and
// its row IDs remain owned by the UI thread.
func readProjectDirectory(ctx context.Context, path string, folders []string, expandAll bool) (projectDirectoryResult, error) {
	tree := newFolderTree()
	root := tree.newProject(path)
	if err := ctx.Err(); err != nil {
		return projectDirectoryResult{}, err
	}
	if err := tree.readChildren(root); err != nil {
		return projectDirectoryResult{}, err
	}
	if expandAll {
		problem := tree.readAllProjectFolders(ctx, root)
		return projectDirectoryResult{root: root, problem: problem}, ctx.Err()
	}
	problem := tree.restoreExpanded(root, folders)
	var discover func(*fileNode) error
	discover = func(node *fileNode) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		for child := tree.discoverCompactChild(node); child != nil; child = tree.discoverCompactChild(node) {
			node = child
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		if node.expanded {
			for _, child := range node.children {
				if child.directory {
					if err := discover(child); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	// The requested directory is visible even when its restored state is collapsed.
	for _, child := range root.children {
		if child.directory {
			if err := discover(child); err != nil {
				return projectDirectoryResult{}, err
			}
		}
	}
	return projectDirectoryResult{root: root, problem: problem}, ctx.Err()
}

func (tree *folderTree) readAllProjectFolders(ctx context.Context, node *fileNode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := tree.readChildren(node); err != nil {
		return err
	}
	node.expanded = true
	var problem error
	for _, child := range node.children {
		// Skip junctions and directory links, which can point outside the project or loop.
		if child.compactable {
			if err := tree.readAllProjectFolders(ctx, child); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if problem == nil {
					problem = err
				}
			}
		}
	}
	return problem
}

func (m *editorApp) expandProjectFolders() {
	tree := m.workspace
	if tree == nil {
		return
	}
	for _, root := range tree.roots {
		tree.queueDirectory(root, nil, "", true)
	}
	m.status = uiText("正在展开项目目录…")
	m.invalidator.Invalidate()
}

func (m *editorApp) collapseProjectFolders() {
	tree := m.workspace
	if tree == nil {
		return
	}
	tree.loads = slices.DeleteFunc(tree.loads, func(job *projectDirectoryJob) bool { return !job.newProject })
	var collapse func(*fileNode)
	collapse = func(node *fileNode) {
		node.expanded, node.pendingExpanded = false, nil
		for _, child := range node.children {
			if child.directory {
				collapse(child)
			}
		}
	}
	for _, root := range tree.roots {
		collapse(root)
	}
	tree.flatten()
	m.status = uiText("全部收起")
	m.invalidator.Invalidate()
}

func (tree *folderTree) queueDirectory(node *fileNode, folders []string, reveal string, expandAll bool) *projectDirectoryJob {
	// A refresh supersedes pending descendants. Effects no longer declared are
	// cancelled by the UI lifecycle; no result can resurrect a removed branch.
	tree.loads = slices.DeleteFunc(tree.loads, func(job *projectDirectoryJob) bool {
		if job.node != node && slices.Contains(tree.roots, job.node) {
			return false
		}
		_, inside := aiRelativeTo(node.path, job.node.path)
		return inside
	})
	folders = slices.Clone(folders)
	node.pendingExpanded = folders
	path := node.path
	job := &projectDirectoryJob{node: node, reveal: reveal, expandAll: expandAll}
	job.request = ui.NewEffect[projectDirectoryResult, int](func(ctx context.Context) (projectDirectoryResult, error) {
		return readProjectDirectory(ctx, path, folders, expandAll)
	})
	tree.loads = append(tree.loads, job)
	return job
}

func (tree *folderTree) findNode(path string) *fileNode {
	var find func(*fileNode) *fileNode
	find = func(node *fileNode) *fileNode {
		if strings.EqualFold(node.path, path) {
			return node
		}
		if _, inside := aiRelativeTo(node.path, path); !inside {
			return nil
		}
		for _, child := range node.children {
			if found := find(child); found != nil {
				return found
			}
		}
		return nil
	}
	for _, root := range tree.roots {
		if node := find(root); node != nil {
			return node
		}
	}
	return nil
}

func (m *editorApp) toggleProjectFolder(node *fileNode) {
	tree := m.workspace
	if tree == nil || !node.directory {
		return
	}
	node.expanded = !node.expanded
	if node.expanded && !node.loaded {
		tree.queueDirectory(node, []string{node.path}, "", false)
	}
	tree.flatten()
}

func (m *editorApp) updateProjectDirectories(c ui.C) {
	tree := m.workspace
	if tree == nil || len(tree.loads) == 0 {
		return
	}
	job := tree.loads[0]
	if !slices.Contains(tree.roots, job.node) && tree.findNode(job.node.path) != job.node {
		tree.loads = slices.Delete(tree.loads, 0, 1)
		m.updateProjectDirectories(c)
		return
	}
	ui.Effect(c.KeyInt(job.node.id), job.request, 0)
	result, err, done := job.request.Take()
	if !done {
		return
	}
	m.invalidator.Invalidate()
	tree.loads = slices.Delete(tree.loads, 0, 1)
	node := job.node
	node.pendingExpanded = nil
	if err != nil {
		node.loadError = err.Error()
		m.status = uiText("无法读取目录：") + node.path + " · " + err.Error()
		if job.newProject {
			tree.roots = slices.DeleteFunc(tree.roots, func(root *fileNode) bool { return root == node })
			tree.flatten()
			m.fail(err)
		}
	} else {
		// Preserve any expand/collapse performed while a refresh was running.
		oldNodes := map[string]*fileNode{}
		var collect func(*fileNode)
		collect = func(old *fileNode) {
			oldNodes[strings.ToLower(old.path)] = old
			for _, child := range old.children {
				collect(child)
			}
		}
		collect(node)
		var unloaded []*fileNode
		var adopt func(*fileNode)
		adopt = func(child *fileNode) {
			if old := oldNodes[strings.ToLower(child.path)]; old != nil {
				child.id = old.id
				if !job.expandAll {
					child.expanded = old.expanded
				}
			} else {
				tree.nextID++
				child.id = tree.nextID
			}
			if job.reveal != "" && child.directory {
				if _, inside := aiRelativeTo(child.path, job.reveal); inside {
					child.expanded = true
				}
			}
			if child.expanded && !child.loaded {
				unloaded = append(unloaded, child)
			}
			for _, nested := range child.children {
				adopt(nested)
			}
		}
		for _, child := range result.root.children {
			adopt(child)
		}
		node.children, node.loaded, node.loadError = result.root.children, true, ""
		if job.expandAll {
			node.expanded = true
			if !slices.ContainsFunc(tree.loads, func(next *projectDirectoryJob) bool { return next.expandAll }) {
				m.status = uiText("已展开项目目录")
			}
		}
		for _, child := range unloaded {
			tree.queueDirectory(child, []string{child.path}, job.reveal, false)
		}
		if result.problem != nil {
			node.loadError = result.problem.Error()
		}
		if job.newProject {
			m.rememberPath(node.path, true)
		}
		if job.notice != "" {
			m.status = job.notice
		}
		tree.flatten()
		if job.reveal != "" {
			for index, row := range tree.rows {
				if strings.EqualFold(row.node.path, job.reveal) {
					tree.state.ScrollIntoView(index)
					break
				}
			}
		}
	}
	// Start the next queued directory without waiting for another user event.
	if len(tree.loads) > 0 {
		m.updateProjectDirectories(c)
	}
}
