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
	request               *ui.EffectState[jsonPreviewResult, int]
}

func (t *editorTab) updateResult() {
	left, right := t.source.text.CommittedString(), t.result.text.CommittedString()
	if t.jsonCompare || t.jsonComposing() || t.resultReady && left == t.resultSource && right == t.resultOutput {
		return
	}
	// Source-to-preview work can be deferred safely. Right-hand edits continue
	// through the existing synchronous merge path before saving the source.
	if len(left) >= backgroundTextBytes && right == t.resultOutput && !t.hasResultDraft() {
		if t.jsonPreview != nil {
			return
		}
		job := &jsonPreviewJob{source: left, right: right, action: t.jsonAction, indent: t.jsonIndent}
		job.request = ui.NewEffect[jsonPreviewResult, int](func(ctx context.Context) (jsonPreviewResult, error) {
			timer := time.NewTimer(150 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return jsonPreviewResult{}, ctx.Err()
			case <-timer.C:
			}
			output, kind, err := transformJSON(job.source, job.action, job.indent)
			return jsonPreviewResult{output: output, kind: kind, problem: err}, nil
		})
		t.jsonPreview = job
		return
	}
	t.updateResultNow()
}

func (t *editorTab) updateJSONPreview(c ui.C) {
	job := t.jsonPreview
	if job == nil {
		return
	}
	ui.Effect(c, job.request, 0)
	if result, err, done := job.request.Take(); done {
		t.jsonPreview = nil
		if err != nil || t.format() != "JSON" || t.jsonCompare || t.jsonComposing() || t.source.text.CommittedString() != job.source || t.result.text.CommittedString() != job.right || t.jsonAction != job.action || t.jsonIndent != job.indent || t.hasResultDraft() {
			return
		}
		t.resultReady, t.resultSource = true, job.source
		t.resultError, t.resultErrorOffset, t.resultErrorPane = "", -1, nil
		if result.problem != nil {
			t.resultError, t.resultErrorOffset = jsonErrorMessage(job.source, result.problem)
			t.resultErrorPane = t.source
			result.output, result.kind = job.source, "JSON"
		}
		t.setJSONResult(result.output, result.kind, false)
	}
}
