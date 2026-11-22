package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
)

func TestUntitledNameReuseThroughUI(t *testing.T) {
	m := newTestEditorWithDocument()
	first := m.active
	m.addTab(document{saved: "已打开的配置", eol: "LF"}, "config.txt")
	m.newDocument()
	second := m.active
	if second.title() != "未命名-2" {
		t.Fatalf("named tabs consumed a number: %q", second.title())
	}
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(s.ClickLabel("关闭 " + first.title()))
	check(s.ClickLabel("关闭 " + second.title()))
	check(s.DoubleClickLabel("双击新建"))
	reused := m.active
	if reused.title() != "未命名-1" || reused.id <= second.id || reused.source.text.String() != "" {
		t.Fatal("closed number was not reused with fresh editor state")
	}
	check(s.Compose("zhongwen"))
	check(s.Type("保留中文"))
	check(s.ClickLabel("关闭 " + reused.title()))
	check(s.ClickLabel("取消"))
	check(s.DoubleClickLabel("双击新建"))
	if m.active.title() != "未命名-2" || reused.source.text.String() != "保留中文" {
		t.Fatal("canceled close released its number or lost text")
	}
	m.activate(reused)
	check(s.Frame())
	savedPath := filepath.Join(t.TempDir(), "已保存.txt")
	check(s.Driver().QueueSaveFile(savedPath, nil))
	if !m.saveTab(reused, false) {
		t.Fatal("save failed: " + m.errorText)
	}
	check(s.Frame())
	check(s.DoubleClickLabel("双击新建"))
	if m.active.title() != "未命名-1" || m.active.id <= reused.id {
		t.Fatal("saved document did not release its display number")
	}
	check(s.Compose("xinwenjian"))
	check(s.Type("新文件中文"))
	if reused.source.text.String() != "保留中文" || m.active.source.text.String() != "新文件中文" {
		t.Fatal("reused name leaked editor or IME state")
	}
}

func TestUntitledNameHolesAndLastTab(t *testing.T) {
	m := newTestEditorWithDocument()
	first := m.active
	m.addTab(document{path: filepath.Join(t.TempDir(), "未命名-2"), eol: "LF"}, "")
	m.newDocument()
	third := m.active
	if third.title() != "未命名-3" {
		t.Fatal("collided with an open file title")
	}
	m.togglePin(third)
	m.closeTab(first)
	m.newDocument()
	if m.active.title() != "未命名-1" || third.title() != "未命名-3" || !third.pinned {
		t.Fatal("smallest hole was not reused or existing tab changed")
	}
	m = newTestEditorWithDocument()
	first = m.active
	m.closeTab(first)
	if len(m.tabs) != 0 || m.active != nil {
		t.Fatal("closing the last tab must leave an empty window")
	}
	m.newDocument()
	if m.active.title() != "未命名-1" || m.active.id == first.id {
		t.Fatal("explicit new document did not reuse the name with a fresh tab")
	}
}

