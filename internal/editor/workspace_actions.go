package editor

import (
	"fmt"
	"slices"
	"strings"
)

const closedTabLimit = 20
const closedTabTextLimit = 16 << 20

// Only text snapshots and file descriptors are retained, never editors or WebViews.
// Large files and Office/preview tabs reopen from disk. This stack is not persisted.
type closedEditorTab struct {
	doc                         document
	name, displayName, language string
	source                      *paneSession
	bytes                       int
}

func (m *editorApp) rememberClosedTab(tab *editorTab) {
	if tab.projectPreview {
		return
	}
	if tab.isTemporaryLink() {
		return
	}
	item := closedEditorTab{doc: document{path: tab.doc.path, encoding: tab.doc.encoding}, name: tab.name,
		displayName: tab.displayName, language: tab.languageOverride}
	if tab.largeText == nil && tab.office == nil && !tab.readOnlyPreview() {
		content := tab.source.text.CommittedString()
		if len(content)+len(tab.doc.saved) <= closedTabTextLimit {
			source := snapshotPane(tab.source)
			source.Wrap = tab.source.view.Snapshot.Wrap
			// Clone so a short view cannot retain a much larger backing string.
			source.Content = strings.Clone(content)
			item.doc = tab.doc
			item.doc.saved = strings.Clone(tab.doc.saved)
			item.source, item.bytes = &source, len(content)+len(tab.doc.saved)
		}
	}
	if item.doc.path == "" && item.source == nil {
		return
	}
	m.closedTabs = append(m.closedTabs, item)
	total := 0
	for _, closed := range m.closedTabs {
		total += closed.bytes
	}
	for len(m.closedTabs) > closedTabLimit || total > closedTabTextLimit {
		total -= m.closedTabs[0].bytes
		m.closedTabs = slices.Delete(m.closedTabs, 0, 1)
	}
}

func (m *editorApp) reopenClosedTab() {
	if len(m.closedTabs) == 0 {
		m.status = "没有可恢复的关闭标签"
		return
	}
	index := len(m.closedTabs) - 1
	item := m.closedTabs[index]
	for _, tab := range m.tabs {
		if item.doc.path != "" && strings.EqualFold(tab.doc.path, item.doc.path) {
			m.closedTabs = slices.Delete(m.closedTabs, index, index+1)
			m.activate(tab)
			return
		}
	}
	var tab *editorTab
	if item.source != nil && (item.doc.path == "" || item.doc.formatDirty || item.source.Content != item.doc.saved) {
		tab = m.addTab(item.doc, item.name)
		restorePane(tab.source, *item.source)
	} else {
		loaded, err := readEditorFile(item.doc.path, item.doc.encoding)
		if err != nil {
			m.fail(err)
			return // Keep the entry available if a removable drive is temporarily absent.
		}
		m.openLoadedFile(loaded)
		tab = m.active
		if item.source != nil && tab.largeText == nil && !tab.readOnlyPreview() {
			state := *item.source
			state.Content = tab.doc.saved
			restorePane(tab.source, state)
		}
	}
	m.closedTabs = slices.Delete(m.closedTabs, index, index+1)
	tab.displayName, tab.languageOverride = item.displayName, item.language
	if item.language != "" {
		tab.suggestedFormat, tab.formatPromptOpen = "", false
	}
	if item.source != nil {
		tab.wrapOverride = true
		tab.setWrap(item.source.Wrap)
	}
	m.activate(tab)
	m.status = "已恢复 " + tab.tabTitle()
}

func (m *editorApp) saveAllTabs() {
	if m.savingAll {
		return
	}
	m.savingAll = true
	// Office editors first flush their latest browser-side changes, as on exit.
	m.flushOfficeTabs(0, func(err error) {
		if err != nil {
			m.savingAll = false
			m.fail(err)
			return
		}
		var tabs []*editorTab
		for _, tab := range m.tabs {
			if tab.modified() {
				tabs = append(tabs, tab)
			}
		}
		m.saveAllNext(tabs, 0)
	})
}

func (m *editorApp) saveAllNext(tabs []*editorTab, index int) {
	if m.windowClosed {
		m.savingAll = false
		return
	}
	if index == len(tabs) {
		m.savingAll = false
		m.status = fmt.Sprintf("已保存 %d 个文件", len(tabs))
		m.focusActiveSource()
		return
	}
	tab := tabs[index]
	m.status = fmt.Sprintf("正在保存 %d/%d · %s", index+1, len(tabs), tab.tabTitle())
	m.saveModifiedTab(tab, func(saved bool) {
		if !saved {
			m.savingAll = false
			m.activate(tab)
			m.status = fmt.Sprintf("全部保存已停止：已保存 %d 个，剩余 %d 个", index, len(tabs)-index)
			return
		}
		next := func() { m.saveAllNext(tabs, index+1) }
		if m.post != nil {
			m.post(next)
		} else {
			next()
		}
	})
}

// A restored, inactive large-file draft needs its editing buffer before saving.
func (m *editorApp) saveModifiedTab(tab *editorTab, complete func(bool)) {
	save := func() {
		m.saveBeforeClosing(tab, complete)
		m.invalidator.Invalidate()
	}
	if state := tab.largeText; state != nil && state.editing == nil {
		state.resumeEdit = false
		if state.edit == nil && !m.startLargeTextEdit(tab, tab.doc.encoding) {
			complete(false)
			return
		}
		state.afterOpen = func(opened bool) {
			if !opened || m.windowClosed {
				complete(false)
				return
			}
			save()
		}
		m.invalidator.Invalidate()
		return
	}
	save()
}
