package editor

import (
	"slices"
	"strings"
)

type editorLocation struct {
	tabID int
	path  string
	caret int
}
type editorNavigation struct {
	points   []editorLocation
	index    int
	last     editorLocation
	skipping bool
}

func (m *editorApp) currentLocation() editorLocation {
	tab := m.active
	if !historyEditableTab(tab) {
		return editorLocation{}
	}
	return editorLocation{tabID: tab.id, path: tab.doc.path, caret: tab.source.text.Caret()}
}

func (m *editorApp) observeNavigation() {
	n := &m.navigation
	next := m.currentLocation()
	if next.tabID == 0 {
		return
	}
	defer func() { n.last = next }()
	if n.skipping {
		n.skipping = false
		return
	}
	if n.last.tabID == 0 {
		n.points = []editorLocation{next}
		n.index = 0
		return
	}
	jump := next.tabID != n.last.tabID
	if !jump && next.caret != n.last.caret {
		editor := m.active.source.text
		from, to := min(next.caret, n.last.caret), min(max(next.caret, n.last.caret), editor.RuneCount())
		if from <= to {
			jump = strings.Count(editor.String()[editor.ByteOffset(from):editor.ByteOffset(to)], "\n") >= 8
		}
	}
	if !jump {
		return
	}
	n.points = slices.Clone(n.points[:n.index+1])
	n.points[n.index] = n.last
	n.points = append(n.points, next)
	if len(n.points) > 100 {
		n.points = slices.Delete(n.points, 0, len(n.points)-100)
	}
	n.index = len(n.points) - 1
}

func (m *editorApp) navigateLocation(delta int) {
	m.observeNavigation()
	n := &m.navigation
	index := n.index + delta
	if index < 0 || index >= len(n.points) {
		return
	}
	n.points[n.index] = m.currentLocation()
	point := n.points[index]
	apply := func(tab *editorTab) {
		if !historyEditableTab(tab) {
			return
		}
		n.index = index
		n.skipping = true
		m.activate(tab)
		tab.source.text.MoveTo(min(point.caret, tab.source.text.RuneCount()), false)
		clear(tab.source.collapsed)
		tab.source.view.Code.Reveal(tab.source.text.ByteOffset(tab.source.text.Caret()))
		tab.source.focus = true
	}
	for _, tab := range m.tabs {
		if tab.id == point.tabID {
			apply(tab)
			return
		}
	}
	if point.path != "" {
		m.loadTextFile(point.path, "", apply)
	}
}
