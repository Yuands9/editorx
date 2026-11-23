package windows

import (
	"path/filepath"
	"unsafe"

	"github.com/vivaui/viva/internal/platform"
)

const webFileDropMessage = "__viva_file_drop__"

// WebView2 is a child window with its own drop target. Only forward files from a native
// drop on its main frame, leaving text selection and internal dragging to the page.
const webFileDropScript = `(() => {
	if (window !== window.top || typeof chrome.webview.postMessageWithAdditionalObjects !== 'function') return;
	const postFiles = chrome.webview.postMessageWithAdditionalObjects.bind(chrome.webview);
	const isFileDrag = event => event.isTrusted && event.dataTransfer?.types.includes('Files');
	window.addEventListener('dragover', event => {
		if (!isFileDrag(event)) return;
		event.preventDefault();
		event.dataTransfer.dropEffect = 'copy';
	}, true);
	window.addEventListener('drop', event => {
		if (!isFileDrag(event)) return;
		event.preventDefault();
		event.stopImmediatePropagation();
		postFiles("` + webFileDropMessage + `", Array.from(event.dataTransfer.files));
	}, true);
})();`

// AdditionalObjects and ICoreWebView2File were added in WebView2 SDK 1.0.1774.30.
var iidWebMessageArgs2 = guid{0x06fc7ab7, 0xc90c, 0x4297, [8]byte{0x93, 0x89, 0x33, 0xca, 0x01, 0xcf, 0x6d, 0x5e}}
var iidWebFile = guid{0xf2c19559, 0x6bc1, 0x4583, [8]byte{0xa7, 0x57, 0x90, 0x02, 0x1b, 0xe9, 0xaf, 0xec}}

const (
	messageGetAdditionalObjects = 6
	webObjectsGetCount          = 3
	webObjectsGetValueAtIndex   = 4
	webFileGetPath              = 3
)

func (page *windowsWeb) receiveFileDrop(args uintptr) {
	if page.closed || !page.shown {
		return
	}
	var args2 uintptr
	if comCall(args, 0, uintptr(unsafe.Pointer(&iidWebMessageArgs2)), uintptr(unsafe.Pointer(&args2))) != 0 || args2 == 0 {
		return
	}
	defer comCall(args2, 2)
	var objects uintptr
	if comCall(args2, messageGetAdditionalObjects, uintptr(unsafe.Pointer(&objects))) != 0 || objects == 0 {
		return
	}
	defer comCall(objects, 2)
	var count uint32
	if comCall(objects, webObjectsGetCount, uintptr(unsafe.Pointer(&count))) != 0 {
		return
	}
	count = min(count, maxDroppedFile)
	paths := make([]string, 0, count)
	for index := uint32(0); index < count; index++ {
		var object uintptr
		if comCall(objects, webObjectsGetValueAtIndex, uintptr(index), uintptr(unsafe.Pointer(&object))) != 0 || object == 0 {
			continue
		}
		path := webDroppedPath(object)
		comCall(object, 2)
		if filepath.IsAbs(path) {
			paths = append(paths, path)
		}
	}
	if len(paths) == 0 {
		return
	}

	state := page.host.state
	var position point
	getCursorPos.Call(uintptr(unsafe.Pointer(&position)))
	screenToClient.Call(state.window, uintptr(unsafe.Pointer(&position)))
	state.reportFileDrop(platform.FileDropEvent{
		Phase: platform.FileDropDrop,
		X:     float32(position.X) / state.scale, Y: float32(position.Y) / state.scale,
		Paths: paths,
	})
}

// Read a path through the runtime's File object. Message text never supplies a path.
func webDroppedPath(object uintptr) string {
	var file uintptr
	if comCall(object, 0, uintptr(unsafe.Pointer(&iidWebFile)), uintptr(unsafe.Pointer(&file))) != 0 || file == 0 {
		return ""
	}
	defer comCall(file, 2)
	var path uintptr
	if comCall(file, webFileGetPath, uintptr(unsafe.Pointer(&path))) != 0 {
		return ""
	}
	return wideString(path)
}
