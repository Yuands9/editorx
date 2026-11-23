package editor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
)

func TestSaveConfirmationCanBeDisabledAndRestored(t *testing.T) {
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	m.newDocument()
	draft := m.active
	draft.source.replace("我会明确放弃这份草稿")
	session := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	click := func(label string) {
		t.Helper()
		check(session.ClickLabel(label))
	}
	closeTab := func(tab *editorTab) {
		t.Helper()
		m.closeTab(tab)
		check(session.Frame())
	}
	checkPreference := func(want bool) {
		t.Helper()
		saved, err := readApplicationPreferences(m.dataDirectory)
		check(err)
		if saved.SkipSaveConfirmation != want {
			t.Fatalf("saved skip confirmation = %t, want %t", saved.SkipSaveConfirmation, want)
		}
	}
	closeTab(draft)
	click("checkbox:不再询问")
	click("button:取消")
	checkPreference(false)
	if m.confirm || m.active != draft || !draft.modified() {
		t.Fatal("canceling the confirmation changed the draft")
	}

	closeTab(draft)
	if m.dontAskToSave {
		t.Fatal("a canceled choice carried over to a new confirmation")
	}
	click("checkbox:不再询问")
	check(session.Driver().QueueSaveFile("", app.ErrCanceled))
	click("保存并继续")
	checkPreference(false)
	if !m.confirm || m.active != draft || !draft.modified() {
		t.Fatal("canceling the save picker closed the draft")
	}
	if preview := os.Getenv("VIVA_PREVIEW_DIR"); preview != "" {
		check(session.Screenshot(filepath.Join(preview, "save-confirmation.png")))
	}
	click("button:放弃修改")
	checkPreference(true)
	if m.confirm || len(m.tabs) != 0 {
		t.Fatal("the explicitly discarded draft was not closed")
	}

	path := filepath.Join(m.dataDirectory, "文档.txt")
	check(os.WriteFile(path, []byte("原内容"), 0600))
	m.load(path)
	m.active.source.replace("关闭时保存")
	closeTab(m.active)
	data, err := os.ReadFile(path)
	check(err)
	if m.confirm || len(m.tabs) != 0 || string(data) != "关闭时保存" {
		t.Fatal("silencing the prompt did not save an existing file before closing")
	}

	m.load(path)
	conflict := m.active
	conflict.source.replace("发生冲突时要保留的草稿")
	check(os.WriteFile(path, []byte("磁盘上的新内容"), 0600))
	closeTab(conflict)
	data, err = os.ReadFile(path)
	check(err)
	if !m.showError || m.active != conflict || !conflict.modified() || string(data) != "磁盘上的新内容" {
		t.Fatal("automatic save overwrote a changed file or closed its draft")
	}
	click("button:确定")

	m.newDocument()
	unnamed := m.active
	unnamed.source.replace("还没有文件名的草稿")
	check(session.Driver().QueueSaveFile("", app.ErrCanceled))
	closeTab(unnamed)
	if m.confirm || m.active != unnamed || !unnamed.modified() {
		t.Fatal("skipping confirmation discarded an unnamed draft when Save As was canceled")
	}

	click("button:设置")
	click("关闭标签时询问保存")
	checkPreference(false)
	if preview := os.Getenv("VIVA_PREVIEW_DIR"); preview != "" {
		check(session.Screenshot(filepath.Join(preview, "save-confirmation-setting.png")))
	}
	click("button:关闭设置")
	closeTab(unnamed)
	if !m.confirm || m.dontAskToSave {
		t.Fatal("enabling the setting did not restore the save confirmation")
	}
}
