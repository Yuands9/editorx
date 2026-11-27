package editor

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
)

func TestSaveConflictActions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conflict.txt")
	writeDisk := func(value string) {
		t.Helper()
		workflowCheck(t, os.WriteFile(path, []byte(value), 0600))
	}
	checkDisk := func(want string) {
		t.Helper()
		data, err := os.ReadFile(path)
		workflowCheck(t, err)
		if string(data) != want {
			t.Fatalf("disk content = %q, want %q", data, want)
		}
	}
	writeDisk("original\n")
	m := newEditorApp()
	m.watchFiles = false // The save check and manual choices work without the watcher.
	m.load(path)
	tab := m.active
	s := openTestSession(t, m)
	wait := func(condition func() bool) {
		t.Helper()
		workflowCheck(t, s.WaitFor(func(app.Snapshot) bool { return condition() }, 8*time.Second))
	}
	click := func(label string) {
		t.Helper()
		workflowCheck(t, s.ClickLabel("button:"+label))
	}
	conflict := func() {
		t.Helper()
		if m.saveTab(tab, false) || m.saveConflict == nil || m.showError {
			t.Fatal("changed file did not open the conflict dialog")
		}
		workflowCheck(t, s.Frame())
		if !s.HasText("保存冲突") {
			t.Fatal("conflict dialog is not visible")
		}
	}
	tab.source.replace("draft\n")
	writeDisk("external\n")
	conflict()
	workflowScreenshot(t, s, "save-disk-conflict")
	click("取消")
	if m.saveConflict != nil || tab.source.text.CommittedString() != "draft\n" {
		t.Fatal("cancel should keep the draft")
	}
	checkDisk("external\n")

	conflict()
	click("比对磁盘内容")
	wait(func() bool { return m.historyCompare != nil })
	comparison := m.historyCompare.comparison
	if comparison.old.saved != "external\n" || comparison.current.saved != "draft\n" {
		t.Fatal("comparison did not show both versions")
	}
	checkDisk("external\n")
	m.closeHistoryComparison()

	conflict()
	writeDisk("external again\n")
	click("覆盖文件")
	wait(func() bool { return m.saveConflict != nil })
	checkDisk("external again\n")
	if !tab.modified() {
		t.Fatal("repeated conflict marked the draft saved")
	}
	click("覆盖文件")
	wait(func() bool { return tab.savingText == nil })
	checkDisk("draft\n")
	if tab.modified() || m.saveConflict != nil {
		t.Fatal("confirmed overwrite did not clear the conflict")
	}

	tab.source.replace("second draft\n")
	writeDisk("reload me\n")
	conflict()
	click("重新读取")
	wait(func() bool { return tab.source.text.CommittedString() == "reload me\n" })
	if tab.modified() || m.saveConflict != nil {
		t.Fatal("reload left an unsaved draft or open dialog")
	}
	checkDisk("reload me\n")
}

func TestLargeEditSaveConflictActions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conflict.out")
	workflowCheck(t, os.WriteFile(path, []byte("original\r\n"), 0600))
	m := newEditorApp()
	page, err := readLargeTextPage(path, 0, -1, "", "")
	workflowCheck(t, err)
	tab := m.openLoadedFile(openedEditorFile{doc: document{path: path}, largeText: &page})
	t.Cleanup(func() { tab.largeText.close() })
	s := openTestSession(t, m)
	wait := func(condition func() bool) {
		t.Helper()
		workflowCheck(t, s.WaitFor(func(app.Snapshot) bool { return condition() }, 8*time.Second))
	}
	workflowCheck(t, s.ClickLabel("button:打开完整编辑"))
	wait(tab.hasLargeEdit)
	workflowCheck(t, s.Type("draft "))
	workflowCheck(t, os.WriteFile(path, []byte("external\r\n"), 0600))
	m.saveTab(tab, false)
	wait(func() bool { return m.saveConflict != nil })
	workflowCheck(t, s.ClickLabel("button:比对磁盘内容"))
	wait(func() bool { return m.historyCompare != nil })
	comparison := m.historyCompare.comparison
	if comparison.old.saved != "external\n" || comparison.current.saved != "draft original\n" ||
		m.historyCompare.canEditCurrent() {
		t.Fatal("large-file comparison did not show both snapshots as read only")
	}
	m.closeHistoryComparison()
	m.saveTab(tab, false)
	wait(func() bool { return m.saveConflict != nil })
	workflowCheck(t, s.ClickLabel("button:重新读取"))
	// The external file is now small enough to return to regular editing.
	wait(func() bool { return tab.largeText == nil && !tab.modified() })
	if value := tab.source.text.CommittedString(); value != "external\n" {
		t.Fatalf("wrong reloaded file: %q", value)
	}
	page, err = readLargeTextPage(path, 0, -1, "", "")
	workflowCheck(t, err)
	tab.startLargeText(page)
	workflowCheck(t, s.Frame())
	workflowCheck(t, s.ClickLabel("button:打开完整编辑"))
	wait(tab.hasLargeEdit)
	workflowCheck(t, s.Type("saved "))
	workflowCheck(t, os.WriteFile(path, []byte("changed again\r\n"), 0600))
	m.saveTab(tab, false)
	wait(func() bool { return m.saveConflict != nil })
	workflowCheck(t, s.ClickLabel("button:覆盖文件"))
	wait(func() bool { return !tab.modified() })
	data, err := os.ReadFile(path)
	workflowCheck(t, err)
	if string(data) != "saved external\r\n" {
		t.Fatalf("wrong overwritten large file: %q", data)
	}
}

func TestOfficeSaveConflictReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conflict.xlsx")
	original, err := os.ReadFile(filepath.Join(examplesDirectory, "示例表格.xlsx"))
	workflowCheck(t, err)
	workflowCheck(t, os.WriteFile(path, original, 0600))
	m := newEditorApp()
	m.load(path)
	tab := m.active
	s := openTestSession(t, m)

	// ZIP readers accept trailing bytes, which makes a distinct valid disk version.
	external := append(append([]byte(nil), original...), '\n')
	workflowCheck(t, os.WriteFile(path, external, 0600))
	tab.office.state.Dirty = true
	m.saveTab(tab, false)
	if m.saveConflict == nil {
		t.Fatalf("missing office save conflict: %s", m.errorText)
	}
	workflowCheck(t, s.Frame())
	workflowCheck(t, s.ClickLabel("button:重新读取"))
	workflowCheck(t, s.WaitFor(func(app.Snapshot) bool {
		return !tab.modified() && len(tab.office.state.Original) == len(external)
	}, 8*time.Second))

	workflowCheck(t, os.WriteFile(path, original, 0600))
	tab.office.state.Dirty = true
	m.saveTab(tab, false)
	workflowCheck(t, s.Frame())
	workflowCheck(t, s.ClickLabel("button:覆盖文件"))
	if tab.modified() || m.saveConflict != nil {
		t.Fatal("office overwrite did not finish")
	}
	data, err := os.ReadFile(path)
	workflowCheck(t, err)
	if len(data) != len(external) {
		t.Fatal("the editor's office version was not saved")
	}
}