func TestTabHeaderAndPin(t *testing.T) {
	for _, light := range []bool{false, true} {
		m := newTestEditorWithDocument()
		m.setLightMode(light)
		m.newDocument()
		pinTarget := m.active
		m.newDocument()
		s := openTestSession(t, m)
		check := func(err error) {
			t.Helper()
			if err != nil {
				t.Fatal(err)
			}
		}
		check(s.Move(800, 700))
		hasIconColor := func(label string, color gfx.Color) bool {
			t.Helper()
			hit, err := s.Locate(label)
			check(err)
			for _, primitive := range s.Snapshot().Primitives {
				point := primitive.Points[0]
				if primitive.Color == color && point.X > hit.Bounds.X && point.Y > hit.Bounds.Y && point.X < hit.Bounds.X+hit.Bounds.Width && point.Y < hit.Bounds.Y+hit.Bounds.Height {
					return true
				}
			}
			return false
		}
		checkIconColor := func(label string, color gfx.Color) {
			t.Helper()
			if !hasIconColor(label, color) {
				t.Fatalf("tab icon %q is hidden or has the wrong color", label)
			}
		}
		checkIconColor("固定 "+pinTarget.title(), m.theme.MutedForeground)
		closeLabel := "关闭 " + pinTarget.title()
		if hasIconColor(closeLabel, m.theme.MutedForeground) {
			t.Fatal("tab close icon was painted without hover")
		}
		tabHit, err := s.Locate(pinTarget.title())
		check(err)
		check(s.Move(tabHit.Bounds.X+40, tabHit.Bounds.Y+tabHit.Bounds.Height/2))
		checkIconColor(closeLabel, m.theme.MutedForeground)
		closeHit, err := s.Locate(closeLabel)
		check(err)
		check(s.Move(closeHit.Bounds.X+closeHit.Bounds.Width/2, closeHit.Bounds.Y+closeHit.Bounds.Height/2))
		checkIconColor(closeLabel, m.theme.MutedForeground)
		still, err := s.Locate(pinTarget.title())
		check(err)
		if still.Bounds != tabHit.Bounds {
			t.Fatal("tab geometry changed when its close button appeared")
		}
		check(s.Move(800, 700))
		if hasIconColor(closeLabel, m.theme.MutedForeground) {
			t.Fatal("tab close icon remained visible after leaving")
		}
		count := 0
		for _, text := range s.Texts() {
			if text == m.active.title() {
				count++
			}
		}
		editor, err := s.Locate("代码编辑区")
		check(err)
		if count != 1 || editor.Bounds.Y > 65 {
			t.Fatalf("duplicate filename or empty header remains: count=%d, y=%g", count, editor.Bounds.Y)
		}
		check(s.ClickLabel("固定 " + pinTarget.title()))
		check(s.Move(800, 700))
		if m.tabs[0] != pinTarget || !pinTarget.pinned {
			t.Fatal("pin failed")
		}
		color := gfx.RGB(104, 169, 255)
		if light {
			color = gfx.RGB(35, 99, 196)
		}
		checkIconColor("取消固定 "+pinTarget.title(), color)
		check(s.ClickLabel("取消固定 " + pinTarget.title()))
		check(s.Move(800, 700))
		checkIconColor("固定 "+pinTarget.title(), m.theme.MutedForeground)
		if pinTarget.pinned {
			t.Fatal("unpin failed")
		}
		before := m.active.source.view.Snapshot.Wrap
		check(s.ClickLabel("自动折行"))
		if m.active.source.view.Snapshot.Wrap == before {
			t.Fatal("relocated wrap button failed")
		}
	}
}

func TestGroupedAndUngroupedTabShareHoverFill(t *testing.T) {
	for _, light := range []bool{false, true} {
		m := newEditorApp()
		m.setLightMode(light)
		outside := m.addTab(document{eol: "LF"}, "组外.txt")
		member := m.addTab(document{eol: "LF"}, "组内.txt")
		m.newTabGroup(member)
		member.group.Name = "开发配置"
		active := m.addTab(document{eol: "LF"}, "当前.txt")
		s := openTestSession(t, m)
		check := func(err error) {
			t.Helper()
			if err != nil {
				t.Fatal(err)
			}
		}
		check(s.Resize(1050, 560))
		hasHoverFill := func(bounds gfx.Rect) bool {
			for _, shape := range s.Snapshot().Primitives {
				if shape.Bounds == bounds && shape.Radius == tabStripRadius && shape.Color == m.theme.Hover {
					return true
				}
			}
			return false
		}
		for _, tab := range []*editorTab{outside, member} {
			hit, err := s.Locate(tab.title())
			check(err)
			check(s.Move(hit.Bounds.X+40, hit.Bounds.Y+hit.Bounds.Height/2))
			if !hasHoverFill(hit.Bounds) || m.active != active {
				t.Fatalf("light=%t: %q is missing the shared rounded hover fill", light, tab.title())
			}
			if output := os.Getenv("VIVA_PREVIEW_DIR"); output != "" && tab == member {
				name := "group-hover-dark.png"
				if light {
					name = "group-hover-light.png"
				}
				check(s.Screenshot(filepath.Join(output, name)))
			}
			check(s.Move(817, 415))
			if hasHoverFill(hit.Bounds) {
				t.Fatalf("hover fill stayed visible after leaving %q", tab.title())
			}
		}
	}
}

