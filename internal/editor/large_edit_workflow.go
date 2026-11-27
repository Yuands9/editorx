package editor

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/vivaui/viva/ui"
)

var errLargeDraftBase = errors.New("原文件已变化，无法应用未保存的改动。请恢复原文件后重试。")

// Large-file sessions store the viewport and patches against the last saved
// version. The temporary document file and undo history never enter the session.
type largeEditSession struct {
	Anchor, Caret, Top    int64
	BasePath              string
	BaseHash              [32]byte
	Changes               []largeEditChange `json:",omitempty"`
	Patch                 string            `json:",omitempty"`
	patchPath, patchError string
}

func (s *largeEditSession) hasChanges() bool {
	return s != nil && (len(s.Changes) > 0 || s.Patch != "" || s.patchError != "")
}

func (s *largeTextView) busyEditing() bool {
	return s.edit != nil || s.saving != nil || s.afterSave != nil || s.replacing != nil
}

func (s *largeTextView) close() {
	if s == nil {
		return
	}
	if s.loadStore != nil {
		s.loadStore.close()
		s.loadStore = nil
	}
	if s.editing != nil {
		s.editing.revision.store.close()
		s.editing = nil
	}
}

func (t *editorTab) snapshotLargeEdit(directory string) *largeEditSession {
	state, edit := t.largeText, t.largeText.editing
	if edit == nil {
		if state.editSession == nil {
			return nil
		}
		draft := *state.editSession
		return &draft
	}
	if edit.draftRevision != edit.revision {
		spans := largeEditSpans(edit.saved, edit.revision)
		var total int64
		for _, span := range spans {
			total += 48 + span.insertEnd - span.insertStart
		}
		draft := largeEditSession{}
		var err error
		if total > largeDraftInlineLimit {
			draft.Patch, draft.patchPath, err = persistLargeEditDraft(directory, edit.revision, spans)
		} else {
			draft.Changes, err = inlineLargeEditChanges(edit.revision, spans)
		}
		if err != nil {
			draft.patchError = fmt.Sprintf(uiText("记录大文件草稿失败：%v"), err)
			state.problem = draft.patchError
		} else {
			edit.draftRevision = edit.revision
		}
		edit.draft = draft
	}
	draft := edit.draft
	draft.Anchor, draft.Caret, draft.Top = edit.position.anchor, edit.position.caret, edit.top
	draft.BaseHash = t.doc.diskHash
	if t.doc.path == "" {
		draft.BasePath = edit.basePath
	}
	return &draft
}

func (m *editorApp) startLargeTextEdit(t *editorTab, encoding textEncoding) bool {
	state := t.largeText
	if state.busyEditing() {
		return false
	}
	store, err := newLargeEditStorage()
	if err != nil {
		state.problem = err.Error()
		return false
	}
	path, start, draft := t.doc.path, state.start, state.editSession
	var browsePage string
	var top, anchor, caret int
	if state.editing == nil && draft == nil {
		pane := t.source
		selection := pane.text.Selection()
		caret = pane.text.ByteOffset(pane.text.Caret())
		anchor = pane.text.ByteOffset(selection.Start)
		if pane.text.Caret() == selection.Start {
			anchor = pane.text.ByteOffset(selection.End)
		}
		top, _ = pane.view.Code.OffsetAtY(pane.view.Code.ScrollY())
		browsePage = pane.text.String()
	}
	if state.editing != nil {
		start = 0
	}
	if draft != nil && draft.BasePath != "" {
		path = draft.BasePath
	}
	state.search, state.allMatches, state.indexing, state.problem = nil, nil, nil, ""
	state.loadStore = store
	state.edit = ui.NewEffect[largeEditOpening, int](func(ctx context.Context) (largeEditOpening, error) {
		loaded, err := loadLargeEdit(ctx, store, path, encoding, start)
		if err != nil {
			return largeEditOpening{}, err
		}
		result, err := restoreLargeEditOpening(ctx, loaded, draft)
		if err == nil && browsePage != "" && max(top, anchor, caret) > 0 {
			r := loaded.revision
			value, readErr := r.read(loaded.start, min(r.size, loaded.start+2*int64(len(browsePage))+2))
			if readErr != nil {
				return largeEditOpening{}, readErr
			}
			window := newLargeEditWindow(loaded.start, 0, value)
			// If the file changed after browsing, start at the new page boundary.
			if strings.HasPrefix(window.value, browsePage) {
				result.top = window.absolute(top)
				result.position = largeEditPosition{window.absolute(anchor), window.absolute(caret)}
			}
		}
		return result, err
	})
	return true
}

