package editor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"github.com/vivaui/viva/window"
)

const macroStepLimit = 1000
const macroTextLimit = 256 << 10
const macroDocumentLimit = 4 << 20

type editorMacro struct {
	Name  string      `json:"name"`
	Steps []macroStep `json:"steps"`
}
type macroStep struct {
	Kind     window.TextEventKind         `json:"kind,omitempty"`
	Command  window.TextCommand           `json:"command,omitempty"`
	Code     components.CodeEditorCommand `json:"code,omitempty"`
	Movement components.TextareaMovement  `json:"movement,omitempty"`
	Extend   bool                         `json:"extend,omitempty"`
	Text     string                       `json:"text,omitempty"`
	Replace  bool                         `json:"replace,omitempty"`
	Start    int                          `json:"start,omitempty"`
	End      int                          `json:"end,omitempty"`
}
type macroRecording struct {
	tab               *editorTab
	editor            *text.Editor
	steps             []macroStep
	bytes             int
	revision          uint64
	composing         bool
	compositionBefore string
	compositionCaret  int
	problem           string
}

func (record *macroRecording) append(step macroStep) {
	if record.problem != "" {
		return
	}
	if len(record.steps) >= macroStepLimit || record.bytes+len(step.Text) > macroTextLimit {
		record.problem = uiText("宏达到 1000 步或 256 KiB 上限，本次录制未保存")
		return
	}
	record.steps = append(record.steps, step)
	record.bytes += len(step.Text)
	record.revision = record.editor.TrackEdits()
}

func (m *editorApp) startMacroRecording() {
	tab := m.active
	if !historyEditableTab(tab) || tab.source.text.CompositionActive() || tab.source.text.SelectionCount() != 1 || len(tab.source.text.String()) > macroDocumentLimit {
		m.status = uiText("宏录制支持 4 MiB 以内的普通文本，请使用单光标并完成输入")
		return
	}
	record := &macroRecording{tab: tab, editor: tab.source.text, revision: tab.source.text.TrackEdits()}
	m.macro, tab.source.recording = record, record
	tab.source.input.Editor, tab.source.input.OnEvent = nil, record.event
	m.focusActiveSource()
}

func (record *macroRecording) event(event window.TextEvent) (window.TextState, error) {
	editor := record.editor
	before, caret := editor.String(), editor.Caret()
	wasComposing := editor.CompositionActive()
	event.Apply(editor)
	if !wasComposing && editor.CompositionActive() {
		record.composing, record.compositionBefore, record.compositionCaret = true, before, caret
	}
	if editor.CompositionActive() {
		record.revision = editor.TrackEdits()
		return app.TextStateOf(editor), nil
	}
	if record.composing {
		record.recordReplacement(record.compositionBefore, record.compositionCaret)
		record.composing, record.compositionBefore = false, ""
	} else if event.Kind == window.TextSetSelection {
		record.append(macroStep{Kind: event.Kind, Start: event.Selection.Start - caret, End: event.Selection.End - caret})
	} else if event.HasReplacement || event.HasReplacementUTF16 || event.CompositionPhase != text.CompositionNone {
		record.recordReplacement(before, caret)
	} else if event.Kind == window.TextCommandEvent || event.Kind == window.TextInsert || event.Kind == window.TextPaste {
		record.append(macroStep{Kind: event.Kind, Command: event.Command, Text: event.Text})
	}
	record.revision = editor.TrackEdits()
	return app.TextStateOf(editor), nil
}

func (record *macroRecording) recordReplacement(before string, caret int) {
	after := record.editor.String()
	if before == after {
		return
	}
	start, end, value := trimHistoryReplacement(before, 0, len(before), after)
	record.append(macroStep{Replace: true, Start: utf8.RuneCountInString(before[:start]) - caret,
		End: utf8.RuneCountInString(before[:end]) - caret, Text: value})
}

func (m *editorApp) stopMacroRecording() bool {
	record := m.macro
	if record == nil {
		return false
	}
	m.macro = nil
	pane := record.tab.source
	pane.recording, pane.input.Editor, pane.input.OnEvent = nil, pane.text, nil
	if record.problem != "" {
		m.status = record.problem
		return false
	}
	if len(record.steps) == 0 {
		m.status = uiText("没有录制到编辑操作")
		return false
	}
	m.lastMacro = &editorMacro{Name: "新录制的宏", Steps: record.steps}
	m.status = fmt.Sprintf(uiText("已录制 %d 步，可在宏管理中命名保存或重放"), len(record.steps))
	return true
}

func (m *editorApp) checkMacroRecording() {
	r := m.macro
	if r == nil {
		return
	}
	if r.tab != m.active || r.editor != r.tab.source.text || !m.hasTab(r.tab) {
		m.stopMacroRecording()
		return
	}
	if r.editor.SelectionCount() > 1 || len(r.editor.String()) > macroDocumentLimit || r.editor.TrackEdits() != r.revision {
		r.problem = uiText("录制遇到多光标或未支持的编辑操作，本次宏未保存。请使用键盘输入、删除和移动。")
	}
	if r.problem != "" {
		m.stopMacroRecording()
	}
}