func TestTabStripAdaptsToRenamedTitles(t *testing.T) {
	m := newEditorApp()
	short := m.addTab(document{eol: "LF"}, "a.txt")
	member := m.addTab(document{eol: "LF"}, "本地开发配置.ini")
	m.newTabGroup(member)
	group := member.group
	group.Name = "配置"
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	bounds := func(label string) gfx.Rect {
		t.Helper()
		hit, err := s.Locate(label)
		check(err)
		return hit.Bounds
	}
	rename := func(target, action, field, name string) {
		t.Helper()
		check(s.RightClickLabel(target))
		check(s.ClickLabel(action))
		check(s.ClickLabel(field))
		check(s.Keys("home"))
		check(s.Keys("shift+end"))
		check(s.Type(name))
		check(s.Keys("enter"))
	}

	before := bounds(short.title())
	if before.Width >= bounds(member.title()).Width {
		t.Fatal("short and long file names did not get different widths")
	}

	const longName = "服务器连接与本地环境配置常用参数和部署说明文档.txt"
	rename(short.title(), "重命名标签…", "标签名称", longName)
	renamed := bounds(longName)
	if renamed.Width <= before.Width || renamed.Width != tabStripMaxTabWidth-2*tabStripInset || !s.HasText("…") {
		t.Fatalf("long tab title did not expand and truncate at the maximum width: %g", renamed.Width)
	}

	chip := bounds(group.label())
	rename(group.label(), "重命名分组…", "分组名称", "运行环境与部署")
	expanded := bounds(group.label())
	if expanded.Width <= chip.Width {
		t.Fatal("group header did not grow after renaming")
	}
	check(s.ClickLabel(group.label()))
	if bounds(group.label()).Width <= expanded.Width {
		t.Fatal("collapsed group did not account for the member count")
	}
}

func TestTabPreviews(t *testing.T) {
	directory := os.Getenv("VIVA_PREVIEW_DIR")
	if directory == "" {
		t.Skip("preview is opt-in")
	}
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, light := range []bool{false, true} {
		m := newEditorApp()
		m.setLightMode(light)
		m.addTab(document{saved: "# 本地开发\n\n使用本地环境启动服务。\n", eol: "LF"}, "README.md")
		firstMember := m.addTab(document{saved: "APP_ENV=development\n", eol: "LF"}, "app.env")
		m.newTabGroup(firstMember)
		group := firstMember.group
		group.Name = "开发配置"
		activeMember := m.addTab(document{saved: "[server]\nport = 8080\nhost = \"127.0.0.1\"\n\n[log]\nlevel = \"info\"\n", eol: "LF"}, "server.toml")
		m.moveToGroup(activeMember, group)
		m.toggleGroupPin(group)
		m.newDocument()
		m.newTabGroup(m.active)
		otherGroup := m.active.group
		otherGroup.Name, otherGroup.Color, otherGroup.Collapsed = "参考资料", 3, true
		m.activate(activeMember)
		s := openTestSession(t, m)
		check(s.Resize(1180, 610))
		check(s.Move(800, 500))
		name := "preview-tabs-dark.png"
		if light {
			name = "preview-tabs-light.png"
		}
		check(s.Screenshot(filepath.Join(directory, name)))

		source, err := s.Locate(activeMember.title())
		check(err)
		check(s.Move(source.Bounds.X+42, source.Bounds.Y+15))
		check(s.Screenshot(filepath.Join(directory, "hover-"+name)))
		target, err := s.Locate(firstMember.title())
		check(err)
		x, y := target.Bounds.X+42, target.Bounds.Y+15
		check(s.Press(source.Bounds.X+42, y))
		check(s.Move(x, y))
		check(s.Advance(150 * time.Millisecond))
		if m.tabGhost != activeMember {
			t.Fatal("grouped tab did not enter its floating drag state")
		}
		check(s.Screenshot(filepath.Join(directory, "dragging-"+name)))
		check(s.Keys("escape"))
		check(s.Release(x, y))
		check(s.ClickLabel(group.label()))
		check(s.Move(800, 500))
		check(s.Screenshot(filepath.Join(directory, "collapsed-"+name)))
	}
}

