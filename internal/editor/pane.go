package editor

import (
	"github.com/vivaui/viva/app"
	"maps"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"github.com/vivaui/viva/ui/codeeditor"
	"github.com/vivaui/viva/window"
)

type editorPane struct {
	recording           *macroRecording
	foldJob             *jsonFoldJob
	columnAnchor        int
	columnDragging      bool
	keyboardColumn      bool
	columnCaret         int
	columnSource        string
	columnPrimary       int
	onFocused           func()
	text                *text.Editor
	view                *codeeditor.Editor
	input               window.TextInput
	focus               bool
	readOnly            bool
	focused             bool
	placeholder         string
	searchKey           func(gfx.KeyEvent) gfx.EventResult
	foldSource          string
	foldLanguage        string
	completion          *editorCompletion
	automaticCompletion bool
	completionTyping    completionTyping
	folds               []components.CodeEditorFoldRange
	collapsed           map[int]bool
	folded              map[int]bool
	foldScope           gfx.Scope
	configHighlight     configHighlight
	syntaxDisabled      bool
	foldDisplaySource   string
	foldDisplayLanguage menuLanguage
	light               bool
	restoreScroll       *gfx.Point
}

func newEditorPane(value string, readOnly bool) *editorPane {
	p := &editorPane{text: text.NewEditor(value), readOnly: readOnly, collapsed: map[int]bool{}}
	p.text.MoveTo(0, false)
	p.input.Editor = p.text
	p.input.CopyText = p.copySelections
	p.view = codeeditor.New(p.text)
	p.applyEditorPreferences(defaultEditorPreferences())
	p.view.Snapshot.CaretWidth = 1
	p.view.Snapshot.Borderless = true
	p.view.Snapshot.ContinuousCaretBlink = !readOnly
	p.view.Snapshot.ShowIndentGuides = true
	p.view.Snapshot.ReadOnly = readOnly
	p.view.Snapshot.ScrollbarID = gfx.Item(1)
	p.view.Snapshot.HorizontalScrollbarID = gfx.Item(2)
	p.view.Snapshot.Highlight = p.highlightLine
	if !readOnly {
		p.placeholder = "开始输入内容"
	}
	p.view.Text = func(window.TextInputContext) *window.TextInput {
		if p.recording != nil {
			p.input.Editor = nil
			p.input.State = app.TextStateOf(p.text)
		}
		return &p.input
	}
	p.view.Key = p.key
	p.view.Pointer = p.pointer
	return p
}

