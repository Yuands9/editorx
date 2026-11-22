package main

import (
	"fmt"
	"maps"
	"sort"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"github.com/vivaui/viva/ui/codeeditor"
	"github.com/vivaui/viva/window"
)

type editorPane struct {
	text              *text.Editor
	view              *codeeditor.Editor
	input             window.TextInput
	focus             bool
	readOnly          bool
	focused           bool
	searchKey         func(gfx.KeyEvent) gfx.EventResult
	foldSource        string
	folds             []components.CodeEditorFoldRange
	collapsed         map[int]bool
	folded            map[int]bool
	foldScope         gfx.Scope
	configHighlight   configHighlight
	syntaxDisabled    bool
	foldDisplaySource string
	light             bool
	restoreScroll     *gfx.Point
}

func newEditorPane(value string, readOnly bool) *editorPane {
	p := &editorPane{text: text.NewEditor(value), readOnly: readOnly, collapsed: map[int]bool{}}
	p.text.SetCode(text.CodeOptions{TabWidth: 4, AutoIndent: true})
	p.text.MoveTo(0, false)
	p.input.Editor = p.text
	p.view = codeeditor.New(p.text)
	p.view.Snapshot.FontSize = 16
	p.view.Snapshot.CaretWidth = 1
	p.view.Snapshot.Borderless = true
	p.view.Snapshot.ContinuousCaretBlink = !readOnly
	p.view.Snapshot.TabWidth = 4
	p.view.Snapshot.ShowIndentGuides = true
	p.view.Snapshot.ReadOnly = readOnly
	p.view.Snapshot.ScrollbarID = gfx.Item(1)
	p.view.Snapshot.HorizontalScrollbarID = gfx.Item(2)
	p.view.Snapshot.Highlight = p.highlightLine
	if !readOnly {
		p.view.Snapshot.Placeholder = "开始输入，或按 Ctrl+O 打开文件"
	}
	p.view.Text = func(window.TextInputContext) *window.TextInput { return &p.input }
	p.view.Key = p.key
	p.view.Pointer = p.pointer
	return p
}

func (p *editorPane) key(event gfx.KeyEvent) gfx.EventResult {
	if _, composing := p.text.Composition(); composing {
		return gfx.EventResult{}
	}
	if p.searchKey != nil {
		if result := p.searchKey(event); result.Handled {
			return result
		}
	}
	if !p.readOnly {
		if command, ok := components.CodeEditorCommandForKey(event); ok && command.Apply(p.text) {
			return gfx.EventResult{Handled: true, Repaint: true}
		}
	}
	movement, extend := components.TextareaMovementForKey(event.Key, event.Modifiers)
	if movement == components.TextareaMoveNone {
		return gfx.EventResult{}
	}
	to := p.view.Code.Move(p.text.String(), p.text.ByteOffset(p.text.Caret()), movement)
	p.text.MoveTo(p.text.RuneIndex(to), extend)
	return gfx.EventResult{Handled: true, Repaint: true}
}

func (p *editorPane) pointer(event gfx.PointerEvent) gfx.EventResult {
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
	p.text = text.NewEditor(value)
	p.text.MoveTo(0, false)
	p.view.Document = p.text
	p.input.Editor = p.text
	p.view.Code = components.CodeEditor{}
	p.collapsed = map[int]bool{}
}
func (p *editorPane) updateFolds() {
	value := p.text.String()
	if p.foldSource == value {
		return
	}
	p.foldSource = value
	p.folds = jsonFolds(value)
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
	box := codeeditor.View(c, label, p.view)
	box.DrawOver(func(d *ui.DrawContext) {
		p.focused = d.Canvas.Focused() == d.Node.ID
		if p.restoreScroll != nil {
			p.view.Code.SetScrollX(p.restoreScroll.X)
			p.view.Code.SetScrollY(p.restoreScroll.Y)
			p.restoreScroll = nil
			d.Canvas.RequestFrame()
		}
	})
	p.view.Snapshot.ShowCaret = showCaret
	if p.view.Snapshot.Language == "JSON" {
		p.updateFolds()
		scope := p.view.Parts()
		// UI 移动、悬停和菜单重绘不重新生成整个文档的折叠标记。
		if p.foldDisplaySource != p.foldSource || p.foldScope != scope || !maps.Equal(p.folded, p.collapsed) {
			p.view.Snapshot.FoldedLines = p.view.Snapshot.FoldedLines[:0]
			p.view.Snapshot.FoldMarkers = p.view.Snapshot.FoldMarkers[:0]
			last := -1
			for i, f := range p.folds {
				p.view.Snapshot.FoldMarkers = append(p.view.Snapshot.FoldMarkers, components.CodeEditorFoldMarker{
					Line: f.Header, ID: scope.At(10 + i), Collapsed: p.collapsed[f.Header], Boxed: true, Label: "折叠或展开",
				})
				if p.collapsed[f.Header] && f.Header > last {
					p.view.Snapshot.FoldedLines = append(p.view.Snapshot.FoldedLines, f)
					last = f.LastLine
				}
			}
			p.foldDisplaySource, p.foldScope = p.foldSource, scope
			p.folded = maps.Clone(p.collapsed)
		}
	} else {
		p.view.Snapshot.FoldedLines = nil
		p.view.Snapshot.FoldMarkers = nil
		p.foldScope = gfx.Scope(0)
	}
	p.view.Snapshot.FoldGutter = p.view.Snapshot.Language == "JSON"
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
	return box
}

func jsonFolds(source string) []components.CodeEditorFoldRange {
	value, err := parseJSON(source)
	if err != nil {
		return nil
	}
	lineStarts := []int{0}
	for i := 0; i < len(source); i++ {
		if source[i] == '\n' {
			lineStarts = append(lineStarts, i+1)
		}
	}
	var folds []components.CodeEditorFoldRange
	var visit func(*jsonValue)
	visit = func(value *jsonValue) {
		if value.kind != "object" && value.kind != "array" {
			return
		}
		header := sort.SearchInts(lineStarts, value.start+1) - 1
		last := sort.SearchInts(lineStarts, value.end) - 1
		if last > header && (len(folds) == 0 || folds[len(folds)-1].Header != header) {
			summary := "Object{...}"
			if value.kind == "array" {
				summary = fmt.Sprintf("Array[%d]", len(value.items))
			}
			end := len(source)
			if last+1 < len(lineStarts) {
				end = lineStarts[last+1] - 1
			}
			prefix := source[lineStarts[header]:value.start]
			// 保留关闭括号后的逗号等内容，摘要只参与绘制。
			display := prefix + summary + strings.TrimSuffix(source[value.end:end], "\r")
			folds = append(folds, components.CodeEditorFoldRange{
				Header: header, LastLine: last, Display: display, SummaryStart: len(prefix),
			})
		}
		for _, member := range value.members {
			visit(member.value)
		}
		for _, item := range value.items {
			visit(item)
		}
	}
	visit(value)
	return folds
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
