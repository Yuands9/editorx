package editor

import (
	"fmt"
	"path/filepath"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

func (s *gitReviewState) logWindowTitle() string {
	name := s.root
	if s.logScope != "" {
		name = s.logScope
	}
	return uiText("Git 提交记录") + " · " + filepath.Base(name)
}

func (m *editorApp) openGitLogAt(directory, scope string, isDirectory bool) {
	if directory == "" || m.windowClosed || m.windowClosePending {
		return
	}
	if previous := m.gitLogWindow; previous != nil && previous.logScope == scope && (scope != "" || previous.root == directory) && previous.window != nil {
		previous.window.Show()
		return
	}
	m.closeGitLog()
	s := &gitReviewState{open: true, history: true, root: directory, logScope: scope, logDirectory: isDirectory, logSplit: ui.SplitterState{Ratio: .67}}
	s.loadLog(false)
	if m.application == nil {
		m.tools.git.review = s
		return
	}
	const width, height = 1240, 780
	var origin *gfx.Point
	if bounds, ok := m.nativeWindow.Frame(); ok {
		origin = &gfx.Point{X: bounds.X + (bounds.Width-width)/2, Y: bounds.Y + (bounds.Height-height)/2}
	}
	config := app.Config{Title: s.logWindowTitle(), Icon: themedApplicationIcon(m.light), Width: width, Height: height, MinWidth: 780, MinHeight: 460,
		Origin: origin, Invalidator: &s.invalidator, OnActivate: s.invalidator.Invalidate, OnClosed: func() {
			if m.gitLogWindow == s {
				m.gitLogWindow = nil
			}
		}}
	window, err := ui.OpenWindow(m.application, config, ui.PageConfig{Theme: &m.theme, Styles: &editorControlStyles, View: func(c ui.C) {
		m.updateGitReview(c.Part("log-effects"), s, s.invalidator.Invalidate)
		ui.Column(c).Fill().MinHeight(0).Pad(12).Gap(7).Children(func() {
			if s.logScope != "" {
				ui.Text(c, s.logScope).FillWidth().Ellipsis().Muted().FontSize(12)
			}
			m.gitReviewBody(c.Part("log-content"), s, false)
		})
	}})
	if err != nil {
		m.fail(fmt.Errorf(uiText("无法打开 Git 提交记录窗口：%w"), err))
		return
	}
	s.window, m.gitLogWindow = window, s
}

func (m *editorApp) closeGitLog() {
	if s := m.gitLogWindow; s != nil {
		m.gitLogWindow = nil
		s.window.Close()
	}
}
