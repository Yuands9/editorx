package editor

import (
	"context"
	"strings"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
)

type projectWatchState struct {
	watch        *app.DirectoryWatch
	dirty, ready bool
	attempted    bool
	delay        *ui.EffectState[bool, int]
}

func (m *editorApp) closeProjectWatches() {
	for _, state := range m.projectWatches {
		if state.watch != nil {
			state.watch.Close()
		}
	}
	m.projectWatches = nil
}

func (m *editorApp) updateProjectWatches(c ui.C) {
	if !m.watchFiles || m.windowClosed || m.workspace == nil {
		m.closeProjectWatches()
		return
	}
	if m.projectWatches == nil {
		m.projectWatches = map[string]*projectWatchState{}
	}
	for path, state := range m.projectWatches {
		if m.workspace.project(path) == nil {
			if state.watch != nil {
				state.watch.Close()
			}
			delete(m.projectWatches, path)
		}
	}
	for _, root := range m.workspace.roots {
		state := m.projectWatches[root.path]
		if state == nil {
			state = &projectWatchState{}
			m.projectWatches[root.path] = state
		}
		if state.watch == nil && (!state.attempted || m.checkFiles) {
			state.attempted = true
			state.watch, _ = app.WatchTree(root.path, func(name string) bool {
				switch strings.ToLower(name) {
				case ".git", ".hg", ".svn", ".dev", "node_modules":
					return true
				}
				return false
			}, m.invalidator.Invalidate)
			// Failed watches retry on activation rather than every repaint.
		}
		if state.watch != nil {
			changes := state.watch.Changes(nil)
			state.dirty = state.dirty || changes.Resync || len(changes.Names) > 0 || m.checkFiles
			if changes.Resync {
				state.watch.Close()
				state.watch = nil
				state.attempted = false
			}
		}
		if !state.dirty {
			continue
		}
		if state.delay == nil && !state.ready {
			state.delay = ui.NewEffect[bool, int](func(ctx context.Context) (bool, error) {
				timer := time.NewTimer(250 * time.Millisecond)
				defer timer.Stop()
				select {
				case <-ctx.Done():
					return false, ctx.Err()
				case <-timer.C:
					return true, nil
				}
			})
		}
		if state.delay != nil {
			ui.Effect(c.KeyInt(root.id), state.delay, 0)
			if ready, _, done := state.delay.Take(); done {
				state.delay, state.ready = nil, ready
			}
		}
		loading := false
		for _, job := range m.workspace.loads {
			if _, inside := aiRelativeTo(root.path, job.node.path); inside {
				loading = true
				break
			}
		}
		if state.ready && !loading {
			state.dirty, state.ready = false, false
			m.workspace.queueDirectory(root, expandedProjectFolders(root), "")
		}
	}
}