func (m *editorApp) exitLargeTextEdit(t *editorTab) {
	state := t.largeText
	if state.editing == nil || state.busyEditing() {
		return
	}
	if _, composing := t.source.text.Composition(); composing {
		state.problem = uiText("请先完成中文选词，再退出完整编辑")
		t.source.focus = true
		return
	}
	resumeBrowsing := func() { m.resumeLargeTextBrowsing(t) }
	if t.doc.path == "" {
		// 从备份恢复的副本需要有单独的磁盘文件供分页浏览。
		m.startLargeEditSave(t, false, func(saved bool) {
			if saved {
				resumeBrowsing()
			}
		})
		return
	}
	m.requestTab(t, resumeBrowsing)
}

func (m *editorApp) resumeLargeTextBrowsing(t *editorTab) {
	state := t.largeText
	page, err := readLargeTextPage(t.doc.path, state.index, -1, "", t.doc.encoding)
	if err != nil {
		m.fail(fmt.Errorf(uiText("返回大文件浏览失败：%w"), err))
		return
	}
	edit := state.editing
	t.updateLargeEditBookmarks(edit.revision, edit.saved)
	t.search.close()
	t.search.ready = false
	m.results.removeTab(t)
	if m.searchBatch != nil && m.searchBatch.origin == t {
		m.searchBatch = nil
	}
	state.close()

	t.source = newEditorPane("", true)
	t.source.light = m.light
	t.source.applyEditorPreferences(m.editorPrefs)
	t.search.target = t.source
	t.bindSearchWindow(m)
	t.startLargeText(page)
	if t == m.active {
		t.source.focus = true
		m.status = uiText("已返回大文件浏览")
	}
	m.releaseClosedTabText(t)
}

type largeEditOpening struct {
	loaded   largeEditLoad
	original *largeEditRevision
	position largeEditPosition
	top      int64
}

func restoreLargeEditOpening(ctx context.Context, loaded largeEditLoad, draft *largeEditSession) (largeEditOpening, error) {
	result := largeEditOpening{loaded: loaded, original: loaded.revision, top: loaded.start,
		position: largeEditPosition{loaded.start, loaded.start}}
	if draft == nil {
		return result, nil
	}
	if draft.hasChanges() {
		if draft.BaseHash != loaded.doc.diskHash {
			return largeEditOpening{}, errLargeDraftBase
		}
		var next *largeEditRevision
		var err error
		if draft.Patch != "" {
			next, err = applyLargeEditDraft(ctx, loaded.revision, draft.patchPath)
		} else {
			next, err = loaded.revision.apply(ctx, draft.Changes)
		}
		if err != nil {
			return largeEditOpening{}, fmt.Errorf(uiText("无法恢复大文件草稿：%w"), err)
		}
		result.loaded.revision = next
	}
	r := result.loaded.revision
	align := func(offset int64) int64 {
		offset = max(0, min(r.size, offset))
		for !r.boundary(offset) {
			offset--
		}
		return offset
	}
	result.position = largeEditPosition{align(draft.Anchor), align(draft.Caret)}
	result.top = align(draft.Top)
	return result, nil
}

func (t *editorTab) finishLargeEditOpen(result largeEditOpening, err error) error {
	state := t.largeText
	if err == nil {
		expected := t.doc.path
		if state.editSession != nil && state.editSession.BasePath != "" {
			expected = state.editSession.BasePath
		}
		if result.loaded.doc.path != expected {
			err = errors.New(uiText("文件已改名，请重新打开编辑"))
		}
	}
	if err != nil {
		state.loadStore.close()
		state.loadStore = nil
		return err
	}
	if state.editing != nil {
		state.editing.revision.store.close()
	}
	state.loadStore = nil
	err = t.enterLargeEdit(result.loaded, result.original, result.position, result.top)
	if state.editSession != nil && state.editSession.BasePath != "" {
		t.doc.path = ""
	}
	if err != nil {
		return err
	}
	if state.editSession != nil {
		edit := state.editing
		edit.draftRevision, edit.draft = edit.revision, *state.editSession
	}
	state.editSession = nil
	return nil
}

