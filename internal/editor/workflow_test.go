package editor

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"github.com/vivaui/viva/window"
)

func workflowCheck(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func workflowScreenshot(t *testing.T, s *headless.Session, name string) {
	t.Helper()
	if dir := os.Getenv("VIVA_PREVIEW_DIR"); dir != "" {
		workflowCheck(t, os.MkdirAll(dir, 0700))
		workflowCheck(t, s.Screenshot(filepath.Join(dir, name+".png")))
	}
}

func TestWorkflowTextCompare(t *testing.T) {
	m := newEditorApp()
	left := m.addTab(document{saved: "first\nold\nlast", eol: "LF"}, "old.txt")
	right := m.addTab(document{saved: "first\nnew\nlast", eol: "LF"}, "current.txt")
	right.source.text.MoveTo(right.source.text.RuneCount(), false)
	right.source.text.Insert("!")
	s := openTestSession(t, m)
	m.openTextComparePicker()
	workflowCheck(t, s.Frame())
	if m.comparePicker.sources[0].tab != left || m.comparePicker.sources[1].tab != right {
		t.Fatal("tab defaults")
	}
	workflowScreenshot(t, s, "text-compare-picker")
	workflowCheck(t, s.ClickLabel("button:开始对比"))
	workflowCheck(t, s.WaitFor(func(app.Snapshot) bool { return m.comparePicker == nil }, 8*time.Second))
	state := m.historyCompare
	if state == nil || !state.generic || state.detached() || !state.canEditCurrent() {
		t.Fatal("comparison missing")
	}
	page := ui.NewPage(state.pageConfig())
	comparison, err := headless.Open(ui.Configure(page, app.Config{Width: 1240, Height: 780, Invalidator: &state.invalidator, OnShortcut: state.shortcut}), nil, headless.Options{Scale: 1})
	workflowCheck(t, err)
	defer comparison.Close()
	workflowScreenshot(t, comparison, "general-text-compare")
	before := right.source.text.String()
	state.block = 0
	m.mergeHistoryChange(state, false)
	if right.source.text.String() == before {
		t.Fatal("merge not applied", state.problem, state.notice)
	}
	_, err = comparison.Driver().ShortcutChord(gfx.ModifierControl, 'z')
	workflowCheck(t, err)
	if right.source.text.String() != before {
		t.Fatal("undo lost current buffer")
	}
	workflowCheck(t, comparison.ClickLabel("textfield:当前版本编辑区"))
	workflowCheck(t, comparison.Type("edit"))
	if !strings.Contains(right.source.text.String(), "edit") {
		t.Fatal("right editing did not reach main tab")
	}
	m.removeTab(right)
	if !state.detached() {
		t.Fatal("closed tab still attached")
	}
}

func TestWorkflowExternalChangesAndFollow(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "trace.log")
	workflowCheck(t, os.WriteFile(path, []byte("first\n"), 0600))
	m := newEditorApp()
	m.watchFiles = true
	m.load(path)
	tab := m.active
	s := openTestSession(t, m)
	defer m.closeFileWatches()
	wait := func(condition func() bool) {
		t.Helper()
		m.invalidator.Invalidate()
		workflowCheck(t, s.WaitFor(func(app.Snapshot) bool { return condition() }, 8*time.Second))
	}
	wait(func() bool { return tab.diskChange != nil && tab.diskChange.request == nil && !tab.diskChange.pending })
	if tab.diskChange.changed {
		t.Fatal("unchanged file reported")
	}
	tab.source.text.Insert("draft ")
	workflowCheck(t, os.WriteFile(path, []byte("external\n"), 0600))
	wait(func() bool { return tab.diskChange.changed })
	if tab.source.text.String() != "draft first\n" {
		t.Fatal("external edit overwrote draft")
	}
	workflowScreenshot(t, s, "external-change")
	workflowCheck(t, s.ClickLabel("button:重新读取"))
	if !tab.diskChange.confirmReload || !strings.Contains(tab.source.text.String(), "draft") {
		t.Fatal("dirty reload skipped confirmation")
	}
	workflowCheck(t, s.ClickLabel("button:确认重新读取"))
	wait(func() bool { return tab.diskChange.request == nil && tab.source.text.String() == "external\n" })
	m.toggleLogFollow(tab)
	wait(func() bool {
		return tab.largeText != nil && tab.source.text.String() == "external\n" && tab.diskChange.request == nil
	})
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	workflowCheck(t, err)
	_, err = file.WriteString("appended 中文\n")
	workflowCheck(t, err)
	workflowCheck(t, file.Close())
	wait(func() bool { return strings.Contains(tab.source.text.String(), "appended 中文") })
	workflowCheck(t, os.WriteFile(path, []byte("rotated\n"), 0600))
	wait(func() bool { return tab.source.text.String() == "rotated\n" })
	workflowCheck(t, os.Remove(path))
	wait(func() bool { return tab.diskChange.problem != "" })
	workflowCheck(t, os.WriteFile(path, []byte("recreated\n"), 0600))
	wait(func() bool { return tab.source.text.String() == "recreated\n" })
	workflowScreenshot(t, s, "log-follow")
	if tab.largeText.indexing != nil || tab.largeText.lines.file != nil {
		t.Fatal("following triggered whole-file indexing")
	}
	if len(tab.source.text.String()) > largeTextPageBytes+8192 {
		t.Fatal("follow kept whole log")
	}
	m.toggleLogFollow(tab)
	workflowCheck(t, os.WriteFile(path, []byte("paused\n"), 0600))
	wait(func() bool { return tab.diskChange.changed })
	if tab.source.text.String() != "recreated\n" {
		t.Fatal("paused log still followed")
	}
	m.removeTab(tab)
	workflowCheck(t, s.Frame())
	if len(m.fileWatches) != 0 {
		t.Fatal("closed tab retained watch")
	}
}