func (p *editorPane) key(event gfx.KeyEvent) gfx.EventResult {
	if event.Release {
		return gfx.EventResult{}
	}
	if event.Key == gfx.KeyEscape && event.Modifiers == 0 && p.text.ClearSecondarySelections() {
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	if _, composing := p.text.Composition(); composing {
		return gfx.EventResult{}
	}
	if result := p.completionKey(event); result.Handled {
		return result
	}
	if result := p.columnKey(event); result.Handled {
		return result
	}
	if p.searchKey != nil {
		if result := p.searchKey(event); result.Handled {
			return result
		}
	}
	if !p.readOnly {
		p.configureCodeLanguage()
		if command, ok := components.CodeEditorCommandForKey(event); ok && command.Apply(p.text) {
			if p.recording != nil {
				p.recording.append(macroStep{Code: command})
			}
			return gfx.EventResult{Handled: true, Repaint: true}
		}
	}
	movement, extend := components.TextareaMovementForKey(event.Key, event.Modifiers)
	if movement == components.TextareaMoveNone {
		return gfx.EventResult{}
	}
	if p.recording != nil {
		p.recording.append(macroStep{Movement: movement, Extend: extend})
	}
	to := p.view.Code.Move(p.text.String(), p.text.ByteOffset(p.text.Caret()), movement)
	if p.text.SelectionCount() > 1 {
		selections := p.text.Selections()
		for index, selection := range selections {
			caret := selection.End
			if movement == components.TextareaMoveUp {
				caret = selection.Start
			}
			offset := p.view.Code.Move(p.text.String(), p.text.ByteOffset(caret), movement)
			end := p.text.RuneIndex(offset)
			if extend {
				selections[index].End = end
			} else {
				selections[index] = text.Range{Start: end, End: end}
			}
		}
		p.text.SetSelections(selections, len(selections)-1)
	} else {
		p.text.MoveTo(p.text.RuneIndex(to), extend)
	}
	return gfx.EventResult{Handled: true, Repaint: true}
}

func (p *editorPane) pointer(event gfx.PointerEvent) gfx.EventResult {
	if result := p.multiPointer(event); result.Handled {
		return result
	}
	if event.Kind != gfx.PointerDown || event.Button != gfx.PointerButtonPrimary {
		return gfx.EventResult{}
	}
	for _, marker := range p.view.Snapshot.FoldMarkers {
		if event.Target == marker.ID {
			if p.collapsed[marker.Line] {
				delete(p.collapsed, marker.Line)
			} else {
				p.collapsed[marker.Line] = true
			}
			return gfx.EventResult{Handled: true, Repaint: true}
		}
	}
	return gfx.EventResult{}
}

func (p *editorPane) replace(value string) {
	p.text.BreakUndoGroup()
	p.text.SetSelection(text.Range{Start: 0, End: p.text.RuneCount()})
	p.text.Insert(value)
	p.text.BreakUndoGroup()
	p.text.MoveTo(0, false)
	p.collapsed = map[int]bool{}
}

func (p *editorPane) setResult(value string) {
	options, _ := p.text.Code()
	p.text = text.NewEditor(value)
	p.text.SetCode(options)
	p.text.MoveTo(0, false)
	p.view.Document = p.text
	p.input.Editor = p.text
	p.view.Code = components.CodeEditor{}
	p.collapsed = map[int]bool{}
	p.completion, p.completionTyping = nil, completionTyping{}
}
func (p *editorPane) updateFolds() {
	value := p.text.String()
	language := p.view.Snapshot.Language
	if p.foldSource == value && p.foldLanguage == language || len(value) >= backgroundTextBytes {
		return
	}
	p.foldSource = value
	p.foldLanguage = language
	p.foldDisplaySource = ""
	p.folds = editorFolds(value, language)
	p.collapsed = map[int]bool{}
}

func (p *editorPane) foldAll(fold bool) {
	p.updateFolds()
	clear(p.collapsed)
	if fold {
		for _, f := range p.folds {
			p.collapsed[f.Header] = true
		}
	}
}

func (p *editorPane) declare(c ui.C, label string, showCaret bool) ui.Box {
	if p.keyboardColumn && p.columnSource != p.text.String() {
		p.keyboardColumn = false
		p.columnSource = ""
	}
	p.configureCodeLanguage()
	language := p.view.Snapshot.Language
	stateful := codeSyntaxByName[language] != nil
	switch language {
	case "XML", "HTML", "YAML", "Properties", "INI", "TOML", "ENV", "Markdown", "CSV", "TSV":
		stateful = true
	}
	if stateful {
		p.configHighlight.prepare(p.text, language)
		if p.view.Snapshot.HighlightLine == nil {
			p.view.Snapshot.HighlightLine = p.highlightConfigLine
		}
	} else if p.view.Snapshot.HighlightLine != nil {
		p.configHighlight = configHighlight{}
		p.view.Snapshot.HighlightLine = nil
	}
	p.paintSelections()
	label = uiText(label)
	p.view.Snapshot.Placeholder = uiText(p.placeholder)
	box := codeeditor.View(c, label, p.view)
	box.DrawOver(func(d *ui.DrawContext) {
		p.focused = d.Canvas.Focused() == d.Node.ID
		if p.focused && p.onFocused != nil {
			p.onFocused()
		}
		p.onFocused = nil
		if foldableLanguage(p.view.Snapshot.Language) && len(p.text.String()) >= backgroundTextBytes && (p.foldSource != p.text.String() || p.foldLanguage != p.view.Snapshot.Language) && p.foldJob == nil {
			d.Canvas.RequestFrame()
		}
		if p.restoreScroll != nil {
			p.view.Code.SetScrollX(p.restoreScroll.X)
			p.view.Code.SetScrollY(p.restoreScroll.Y)
			p.restoreScroll = nil
			d.Canvas.RequestFrame()
		}
	})
	p.view.Snapshot.ShowCaret = showCaret
	if foldableLanguage(p.view.Snapshot.Language) {
		p.updateFoldJob(c.Part("fold-job"))
		p.updateFolds()
		scope := p.view.Parts()
		// UI 移动、悬停和菜单重绘不重新生成整个文档的折叠标记。
		if p.foldDisplaySource != p.foldSource || p.foldDisplayLanguage != currentUILanguage() || p.foldScope != scope || !maps.Equal(p.folded, p.collapsed) {
			p.view.Snapshot.FoldedLines = p.view.Snapshot.FoldedLines[:0]
			p.view.Snapshot.FoldMarkers = p.view.Snapshot.FoldMarkers[:0]
			last := -1
			for i, f := range p.folds {
				p.view.Snapshot.FoldMarkers = append(p.view.Snapshot.FoldMarkers, components.CodeEditorFoldMarker{
					Line: f.Header, ID: scope.At(10 + i), Collapsed: p.collapsed[f.Header], Boxed: true, Label: uiText("折叠或展开"),
				})
				if p.collapsed[f.Header] && f.Header > last {
					p.view.Snapshot.FoldedLines = append(p.view.Snapshot.FoldedLines, f)
					last = f.LastLine
				}
			}
			p.foldDisplaySource, p.foldDisplayLanguage, p.foldScope = p.foldSource, currentUILanguage(), scope
			p.folded = maps.Clone(p.collapsed)
		}
	} else {
		p.foldJob = nil
		p.view.Snapshot.FoldedLines = nil
		p.view.Snapshot.FoldMarkers = nil
		p.foldScope = gfx.Scope(0)
	}
	p.view.Snapshot.FoldGutter = foldableLanguage(p.view.Snapshot.Language)
	// 让原生多行输入和候选框使用代码编辑器的真实文字定位。
	box.HitWith(box.N().ID, label, gfx.HitOptions{
		Role: gfx.RoleTextField, Cursor: gfx.PointerCursorText, Value: p.text.String(),
		Multiline: true, ReadOnly: p.readOnly, TextSize: p.view.Snapshot.FontSize,
		HideNativeOverlays: true, Placement: &p.view.Code,
	})
	if p.focus && showCaret {
		box.AutoFocus()
		p.focus = false
	}
	p.completionView(c.Part("completion"), box.N().ID, showCaret)
	return box
}

func (p *editorPane) highlightLine(language, line string, dst []components.CodeEditorRun) []components.CodeEditorRun {
	highlight := syntaxLine{syntaxPalette: syntaxColors(p.light), runs: dst}
	switch language {
	case "JSON", "JSON Lines":
		highlight.json(line)
	case "日志":
		highlight.plain(line, true)
	case "纯文本":
		if !p.syntaxDisabled {
			highlight.plain(line, false)
		}
	}
	return highlight.runs
}
