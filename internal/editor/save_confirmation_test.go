package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

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
	if m.saveConflict == nil || m.active != conflict || !conflict.modified() || string(data) != "磁盘上的新内容" {
		t.Fatal("automatic save overwrote a changed file or closed its draft")
	}
	click("button:取消")

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

func TestDesktopExitSaveConfirmation(t *testing.T) {
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	directory := t.TempDir()
	store, err := openSessionStore(directory)
	check(err)
	preferences, err := readApplicationPreferences(directory)
	check(err)
	preferences.ShowTrayIcon, preferences.SkipSaveConfirmation = false, true
	check(preferences.save(directory))
	desktop := &editorDesktop{store: store, preferences: preferences}
	first, second := newEditorApp(), newEditorApp()
	for _, window := range []*editorApp{first, second} {
		store.attach(window)
		window.desktop = desktop
	}
	path := filepath.Join(directory, "文档.txt")
	check(os.WriteFile(path, []byte("原内容"), 0600))
	first.load(path)
	first.active.source.replace("退出时保存")
	second.newDocument()
	const draft = "退出时保留的未命名草稿"
	second.active.source.replace(draft)
	firstSession, secondSession := openTestSession(t, first), openTestSession(t, second)

	desktop.quit()
	check(firstSession.Frame())
	check(secondSession.Frame())
	if !firstSession.HasText("有 2 个文件尚未保存。") || !firstSession.HasText(second.active.title()) ||
		secondSession.HasText("退出 EditorX 前保存？") {
		t.Fatal("exit did not list both windows' files in a single dialog")
	}
	if _, err := firstSession.Locate("button:取消退出"); err == nil {
		t.Fatal("the footer still has a cancel button")
	}
	check(firstSession.ClickLabel("checkbox:以后退出时不再询问"))
	check(firstSession.ClickLabel("button:Close dialog"))
	data, err := os.ReadFile(path)
	check(err)
	if desktop.exiting || desktop.exitConfirmation != nil || string(data) != "原内容" ||
		!first.active.modified() || !second.active.modified() {
		t.Fatal("the top-right close button did not cancel exit and preserve both edits")
	}

	desktop.quit()
	check(firstSession.Frame())
	check(firstSession.ClickLabel("checkbox:以后退出时不再询问"))
	check(firstSession.Driver().QueueSaveFile("", app.ErrCanceled))
	check(firstSession.ClickLabel("button:保存并退出"))
	data, err = os.ReadFile(path)
	check(err)
	if desktop.exiting || desktop.exitConfirmation != nil || string(data) != "退出时保存" ||
		!second.active.modified() || second.active.source.text.String() != draft {
		t.Fatal("canceling Save As did not stop exit and preserve the remaining draft")
	}
	savedPreferences, err := readApplicationPreferences(directory)
	check(err)
	if savedPreferences.SkipExitSaveConfirmation {
		t.Fatal("a canceled exit changed the prompt preference")
	}

	desktop.quit()
	check(secondSession.Frame())
	if !secondSession.HasText("有 1 个文件尚未保存。") {
		t.Fatal("the retry did not show the remaining draft")
	}
	check(secondSession.Driver().QueueSaveFile(path, nil))
	check(secondSession.ClickLabel("button:保存并退出"))
	data, err = os.ReadFile(path)
	check(err)
	if desktop.exiting || !second.showError || string(data) != "退出时保存" || !second.active.modified() {
		t.Fatal("Save As proceeded over a file open in another window")
	}
	check(secondSession.ClickLabel("button:确定"))
	desktop.quit()
	check(secondSession.Frame())
	draftPath := filepath.Join(directory, "新文件.txt")
	check(secondSession.Driver().QueueSaveFile(draftPath, nil))
	check(secondSession.ClickLabel("button:保存并退出"))
	data, err = os.ReadFile(draftPath)
	check(err)
	if !desktop.exiting || desktop.exitConfirmation != nil || string(data) != draft || second.active.modified() {
		t.Fatal("saving from the exit dialog did not save the last file and finish exit")
	}
}

func TestDesktopExitSaveOverviewKeepsAllDrafts(t *testing.T) {
	directory := t.TempDir()
	store, err := openSessionStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	host, other := newEditorApp(), newEditorApp()
	desktop := &editorDesktop{store: store, lastActive: host, preferences: applicationPreferences{SkipSaveConfirmation: true}}
	for _, window := range []*editorApp{host, other} {
		store.attach(window)
		window.desktop = desktop
	}
	const total = 7
	for i := range total {
		name := fmt.Sprintf("待保存的文件_%d.txt", i+1)
		tab := other.addTab(document{}, name)
		tab.source.replace(name)
	}
	session := openTestSession(t, host)
	desktop.quit()
	if err := session.Frame(); err != nil {
		t.Fatal(err)
	}
	if !session.HasText("有 7 个文件尚未保存。") || !session.HasText("…") {
		t.Fatal("the exit dialog did not show the total and overflow mark")
	}
	for i, tab := range other.tabs {
		if session.HasText(tab.title()) != (i < visibleExitFileLimit) {
			t.Fatalf("unexpected visibility for %s", tab.title())
		}
	}
	if preview := os.Getenv("VIVA_PREVIEW_DIR"); preview != "" {
		if err := session.Screenshot(filepath.Join(preview, "exit-save-overview.png")); err != nil {
			t.Fatal(err)
		}
	}
	if err := session.ClickLabel("checkbox:以后退出时不再询问"); err != nil {
		t.Fatal(err)
	}
	if err := session.ClickLabel("button:保留草稿并退出"); err != nil {
		t.Fatal(err)
	}
	savedPreferences, err := readApplicationPreferences(directory)
	if err != nil || !desktop.exiting || desktop.exitConfirmation != nil ||
		!savedPreferences.SkipExitSaveConfirmation || !savedPreferences.SkipSaveConfirmation {
		t.Fatalf("keeping drafts did not finish exit and persist the independent preference: %v", err)
	}
	reopened, err := openSessionStore(directory)
	if err != nil || len(reopened.windows) != 2 || len(reopened.windows[1].tabs) != total {
		t.Fatalf("keeping drafts lost some of the files: %v", err)
	}
	for _, tab := range reopened.windows[1].tabs {
		if tab.source.text.String() != tab.title() || !tab.modified() {
			t.Fatalf("the draft for %s was not restored", tab.title())
		}
	}
}