func TestWorkflowMacroRecordAndReplay(t *testing.T) {
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	tab := m.addTab(document{saved: "short\nlonger line\n中文"}, "macro.txt")
	s := openTestSession(t, m)
	m.startMacroRecording()
	workflowCheck(t, s.Frame())
	workflowCheck(t, s.Type("> "))
	workflowCheck(t, s.Driver().Key(gfx.KeyDown, 0))
	workflowCheck(t, s.Driver().Key(gfx.KeyHome, 0))
	if m.macro == nil {
		t.Fatal(m.status)
	}
	m.stopMacroRecording()
	if m.lastMacro == nil || len(m.lastMacro.Steps) == 0 {
		t.Fatal("recording lost", m.status)
	}
	before := tab.source.text.String()
	workflowCheck(t, replayMacro(tab.source.text, *m.lastMacro, 2))
	if tab.source.text.String() != "> short\n> longer line\n> 中文" {
		t.Fatal(tab.source.text.String(), m.lastMacro.Steps)
	}
	if !tab.source.text.Undo() || tab.source.text.String() != before {
		t.Fatal("whole replay undo failed")
	}
	m.openWorkflowDialog("宏")
	m.workflow.name.SetText("行首前缀")
	m.saveWorkflowItem(false)
	prefs, err := readWorkflowPreferences(m.dataDirectory)
	workflowCheck(t, err)
	if len(prefs.Macros) != 1 || prefs.Macros[0].Name != "行首前缀" {
		t.Fatal(prefs, m.workflow.problem)
	}
	workflowCheck(t, s.Frame())
	workflowScreenshot(t, s, "macro-manager")
	tab.source.setResult("short\nlonger line\n中文")
	m.workflow.repeat.SetText("3")
	workflowCheck(t, s.Frame())
	workflowCheck(t, s.ClickLabel("button:重放宏"))
	workflowCheck(t, s.WaitFor(func(app.Snapshot) bool { return m.workflow == nil }, 8*time.Second))
	if tab.source.text.String() != "> short\n> longer line\n> 中文" {
		t.Fatal("background macro", tab.source.text.String())
	}
	if !tab.source.text.Undo() || tab.source.text.String() != "short\nlonger line\n中文" {
		t.Fatal("background macro undo")
	}
	editor := text.NewEditor("abcd")
	editor.MoveTo(0, false)
	err = replayMacro(editor, editorMacro{Steps: []macroStep{{Kind: window.TextInsert, Text: "safe"}, {Replace: true, Start: 9999, End: 9999, Text: "bad"}}}, 1)
	if err == nil || editor.String() != "abcd" {
		t.Fatal("failed replay partially applied")
	}
	// IME candidates are omitted; only committed text appears in the recorded step.
	m.workflow = nil
	tab.source.text.MoveTo(0, false)
	m.startMacroRecording()
	r := m.macro
	_, err = r.event(window.TextEvent{Kind: window.TextSetMarked, Text: "zhong", MarkedSelection: window.TextRange{Start: 5, End: 5}})
	workflowCheck(t, err)
	_, err = r.event(window.TextEvent{Kind: window.TextInsert, Text: "中"})
	workflowCheck(t, err)
	m.stopMacroRecording()
	if m.lastMacro == nil || len(m.lastMacro.Steps) != 1 || m.lastMacro.Steps[0].Text != "中" {
		t.Fatal(m.lastMacro)
	}
}