func TestTabContextCloseRanges(t *testing.T) {
	for _, tc := range []struct {
		label, anchor string
		want          []string
	}{
		{"关闭左侧全部标签页", "中.txt", []string{"未命名-1", "固定.txt", "中.txt", "右.txt"}},
		{"关闭右侧全部标签页", "● 未命名-1", []string{"未命名-1", "固定.txt"}},
		{"关闭全部标签页", "左.txt", []string{"未命名-1", "固定.txt"}},
	} {
		t.Run(tc.label, func(t *testing.T) {
			m := newTestEditorWithDocument()
			original := m.active
			original.source.replace("未保存的固定文档")
			m.togglePin(original)
			m.togglePin(m.addTab(document{eol: "LF"}, "固定.txt"))
			m.addTab(document{eol: "LF"}, "左.txt")
			m.addTab(document{eol: "LF"}, "中.txt")
			m.addTab(document{eol: "LF"}, "右.txt")
			s := openTestSession(t, m)
			if err := s.RightClickLabel(tc.anchor); err != nil {
				t.Fatal(err)
			}
			if err := s.ClickLabel(tc.label); err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, tab := range m.tabs {
				got = append(got, tab.title())
			}
			if !slices.Equal(got, tc.want) || !slices.Contains(m.tabs, m.active) {
				t.Fatalf("remaining tabs: %v, want %v", got, tc.want)
			}
			if m.confirm || m.tabs[0] != original || original.source.text.String() != "未保存的固定文档" {
				t.Fatal("batch close changed or prompted to save a pinned draft")
			}
		})
	}
}