func TestDesktopExitSavesRestoredLargeFile(t *testing.T) {
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	directory := t.TempDir()
	store, err := openSessionStore(directory)
	check(err)
	window := newEditorApp()
	store.attach(window)
	desktop := &editorDesktop{store: store}
	window.desktop = desktop
	path := filepath.Join(directory, "日志.out")
	check(os.WriteFile(path, []byte("before\r\n"), 0600))
	doc, err := readDocument(path)
	check(err)
	page, err := readLargeTextPage(path, 0, -1, "", "")
	check(err)
	tab := window.openLoadedFile(openedEditorFile{doc: document{path: path}, largeText: &page})
	tab.largeText.editSession = &largeEditSession{
		BaseHash: doc.diskHash,
		Changes:  []largeEditChange{{Start: 0, End: int64(len("before")), Text: "after"}},
	}
	tab.largeText.resumeEdit = true
	t.Cleanup(tab.largeText.close)
	window.newDocument()
	session := openTestSession(t, window)
	desktop.quit()
	check(session.Frame())
	check(session.ClickLabel("button:保存并退出"))
	check(session.WaitFor(func(app.Snapshot) bool { return desktop.exitConfirmation == nil }, 3*time.Second))
	data, err := os.ReadFile(path)
	check(err)
	if !desktop.exiting || window.showError || tab.modified() || string(data) != "after\r\n" {
		t.Fatalf("exit did not reopen and save the inactive large-file draft: %s", data)
	}
}

func TestDesktopExitStopsOnLargeFileSaveFailure(t *testing.T) {
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	directory := t.TempDir()
	store, err := openSessionStore(directory)
	check(err)
	window := newEditorApp()
	store.attach(window)
	desktop := &editorDesktop{store: store}
	window.desktop = desktop
	path := filepath.Join(directory, "日志.out")
	check(os.WriteFile(path, []byte("original\r\n"), 0600))
	page, err := readLargeTextPage(path, 0, -1, "", "")
	check(err)
	tab := window.openLoadedFile(openedEditorFile{doc: document{path: path}, largeText: &page})
	t.Cleanup(tab.largeText.close)
	session := openTestSession(t, window)
	check(session.ClickLabel("button:打开完整编辑"))
	check(session.WaitFor(func(app.Snapshot) bool { return tab.hasLargeEdit() }, 3*time.Second))
	check(session.Type("unsaved"))
	check(os.WriteFile(path, []byte("externally changed\r\n"), 0600))

	desktop.quit()
	check(session.Frame())
	check(session.ClickLabel("button:保存并退出"))
	check(session.WaitFor(func(app.Snapshot) bool { return desktop.exitConfirmation == nil }, 3*time.Second))
	data, err := os.ReadFile(path)
	check(err)
	if desktop.exiting || window.saveConflict == nil || !tab.modified() || string(data) != "externally changed\r\n" {
		t.Fatal("the failed asynchronous save did not stop exit and protect the file and draft")
	}
}

func TestDesktopExitSaveConfirmationSetting(t *testing.T) {
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	directory := t.TempDir()
	store, err := openSessionStore(directory)
	check(err)
	preferences, err := readApplicationPreferences(directory)
	check(err)
	preferences.ShowTrayIcon = false
	desktop := &editorDesktop{store: store, preferences: preferences}
	window := newTestEditorWithDocument()
	store.attach(window)
	window.desktop = desktop
	window.active.source.replace("关闭询问后恢复的草稿")
	window.showSettings(settingsAppearance)
	session := openTestSession(t, window)
	check(session.ClickLabel("switch:退出时询问保存"))
	saved, err := readApplicationPreferences(directory)
	check(err)
	if !saved.SkipExitSaveConfirmation || saved.SkipSaveConfirmation {
		t.Fatal("the exit setting did not persist independently")
	}

	window.closeSettings()
	desktop.close(window, func() { t.Fatal("closing the last window did not use application exit") })
	if desktop.exitConfirmation != nil || !desktop.exiting {
		t.Fatal("closing the last window ignored the disabled exit prompt")
	}

	reopened, err := openSessionStore(directory)
	check(err)
	if len(reopened.windows) != 1 || reopened.windows[0].active == nil ||
		reopened.windows[0].active.source.text.String() != "关闭询问后恢复的草稿" {
		t.Fatal("exit with confirmation disabled lost the draft")
	}
	window = reopened.windows[0]
	desktop = &editorDesktop{store: reopened, preferences: saved}
	window.desktop = desktop
	window.showSettings(settingsAppearance)
	session = openTestSession(t, window)
	check(session.ClickLabel("switch:退出时询问保存"))
	window.closeSettings()
	desktop.close(window, func() { t.Fatal("closing the last window did not use application exit") })
	check(session.Frame())
	if desktop.exitConfirmation == nil || desktop.exitConfirmation.window != window {
		t.Fatal("enabling the setting did not restore the last-window prompt")
	}
	check(session.Keys("escape"))
	if desktop.exiting || desktop.exitConfirmation != nil {
		t.Fatal("Escape did not cancel the exit")
	}
}
