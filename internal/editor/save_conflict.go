package editor

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/vivaui/viva/ui"
)

type saveConflictState struct {
	open    bool
	tab     *editorTab
	path    string
	version *documentVersionChanged
}

func (m *editorApp) reportSaveError(tab *editorTab, path string, err error) {
	if !errors.Is(err, errDocumentChanged) || !strings.EqualFold(path, tab.doc.path) {
		m.fail(fmt.Errorf(uiText("保存 %s 失败\n%w"), path, err))
		return
	}
	m.debug.record("document.save", err)
	conflict := &saveConflictState{open: true, tab: tab, path: path}
	errors.As(err, &conflict.version)
	m.saveConflict = conflict
	if m.confirmTab == tab {
		m.clearSaveConfirmation()
	}
	m.activate(tab)
	m.status = uiText("磁盘文件已变化")
	m.invalidator.Invalidate()
}

func (m *editorApp) saveConflictDialog(c ui.C) {
	s := m.saveConflict
	if s == nil {
		return
	}
	t := s.tab
	if !m.hasTab(t) || !strings.EqualFold(t.doc.path, s.path) {
		m.saveConflict = nil
		return
	}
	closeDialog := func() {
		m.saveConflict = nil
		m.focusActiveSource()
	}
	sameFile := func() bool { return m.hasTab(t) && strings.EqualFold(t.doc.path, s.path) }
	compare := historyEditableTab(t) || t.largeText != nil && t.largeText.editing != nil
	spec := ui.ModalSpec{Width: 660, Description: fmt.Sprintf(uiText("「%s」在磁盘上已被其他程序修改。"), filepath.Base(s.path))}
	dismissed := ui.ModalWith(c, &s.open, uiText("保存冲突"), spec, func() {
		ui.Wrap(c, uiText("覆盖文件会用编辑区的内容替换磁盘文件。重新读取会放弃未保存的修改。")).FillWidth()
	}, func() {
		if ui.Button(c, uiText("取消")).Ghost().Clicked() {
			c.After(closeDialog)
		}
		if ui.Button(c, uiText("比对磁盘内容")).Outline().Disabled(!compare).Clicked() {
			c.After(func() {
				closeDialog()
				if sameFile() {
					m.startDiskRead(t, "compare")
				}
			})
		}
		if ui.Button(c, uiText("重新读取")).Outline().Clicked() {
			c.After(func() {
				closeDialog()
				if sameFile() {
					m.startDiskRead(t, "reload-save-conflict")
				}
			})
		}
		if ui.Button(c, uiText("覆盖文件")).Destructive().Disabled(s.version == nil).Clicked() {
			c.After(func() {
				closeDialog()
				if !sameFile() {
					return
				}
				digest := &s.version.digest
				switch {
				case t.office != nil:
					m.saveOfficeTabWithVersion(t, false, digest, nil)
				case t.largeText != nil && t.largeText.editing != nil:
					m.saveLargeEditTo(t, s.path, digest, nil)
				default:
					m.saveTextSnapshot(t, s.path, t.source.text.CommittedString(), digest, nil)
				}
			})
		}
	})
	if dismissed {
		c.After(closeDialog)
	}
}

func (m *editorApp) markDiskSaved(tab *editorTab) {
	s := tab.diskChange
	if s == nil || s.path != tab.doc.path {
		return
	}
	if s.action == "check" {
		s.request = nil
	}
	s.changed, s.missing, s.confirmReload, s.ignoredMissing = false, false, false, false
	s.ignored, s.problem = [32]byte{}, ""
}
