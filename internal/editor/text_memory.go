package editor

import (
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/vivaui/viva/app"
)

var closedTextCollectionPending atomic.Bool

func (m *editorApp) releaseClosedTabText(tab *editorTab) {
	const largeTabRunes = 512 << 10
	if m.nativeWindow == nil ||
		tab.largeText == nil && tab.source.text.RuneCount()+tab.result.text.RuneCount()+tab.comparison.text.RuneCount() < largeTabRunes {
		return
	}
	if engine, ok := app.PlatformTextEngine().(interface{ TrimLongTextLayouts() }); ok {
		engine.TrimLongTextLayouts()
	}
	if closedTextCollectionPending.CompareAndSwap(false, true) {
		// Let the closed tab and the previous frame become unreachable first.
		time.AfterFunc(500*time.Millisecond, func() {
			debug.FreeOSMemory()
			closedTextCollectionPending.Store(false)
		})
	}
}
