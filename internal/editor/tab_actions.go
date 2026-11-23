package editor

import (
	"path/filepath"
	"slices"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

func (m *editorApp) tabContextMenu(c ui.C, target ui.Box, index int, tab *editorTab) {
	ui.ContextMenu(c, target, func(menu *ui.MenuItems) {
		if menu.Item("重命名标签…").Clicked() {
			c.After(func() { m.rename.begin(tab, nil) })
		}
		label := "固定标签"
		if tab.pinned {
			label = "取消固定标签"
		}
		if menu.Item(label).Icon(pinStateIcon(tab.pinned)).Clicked() {
			c.After(func() { m.togglePin(tab) })
		}
		if menu.Item("关闭标签").Icon(closeIcon).Clicked() {
			c.After(func() { m.closeTab(tab) })
		}
		if aiTextTab(tab) && menu.Item("在 AI 助手中打开").Icon(aiIcon).Clicked() {
			c.After(func() { m.openAIAt(tab, "", false) })
		}
		if menu.Item("打开所在文件夹").Icon(folderOpenIcon).Disabled(tab.doc.path == "").Clicked() {
			c.After(func() { m.openExplorerFolder(filepath.Dir(tab.doc.path)) })
		}
		menu.Separator()
		if menu.Item("添加到新分组").Clicked() {
			c.After(func() { m.newTabGroup(tab) })
		}
		var groups []*tabGroup
		for i, other := range m.tabs {
			if other.group != nil && other.group != tab.group && (i == 0 || m.tabs[i-1].group != other.group) {
				groups = append(groups, other.group)
			}
		}
		if len(groups) > 0 {
			menu.Sub("移到已有分组", func(groupsMenu *ui.MenuItems) {
				for _, group := range groups {
					if groupsMenu.Item(group.Name).Clicked() {
						c.After(func() { m.moveToGroup(tab, group) })
					}
				}
			})
		}
		if tab.group != nil && menu.Item("移出分组").Clicked() {
			c.After(func() { m.moveToGroup(tab, nil) })
		}
		menu.Separator()
		for _, action := range []struct {
			label string
			tabs  []*editorTab
		}{
			{"关闭全部标签页", m.tabs},
			{"关闭左侧全部标签页", m.tabs[:index]},
			{"关闭右侧全部标签页", m.tabs[index+1:]},
		} {
			if menu.Item(action.label).Disabled(!hasUnpinnedTab(action.tabs)).Clicked() {
				tabs := slices.Clone(action.tabs)
				c.After(func() { m.closeTabs(tabs, true) })
			}
		}
	})
}

func hasUnpinnedTab(tabs []*editorTab) bool {
	return slices.ContainsFunc(tabs, func(tab *editorTab) bool { return !tab.isPinned() })
}

type tabDragSource struct {
	first, end int
	group      *tabGroup
}

func (source tabDragSource) firstSlot() int {
	if source.group != nil {
		return 2 * source.first
	}
	return 2*source.first + 1
}

type tabDropPosition struct {
	index int
	group *tabGroup
}

func (m *editorApp) tabDragSource(hit gfx.ID) tabDragSource {
	if !hit.IsZero() {
		for index, tab := range m.tabs {
			if tab.tabHit == hit && tabVisible(tab) {
				return tabDragSource{first: index, end: index + 1}
			}
			if tab.group != nil && tab.group.hit == hit {
				_, end := m.groupTabs(tab.group)
				return tabDragSource{first: index, end: end, group: tab.group}
			}
		}
	}
	return tabDragSource{first: -1}
}

// Drop positions are measured between tabs, before removing the dragged range.
func (m *editorApp) tabDropDestination(source tabDragSource, target gfx.ID) tabDropPosition {
	invalid := tabDropPosition{index: -1}
	if source.first < 0 || target.IsZero() {
		return invalid
	}
	pinned := m.tabs[source.first].pinned
	if source.group != nil {
		pinned = source.group.Pinned
	}
	if target == m.tabEndHit && !pinned {
		return tabDropPosition{index: len(m.tabs)}
	}
	for index, tab := range m.tabs {
		group := tab.group
		if tab.tabHit == target && tabVisible(tab) && source.group == nil {
			if group == nil && tab.pinned != pinned {
				return invalid
			}
			if source.first < index {
				index++
			}
			return tabDropPosition{index: index, group: group}
		}
		if group == nil {
			if tab.tabHit != target || tab.pinned != pinned {
				continue
			}
			if source.first < index {
				index++
			}
			return tabDropPosition{index: index}
		}
		switch target {
		case group.hit, group.beforeDrop, group.afterDrop, tab.tabHit:
		default:
			continue
		}
		first, end := m.groupTabs(group)
		if source.group == nil && target == group.hit {
			return tabDropPosition{index: end, group: group}
		}
		if source.group == group || group.Pinned != pinned {
			return invalid
		}
		if target == group.afterDrop || target != group.beforeDrop && source.first < first {
			return tabDropPosition{index: end}
		}
		return tabDropPosition{index: first}
	}
	return invalid
}

func tabDropChangesOrder(source tabDragSource, target tabDropPosition, tabs []*editorTab) bool {
	if target.index < 0 {
		return false
	}
	if source.group != nil {
		return target.index < source.first || target.index > source.end
	}
	return target.index < source.first || target.index > source.end || tabs[source.first].group != target.group
}

func (m *editorApp) acceptTabDrop(source, target gfx.ID) bool {
	dragged := m.tabDragSource(source)
	return tabDropChangesOrder(dragged, m.tabDropDestination(dragged, target), m.tabs)
}

func (m *editorApp) dropTab(source, target gfx.ID) {
	dragged := m.tabDragSource(source)
	position := m.tabDropDestination(dragged, target)
	if !tabDropChangesOrder(dragged, position, m.tabs) {
		return
	}
	index := position.index
	if dragged.first < index {
		index -= dragged.end - dragged.first
	}
	if dragged.group == nil {
		m.moveTab(dragged.first, index, position.group)
		return
	}
	members := slices.Clone(m.tabs[dragged.first:dragged.end])
	m.tabs = slices.Insert(slices.Delete(m.tabs, dragged.first, dragged.end), index, members...)
}

func (m *editorApp) registerTabDrop(d *ui.DrawContext, id gfx.ID, slot, strip gfx.Rect, accept func(gfx.ID, gfx.ID) bool, commit func(gfx.ID, gfx.ID)) {
	left, right := max(slot.X, strip.X), min(slot.X+slot.Width, strip.X+strip.Width)
	if !id.IsZero() && right > left {
		d.DropTarget(&m.tabDrag, id, gfx.Rect{X: left, Y: slot.Y, Width: right - left, Height: slot.Height}, accept, commit)
	}
}

func (m *editorApp) registerGroupDrop(d *ui.DrawContext, group *tabGroup, slot, strip gfx.Rect, accept func(gfx.ID, gfx.ID) bool, commit func(gfx.ID, gfx.ID)) {
	edge := min(18, slot.Width/4)
	before := gfx.Rect{X: slot.X, Y: slot.Y, Width: edge, Height: slot.Height}
	inside := gfx.Rect{X: slot.X + edge, Y: slot.Y, Width: slot.Width - 2*edge, Height: slot.Height}
	after := gfx.Rect{X: slot.X + slot.Width - edge, Y: slot.Y, Width: edge, Height: slot.Height}
	m.registerTabDrop(d, group.beforeDrop, before, strip, accept, commit)
	m.registerTabDrop(d, group.hit, inside, strip, accept, commit)
	m.registerTabDrop(d, group.afterDrop, after, strip, accept, commit)
}

func (m *editorApp) scrollTabDrag(d *ui.DrawContext, state *ui.ScrollState) {
	at, strip := m.tabDrag.At(), d.Rect
	if at.X < strip.X || at.X >= strip.X+strip.Width || at.Y < strip.Y || at.Y >= strip.Y+strip.Height {
		return
	}
	var step float32
	if at.X < strip.X+28 {
		step = -24
	} else if at.X > strip.X+strip.Width-28 {
		step = 24
	}
	next := max(0, min(state.OffsetX+step, state.MaxX()))
	if next == state.OffsetX {
		return
	}
	now := d.Canvas.Now()
	if now >= m.tabScrollAt {
		state.ScrollToX(next)
		m.tabScrollAt = now + 32*time.Millisecond
	}
	d.Canvas.RequestFrameIn(m.tabScrollAt - now)
}
