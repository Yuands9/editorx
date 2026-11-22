package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestPinnedTabCanJoinAGroupByMenuAndDrag(t *testing.T) {
	m := newTestEditorWithDocument()
	outside := m.active
	member := m.addTab(document{eol: "LF"}, "组员.txt")
	m.newTabGroup(member)
	group := member.group
	menuTab := m.addTab(document{eol: "LF"}, "从菜单加入.txt")
	dragTab := m.addTab(document{eol: "LF"}, "拖动加入.txt")
	m.togglePin(menuTab)
	m.togglePin(dragTab)
	m.activate(outside)
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	check(s.RightClickLabel(menuTab.title()))
	check(s.ClickLabel("移到已有分组"))
	check(s.ClickLabel("menuitem:" + group.Name))
	if menuTab.group != group || !menuTab.pinned || group.Pinned {
		t.Fatal("menu did not join the group or changed a pin")
	}

	source, err := s.Locate(dragTab.title())
	check(err)
	header, err := s.Locate(group.label())
	check(err)
	check(s.Press(source.Bounds.X+35, source.Bounds.Y+14))
	check(s.Move(header.Bounds.X+22, header.Bounds.Y+12))
	check(s.Release(header.Bounds.X+22, header.Bounds.Y+12))
	if dragTab.group != group || !dragTab.pinned || !slices.Equal(m.tabs, []*editorTab{outside, member, menuTab, dragTab}) {
		t.Fatal("dragging a pinned tab did not keep the group contiguous and preserve its pin")
	}
	check(s.ClickLabel("取消固定 " + dragTab.title()))
	if dragTab.group != group || dragTab.isPinned() || !menuTab.isPinned() {
		t.Fatal("changing the tab's individual pin affected its membership or a neighbor")
	}
}

func TestPinnedGroupProtectsAllMembersAndRestoresBothPins(t *testing.T) {
	m := newTestEditorWithDocument()
	outside := m.active
	member := m.addTab(document{eol: "LF"}, "组内普通.txt")
	m.newTabGroup(member)
	group := member.group
	personal := m.addTab(document{eol: "LF"}, "组内单独固定.txt")
	personal.source.replace("固定的未保存草稿")
	m.moveToGroup(personal, group)
	m.togglePin(personal)
	m.activate(member)
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
	disabled := func(label string) gfx.Hit {
		t.Helper()
		for _, hit := range s.Snapshot().Hits {
			if hit.Label == label && hit.Disabled && hit.Bounds.Width > 0 {
				return hit
			}
		}
		t.Fatalf("%s is missing or still enabled", label)
		return gfx.Hit{}
	}
	closeShortcut := func(hint string) {
		t.Helper()
		handled, err := s.Driver().ShortcutChord(gfx.ModifierControl, 'w')
		check(err)
		if !handled || m.confirm || m.status != hint {
			t.Fatalf("Ctrl+W was not blocked with the correct reason: %q", m.status)
		}
	}

	check(s.ClickLabel("固定分组 " + group.Name))
	if !group.Pinned || !member.isPinned() || member.pinned || !personal.pinned || !slices.Equal(m.tabs, []*editorTab{member, personal, outside}) {
		t.Fatal("pinning the group did not move the block or incorrectly changed individual pins")
	}
	newMember := m.addTab(document{eol: "LF"}, "后来加入.txt")
	m.moveToGroup(newMember, group)
	m.activate(member)
	check(s.Frame())
	if !newMember.isPinned() || newMember.pinned {
		t.Fatal("a tab added after the group was pinned did not inherit protection")
	}

	close := disabled("关闭 " + member.title())
	check(s.Click(close.Bounds.X+close.Bounds.Width/2, close.Bounds.Y+close.Bounds.Height/2))
	check(s.RightClickLabel(member.title()))
	disabled("关闭标签")
	check(s.Keys("escape"))
	check(s.ClickLabel("主菜单"))
	disabled("关闭当前标签")
	check(s.Keys("escape"))
	closeShortcut("请先取消固定分组")
	if !slices.Contains(m.tabs, member) {
		t.Fatal("a close entry point removed a protected group member")
	}

	check(s.ClickLabel(group.label()))
	if !group.Collapsed || m.active != outside {
		t.Fatal("a pinned group could not be collapsed")
	}
	check(store.save())
	restored, err := openSessionStore(store.directory)
	check(err)
	again := restored.windows[0]
	restoredGroup := again.tabs[0].group
	if restoredGroup == nil || !restoredGroup.Pinned || !restoredGroup.Collapsed || again.tabs[1].group != restoredGroup ||
		again.tabs[0].pinned || !again.tabs[1].pinned || !again.tabs[2].isPinned() || again.active != again.tabs[3] {
		t.Fatal("session restore lost group membership, collapse state, inherited or individual pins")
	}

	check(s.RightClickLabel(outside.title()))
	check(s.ClickLabel("关闭全部标签页"))
	if !slices.Equal(m.tabs, []*editorTab{member, personal, newMember}) || m.confirm {
		t.Fatal("close all did not skip the pinned group")
	}
	check(s.RightClickLabel(group.label()))
	disabled("关闭整个分组")
	check(s.ClickLabel("取消固定分组"))
	if group.Pinned || member.isPinned() || !personal.isPinned() {
		t.Fatal("unpinning the group removed its member's individual protection")
	}
	check(s.RightClickLabel(group.label()))
	check(s.ClickLabel("关闭分组内未固定标签"))
	if !slices.Equal(m.tabs, []*editorTab{personal}) || personal.source.text.String() != "固定的未保存草稿" || m.confirm {
		t.Fatal("group close removed or prompted to save an individually pinned draft")
	}
	closeShortcut("请先取消固定标签")
	check(s.ClickLabel("取消固定 " + personal.title()))
	check(s.ClickLabel("关闭 " + personal.title()))
	if !m.confirm || m.confirmTab != personal {
		t.Fatal("unpinning the tab did not restore its regular close and save confirmation")
	}
	check(s.ClickLabel("放弃修改"))
	if slices.Contains(m.tabs, personal) {
		t.Fatal("unpinned tab could not be closed")
	}
}

func TestFirstFileKeepsEmptyTabInPinnedGroup(t *testing.T) {
	m := newTestEditorWithDocument()
	blank := m.active
	m.newTabGroup(blank)
	m.toggleGroupPin(blank.group)
	path := filepath.Join(t.TempDir(), "opened.txt")
	if err := os.WriteFile(path, []byte("打开文件时保留固定分组"), 0600); err != nil {
		t.Fatal(err)
	}
	m.load(path)
	if len(m.tabs) != 2 || m.tabs[0] != blank || !blank.isPinned() || m.active.doc.path != path {
		t.Fatal("opening the first file silently replaced a blank tab in a pinned group")
	}
}
