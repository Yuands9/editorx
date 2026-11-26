package editor

import (
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/vivaui/viva/app"
)

var closedTextCollectionPending atomic.Bool

const closedTextCollectionThreshold = 512 << 10

func (m *editorApp) releaseClosedTabText(tab *editorTab) {
	if m.nativeWindow == nil ||
		tab.largeText == nil && tab.source.text.RuneCount()+tab.result.text.RuneCount()+tab.comparison.text.RuneCount() < closedTextCollectionThreshold {
		return
	}
	releaseClosedTextMemory()
}

func releaseClosedTextMemory() {
	if engine, ok := app.PlatformTextEngine().(interface{ TrimLongTextLayouts() }); ok {
		engine.TrimLongTextLayouts()
	}
	if closedTextCollectionPending.CompareAndSwap(false, true) {
		// Let the closed text and its previous frame become unreachable first.
		time.AfterFunc(500*time.Millisecond, func() {
			debug.FreeOSMemory()
			closedTextCollectionPending.Store(false)
		})
	}
}
