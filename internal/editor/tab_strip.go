package editor

import (
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

const (
	tabStripItemTop       = float32(4)
	tabStripItemSize      = float32(34)
	tabStripRadius        = tabStripItemSize / 2
	tabStripHeight        = tabStripItemTop + tabStripItemSize + 6
	tabStripTitleSize     = float32(13)
	tabStripInset         = float32(3)
	tabStripPaddingLeft   = float32(10)
	tabStripPaddingRight  = float32(5)
	tabStripGap           = float32(5)
	tabStripIconSize      = float32(16)
	tabStripButtonSize    = float32(24)
	tabStripDirtySize     = float32(8)
	tabStripCountSize     = float32(11)
	tabStripMaxTabWidth   = float32(260)
	tabStripMaxGroupWidth = float32(230)
)

func (m *editorApp) tabStrip(c ui.C, scroll ui.Box, blocked bool) {
	state := scroll.ScrollState()
	dragged := tabDragSource{first: -1}
	drop := tabDropPosition{index: -1}
	dropSlot := -1
	var dragWidth, dropX float32
	drawDropLine := false
	if m.tabDrag.Dragging() && !blocked {
		dragged = m.tabDragSource(m.tabDrag.Source())
		drop = m.tabDropDestination(dragged, m.tabDrag.Target())
		if tabDropChangesOrder(dragged, drop, m.tabs) {
			dropSlot = 2 * drop.index
			if drop.group != nil && drop.index < len(m.tabs) && m.tabs[drop.index].group == drop.group &&
				(drop.index == 0 || m.tabs[drop.index-1].group != drop.group) {
				dropSlot++
			}
		}
		if dragged.group != nil {
			dragWidth = dragged.group.bounds.Width
			if m.groupGhost != dragged.group {
				m.groupGhost, m.tabGhost = dragged.group, nil
				m.tabGrabX = max(8, m.tabDrag.GetStart().X-dragged.group.bounds.X)
			}
		} else if dragged.first >= 0 {
			tab := m.tabs[dragged.first]
			dragWidth = tab.tabBounds.Width
			if m.tabGhost != tab {
				m.tabGhost, m.groupGhost = tab, nil
				m.tabGrabX = min(dragWidth-8, max(8, m.tabDrag.GetStart().X-tab.tabBounds.X))
			}
		}
		scroll.DrawOver(func(d *ui.DrawContext) {
			m.paintTabGhost(d, dragWidth, dragged.end-dragged.first)
			if drawDropLine && dropX >= d.Rect.X && dropX < d.Rect.X+d.Rect.Width {
				marker := gfx.Rect{X: dropX - 1.5, Y: d.Rect.Y + 6, Width: 3, Height: tabStripItemSize + 1}
				d.Canvas.Rounded(marker, 1.5, m.theme.Ring)
			}
			m.scrollTabDrag(d, state)
		})
	} else {
		m.tabGhost, m.groupGhost = nil, nil
		m.tabScrollAt = 0
	}

	accept, commit := m.acceptTabDrop, m.dropTab
	var activeLeft, activeRight float32
	scroll.Children(func() {
		ui.Row(c).FillWidth().Height(tabStripHeight).Children(func() {
			var left float32
			addTab := func(index int) {
				tab := m.tabs[index]
				modified := tab.modified()
				width := tabCardWidth(c.Canvas(), tab.tabTitle(), modified)
				if tab == m.active {
					activeLeft, activeRight = left, left+width
				}
				left += width
				slot := ui.Stack(c.KeyInt(tab.id)).Width(width).Height(tabStripHeight).Shrink(0)
				if index < dragged.first || index >= dragged.end || dragged.first < 0 {
					slot.Children(func() {
						dx := tabSlide(c, tab.tabHit, dragged, dropSlot, 2*index+1, dragWidth)
						m.drawTabCard(c, tab, index, width, dx, modified, blocked)
					})
				}
				slot.DrawOver(func(d *ui.DrawContext) {
					tab.tabBounds = d.Rect
					if dropSlot >= 0 && drop.group == nil && drop.index == index && tab.group == nil {
						dropX, drawDropLine = d.Rect.X+2, true
					}
					if !blocked {
						m.registerTabDrop(d, tab.tabHit, d.Rect, scroll.Rect(), accept, commit)
					}
				})
			}
			for index := 0; index < len(m.tabs); {
				group := m.tabs[index].group
				if group == nil {
					addTab(index)
					index++
					continue
				}
				end := index + 1
				for end < len(m.tabs) && m.tabs[end].group == group {
					end++
				}
				first, members := index, end-index
				width := tabGroupHeaderWidth(c.Canvas(), group, members)
				groupLeft := left
				var groupWidth, headerOffset float32
				joining := dropSlot >= 0 && drop.group == group
				var widthChange float32
				if dropSlot >= 0 && dragged.group == nil && !group.Collapsed {
					original := m.tabs[dragged.first].group
					if original == group && !joining {
						widthChange = -dragWidth
					} else if original != group && joining {
						widthChange = dragWidth
					}
				}
				slot := ui.Stack(c.KeyInt(-group.ID)).Width(width).Height(tabStripHeight).Shrink(0)
				slot.Children(func() {
					group.beforeDrop = ui.ID(c.Part("insert-before"))
					group.afterDrop = ui.ID(c.Part("insert-after"))
					if group != dragged.group {
						headerOffset = tabSlide(c, group.hit, dragged, dropSlot, 2*index, dragWidth)
						m.drawTabGroupHeader(c, group, members, width, headerOffset, joining, blocked)
					}
				})
				if group != dragged.group {
					slot.Draw(func(d *ui.DrawContext) {
						accent := group.accent()
						line := gfx.Rect{X: d.Rect.X + headerOffset + 8, Y: d.Rect.Y + tabStripHeight - 4, Width: groupWidth + widthChange - 16, Height: 2}
						if joining {
							line.Y--
							line.Height = 4
						} else {
							accent.Alpha = .72
						}
						d.Canvas.Rounded(line, line.Height/2, d.Fade(accent))
					})
				}
				slot.DrawOver(func(d *ui.DrawContext) {
					group.bounds = d.Rect
					group.bounds.Width = groupWidth
					if dropSlot >= 0 && drop.group == nil && drop.index == first {
						dropX, drawDropLine = d.Rect.X+2, true
					}
					if !blocked {
						m.registerGroupDrop(d, group, d.Rect, scroll.Rect(), accept, commit)
					}
				})
				left += width
				if !group.Collapsed {
					for member := index; member < end; member++ {
						addTab(member)
					}
				}
				groupWidth = left - groupLeft
				index = end
			}
			blank := ui.Slot(c).Grow(1).MinWidth(44).FillHeight()
			m.tabEndHit = ui.ID(c)
			blank.HitWith(m.tabEndHit, "双击新建", gfx.HitOptions{Role: gfx.RoleButton, Disabled: blocked})
			blank.DrawOver(func(d *ui.DrawContext) {
				if dropSlot >= 0 && drop.group == nil && drop.index == len(m.tabs) {
					dropX, drawDropLine = d.Rect.X+2, true
				}
				if !blocked {
					m.registerTabDrop(d, m.tabEndHit, d.Rect, scroll.Rect(), accept, commit)
				}
			})
			if !blocked && blank.DoubleClicked() {
				c.After(m.newDocument)
			}
		})
	})
	if m.tabReveal && activeRight > activeLeft {
		if state.ViewportWidth == 0 || activeLeft < state.OffsetX {
			state.ScrollToX(activeLeft)
		} else if activeRight > state.OffsetX+state.ViewportWidth {
			state.ScrollToX(activeRight - state.ViewportWidth)
		}
	}
	m.tabReveal = false
}

func tabSlide(c ui.C, id gfx.ID, source tabDragSource, to, slot int, width float32) float32 {
	var dx float32
	if to >= 0 {
		first, end := source.firstSlot(), 2*source.end
		switch {
		case to < first && slot >= to && slot < first:
			dx = width
		case to > end && slot >= end && slot < to:
			dx = -width
		}
	}
	motion := gfx.Motion{}
	if source.first >= 0 {
		motion = gfx.Motion{Duration: 120 * time.Millisecond, Easing: gfx.EaseOut}
	}
	if canvas := c.Canvas(); canvas != nil {
		return canvas.Animate(id, 0, dx, motion)
	}
	return dx
}

func tabCardWidth(canvas *gfx.Canvas, title string, modified bool) float32 {
	if canvas == nil {
		return tabStripMaxTabWidth
	}
	const controlsWidth = 2*tabStripInset + tabStripPaddingLeft + tabStripPaddingRight +
		tabStripIconSize + 2*tabStripButtonSize + 3*tabStripGap
	width := controlsWidth + canvas.TextWidth(title, tabStripTitleSize) + 2
	if modified {
		width += tabStripGap + canvas.TextWidth("●", tabStripDirtySize)
	}
	return min(tabStripMaxTabWidth, float32(math.Ceil(float64(width))))
}

func (m *editorApp) drawTabCard(c ui.C, tab *editorTab, index int, width, dx float32, modified, blocked bool) {
	active := tab == m.active && !m.vaultActive()
	card := ui.Column(c).Absolute().Left(tabStripInset+dx).Top(tabStripItemTop).Size(width-2*tabStripInset, tabStripItemSize)
	if active || tab.group == nil {
		background := m.theme.Background
		if active {
			background = m.theme.CardElevated
		}
		card.Radius(tabStripRadius).Background(background)
	}
	buttonRecipe := m.pinButtonRecipe
	if tab.group != nil {
		buttonRecipe = groupedTabButtonRecipe
	}
	card.Children(func() {
		title := tab.tabTitle()
		label := title
		if modified {
			label = "● " + label
		}
		target := ui.Row(c).FillWidth().Grow(1).PadLeft(tabStripPaddingLeft).PadRight(tabStripPaddingRight).
			Gap(tabStripGap).Items(ui.Center).Radius(tabStripRadius).Hover(ui.Look{}.Background(m.theme.Hover))
		target.HitWith(ui.ID(c), label, gfx.HitOptions{Role: gfx.RoleButton, Selected: active, Disabled: blocked})
		if tab.group != nil {
			target.DrawOver(func(d *ui.DrawContext) {
				drawGroupUnderlineHighlight(d, tab.group.accent())
			})
		}
		tab.tabHit = target.N().ID
		if !blocked {
			target.DragSource(&m.tabDrag)
		}
		if !m.tabDrag.Dragging() {
			hint := tab.doc.path
			if width == tabStripMaxTabWidth && (hint == "" || tab.displayName != "") {
				hint = title
				if tab.doc.path != "" {
					hint += "\n" + tab.doc.path
				}
			}
			target.Tooltip(hint)
		}

		target.Children(func() {
			icon, color := m.fileAppearance(tab.title())
			ui.Icon(c, icon).Size(tabStripIconSize, tabStripIconSize).Shrink(0).Color(color)
			name := ui.Text(c, title).FontSize(tabStripTitleSize).Grow(1).MinWidth(0).Ellipsis()
			if !active {
				name.Muted()
			}
			if modified {
				ui.Text(c, "●").FontSize(tabStripDirtySize).Shrink(0).Color(m.theme.Ring)
			}
			pinLabel := "固定 " + title
			if tab.pinned {
				pinLabel = "取消固定 " + title
			}
			pin := ui.Button(c, pinLabel).Icon(pinStateIcon(tab.pinned)).IconOnly().Ghost().
				Size(tabStripButtonSize, tabStripButtonSize).Shrink(0).ButtonRecipe(buttonRecipe).Tooltip(pinLabel).Disabled(blocked)
			if tab.pinned {
				pin.Secondary()
			}
			if pin.Clicked() {
				c.After(func() { m.togglePin(tab) })
			}
			closeHint := m.shortcutHint("关闭", shortcutCloseTab)
			closeButton := ui.Button(c, "关闭 "+title).Icon(closeIcon).IconOnly().Ghost().
				Size(tabStripButtonSize, tabStripButtonSize).Shrink(0).ButtonRecipe(buttonRecipe).
				Tooltip(closeHint).Disabled(blocked)
			showClose, hoveringControl := tabStripCloseVisible(c.Canvas(), tab.tabHit, pin.N().ID, closeButton.N().ID, blocked || m.tabDrag.Dragging())
			if hoveringControl {
				target.Background(m.theme.Hover)
			}
			if !showClose {
				closeButton.Opacity(0)
			}
			if showClose && closeButton.Clicked() {
				c.After(func() { m.closeTab(tab) })
			}
		})
		if !blocked {
			if target.DoubleClicked() {
				c.After(func() { m.rename.begin(tab, nil) })
			} else if target.Clicked() {
				c.After(func() { m.activate(tab) })
			}
			m.tabContextMenu(c, target, index, tab)
		}
	})
}

func tabGroupHeaderWidth(canvas *gfx.Canvas, group *tabGroup, members int) float32 {
	if canvas == nil {
		return tabStripMaxGroupWidth
	}
	const controlsWidth = 2*tabStripInset + tabStripPaddingLeft + tabStripPaddingRight +
		tabStripIconSize + 2*tabStripButtonSize + 3*tabStripGap
	width := controlsWidth + canvas.TextWidth(group.Name, tabStripTitleSize) + 2
	if group.Collapsed {
		width += tabStripGap + canvas.TextWidth(strconv.Itoa(members), tabStripCountSize)
	}
	return min(tabStripMaxGroupWidth, float32(math.Ceil(float64(width))))
}

func (m *editorApp) drawTabGroupHeader(c ui.C, group *tabGroup, members int, width, dx float32, joining, blocked bool) {
	header := ui.Row(c).Absolute().Left(tabStripInset+dx).Top(tabStripItemTop).Size(width-2*tabStripInset, tabStripItemSize).
		PadLeft(tabStripPaddingLeft).PadRight(tabStripPaddingRight).Gap(tabStripGap).Items(ui.Center)
	if joining {
		tint := group.accent()
		tint.Alpha = .22
		header.Radius(tabStripRadius).Background(tint.Over(m.theme.Background)).
			Border(1, group.accent())
	}
	description := fmt.Sprintf("%d 个标签", members)
	header.HitWith(ui.ID(c), group.label(), gfx.HitOptions{
		Role: gfx.RoleButton, Expandable: true, Expanded: !group.Collapsed, Disabled: blocked, Description: description,
	}).Tooltip(group.Name + " · " + description + " · 双击重命名")
	if !joining {
		header.DrawOver(func(d *ui.DrawContext) {
			drawGroupUnderlineHighlight(d, group.accent())
		})
	}
	group.hit = header.N().ID
	if !blocked {
		header.DragSource(&m.tabDrag)
	}
	header.Children(func() {
		icon := folderOpenIcon
		if group.Collapsed {
			icon = folderIcon
		}
		ui.Icon(c, icon).Size(tabStripIconSize, tabStripIconSize).Shrink(0).Color(group.accent())
		ui.Text(c, group.Name).FontSize(tabStripTitleSize).Grow(1).MinWidth(0).Ellipsis()
		if group.Collapsed {
			ui.Text(c, strconv.Itoa(members)).FontSize(tabStripCountSize).Muted().Shrink(0)
		}
		label := group.pinLabel() + " " + group.Name
		pin := ui.Button(c, label).Icon(pinStateIcon(group.Pinned)).IconOnly().Ghost().
			Size(tabStripButtonSize, tabStripButtonSize).Shrink(0).ButtonRecipe(groupedTabButtonRecipe).Tooltip(label).Disabled(blocked)
		if group.Pinned {
			pin.Secondary()
		}
		if pin.Clicked() {
			c.After(func() { m.toggleGroupPin(group) })
		}
		closeButton := ui.Button(c, "关闭分组 "+group.Name).Icon(closeIcon).IconOnly().Ghost().
			Size(tabStripButtonSize, tabStripButtonSize).Shrink(0).ButtonRecipe(groupedTabButtonRecipe).
			Tooltip("关闭分组").Disabled(blocked)
		showClose, _ := tabStripCloseVisible(c.Canvas(), group.hit, pin.N().ID, closeButton.N().ID, blocked || m.tabDrag.Dragging())
		if !showClose {
			closeButton.Opacity(0)
		}
		if showClose && closeButton.Clicked() {
			c.After(func() { m.closeTabGroup(group) })
		}
	})
	if !blocked {
		if header.Clicked() {
			doubleClick := header.DoubleClicked()
			c.After(func() { m.clickTabGroup(group, doubleClick) })
		}
		m.groupContextMenu(c, header, group)
	}
}

func tabStripCloseVisible(canvas *gfx.Canvas, card, pin, close gfx.ID, blocked bool) (visible, hoveringControl bool) {
	if blocked || canvas == nil {
		return false, false
	}
	hovered, focused := canvas.Hovered(), canvas.FocusVisible()
	hoveringControl = hovered == pin || hovered == close
	return hoveringControl || hovered == card || focused == card || focused == pin || focused == close, hoveringControl
}

func drawGroupUnderlineHighlight(d *ui.DrawContext, accent gfx.Color) {
	if d.State.Disabled || !d.State.Hovered && !d.State.FocusVisible {
		return
	}
	line := gfx.Rect{X: d.Rect.X + 6, Y: d.Rect.Y + d.Rect.Height + 2, Width: d.Rect.Width - 12, Height: 2}
	d.Canvas.Rounded(line, 1, d.Fade(accent))
}

func (m *editorApp) paintTabGhost(d *ui.DrawContext, slotWidth float32, count int) {
	tab, group := m.tabGhost, m.groupGhost
	if tab == nil && group == nil {
		return
	}
	width := min(slotWidth-2*tabStripInset, d.Rect.Width)
	const textLeft = tabStripPaddingLeft + tabStripIconSize + tabStripGap
	if group != nil {
		width = min(width, tabStripMaxGroupWidth)
	}
	if width <= textLeft+tabStripPaddingRight {
		return
	}
	at := m.tabDrag.At()
	grabX := min(m.tabGrabX, width-8)
	bounds := gfx.Rect{X: max(d.Rect.X, min(at.X-grabX+tabStripInset, d.Rect.X+d.Rect.Width-width)), Y: d.Rect.Y + tabStripItemTop, Width: width, Height: tabStripItemSize}
	var title string
	var icon gfx.Icon
	accent := m.theme.Ring
	var tint gfx.Color
	if group != nil {
		title = group.Name + " · " + strconv.Itoa(count)
		icon, tint = folderOpenIcon, group.accent()
		if group.Collapsed {
			icon = folderIcon
		}
		accent = tint
	} else {
		title = tab.tabTitle()
		icon, tint = m.fileAppearance(tab.title())
		if tab.group != nil {
			accent = tab.group.accent()
		}
	}
	d.Canvas.Elevate(bounds, tabStripRadius, gfx.ShadowSM)
	d.Canvas.RoundedBorder(bounds, tabStripRadius, 1, accent, m.theme.CardElevated)
	d.Canvas.DrawIcon(icon, gfx.Rect{X: bounds.X + tabStripPaddingLeft, Y: bounds.Y + (tabStripItemSize-tabStripIconSize)/2, Width: tabStripIconSize, Height: tabStripIconSize}, tint)
	style := gfx.TextStyle{Size: tabStripTitleSize, Color: m.theme.Foreground}
	textBounds := gfx.Rect{X: bounds.X + textLeft, Y: bounds.Y + (tabStripItemSize-24)/2, Width: bounds.Width - textLeft - tabStripPaddingRight, Height: 24}
	var lines [1]string
	if visible := d.Canvas.WrapLines(lines[:0], title, style.Size, style.Weight, textBounds.Width, 1); len(visible) != 0 {
		d.Canvas.Label(visible[0], textBounds, style)
	}
}
