package editor

import (
	"math"

	"github.com/vivaui/viva/gfx"
)

type scrollSync struct {
	enabled, ready bool
	kind           string
	left, right    float32
	source, result string
}

// 在两侧完成布局后同步，覆盖滚轮、滚动条、键盘和查找定位。
func (t *editorTab) syncViews(canvas *gfx.Canvas) {
	state := &t.scroll
	kind := t.format()
	if !state.enabled || t.showingDocumentTranslation() || kind != "JSON" && (kind != "Markdown" || t.markdownMode != "分屏") {
		state.ready = false
		return
	}
	left := &t.source.view.Code
	leftY, leftMax := left.ScrollY(), left.MaxScrollY()
	rightY, rightMax := t.markdown.Offset, max(0, t.markdown.Height()-t.previewHeight)
	pane := t.result
	comparing := kind == "JSON" && t.jsonCompare
	if comparing {
		pane = t.comparison
	}
	if kind == "JSON" {
		if !comparing && t.resultError != "" {
			state.ready = false
			return
		}
		rightY, rightMax = pane.view.Code.ScrollY(), pane.view.Code.MaxScrollY()
	}
	mode := kind
	if comparing {
		mode += ":compare"
	}
	source, result := t.source.text.String(), pane.text.String()
	reset := !state.ready || state.kind != mode || state.source != source || state.result != result
	leftChanged := math.Abs(float64(leftY-state.left)) > 0.5
	rightChanged := math.Abs(float64(rightY-state.right)) > 0.5
	state.ready, state.kind, state.source, state.result = true, mode, source, result
	if reset {
		// 内容变化导致重新布局时，以光标所在的 JSON 编辑区为基准。
		if kind == "JSON" && pane.focused {
			leftChanged, rightChanged = false, rightMax > 0
		} else {
			leftChanged, rightChanged = leftMax > 0, false
		}
	}
	changed := false
	if leftChanged && leftMax > 0 {
		desired := leftY / leftMax * rightMax
		if kind == "Markdown" && leftY > 0.5 && leftY < leftMax-0.5 {
			if offset, ok := left.OffsetAtY(leftY); ok {
				if y, ok := t.markdown.View.YAtSource(source, offset); ok {
					desired = y
				}
			}
		}
		desired = max(0, min(desired, rightMax))
		if math.Abs(float64(desired-rightY)) > 0.5 {
			if kind == "Markdown" {
				t.markdown.Offset = desired
			} else {
				pane.view.Code.SetScrollY(desired)
			}
			rightY, changed = desired, true
		}
	} else if rightChanged && rightMax > 0 {
		desired := rightY / rightMax * leftMax
		if kind == "Markdown" && rightY > 0.5 && rightY < rightMax-0.5 {
			if offset, ok := t.markdown.View.SourceAtY(source, rightY); ok {
				if y, ok := left.YAtOffset(offset); ok {
					desired = y
				}
			}
		}
		desired = max(0, min(desired, leftMax))
		if math.Abs(float64(desired-leftY)) > 0.5 {
			left.SetScrollY(desired)
			leftY, changed = desired, true
		}
	}
	state.left, state.right = leftY, rightY
	if changed {
		canvas.RequestFrame()
	}
}
