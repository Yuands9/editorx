package editor

import (
	"context"
	"fmt"
	"strconv"

	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

type macroReplayResult struct {
	value     string
	selection text.Range
}
type macroReplayJob struct {
	tab       *editorTab
	editor    *text.Editor
	before    string
	selection text.Range
	request   *ui.EffectState[macroReplayResult, int]
}

func (m *editorApp) startMacroReplay() {
	s := m.workflow
	if s == nil || s.replay != nil || !historyEditableTab(m.active) {
		return
	}
	editor := m.active.source.text
	count, _ := strconv.Atoi(s.repeat.Text())
	if count < 1 || count > 100 || editor.CompositionActive() || editor.SelectionCount() != 1 || len(editor.String()) > macroDocumentLimit {
		s.problem = uiText("请选择 1–100 次重复，并使用 4 MiB 以内的单光标文本")
		return
	}
	selection := editor.Selection()
	if editor.Caret() == selection.Start {
		selection.Start, selection.End = selection.End, selection.Start
	}
	job := &macroReplayJob{tab: m.active, editor: editor, before: editor.String(), selection: selection}
	options, _ := editor.Code()
	macro := s.macro
	job.request = ui.NewEffect[macroReplayResult, int](func(ctx context.Context) (macroReplayResult, error) {
		work := text.NewEditor(job.before)
		work.SetCode(options)
		work.SetSelection(selection)
		if err := replayMacroContext(ctx, work, macro, count); err != nil {
			return macroReplayResult{}, err
		}
		selection := work.Selection()
		if work.Caret() == selection.Start {
			selection.Start, selection.End = selection.End, selection.Start
		}
		return macroReplayResult{value: work.String(), selection: selection}, ctx.Err()
	})
	s.replay, s.problem, s.notice = job, "", uiText("正在重放宏…")
}

func (m *editorApp) updateMacroReplay(c ui.C, s *workflowDialog) {
	job := s.replay
	if job == nil {
		return
	}
	ui.Effect(c, job.request, 0)
	result, err, done := job.request.Take()
	if !done {
		return
	}
	s.replay = nil
	if err != nil {
		s.problem = fmt.Sprintf(uiText("宏未应用：%v"), err)
		return
	}
	editor := job.editor
	selection := editor.Selection()
	if editor.Caret() == selection.Start {
		selection.Start, selection.End = selection.End, selection.Start
	}
	if !m.hasTab(job.tab) || job.tab.source.text != editor || editor.String() != job.before || selection != job.selection || editor.CompositionActive() {
		s.problem = uiText("编辑内容已变化，宏结果未应用，请重新执行")
		return
	}
	start, end, value := trimHistoryReplacement(job.before, 0, len(job.before), result.value)
	if job.before != result.value {
		editor.ReplaceRange(text.Range{Start: editor.RuneIndex(start), End: editor.RuneIndex(end)}, value)
	}
	editor.SetSelection(result.selection)
	m.workflow = nil
	m.activate(job.tab)
	m.focusActiveSource()
	m.status = uiText("宏已重放 · Ctrl+Z 撤销整次操作")
}
