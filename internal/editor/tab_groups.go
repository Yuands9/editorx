package editor

import (
	"slices"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

type tabGroup struct {
	ID                         int
	Name                       string
	Color                      int
	Collapsed                  bool
	Pinned                     bool `json:",omitempty"`
	hit, beforeDrop, afterDrop gfx.ID
	bounds                     gfx.Rect
}

type tabGroupClick struct {
	groupID   int
	activeID  int
	collapsed bool
}

var tabGroupColors = []struct {
	name  string
	color gfx.Color
}{
	{"蓝色", gfx.RGB(105, 161, 245)},
	{"绿色", gfx.RGB(99, 181, 138)},
	{"紫色", gfx.RGB(183, 145, 236)},
	{"橙色", gfx.RGB(226, 158, 84)},
	{"粉色", gfx.RGB(230, 135, 172)},
	{"青色", gfx.RGB(82, 183, 195)},
}

func (g *tabGroup) label() string { return "分组：" + g.Name }

func (g *tabGroup) pinLabel() string {
	if g.Pinned {
		return "取消固定分组"
	}
	return "固定分组"
}

func (t *editorTab) isPinned() bool {
	return t.pinned || (t.group != nil && t.group.Pinned)
}

// 分组作为整体排序，组员单独固定时仍留在原组内。
func (t *editorTab) pinnedInTabStrip() bool {
	if t.group != nil {
		return t.group.Pinned
	}
	return t.pinned
}

func (m *editorApp) sortPinnedTabs() {
	slices.SortStableFunc(m.tabs, func(a, b *editorTab) int {
		first, second := a.pinnedInTabStrip(), b.pinnedInTabStrip()
		if first == second {
			return 0
		}
		if first {
			return -1
		}
		return 1
	})
}

func (m *editorApp) togglePin(tab *editorTab) {
	tab.pinned = !tab.pinned
	if tab.group == nil {
		m.sortPinnedTabs()
		m.tabReveal = true
	}
}

func (m *editorApp) toggleGroupPin(group *tabGroup) {
	group.Pinned = !group.Pinned
	m.sortPinnedTabs()
	m.tabReveal = true
}

func (g *tabGroup) accent() gfx.Color {
	return tabGroupColors[max(0, min(g.Color, len(tabGroupColors)-1))].color
}

func (m *editorApp) groupTabs(group *tabGroup) (first, end int) {
	first = slices.IndexFunc(m.tabs, func(t *editorTab) bool { return t.group == group })
	if first < 0 {
		return -1, -1
	}
	end = first + 1
	for end < len(m.tabs) && m.tabs[end].group == group {
		end++
	}
	return first, end
}

func (m *editorApp) newTabGroup(tab *editorTab) {
	usedNames := make(map[string]bool)
	for _, other := range m.tabs {
		if other.group != nil {
			usedNames[other.group.Name] = true
		}
	}
	m.nextGroupID++
	m.moveToGroup(tab, &tabGroup{ID: m.nextGroupID, Name: nextUntitledName(usedNames), Color: (m.nextGroupID - 1) % len(tabGroupColors)})
}

// An added tab goes to the end of the destination group. Removing it from its
// group leaves it immediately after its former neighbors.
func (m *editorApp) moveToGroup(tab *editorTab, group *tabGroup) {
	from := slices.Index(m.tabs, tab)
	if from < 0 || tab.group == group {
		return
	}
	end := from + 1
	if group != nil {
		if _, last := m.groupTabs(group); last >= 0 {
			end = last
		} else if tab.group != nil {
			_, end = m.groupTabs(tab.group)
		}
	} else {
		_, end = m.groupTabs(tab.group)
	}
	if from < end {
		end--
	}
	m.moveTab(from, end, group)
	m.tabReveal = true
}

func (m *editorApp) moveTab(from, to int, group *tabGroup) {
	tab := m.tabs[from]
	if from < to {
		copy(m.tabs[from:to], m.tabs[from+1:to+1])
	} else if from > to {
		copy(m.tabs[to+1:from+1], m.tabs[to:from])
	}
	m.tabs[to] = tab
	tab.group = group
	if group != nil && tab == m.active {
		group.Collapsed = false
	}
	m.sortPinnedTabs()
}

func tabVisible(tab *editorTab) bool { return tab.group == nil || !tab.group.Collapsed }

func (m *editorApp) visibleTabNear(index int) *editorTab {
	for _, tab := range m.tabs[index:] {
		if tabVisible(tab) {
			return tab
		}
	}
	for i := index - 1; i >= 0; i-- {
		if tabVisible(m.tabs[i]) {
			return m.tabs[i]
		}
	}
	return nil
}

func (m *editorApp) stepTab(step int) {
	index := slices.Index(m.tabs, m.active)
	for range m.tabs {
		index = (index + step + len(m.tabs)) % len(m.tabs)
		if tabVisible(m.tabs[index]) {
			m.activate(m.tabs[index])
			return
		}
	}
}

// A group's first click can move the tabs under the pointer. Match the second
// click to the first group before the tab strip handles the new hit target.
func (m *editorApp) handleGroupDoubleClick(c ui.C) bool {
	if m.groupClick.groupID == 0 || c.Canvas() == nil {
		return false
	}
	click, delivered := c.Canvas().ClickInfo()
	if !delivered {
		return false
	}
	if click.Clicks >= 2 {
		for _, tab := range m.tabs {
			if group := tab.group; group != nil && group.ID == m.groupClick.groupID {
				c.After(func() { m.clickTabGroup(group, true) })
				return true
			}
		}
	}
	m.groupClick = tabGroupClick{}
	return false
}

func (m *editorApp) clickTabGroup(group *tabGroup, doubleClick bool) {
	if !doubleClick {
		m.groupClick = tabGroupClick{groupID: group.ID, collapsed: group.Collapsed}
		if m.active != nil {
			m.groupClick.activeID = m.active.id
		}
		m.toggleTabGroup(group)
		return
	}

	// 单击先响应折叠，第二击到达后还原双击之前的状态。
	previous := m.groupClick
	m.groupClick = tabGroupClick{}
	if previous.groupID == group.ID {
		group.Collapsed = previous.collapsed
		if previous.activeID == 0 {
			m.activate(nil)
		} else {
			for _, tab := range m.tabs {
				if tab.id == previous.activeID && m.active != tab {
					m.activate(tab)
					break
				}
			}
		}
	}
	m.rename.begin(nil, group)
}

func (m *editorApp) toggleTabGroup(group *tabGroup) {
	group.Collapsed = !group.Collapsed
	if !group.Collapsed {
		if m.active == nil {
			first, _ := m.groupTabs(group)
			m.activate(m.tabs[first])
		}
		return
	}
	if m.active != nil && m.active.group == group {
		_, end := m.groupTabs(group)
		m.activate(m.visibleTabNear(end))
	}
}

func (m *editorApp) closeTabGroup(group *tabGroup) {
	first, end := m.groupTabs(group)
	if first >= 0 {
		m.closeTabs(slices.Clone(m.tabs[first:end]), false)
	}
}

func (m *editorApp) groupContextMenu(c ui.C, target ui.Box, group *tabGroup) {
	ui.ContextMenu(c, target, func(menu *ui.MenuItems) {
		if menu.Item(group.pinLabel()).Icon(pinStateIcon(group.Pinned)).Clicked() {
			c.After(func() { m.toggleGroupPin(group) })
		}
		if menu.Item("重命名分组…").Clicked() {
			c.After(func() { m.rename.begin(nil, group) })
		}
		menu.Sub("分组颜色", func(colors *ui.MenuItems) {
			for index, color := range tabGroupColors {
				if colors.Item(color.name).Checked(index == group.Color).Clicked() {
					c.After(func() { group.Color = index })
				}
			}
		})
		menu.Separator()
		if menu.Item("解散分组").Clicked() {
			c.After(func() {
				first, end := m.groupTabs(group)
				if first < 0 {
					return
				}
				for _, tab := range m.tabs[first:end] {
					tab.group = nil
				}
				m.sortPinnedTabs()
				if m.active == nil {
					m.activate(m.visibleTabNear(0))
				}
				m.tabReveal = true
			})
		}
		if menu.Item("关闭整个分组").Icon(closeIcon).Clicked() {
			c.After(func() { m.closeTabGroup(group) })
		}
	})
}