// Replay in a temporary editor, then commit one undoable replacement. Invalid
// relative ranges or oversized output never partially change the user's text.
func replayMacro(editor *text.Editor, macro editorMacro, count int) error {
	return replayMacroContext(context.Background(), editor, macro, count)
}

func replayMacroContext(ctx context.Context, editor *text.Editor, macro editorMacro, count int) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if count < 1 || count > 100 || len(macro.Steps) == 0 || len(macro.Steps) > macroStepLimit {
		return errors.New(uiText("请选择 1–100 次重复和有效的宏"))
	}
	before := editor.String()
	if len(before) > macroDocumentLimit || editor.CompositionActive() || editor.SelectionCount() != 1 {
		return errors.New(uiText("宏重放需要 4 MiB 以内的单光标文本，并完成输入"))
	}
	work := text.NewEditor(before)
	options, _ := editor.Code()
	work.SetCode(options)
	selection := editor.Selection()
	if editor.Caret() == selection.Start {
		selection.Start, selection.End = selection.End, selection.Start
	}
	work.SetSelection(selection)
	for n := 0; n < count; n++ {
		for _, step := range macro.Steps {
			if err := ctx.Err(); err != nil {
				return err
			}
			switch {
			case step.Replace || step.Kind == window.TextSetSelection:
				start, end := work.Caret()+step.Start, work.Caret()+step.End
				if min(start, end) < 0 || max(start, end) > work.RuneCount() {
					return errors.New(uiText("宏的相对选区越界，编辑内容未改变"))
				}
				if step.Replace {
					work.ReplaceRange(text.Range{Start: start, End: end}, step.Text)
				} else {
					work.SetSelection(text.Range{Start: start, End: end})
				}
			case step.Code != 0:
				step.Code.Apply(work)
			case step.Movement != components.TextareaMoveNone:
				moveMacroCaret(work, step.Movement, step.Extend)
			default:
				(window.TextEvent{Kind: step.Kind, Command: step.Command, Text: step.Text}).Apply(work)
			}
			if len(work.String()) > macroDocumentLimit {
				return errors.New(uiText("宏结果超过 4 MiB，编辑内容未改变"))
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	start, end, value := trimHistoryReplacement(before, 0, len(before), work.String())
	if utf8.RuneCountInString(before[start:end])+utf8.RuneCountInString(value) > historyMergeUndoRunes {
		return errors.New(uiText("宏改动超过可撤销上限，编辑内容未改变"))
	}
	if before != work.String() {
		editor.ReplaceRange(text.Range{Start: editor.RuneIndex(start), End: editor.RuneIndex(end)}, value)
	}
	selection = work.Selection()
	if work.Caret() == selection.Start {
		selection.Start, selection.End = selection.End, selection.Start
	}
	editor.SetSelection(selection)
	return nil
}

func moveMacroCaret(editor *text.Editor, movement components.TextareaMovement, extend bool) {
	value, caret := editor.String(), editor.ByteOffset(editor.Caret())
	start := strings.LastIndexByte(value[:caret], '\n') + 1
	end := strings.IndexByte(value[caret:], '\n')
	if end < 0 {
		end = len(value)
	} else {
		end += caret
	}
	to := caret
	switch movement {
	case components.TextareaMoveLineStart:
		to = start
	case components.TextareaMoveLineEnd:
		to = end
	case components.TextareaMoveTextStart:
		to = 0
	case components.TextareaMoveTextEnd:
		to = len(value)
	case components.TextareaMoveUp, components.TextareaMovePageUp, components.TextareaMoveDown, components.TextareaMovePageDown:
		column := utf8.RuneCountInString(value[start:caret])
		if movement == components.TextareaMoveUp || movement == components.TextareaMovePageUp {
			if start == 0 {
				return
			}
			end = start - 1
			start = strings.LastIndexByte(value[:end], '\n') + 1
		} else {
			if end == len(value) {
				return
			}
			start = end + 1
			end = strings.IndexByte(value[start:], '\n')
			if end < 0 {
				end = len(value)
			} else {
				end += start
			}
		}
		to = start
		for column > 0 && to < end {
			_, size := utf8.DecodeRuneInString(value[to:end])
			to += size
			column--
		}
	}
	editor.MoveTo(editor.RuneIndex(to), extend)
}

func (m *editorApp) macroRecordingBar(c ui.C, tab *editorTab) {
	if m.macro == nil || m.macro.tab != tab {
		return
	}
	ui.Row(c).FillWidth().Height(32).PadX(10).Gap(10).Items(ui.Center).Children(func() {
		ui.Text(c, fmt.Sprintf(uiText("正在录制宏 · %d 步"), len(m.macro.steps))).FontSize(12).Color(m.theme.Danger).Grow(1)
		if ui.Button(c, uiText("停止录制")).Ghost().Height(28).Disabled(m.macro.editor.CompositionActive()).Clicked() {
			c.After(func() {
				if m.stopMacroRecording() {
					m.openWorkflowDialog("宏")
				}
			})
		}
	})
}
