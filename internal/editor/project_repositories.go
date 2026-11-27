package editor

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/vivaui/viva/ui"
)

type projectGitRepositories struct {
	roots     []string
	byProject map[string][]string
	scan      *ui.EffectState[map[string][]string, int]
	refresh   bool
}

func discoverProjectRepositories(ctx context.Context, projects []string) map[string][]string {
	const maxDirectories = 2500
	discovered := make(map[string][]string, len(projects))
	type directory struct{ path, project string }
	queue := make([]directory, 0, len(projects))
	owners := make(map[string]string, len(projects))
	for _, project := range projects {
		queue = append(queue, directory{project, project})
		owners[strings.ToLower(project)] = project
	}
	// Scan breadth first so nearby repos are found even in large workspaces.
	for scanned := 0; scanned < len(queue) && scanned < maxDirectories && ctx.Err() == nil; scanned++ {
		current := queue[scanned]
		entries, err := os.ReadDir(current.path)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			name := strings.ToLower(entry.Name())
			if name == ".git" && (entry.IsDir() || entry.Type().IsRegular()) {
				discovered[current.project] = append(discovered[current.project], current.path)
				continue
			}
			if entry.Type() != os.ModeDir || len(queue) >= maxDirectories ||
				strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "target" || name == "dist" || name == "build" {
				continue
			}
			path := filepath.Join(current.path, entry.Name())
			if owners[strings.ToLower(path)] == "" {
				queue = append(queue, directory{path, current.project})
			}
		}
	}
	return discovered
}

func (m *editorApp) updateProjectRepositories(c ui.C) {
	state := &m.tools.repositories
	roots := m.projectDirectories()
	if len(roots) == 0 {
		*state = projectGitRepositories{}
		return
	}
	if state.refresh || !slices.Equal(state.roots, roots) {
		state.roots, state.refresh = roots, false
		state.scan = ui.NewEffect[map[string][]string, int](func(ctx context.Context) (map[string][]string, error) {
			ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			return discoverProjectRepositories(ctx, roots), nil
		})
	}
	if state.scan != nil {
		ui.Effect(c, state.scan, 0)
		repositories, _, ready := state.scan.Take()
		if !ready {
			return
		}
		state.byProject, state.scan = repositories, nil
	}
	git := &m.tools.git
	if !m.tools.gitOpen || git.request != nil || git.snapshot.root != "" || git.issue != "" {
		return
	}
	if choice := m.gitProjectDirectory(); choice != "" && git.directory != choice {
		m.openProjectGit(choice)
	}
}

func (m *editorApp) projectDirectories() []string {
	var paths []string
	if m.workspace != nil {
		for _, root := range m.workspace.roots {
			paths = append(paths, root.path)
		}
	}
	return paths
}

func (m *editorApp) gitProjects() []string {
	var paths []string
	for _, project := range m.projectDirectories() {
		if repositories := m.tools.repositories.byProject[project]; len(repositories) != 0 {
			paths = append(paths, repositories...)
		} else {
			// Keep an uninitialized project available for git init.
			paths = append(paths, project)
		}
	}
	return paths
}

func (m *editorApp) gitRepositoryAt(path string) string {
	best := ""
	project := m.projectRoot(path)
	for _, root := range m.tools.repositories.byProject[project] {
		if len(root) > len(best) && gitPathWithin(root, path) {
			best = root
		}
	}
	return best
}

func (m *editorApp) gitRepositoryLabel(path string) string {
	project := m.projectRoot(path)
	if project != "" && !strings.EqualFold(path, project) {
		if relative, err := filepath.Rel(project, path); err == nil {
			return filepath.Base(project) + " / " + filepath.ToSlash(relative)
		}
	}
	return filepath.Base(path)
}
