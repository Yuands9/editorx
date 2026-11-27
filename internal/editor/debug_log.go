package editor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/Yuands9/editorx/internal/version"
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
)

const editorDebugLimit = 1 << 20

// Each window owns its diagnostics. Callers pass fixed event names and errors are summarized without text.
type editorDebugLog struct {
	enabled bool
	data    []byte
}

func (log *editorDebugLog) record(event string, err error) {
	if !log.enabled {
		return
	}
	result := "ok"
	var response *githubUpdateResponseError
	switch {
	case err == nil:
	case errors.As(err, &response):
		result = fmt.Sprintf("HTTP %d", response.status)
	case errors.Is(err, errDocumentChanged):
		result = "disk-conflict"
	case errors.Is(err, os.ErrNotExist):
		result = "not-found"
	case errors.Is(err, os.ErrPermission):
		result = "permission-denied"
	case errors.Is(err, context.DeadlineExceeded):
		result = "timeout"
	case errors.Is(err, context.Canceled):
		result = "canceled"
	default:
		for errors.Unwrap(err) != nil {
			err = errors.Unwrap(err)
		}
		result = fmt.Sprintf("%T", err)
	}
	line := fmt.Appendf(nil, "[%s] [DEBUG] [EditorX][%s] %s\n", time.Now().Format(time.RFC3339Nano), event, result)
	if len(line) > editorDebugLimit {
		return
	}
	if log.data == nil {
		log.data = make([]byte, 0, editorDebugLimit)
	}
	if excess := len(log.data) + len(line) - editorDebugLimit; excess > 0 {
		excess = max(excess, len(log.data)/8)
		keepFrom := excess + bytes.IndexByte(log.data[excess-1:], '\n')
		log.data = log.data[:copy(log.data, log.data[keepFrom:])]
	}
	log.data = append(log.data, line...)
}

func (m *editorApp) diagnosticSettings(c ui.C) {
	log := &m.debug
	enabled := log.enabled
	ui.Column(c).FillWidth().Pad(14).Gap(10).
		Radius(12).Border(1, m.theme.Border).Background(m.theme.Background).Children(func() {
		ui.Row(c.Part("header")).FillWidth().Gap(16).Items(ui.Center).Children(func() {
			label := uiText("启用调试日志")
			ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
				ui.Text(c, label).FontSize(14).Bold()
				ui.Wrap(c, uiText("记录当前窗口的诊断事件。默认关闭，最多保留 1 MiB，关闭窗口时清除。")).
					FillWidth().FontSize(12).Muted()
			})
			if ui.Switch(c.Part("enabled"), &enabled, "").Label(label).Changed() {
				c.After(func() {
					*log = editorDebugLog{enabled: enabled}
					log.record("diagnostics.enabled", nil)
					m.appSettings.debugNotice, m.appSettings.problem = "", ""
				})
			}
		})
		ui.Row(c.Part("actions")).FillWidth().Gap(8).Items(ui.Center).Children(func() {
			ui.Text(c.Part("notice"), m.appSettings.debugNotice).Grow(1).MinWidth(0).Ellipsis().FontSize(12).Muted()
			disabled := len(log.data) == 0
			if ui.Button(c.Part("clear"), uiText("清空日志")).Icon(aiDeleteIcon).Secondary().Height(32).Radius(8).Disabled(disabled).Clicked() {
				c.After(func() {
					log.data = nil
					m.appSettings.debugNotice, m.appSettings.problem = uiText("日志已清空。"), ""
				})
			}
			if ui.Button(c.Part("copy"), uiText("复制日志")).Icon(copyIcon).Secondary().Height(32).Radius(8).Disabled(disabled).Clicked() {
				c.After(func() {
					m.appSettings.debugNotice = ""
					if err := app.SetClipboardText(m.diagnosticReport()); err != nil {
						m.appSettings.problem = fmt.Sprintf(uiText("无法复制调试日志：%v"), err)
						return
					}
					m.appSettings.debugNotice, m.appSettings.problem = uiText("已复制调试日志。"), ""
				})
			}
			if ui.Button(c.Part("download"), uiText("下载日志")).Icon(downloadIcon).Secondary().Height(32).Radius(8).Disabled(disabled).Clicked() {
				c.After(m.exportDiagnosticLog)
			}
		})
	})
}

func (m *editorApp) diagnosticReport() string {
	header := fmt.Sprintf("EditorX debug log\nVersion: %s\nRuntime: %s %s/%s\n\n", version.Number, runtime.Version(), runtime.GOOS, runtime.GOARCH)
	data := m.debug.data
	if excess := len(header) + len(data) - editorDebugLimit; excess > 0 {
		data = data[excess+bytes.IndexByte(data[excess-1:], '\n'):]
	}
	return header + string(data)
}

func (m *editorApp) exportDiagnosticLog() {
	path, err := app.SaveFile(app.SaveFileOptions{
		Title: uiText("下载日志"), SuggestedName: "EditorX-debug.log", DefaultExtension: "log",
		Filters: []app.FileFilter{{Name: uiText("日志"), Pattern: "*.log"}},
	})
	if errors.Is(err, app.ErrCanceled) {
		return
	}
	if err == nil {
		err = writeFile(path, []byte(m.diagnosticReport()))
	}
	m.appSettings.debugNotice = ""
	if err != nil {
		m.appSettings.problem = fmt.Sprintf(uiText("无法导出调试日志：%v"), err)
		return
	}
	m.appSettings.debugNotice, m.appSettings.problem = uiText("已保存调试日志。"), ""
}
