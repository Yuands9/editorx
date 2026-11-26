package editor

import (
	"errors"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/text"
)

func (t *editorTab) jsonLiveResult() bool {
	return t.jsonAction == "格式化" || t.jsonAction == "压缩" || t.jsonAction == "键排序"
}

func (t *editorTab) jsonComposing() bool {
	_, left := t.source.text.Composition()
	_, right := t.result.text.Composition()
	return left || right
}

func (t *editorTab) hasResultDraft() bool {
	return t.resultDraft || !t.jsonLiveResult() && t.result.text.CommittedString() != t.resultOutput
}

func (t *editorTab) updateResultNow() {
	if t.jsonCompare || t.jsonComposing() {
		return
	}
	left, right := t.source.text.CommittedString(), t.result.text.CommittedString()
	if t.resultReady && left == t.resultSource && right == t.resultOutput {
		return
	}
	live := t.jsonLiveResult()
	draft := t.hasResultDraft()
	editor, input := t.source, left
	if live && !draft && right != t.resultOutput && (left == t.resultSource || t.result.focused) {
		editor, input = t.result, right
	}

	formatted := input
	var formatError error
	if strings.TrimSpace(input) != "" {
		if value, _, err := transformJSON(input, "格式化", t.jsonIndent); err == nil {
			formatted = value
		} else {
			formatError = err
		}
	}
	// 左侧始终保留原始输入（含空行、缩进和未完成的 JSON）。
	// 只有用户主动编辑右侧时才同步写回，预览刷新不修改原文或撤销记录。
	if editor == t.result {
		t.source.syncJSONText(formatted, false)
	}
	t.resultSource = t.source.text.CommittedString()
	t.resultReady = true

	if draft {
		t.resultDraft, t.resultOutput = true, right
		t.resultError, t.resultErrorOffset, t.resultErrorPane = "", -1, nil
		return
	}
	output, kind, err := formatted, "JSON", formatError
	if !live || t.jsonAction != "格式化" && err == nil && strings.TrimSpace(input) != "" {
		output, kind, err = transformJSON(formatted, t.jsonAction, t.jsonIndent)
		if err != nil {
			output, kind = "", "JSON"
		}
	}
	t.resultError, t.resultErrorOffset, t.resultErrorPane = "", -1, nil
	if err != nil {
		t.resultError, t.resultErrorOffset = jsonErrorMessage(input, err)
		t.resultErrorPane = editor
	}
	if editor == t.result && t.result.text.CanRedo() {
		output = right
	}
	t.setJSONResult(output, kind, editor == t.result && t.jsonAction == "格式化" && formatError == nil)
}

func (t *editorTab) setJSONResult(output, kind string, formattingOnly bool) {
	if t.resultOutput == "" && t.result.text.RuneCount() == 0 && !t.result.text.CanUndo() && !t.result.text.CanRedo() {
		if output != "" {
			t.result.setResult(output)
		}
	} else {
		t.result.syncJSONText(output, formattingOnly)
	}
	t.resultKind, t.resultOutput = kind, t.result.text.CommittedString()
	t.result.view.Snapshot.Language = kind
}

func isJSONExtraAction(action string) bool {
	return slices.Contains(jsonExtraActions[:], action)
}

func (m *editorApp) jsonAction(t *editorTab, action string) {
	if t.jsonComposing() {
		m.status = "请先完成中文选词，再处理 JSON"
		return
	}
	t.updateResult()
	cancel := action == t.jsonAction && isJSONExtraAction(action)
	if cancel {
		action = "格式化"
	}
	modeChanged := action != t.jsonAction || t.resultDraft
	source := t.source.text.CommittedString()
	if len(source) >= backgroundTextBytes {
		t.jsonAction, t.resultDraft, t.resultReady = action, false, false
		t.resultOutput = t.result.text.CommittedString()
		t.jsonCompare, t.diffReady = false, false
		if modeChanged {
			t.result.text.ClearHistory()
		}
		t.updateResult()
		m.status = "JSON · " + action + "…"
		return
	}
	if action == "格式化" {
		t.jsonAction, t.resultDraft, t.resultReady = action, false, false
		// 从源文恢复同步，避免把取消前的预览误当成一次右侧编辑。
		t.resultOutput = t.result.text.CommittedString()
		t.updateResult()
	} else {
		output, kind, err := transformJSON(source, action, t.jsonIndent)
		if err != nil {
			message, _ := jsonErrorMessage(source, err)
			m.fail(errors.New(message))
			return
		}
		t.jsonAction, t.resultDraft, t.resultReady = action, false, true
		t.resultSource = source
		t.resultError, t.resultErrorOffset, t.resultErrorPane = "", -1, nil
		t.setJSONResult(output, kind, false)
	}
	t.comparison.text.CancelComposition()
	t.jsonCompare, t.diffReady = false, false
	if modeChanged {
		// 模式切换后的撤销不能把上一个预览写入原文件。
		t.result.text.ClearHistory()
	}
	if cancel {
		m.status = "已取消 JSON 转换"
	} else {
		m.status = "JSON · " + action
	}
}

// 保留最小修改区间，让同步编辑沿用各栏的选区、布局和撤销历史。
func (p *editorPane) syncJSONText(value string, formattingOnly bool) {
	current := p.text.CommittedString()
	if current == value {
		return
	}
	start := 0
	for start < min(len(current), len(value)) && current[start] == value[start] {
		start++
	}
	for start > 0 && start < len(current) && !utf8.RuneStart(current[start]) {
		start--
	}
	end, replacementEnd := len(current), len(value)
	for end > start && replacementEnd > start && current[end-1] == value[replacementEnd-1] {
		end--
		replacementEnd--
	}
	for end < len(current) && !utf8.RuneStart(current[end]) {
		end++
		replacementEnd++
	}

	selection := p.text.Selection()
	if p.text.Caret() == selection.Start {
		selection.Start, selection.End = selection.End, selection.Start
	}
	mapOffset := func(index int) int {
		offset := p.text.ByteOffset(index)
		if formattingOnly {
			offset = jsonIndentedOffset(current, value, offset)
		} else if offset >= end {
			offset += replacementEnd - end
		} else if offset > start {
			offset = replacementEnd
		}
		return utf8.RuneCountInString(value[:offset])
	}
	after := text.Range{Start: mapOffset(selection.Start), End: mapOffset(selection.End)}
	target := text.Range{Start: p.text.RuneIndex(start), End: p.text.RuneIndex(end)}
	_ = p.text.ReplaceRangesWith([]text.Replacement{{Range: target, Text: value[start:replacementEnd]}},
		text.ReplaceRangesOptions{AfterSelection: &after})
}

// json.Indent preserves the bytes of tokens, so skip inserted or removed whitespace.
func jsonIndentedOffset(source, formatted string, caret int) int {
	original, result := 0, 0
	for original < caret && result < len(formatted) {
		switch {
		case source[original] == formatted[result]:
			original++
			result++
		case strings.ContainsRune(" \t\r\n", rune(source[original])):
			original++
		case strings.ContainsRune(" \t\r\n", rune(formatted[result])):
			result++
		default:
			return result
		}
	}
	if original == len(source) {
		return len(formatted)
	}
	return result
}
