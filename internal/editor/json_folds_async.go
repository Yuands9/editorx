package editor

import (
	"context"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
)

type jsonFoldJob struct {
	source  string
	request *ui.EffectState[[]components.CodeEditorFoldRange, int]
}

func (p *editorPane) updateFoldJob(c ui.C) {
	value := p.text.String()
	if len(value) < backgroundTextBytes {
		p.foldJob = nil
		return
	}
	if p.foldSource != value {
		p.folds = nil
		p.foldDisplaySource = ""
	}
	if p.foldJob == nil && p.foldSource != value {
		job := &jsonFoldJob{source: value}
		job.request = ui.NewEffect[[]components.CodeEditorFoldRange, int](func(ctx context.Context) ([]components.CodeEditorFoldRange, error) {
			timer := time.NewTimer(150 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-timer.C:
			}
			return jsonFolds(job.source), nil
		})
		p.foldJob = job
	}
	if job := p.foldJob; job != nil {
		ui.Effect(c, job.request, 0)
		if folds, err, done := job.request.Take(); done {
			p.foldJob = nil
			if err == nil && value == job.source {
				p.foldSource, p.folds = value, folds
				clear(p.collapsed)
			}
		}
	}
}
