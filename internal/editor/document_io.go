package editor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vivaui/viva/ui"
)

const backgroundTextBytes = 1 << 20

type textOpenJob struct {
	path     string
	request  *ui.EffectState[openedEditorFile, int]
	complete []func(*editorTab)
}
type textSaveJob struct {
	path     string
	doc      document
	request  *ui.EffectState[document, int]
	complete func(bool)
}

func (m *editorApp) loadTextFile(path string, enc textEncoding, complete func(*editorTab)) {
	path, err := filepath.Abs(path)
	if err != nil {
		m.fail(err)
		return
	}
	if tab := m.tabAtPath(path); tab != nil {
		m.activate(tab)
		if enc != "" && tab.canFind() {
			m.reopenTextEncoding(tab, enc)
		}
		m.rememberPath(path, false)
		if complete != nil {
			complete(tab)
		}
		return
	}
	for _, job := range m.textOpens {
		if strings.EqualFold(job.path, path) {
			if complete != nil {
				job.complete = append(job.complete, complete)
			}
			return
		}
	}
	// Small local files retain their immediate open path. Large decoding and
	// content detection run sequentially off the UI thread, including multi-drop.
	info, err := os.Stat(path)
	if err != nil {
		m.fail(fmt.Errorf("打开 %s 失败\n%w", path, err))
		return
	}
	if (info.Size() < backgroundTextBytes || largeTextFile(path)) && len(m.textOpens) == 0 {
		file, err := readEditorFile(path, enc)
		if err != nil {
			m.fail(fmt.Errorf("打开 %s 失败\n%w", path, err))
			return
		}
		tab := m.openLoadedFile(file)
		if complete != nil {
			complete(tab)
		}
		return
	}
	job := &textOpenJob{path: path}
	if complete != nil {
		job.complete = append(job.complete, complete)
	}
	job.request = ui.NewEffect[openedEditorFile, int](func(ctx context.Context) (openedEditorFile, error) {
		if err := ctx.Err(); err != nil {
			return openedEditorFile{}, err
		}
		file, err := readEditorFile(path, enc)
		if err == nil {
			err = ctx.Err()
		}
		return file, err
	})
	m.textOpens = append(m.textOpens, job)
	m.invalidator.Invalidate()
	m.status = "正在打开 " + filepath.Base(path) + "…"
}

func (m *editorApp) saveTextTabWithCallback(t *editorTab, saveAs bool, complete func(bool)) bool {
	finish := func(ok bool) bool {
		if complete != nil {
			complete(ok)
		}
		return ok
	}
	if t.savingText != nil {
		return finish(false)
	}
	path, ok := m.textSavePath(t, saveAs)
	if !ok {
		return finish(false)
	}
	value := t.source.text.String()
	if max(len(value), len(t.doc.saved)) < backgroundTextBytes {
		if err := t.doc.save(path, value, m.documentHistory()); err != nil {
			m.fail(fmt.Errorf("保存 %s 失败\n%w", path, err))
			return finish(false)
		}
		m.finishTextSave(t, path)
		return finish(true)
	}
	doc, history := t.doc, m.documentHistory()
	job := &textSaveJob{path: path, doc: doc, complete: complete}
	job.request = ui.NewEffect[document, int](func(ctx context.Context) (document, error) {
		if err := ctx.Err(); err != nil {
			return doc, err
		}
		err := doc.save(path, value, history)
		return doc, err
	})
	t.savingText = job
	m.invalidator.Invalidate()
	m.status = "正在保存 " + filepath.Base(path) + "…"
	return false
}

func (m *editorApp) finishTextSave(t *editorTab, path string) {
	m.rememberPath(path, false)
	if t.html.view != nil {
		m.refreshHTML(t)
	}
	m.status = "已保存 " + filepath.Base(path)
}

func (m *editorApp) updateTextIO(c ui.C) {
	var completions []func()
	if len(m.textOpens) > 0 {
		job := m.textOpens[0]
		ui.Effect(c.Part("open"), job.request, 0)
		if file, err, done := job.request.Take(); done {
			m.textOpens[0] = nil
			m.textOpens = m.textOpens[1:]
			if err != nil {
				m.fail(fmt.Errorf("打开 %s 失败\n%w", job.path, err))
			} else if !m.windowClosed {
				tab := m.tabAtPath(file.doc.path)
				if tab == nil {
					tab = m.openLoadedFile(file)
				} else {
					m.activate(tab)
				}
				for _, complete := range job.complete {
					complete(tab)
				}
			}
		}
	}
	for _, tab := range m.tabs {
		job := tab.savingText
		if job == nil {
			continue
		}
		ui.Effect(c.Part("save").KeyInt(tab.id), job.request, 0)
		if doc, err, done := job.request.Take(); done {
			tab.savingText = nil
			if err != nil {
				m.fail(fmt.Errorf("保存 %s 失败\n%w", job.path, err))
			} else {
				// Edits made during the write remain dirty against the saved snapshot.
				tab.doc.path, tab.doc.saved, tab.doc.diskHash = doc.path, doc.saved, doc.diskHash
				if tab.doc.encoding == job.doc.encoding && tab.doc.bom == job.doc.bom && tab.doc.eol == job.doc.eol {
					tab.doc.formatDirty = false
				}
				m.finishTextSave(tab, job.path)
			}
			if job.complete != nil {
				completions = append(completions, func() { job.complete(err == nil && !tab.modified()) })
			}
		}
	}
	for _, complete := range completions {
		if m.post != nil {
			m.post(complete)
		} else {
			complete()
		}
	}
}