func (m *editorApp) updateLargeTextEdit(c ui.C, t *editorTab) func() {
	state := t.largeText
	if state == nil {
		return nil
	}
	if state.resumeEdit && t == m.active && !m.appSettings.open && !m.vaultActive() {
		state.resumeEdit = false
		m.startLargeTextEdit(t, t.doc.encoding)
	}
	if state.edit != nil {
		ui.Effect(c.Part("open"), state.edit, 0)
		if result, err, ready := state.edit.Take(); ready {
			after := state.afterOpen
			state.edit, state.afterOpen = nil, nil
			err = t.finishLargeEditOpen(result, err)
			if err != nil {
				state.problem = err.Error()
			} else if t == m.active {
				t.source.focus = true
				m.status = uiText("已打开完整编辑")
			}
			if after != nil {
				return func() { after(err == nil) }
			}
			if err != nil {
				return nil
			}
		}
	}
	if state.editing != nil {
		t.updateLargeEditSearch(c.Part("find"))
	}
	if state.replacing != nil {
		ui.Effect(c.Part("replace"), state.replacing, 0)
		if result, err, ready := state.replacing.Take(); ready {
			state.replacing = nil
			t.reportLargeEditReplacement(result, err)
		}
	}
	if state.saving != nil {
		ui.Effect(c.Part("save"), state.saving, 0)
		if doc, err, ready := state.saving.Take(); ready {
			after := state.afterSave
			path := state.savePath
			state.saving, state.savePath = nil, ""
			edit := state.editing
			if err != nil {
				m.reportSaveError(t, path, err)
			} else {
				t.doc = doc
				m.markDiskSaved(t)
				edit.basePath = doc.path
				edit.saved = edit.revision
				edit.draftRevision, edit.draft = nil, largeEditSession{}
				state.problem = ""
				m.rememberPath(doc.path, false)
				m.status = uiText("已保存 ") + filepath.Base(doc.path)
				if t == m.active {
					t.source.focus = true
				}
			}
			if after != nil {
				return func() {
					state.afterSave = nil
					if m.windowClosed || t.largeText != state || state.editing != edit {
						return
					}
					after(err == nil)
					m.invalidator.Invalidate()
				}
			}
		}
	}
	return nil
}

func (m *editorApp) startLargeEditSave(t *editorTab, saveAs bool, after func(bool)) {
	state := t.largeText
	complete := func(saved bool) {
		if after != nil {
			after(saved)
		}
	}
	if state.busyEditing() {
		m.status = uiText("请等待当前大文件操作完成")
		complete(false)
		return
	}
	if _, composing := t.source.text.Composition(); composing {
		m.fail(errors.New(uiText("请先完成中文选词，再保存文件")))
		complete(false)
		return
	}
	if !saveAs && t.doc.path != "" && !t.modified() {
		complete(true)
		return
	}
	path, ok := m.textSavePath(t, saveAs)
	if !ok {
		complete(false)
		return
	}
	m.saveLargeEditTo(t, path, nil, after)
}

func (m *editorApp) saveLargeEditTo(t *editorTab, path string, overwrite *[32]byte, after func(bool)) {
	state := t.largeText
	doc, r := t.doc, state.editing.revision
	if overwrite != nil {
		doc.diskHash = *overwrite
	}
	history := m.documentHistory()
	state.afterSave, state.savePath = after, path
	state.saving = ui.NewEffect[document, int](func(ctx context.Context) (document, error) {
		return saveLargeEdit(ctx, doc, path, r, history)
	})
}

func (m *editorApp) largeEditOperationPending() bool {
	for _, t := range m.tabs {
		if t.savingText != nil || t.largeText != nil && t.largeText.saving != nil {
			m.status = uiText("请等待大文件保存完成")
			return true
		}
	}
	return false
}

func (tab *editorTab) restoreLargeTextTab(page largeTextPage, item tabSession, index int64, readErr error) {
	tab.startLargeText(page)
	state := tab.largeText
	if readErr != nil {
		state.index = index
	}
	tab.bookmarks.lines = item.Bookmarks
	if item.LargeEdit != nil {
		draft := *item.LargeEdit
		state.editSession, state.resumeEdit = &draft, true
	}
	if readErr != nil {
		state.problem = uiText("原文件暂时无法读取：") + readErr.Error()
	}
}

func largeEditSearchSelection(tab *editorTab) string {
	edit := tab.largeText.editing
	start, end := edit.position.rangeOffsets()
	if start == end || end-start > largeTextQueryLimit {
		return ""
	}
	if _, composing := tab.source.text.Composition(); composing {
		return ""
	}
	value, err := edit.revision.read(start, end)
	if err != nil {
		return ""
	}
	return value
}

func (t *editorTab) canReplace() bool {
	return t.canSearchText() || t.largeText != nil && t.largeText.editing != nil && !t.largeText.busyEditing()
}