func TestWorkflowCustomTool(t *testing.T) {
	values := toolContext{file: `C:\目录 with spaces\a.txt`, selection: `hello & $(no command) {file}`, directory: t.TempDir()}
	directory, args, err := toolArguments(customEditorTool{Arguments: []string{"{file}", "{selection}"}}, values)
	workflowCheck(t, err)
	if directory != values.directory || args[0] != values.file || args[1] != values.selection {
		t.Fatal(args)
	}
	if _, _, err = toolArguments(customEditorTool{Arguments: []string{"{file}"}}, toolContext{}); err == nil {
		t.Fatal("missing path accepted")
	}
	tool := customEditorTool{Name: "roundtrip", Executable: "powershell.exe", Arguments: []string{"-NoProfile", "-Command", "[Console]::Out.Write([Console]::In.ReadToEnd()); exit 7"}, SelectionInput: true}
	result, err := runCustomTool(context.Background(), tool, values)
	if err == nil || !strings.Contains(result.output, values.selection) {
		t.Fatal(result, err)
	}
	tool.Arguments = []string{"-NoProfile", "-Command", "[Console]::Out.Write(('x' * 1100000))"}
	result, err = runCustomTool(context.Background(), tool, values)
	workflowCheck(t, err)
	if !result.truncated || len(result.output) != toolOutputLimit {
		t.Fatal("output unbounded", len(result.output))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	tool.Arguments = []string{"-NoProfile", "-Command", "Start-Sleep -Seconds 60"}
	_, err = runCustomTool(ctx, tool, values)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("cancel", err)
	}
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	m.openWorkflowDialog("自定义工具")
	m.workflow.name.SetText("统计")
	m.workflow.executable.SetText("powershell.exe")
	m.workflow.arguments.SetText("-NoProfile\n-Command\n[Console]::In.ReadToEnd().Length")
	m.workflow.selectionInput = true
	m.saveWorkflowItem(false)
	s := openTestSession(t, m)
	workflowScreenshot(t, s, "custom-tools")
	workflowCheck(t, s.Resize(900, 560))
	workflowScreenshot(t, s, "custom-tools-small")
	m.workflow.output.SetText("output\nline2\nline3")
	workflowCheck(t, s.Frame())
	workflowScreenshot(t, s, "custom-tools-output-small")
	if _, err = s.Locate("button:保存自定义工具"); err != nil {
		t.Fatal(err)
	}
}

func TestWorkflowEncodingRoundTrips(t *testing.T) {
	// Known byte sequences include GB18030's four-byte supplementary character.
	for _, test := range []struct {
		enc   textEncoding
		bytes []byte
		bom   bool
	}{
		{gb18030Encoding, []byte{0xd6, 0xd0, 0xce, 0xc4, 0x94, 0x39, 0xfc, 0x36, 0x0d, 0x0a}, false},
		{utf16LEEncoding, []byte{0xff, 0xfe, 0x2d, 0x4e, 0x87, 0x65, 0x3d, 0xd8, 0x00, 0xde, 0x0d, 0x00, 0x0a, 0x00}, true},
		{utf16BEEncoding, []byte{0xfe, 0xff, 0x4e, 0x2d, 0x65, 0x87, 0xd8, 0x3d, 0xde, 0x00, 0x00, 0x0d, 0x00, 0x0a}, true},
	} {
		t.Run(string(test.enc), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "encoding.txt")
			original := []byte("中文😀\r\n")
			workflowCheck(t, os.WriteFile(path, original, 0600))
			m := newEditorApp()
			m.load(path)
			tab := m.active
			m.convertTextEncoding(tab, test.enc, test.bom)
			if !m.saveTextTab(tab, false) {
				t.Fatal(m.errorText)
			}
			data, err := os.ReadFile(path)
			workflowCheck(t, err)
			if !bytes.Equal(data, test.bytes) {
				t.Fatalf("encoded %x want %x", data, test.bytes)
			}
			doc, err := readDocumentAs(path, test.enc)
			workflowCheck(t, err)
			if doc.saved != "中文😀\n" || doc.eol != "CRLF" {
				t.Fatal(doc)
			}
			m.convertTextEncoding(tab, utf8Encoding, false)
			if !m.saveTextTab(tab, false) {
				t.Fatal(m.errorText)
			}
			data, err = os.ReadFile(path)
			workflowCheck(t, err)
			if !bytes.Equal(data, original) {
				t.Fatal("UTF-8 roundtrip lost bytes")
			}
		})
	}
}

func TestWorkflowMacroCommands(t *testing.T) {
	editor := text.NewEditor("alpha\nbeta")
	editor.MoveTo(0, false)
	macro := editorMacro{Steps: []macroStep{{Code: components.CodeEditorDuplicateLinesDown}}}
	workflowCheck(t, replayMacro(editor, macro, 1))
	if editor.String() != "alpha\nalpha\nbeta" {
		t.Fatal(editor.String())
	}
}
