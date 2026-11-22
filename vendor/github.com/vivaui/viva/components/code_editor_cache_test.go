package components

import (
	"github.com/vivaui/viva/gfx"
	"testing"
)

func TestBracketCacheFollowsTextAndCaret(t *testing.T) {
	var editor CodeEditor
	var canvas gfx.Canvas
	for _, sample := range []struct {
		value string
		caret int
		pair  [2]int
		found bool
	}{
		{"[1]", 0, [2]int{0, 2}, true},
		{"[1]", 0, [2]int{0, 2}, true},
		{"[中文]", 0, [2]int{0, 7}, true},
		{"[中文", 0, [2]int{}, false},
		{"[中文", 0, [2]int{}, false},
		{"[{}]", 2, [2]int{1, 2}, true},
		{"[{}]", 4, [2]int{0, 3}, true},
		{"abc", 1, [2]int{}, false},
	} {
		canvas.Reset()
		editor.Paint(&canvas, gfx.DarkTheme(), gfx.Rect{Width: 600, Height: 400}, CodeEditorSnapshot{Value: sample.value, Caret: sample.caret, MatchBrackets: true})
		if editor.bracketFound != sample.found || editor.bracketPair != sample.pair {
			t.Fatalf("%q caret %d: %v %v", sample.value, sample.caret, editor.bracketPair, editor.bracketFound)
		}
	}
}
