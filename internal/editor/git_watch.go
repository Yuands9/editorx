package editor

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
)

type gitWatchState struct {
	root              string
	working, metadata *app.DirectoryWatch
	location          *ui.EffectState[string, int]
	delay             *ui.EffectState[bool, int]
	dirty, attempted  bool
}

func (s *gitWatchState) close() {
	if s.working != nil {
		s.working.Close()
	}
	if s.metadata != nil {
		s.metadata.Close()
	}
	*s = gitWatchState{}
}

func (m *editorApp) updateGitWatch(c ui.C) {
	git := &m.tools.git
	s := &git.watch
	if !m.watchFiles || !m.tools.gitOpen || m.windowClosed || git.snapshot.root == "" {
		s.close()
		return
	}
	if s.root != git.snapshot.root {
		s.close()
		s.root = git.snapshot.root
	}
	if !s.attempted {
		s.attempted = true
		s.working, _ = app.WatchTree(s.root, func(name string) bool { return name == ".git" || name == "node_modules" || name == ".dev" }, m.invalidator.Invalidate)
		root := s.root
		s.location = ui.NewEffect[string, int](func(ctx context.Context) (string, error) {
			ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			exe, err := exec.LookPath("git.exe")
			if err != nil {
				return "", err
			}
			return runGitCommand(ctx, exe, root, "rev-parse", "--git-common-dir")
		})
	}
	if s.location != nil {
		ui.Effect(c.Part("location"), s.location, 0)
		if path, err, done := s.location.Take(); done {
			s.location = nil
			if err == nil {
				s.metadata, _ = app.WatchTree(gitMetadataPath(s.root, strings.TrimSpace(path)), func(name string) bool { return name == "objects" || name == "logs" }, m.invalidator.Invalidate)
			}
		}
	}
	for _, watch := range []*app.DirectoryWatch{s.working, s.metadata} {
		if watch == nil {
			continue
		}
		changes := watch.Changes(nil)
		s.dirty = s.dirty || changes.Resync || len(changes.Names) > 0
	}
	if s.dirty && s.delay == nil {
		s.delay = ui.NewEffect[bool, int](func(ctx context.Context) (bool, error) {
			timer := time.NewTimer(350 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return false, ctx.Err()
			case <-timer.C:
				return true, nil
			}
		})
	}
	if s.delay != nil {
		ui.Effect(c.Part("delay"), s.delay, 0)
		if git.request == nil && (git.review == nil || git.review.apply == nil) {
			if ready, _, done := s.delay.Take(); done {
				s.delay, s.dirty = nil, false
				if ready {
					m.startGit(gitRefresh, "", nil)
				}
			}
		}
	}
}
