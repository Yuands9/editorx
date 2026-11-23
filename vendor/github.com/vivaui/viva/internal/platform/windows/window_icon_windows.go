// Modified for EditorX. See patches/ in the project root.
package windows

import (
	"errors"

	"github.com/vivaui/viva/internal/platform"
)

const (
	wmSetIcon = 0x0080
	iconSmall = 0
	iconBig   = 1

	smIconWidth = 11
)

var sendMessage = user.NewProc("SendMessageW")

// SetIcon queues an icon update on the event-loop thread.
func (window *nativeWindow) SetIcon(icon platform.WindowIcon) error {
	if window == nil || window.closed.Load() {
		return errors.New("windows: window is closed")
	}
	state := window.state
	state.application.Post(func() {
		if !window.closed.Load() && state.window != 0 {
			state.setIcons(icon)
		}
	})
	return nil
}

// setIcons sets the small title bar icon and the larger taskbar and Alt+Tab icon.
func (state *windowState) setIcons(icon platform.WindowIcon) {
	if !icon.Valid() {
		return
	}
	for kind, metric := range [...]uintptr{smSmallIconWidth, smIconWidth} {
		size, _, _ := getSystemMetrics.Call(metric)
		if kind == iconBig {
			// The shell scales a big icon down for the dpi it draws at, never up.
			size *= 2
		}
		size = max(size, 16)
		// Windows draws both of these small: the title bar's icon and the
		// taskbar button. Crop the canvas margin away so the mark fills them.
		scaled := icon.TileMark(int(size))
		handle, err := newIconHandle(platform.StatusIcon{Size: scaled.Size, RGBA: scaled.RGBA})
		if err != nil {
			continue
		}
		previous := state.icons[kind]
		sendMessage.Call(state.window, wmSetIcon, uintptr(kind), handle)
		state.icons[kind] = handle
		if previous != 0 {
			destroyIcon.Call(previous)
		}
	}
}

// destroyIcons frees the icons setIcons made, once the window no longer shows them.
func (state *windowState) destroyIcons() {
	for index, handle := range state.icons {
		if handle != 0 {
			destroyIcon.Call(handle)
			state.icons[index] = 0
		}
	}
}
