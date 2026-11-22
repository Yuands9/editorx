package windows

import (
	"runtime"
	"testing"
	"unsafe"
)

func liveWebHandlers() int {
	handlerMu.Lock()
	defer handlerMu.Unlock()
	return len(handlerLive)
}

func TestWebHandlerReferenceLifetime(t *testing.T) {
	before := liveWebHandlers()
	calls := 0
	handler := newHandler(func(first, second uintptr) uintptr { calls++; return first + second })
	unknown := guid{}
	var queried uintptr
	if status := comCall(handler, 0, uintptr(unsafe.Pointer(&unknown)), uintptr(unsafe.Pointer(&queried))); status != 0 || queried != handler {
		t.Fatalf("QueryInterface status=%x handler=%x", status, queried)
	}
	if remaining := comCall(queried, 2); remaining != 1 {
		t.Fatalf("QueryInterface reference count=%d", remaining)
	}
	agile := guid{Data1: 0x94ea2b94}
	if status := comCall(handler, 0, uintptr(unsafe.Pointer(&agile)), uintptr(unsafe.Pointer(&queried))); status != hresultNoInterface || queried != 0 {
		t.Fatal("unsupported interface retained a reference")
	}
	// The runtime retains the callback before an asynchronous create call returns.
	if remaining := comCall(handler, 1); remaining != 2 {
		t.Fatalf("AddRef=%d", remaining)
	}
	if remaining := comCall(handler, 2); remaining != 1 {
		t.Fatalf("caller Release=%d", remaining)
	}
	runtime.GC()
	if value := comCall(handler, 3, 4, 7); value != 11 || calls != 1 {
		t.Fatal("asynchronous callback was lost")
	}
	if remaining := comCall(handler, 2); remaining != 0 {
		t.Fatalf("runtime Release=%d", remaining)
	}
	if actual := liveWebHandlers(); actual != before {
		t.Fatalf("handlers=%d want %d", actual, before)
	}
}

func TestWebHandlerRepeatedRelease(t *testing.T) {
	before := liveWebHandlers()
	for range 1000 {
		handler := newHandler(func(_, _ uintptr) uintptr { return 0 })
		// A failed creation/registration never obtains a runtime reference.
		comCall(handler, 2)
	}
	runtime.GC()
	if actual := liveWebHandlers(); actual != before {
		t.Fatalf("failed registrations retained %d handlers", actual-before)
	}
}
