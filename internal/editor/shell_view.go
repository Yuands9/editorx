package editor

import (
	"bytes"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

//go:embed assets/terminal
var terminalAssets embed.FS

type localShell struct {
	directory, issue string
	view             *app.WebView
	process          *terminalProcess
	starting, exited bool
	viewReady        atomic.Bool
	size             terminalSize
	wantFocus        bool
	mu               sync.Mutex
	pending          bytes.Buffer
}

const shellOutputLimit = 512 << 10

func (shell *localShell) appendOutput(data []byte) {
	shell.mu.Lock()
	defer shell.mu.Unlock()
	if excess := shell.pending.Len() + len(data) - shellOutputLimit; excess > 0 {
		shell.pending.Next(min(excess, shell.pending.Len()))
	}
	shell.pending.Write(data)
}

func (shell *localShell) flushOutput() {
	if !shell.viewReady.Load() || shell.view == nil {
		return
	}
	shell.mu.Lock()
	if shell.pending.Len() == 0 {
		shell.mu.Unlock()
		return
	}
	data := base64.StdEncoding.EncodeToString(shell.pending.Bytes())
	shell.pending.Reset()
	shell.mu.Unlock()
	shell.view.Eval(`window.vivaTerminal?.write("` + data + `")`)
}

func (m *editorApp) openLocalShell(directory string) {
	m.tools.active = "终端"
	if directory == "" {
		directory = m.toolDirectory()
	}
	if directory == "" && m.dataDirectory != "" {
		directory = filepath.Dir(m.dataDirectory)
	}
	for _, shell := range m.tools.terminals {
		if strings.EqualFold(shell.directory, directory) {
			m.tools.terminal = shell
			shell.wantFocus = true
			return
		}
	}
	shell := &localShell{directory: directory, wantFocus: true}
	m.tools.terminals = append(m.tools.terminals, shell)
	m.tools.terminal = shell
	if m.webReady {
		m.mountShell(shell)
	}
	m.startShellProcess(shell)
}

func (m *editorApp) closeLocalShell(shell *localShell) {
	if shell == nil {
		return
	}
	index := slices.Index(m.tools.terminals, shell)
	if index < 0 {
		return
	}
	m.tools.terminals = slices.Delete(m.tools.terminals, index, index+1)
	if m.tools.terminal == shell {
		m.tools.terminal = nil
		if len(m.tools.terminals) > 0 {
			m.tools.terminal = m.tools.terminals[min(index, len(m.tools.terminals)-1)]
			m.tools.terminal.wantFocus = true
		}
	}
	if shell.process != nil {
		shell.process.close()
		shell.process = nil
	}
	if shell.view != nil {
		shell.view.Close()
		shell.view = nil
	}
}

func (m *editorApp) mountShell(shell *localShell) {
	if !m.webReady || shell.view != nil || shell.issue != "" || !slices.Contains(m.tools.terminals, shell) {
		return
	}
	const location = "terminal://local/index.html"
	view, err := m.webHost.Open(app.WebViewConfig{
		URL: location, Ephemeral: true, BridgeMainFrameOnly: true,
		Scheme:   &app.WebScheme{Name: "terminal", Serve: serveTerminalAsset},
		Navigate: func(nav app.WebNavigation) bool { return nav.URL == location },
		LetterShortcut: func(letter rune, modifiers gfx.Modifiers) func() {
			if letter == 't' && modifiers == gfx.ModifierControl|gfx.ModifierAlt {
				return func() { m.useTool("终端"); m.invalidator.Invalidate() }
			}
			return m.openLinkWebShortcut(letter, modifiers)
		},
		OnMessage: func(raw string) {
			m.handleShellMessage(shell, raw)
		},
	})
	if err != nil {
		shell.issue = "无法打开本地终端界面：" + err.Error()
		return
	}
	shell.view = view
}

func serveTerminalAsset(path string) (string, []byte, bool) {
	var mime string
	switch path {
	case "index.html":
		mime = "text/html; charset=utf-8"
	case "xterm.css":
		mime = "text/css; charset=utf-8"
	case "terminal.js", "xterm.js", "addon-fit.js":
		mime = "text/javascript; charset=utf-8"
	default:
		return "", nil, false
	}
	data, err := terminalAssets.ReadFile("assets/terminal/" + path)
	return mime, data, err == nil
}

func (m *editorApp) startShellProcess(shell *localShell) {
	if m.post == nil || shell.starting || shell.process != nil || shell.exited {
		return
	}
	shell.starting = true
	initialSize := terminalSize{cols: 100, rows: 25}
	go func() {
		process, err := startTerminalProcess(shell.directory, initialSize)
		m.post(func() {
			shell.starting = false
			if !slices.Contains(m.tools.terminals, shell) {
				if process != nil {
					process.close()
				}
				return
			}
			if err != nil {
				shell.issue = err.Error()
			} else {
				shell.process = process
				if shell.size != initialSize && shell.size.cols > 0 && shell.size.rows > 0 {
					process.enqueue(terminalInput{size: shell.size})
				}
			}
			m.invalidator.Invalidate()
		})
		if err != nil {
			return
		}
		process.run(func(data []byte) {
			shell.appendOutput(data)
			if shell.viewReady.Load() {
				m.invalidator.Invalidate()
			}
		}, func() {
			m.post(func() {
				if shell.process == process {
					shell.exited, shell.process = true, nil
					shell.appendOutput([]byte("\r\n[PowerShell 已退出]\r\n"))
					m.invalidator.Invalidate()
				}
			})
		})
	}()
}

func (m *editorApp) handleShellMessage(shell *localShell, raw string) {
	if len(raw) > 256<<10 || !slices.Contains(m.tools.terminals, shell) {
		return
	}
	var message struct {
		Kind, Text string
		Cols, Rows int
	}
	if json.Unmarshal([]byte(raw), &message) != nil {
		return
	}
	switch message.Kind {
	case "ready", "resize":
		if message.Kind == "ready" {
			shell.viewReady.Store(true)
			shell.view.Eval(previewThemeJS(m.light))
			shell.flushOutput()
			m.invalidator.Invalidate()
		}
		size := terminalSize{cols: message.Cols, rows: message.Rows}
		if size.cols > 0 && size.rows > 0 && size != shell.size {
			shell.size = size
			if shell.process != nil {
				shell.process.enqueue(terminalInput{size: size})
			}
		}
	case "input":
		if shell.process != nil && len(message.Text) <= 64<<10 {
			if !shell.process.enqueue(terminalInput{text: message.Text}) {
				shell.appendOutput([]byte("\r\n[终端输入缓冲已满，请稍后再试]\r\n"))
				m.invalidator.Invalidate()
			}
		}
	case "copy":
		if message.Text != "" {
			_ = app.SetClipboardText(message.Text)
		}
	case "paste":
		if value, err := app.ClipboardText(); err == nil && len(value) <= 64<<10 && shell.view != nil {
			data, _ := json.Marshal(value)
			shell.view.Eval("window.vivaTerminal?.paste(" + string(data) + ")")
		}
	}
}

func (m *editorApp) updateLocalShell() {
	shell := m.tools.terminal
	if shell == nil || m.tools.active != "终端" {
		return
	}
	shell.flushOutput()
}

func (m *editorApp) shellPanel(c ui.C, blocked bool) {
	shell := m.tools.terminal
	if shell == nil {
		return
	}
	if shell.issue != "" {
		ui.Wrap(c, shell.issue).Pad(10).FillWidth().Color(m.theme.Danger).FontSize(12)
		return
	}
	if shell.view == nil {
		ui.Text(c, "正在准备本地终端…").Pad(10).Muted().FontSize(12)
		return
	}
	if !blocked {
		ui.Slot(c).FillWidth().Grow(1).MinHeight(0).NativeView(shell.view.ID())
		if shell.wantFocus && shell.viewReady.Load() {
			shell.wantFocus = false
			view := shell.view
			// The native host applies the view's visibility after this render.
			m.post(func() {
				if m.tools.active == "终端" && m.tools.terminal == shell && shell.view == view && !m.appSettings.open && !m.interactionBlocked() {
					view.Focus()
				}
			})
		}
	}
}

func (m *editorApp) shellActions(c ui.C, blocked bool) {
	shell := m.tools.terminal
	if shell == nil {
		return
	}
	selected := shell.directory
	if len(m.tools.terminals) > 1 {
		paths := make([]string, 0, len(m.tools.terminals))
		for _, session := range m.tools.terminals {
			paths = append(paths, session.directory)
		}
		if ui.Select(c, &selected, paths, filepath.Base).Label("终端会话").Width(150).Height(26).Disabled(blocked).Changed() {
			c.After(func() { m.openLocalShell(selected) })
		}
	} else {
		ui.Text(c, fmt.Sprintf("PowerShell · %s", shell.directory)).Grow(1).MinWidth(0).Ellipsis().Muted().FontSize(11)
	}
	if shell.exited || shell.issue != "" {
		if ui.Button(c, "重启终端").Icon(refreshIcon).IconOnly().Ghost().Size(28, 28).Tooltip("重启终端").Disabled(blocked).Clicked() {
			c.After(func() {
				m.closeLocalShell(shell)
				m.openLocalShell(shell.directory)
			})
		}
	}
	if ui.Button(c, "结束终端").Icon(aiStopIcon).IconOnly().Ghost().Size(28, 28).Tooltip("结束终端").Disabled(blocked).Clicked() {
		c.After(func() {
			m.closeLocalShell(shell)
			if m.tools.terminal == nil {
				m.tools.active = ""
				m.focusActiveSource()
			}
		})
	}
}