func TestBatchTabCloseConfirmsEachUnsavedDocument(t *testing.T) {
	m := newTestEditorWithDocument()
	anchor := m.active
	first := m.addTab(document{eol: "LF"}, "第一份.txt")
	first.source.replace("需要保存的中文")
	second := m.addTab(document{eol: "LF"}, "第二份.txt")
	second.source.replace("保留的草稿")
	last := m.addTab(document{eol: "LF"}, "后续文档.txt")
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(s.RightClickLabel(anchor.title()))
	check(s.ClickLabel("关闭右侧全部标签页"))
	if m.confirmTab != first {
		t.Fatal("first modified tab was not confirmed")
	}
	path := filepath.Join(t.TempDir(), "保存结果.txt")
	check(s.Driver().QueueSaveFile(path, nil))
	check(s.Keys("enter"))
	if m.confirmTab != second || slices.Contains(m.tabs, first) {
		t.Fatal("batch did not proceed to the next modified tab")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "需要保存的中文" {
		t.Fatalf("saved draft: %q, %v", data, err)
	}
	check(s.ClickLabel("取消"))
	if m.confirm || !slices.Equal(m.tabs, []*editorTab{anchor, second, last}) || second.source.text.String() != "保留的草稿" {
		t.Fatal("cancel did not preserve the remaining tabs")
	}
}

func TestDragTabsKeepsOrderStateAndPinnedGroup(t *testing.T) {
	m := newTestEditorWithDocument()
	pinned := m.active
	m.togglePin(pinned)
	first := m.addTab(document{eol: "LF"}, "甲.txt")
	second := m.addTab(document{eol: "LF"}, "乙.txt")
	third := m.addTab(document{eol: "LF"}, "丙.txt")
	third.source.replace("未保存中文")
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	point := func(label string) gfx.Point {
		t.Helper()
		hit, err := s.Locate(label)
		check(err)
		return gfx.Point{X: hit.Bounds.X + 40, Y: hit.Bounds.Y + 15}
	}
	drag := func(from, to gfx.Point, cancel bool) {
		t.Helper()
		check(s.Driver().Press(from.X, from.Y, gfx.PointerButtonPrimary))
		check(s.Driver().Move(to.X, to.Y))
		if !m.tabDrag.Dragging() {
			t.Fatal("tab drag did not start")
		}
		if cancel {
			check(s.Driver().Key(gfx.KeyEscape, 0))
		}
		check(s.Driver().Release(to.X, to.Y, gfx.PointerButtonPrimary))
	}
	drag(point("● 丙.txt"), point("甲.txt"), true)
	drag(point("甲.txt"), point(pinned.title()), false)
	if !slices.Equal(m.tabs, []*editorTab{pinned, first, second, third}) {
		t.Fatal("canceled or cross-group drag changed order")
	}
	drag(point("● 丙.txt"), point("甲.txt"), false)
	if !slices.Equal(m.tabs, []*editorTab{pinned, third, first, second}) || m.active != third || third.source.text.String() != "未保存中文" {
		t.Fatal("drag lost order, active tab, or draft text")
	}
	drag(point("甲.txt"), point("乙.txt"), false)
	if !slices.Equal(m.tabs, []*editorTab{pinned, third, second, first}) || m.active != third {
		t.Fatal("dragging an inactive tab to the right changed the active editor")
	}
}

func TestTabDragScrollsToOffscreenTarget(t *testing.T) {
	m := newTestEditorWithDocument()
	first := m.active
	for _, name := range []string{"第二个配置文件.txt", "第三.txt", "第四个文件.txt", "第五.txt", "第六个配置文件.txt", "第七.txt"} {
		m.addTab(document{eol: "LF"}, name)
	}
	last := m.active
	m.activate(first)
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(s.Resize(900, 560))
	start, err := s.Locate(first.title())
	check(err)
	newTab, err := s.Locate("新建标签")
	check(err)
	if _, err = s.Locate(last.title()); err == nil {
		t.Fatal("test target must start offscreen")
	}
	y := start.Bounds.Y + 14
	check(s.Driver().Press(start.Bounds.X+40, y, gfx.PointerButtonPrimary))
	check(s.Driver().Move(newTab.Bounds.X-12, y))
	check(s.Advance(1200 * time.Millisecond))
	end, err := s.Locate(last.title())
	check(err)
	check(s.Driver().Move(end.Bounds.X+30, y))
	check(s.Driver().Release(end.Bounds.X+30, y, gfx.PointerButtonPrimary))
	if m.tabs[len(m.tabs)-1] != first {
		t.Fatal("could not drop on a tab reached by edge scrolling")
	}
}

func TestTabDragSlidesAdjacentCardsBeforeDrop(t *testing.T) {
	m := newTestEditorWithDocument()
	first := m.active
	middle := m.addTab(document{eol: "LF"}, "中间.txt")
	last := m.addTab(document{eol: "LF"}, "更长的末尾文档名称.txt")
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	firstSlot, err := s.Locate(first.title())
	check(err)
	lastSlot, err := s.Locate(last.title())
	check(err)
	dragWidth := last.tabBounds.Width
	if dragWidth <= first.tabBounds.Width {
		t.Fatal("test requires a wider dragged tab")
	}
	x, y := firstSlot.Bounds.X+40, firstSlot.Bounds.Y+14
	check(s.Press(lastSlot.Bounds.X+40, y))
	check(s.Move(x, y))
	check(s.Advance(150 * time.Millisecond))
	shifted, err := s.Locate(first.title())
	check(err)
	want := firstSlot.Bounds.X + dragWidth
	if delta := shifted.Bounds.X - want; delta < -.1 || delta > .1 || m.tabGhost != last || !slices.Equal(m.tabs, []*editorTab{first, middle, last}) {
		t.Fatalf("neighbor did not slide by the dragged tab width: x=%g, want %g", shifted.Bounds.X, want)
	}
	check(s.Release(x, y))
	if !slices.Equal(m.tabs, []*editorTab{last, first, middle}) || m.active != last || m.tabGhost != nil {
		t.Fatal("drop on original slot failed or changed the active tab")
	}
}

func TestTabGroupsThroughUIAndSession(t *testing.T) {
	m := newTestEditorWithDocument()
	outside := m.active
	first := m.addTab(document{eol: "LF"}, "甲.txt")
	second := m.addTab(document{eol: "LF"}, "乙.txt")
	second.source.replace("保留中文草稿")
	store, err := openSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.attach(m)
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(s.RightClickLabel(first.title()))
	check(s.ClickLabel("添加到新分组"))
	group := first.group
	if group == nil {
		t.Fatal("tab context menu did not create a group")
	}
	chip, err := s.Locate(group.label())
	check(err)
	if !chip.Expanded || chip.Description != "1 个标签" {
		t.Fatal("the group header should announce its expanded state and member count")
	}
	tab, err := s.Locate("● " + second.title())
	check(err)
	x, y := chip.Bounds.X+20, chip.Bounds.Y+14
	check(s.Press(tab.Bounds.X+35, tab.Bounds.Y+14))
	check(s.Move(x, y))
	check(s.Release(x, y))
	if second.group != group || !slices.Equal(m.tabs, []*editorTab{outside, first, second}) {
		t.Fatal("dragging a tab onto the group chip did not join it")
	}
	chip, err = s.Locate(group.label())
	check(err)
	if chip.Description != "2 个标签" {
		t.Fatal("group count did not update after dragging in a tab")
	}

	check(s.RightClickLabel(group.label()))
	check(s.ClickLabel("重命名分组…"))
	check(s.ClickLabel("分组名称"))
	check(s.Keys("home"))
	check(s.Keys("shift+end"))
	check(s.Type("开发配置"))
	check(s.Keys("enter"))
	check(s.ClickLabel("分组：开发配置"))
	if _, err := s.Locate(first.title()); err == nil || !group.Collapsed || m.active != outside {
		t.Fatal("collapse did not hide the members and select a visible tab")
	}
	chip, err = s.Locate(group.label())
	check(err)
	if chip.Expanded || chip.Description != "2 个标签" || !slices.Contains(s.Texts(), "2") {
		t.Fatal("the collapsed header did not expose its member count")
	}
	check(s.Keys("ctrl+tab"))
	if m.active != outside || second.source.text.String() != "保留中文草稿" {
		t.Fatal("keyboard navigation entered a collapsed group or lost a draft")
	}

	check(store.save())
	restored, err := openSessionStore(store.directory)
	check(err)
	again := restored.windows[0]
	restoredGroup := again.tabs[1].group
	if restoredGroup == nil || restoredGroup != again.tabs[2].group || restoredGroup.Name != "开发配置" || !restoredGroup.Collapsed || again.active != again.tabs[0] || again.tabs[2].source.text.String() != "保留中文草稿" {
		t.Fatal("session did not restore group membership, collapse state, or draft")
	}

	// Closing a collapsed group still prompts for its modified tabs.
	check(s.RightClickLabel(group.label()))
	check(s.ClickLabel("关闭整个分组"))
	if m.confirmTab != second {
		t.Fatal("closing the group bypassed the unsaved draft confirmation")
	}
	check(s.ClickLabel("取消"))
	if !slices.Contains(m.tabs, second) || second.group != group || second.source.text.String() != "保留中文草稿" {
		t.Fatal("canceling group close discarded the unsaved tab")
	}
	check(s.RightClickLabel(group.label()))
	check(s.ClickLabel("关闭整个分组"))
	check(s.ClickLabel("放弃修改"))
	if !slices.Equal(m.tabs, []*editorTab{outside}) || m.active != outside {
		t.Fatal("group close removed a tab outside the group")
	}
}

func TestGroupDropBeforeOrInsideAndCloseThroughUI(t *testing.T) {
	m := newEditorApp()
	outside := m.addTab(document{eol: "LF"}, "外侧.txt")
	first := m.addTab(document{eol: "LF"}, "甲.txt")
	m.newTabGroup(first)
	group := first.group
	group.Name = "配置"
	second := m.addTab(document{eol: "LF"}, "乙.txt")
	m.moveToGroup(second, group)
	mover := m.addTab(document{eol: "LF"}, "挪动.txt")
	tail := m.addTab(document{eol: "LF"}, "尾部.txt")
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	hit := func(label string) gfx.Hit {
		t.Helper()
		item, err := s.Locate(label)
		check(err)
		return item
	}
	check(s.Resize(1240, 710))
	header := hit(group.label()).Bounds
	file := hit(mover.title()).Bounds
	y := header.Y + header.Height/2
	check(s.Press(file.X+42, y))
	check(s.Move(header.X+2, y))
	check(s.Advance(140 * time.Millisecond))
	hasInsertion := false
	for _, shape := range s.Snapshot().Primitives {
		if shape.Color == m.theme.Ring && shape.Bounds.Width == 3 && shape.Bounds.Height == tabStripItemSize+1 {
			hasInsertion = true
			break
		}
	}
	if !hasInsertion {
		t.Fatal("dragging before a group did not paint a vertical marker")
	}
	if output := os.Getenv("VIVA_PREVIEW_DIR"); output != "" {
		check(s.Screenshot(filepath.Join(output, "tab-drop-before-group.png")))
	}
	check(s.Release(header.X+2, y))
	if mover.group != nil || !slices.Equal(m.tabs, []*editorTab{outside, mover, first, second, tail}) {
		t.Fatal("dropping on the leading edge inserted the tab into the group")
	}

	header = hit(group.label()).Bounds
	file = hit(mover.title()).Bounds
	check(s.Press(file.X+42, y))
	check(s.Move(header.X+30, y))
	check(s.Advance(140 * time.Millisecond))
	hasGroupHighlight := false
	for _, shape := range s.Snapshot().Primitives {
		if shape.Color == group.accent() && shape.Bounds.Height == 4 && shape.Bounds.Width >= header.Width {
			hasGroupHighlight = true
			break
		}
	}
	if !hasGroupHighlight {
		t.Fatal("dragging into the group did not highlight it")
	}
	if output := os.Getenv("VIVA_PREVIEW_DIR"); output != "" {
		check(s.Screenshot(filepath.Join(output, "tab-drop-into-group.png")))
	}
	check(s.Release(header.X+30, y))
	if mover.group != group || !slices.Equal(m.tabs, []*editorTab{outside, first, second, mover, tail}) {
		t.Fatal("dropping inside did not append to the group")
	}

	check(s.ClickLabel(group.label()))
	start := hit(group.label()).Bounds
	end := hit(tail.title()).Bounds
	check(s.Press(start.X+33, y))
	check(s.Move(end.X+40, y))
	if m.groupGhost != group {
		t.Fatal("dragging a collapsed group did not show a group preview")
	}
	check(s.Release(end.X+40, y))
	if !group.Collapsed || !slices.Equal(m.tabs, []*editorTab{outside, tail, first, second, mover}) {
		t.Fatal("moving the group did not keep its order, membership and collapsed state")
	}

	m.togglePin(first)
	mover.source.replace("未保存的修改")
	check(s.Frame())
	header = hit(group.label()).Bounds
	check(s.Move(header.X+33, y))
	check(s.ClickLabel("关闭分组 " + group.Name))
	if m.confirmTab != mover || !slices.Contains(m.tabs, first) || slices.Contains(m.tabs, second) {
		t.Fatal("closing the group did not honor pins and unsaved changes")
	}
	check(s.ClickLabel("取消"))
	if mover.group != group || !mover.modified() || first.group != group {
		t.Fatal("canceling the close lost a protected tab or unsaved changes")
	}
}

func TestTabGroupReusesSmallestUntitledName(t *testing.T) {
	m := newTestEditorWithDocument()
	first := m.active
	m.newTabGroup(first)
	id := first.group.ID
	second := m.addTab(document{eol: "LF"}, "第二.txt")
	m.newTabGroup(second)
	if first.group.Name != "未命名-1" || second.group.Name != "未命名-2" {
		t.Fatal("default names did not use the smallest available numbers")
	}
	m.moveToGroup(first, nil)
	third := m.addTab(document{eol: "LF"}, "第三.txt")
	m.newTabGroup(third)
	if third.group.Name != "未命名-1" || third.group.ID <= id || second.group.Name != "未命名-2" {
		t.Fatal("empty group number was not reused with a fresh group ID")
	}
	second.group.Name = "项目设置"
	m.newTabGroup(first)
	if first.group.Name != "未命名-2" {
		t.Fatal("renaming a group did not release its former default name")
	}
}
