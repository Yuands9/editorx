package editor

import (
	"context"
	"time"

	"github.com/vivaui/viva/ui"
)

type jsonPreviewResult struct {
	output, kind string
	problem      error
}
type jsonPreviewJob struct {
	source, right, action string
	indent                int
	fromRight             bool
	request               *ui.EffectState[jsonPreviewResult, int]
}

func (t *editorTab) updateResult() {
	left, right := t.source.text.CommittedString(), t.result.text.CommittedString()
	if t.jsonCompare || t.jsonComposing() || t.resultReady && left == t.resultSource && right == t.resultOutput {
		return
	}
	if max(len(left), len(right)) >= backgroundTextBytes && !t.hasResultDraft() {
		fromRight := t.jsonLiveResult() && right != t.resultOutput && (left == t.resultSource || t.result.focused)
		if fromRight {
			// Copy the actual edit before save/session/close can observe the source.
			// Parsing and formatting are deferred; incomplete JSON stays editable.
			value := right
			if left == t.resultSource && (t.jsonAction == "格式化" || t.jsonAction == "压缩") {
				if mapped, ok := jsonPreviewEdit(left, t.resultOutput, right); ok {
					value = mapped
				}
			}
			t.source.syncJSONText(value, false)
			left = t.source.text.CommittedString()
			t.resultSource, t.resultOutput, t.resultReady = left, right, true
		}
		if t.jsonPreview != nil {
			t.jsonPreview.fromRight = t.jsonPreview.fromRight || fromRight
			return
		}
		// A superseded job is replaced after Take, so at most one transform runs
		// for this tab. The current snapshot is checked again before applying it.
		t.startJSONPreview(left, right, fromRight)
		return
	}
	t.updateResultNow()
}

func (t *editorTab) startJSONPreview(left, right string, fromRight bool) {
	job := &jsonPreviewJob{source: left, right: right, action: t.jsonAction, indent: t.jsonIndent, fromRight: fromRight}
	job.request = ui.NewEffect[jsonPreviewResult, int](func(ctx context.Context) (jsonPreviewResult, error) {
		timer := time.NewTimer(150 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return jsonPreviewResult{}, ctx.Err()
		case <-timer.C:
		}
		input := job.source
		if fromRight {
			input = job.right
		}
		output, kind, err := transformJSON(input, job.action, job.indent)
		return jsonPreviewResult{output: output, kind: kind, problem: err}, nil
	})
	t.jsonPreview = job
}

func (t *editorTab) updateJSONPreview(c ui.C) {
	job := t.jsonPreview
	if job == nil {
		return
	}
	ui.Effect(c, job.request, 0)
	if result, err, done := job.request.Take(); done {
		t.jsonPreview = nil
		if err != nil || t.format() != "JSON" || t.jsonCompare || t.hasResultDraft() {
			return
		}
		if t.jsonComposing() {
			t.resultReady = false
			return
		}
		if t.source.text.CommittedString() != job.source || t.result.text.CommittedString() != job.right || t.jsonAction != job.action || t.jsonIndent != job.indent {
			// Keep rendering the replacement effect in this frame. No extra input
			// event should be necessary to refresh the final keystroke.
			left, right := t.source.text.CommittedString(), t.result.text.CommittedString()
			t.startJSONPreview(left, right, job.fromRight && left == t.resultSource)
			ui.Effect(c, t.jsonPreview.request, 0)
			return
		}
		t.resultReady, t.resultSource = true, job.source
		t.resultError, t.resultErrorOffset, t.resultErrorPane = "", -1, nil
		if result.problem != nil {
			input := job.source
			if job.fromRight {
				input = job.right
			}
			t.resultError, t.resultErrorOffset = jsonErrorMessage(input, result.problem)
			t.resultErrorPane = t.source
			result.output, result.kind = input, "JSON"
		}
		if job.fromRight {
			t.resultErrorPane = t.result
			if t.result.text.CanRedo() {
				result.output = job.right
			}
		}
		t.setJSONResult(result.output, result.kind, job.fromRight && job.action == "格式化" && result.problem == nil)
	}
}
