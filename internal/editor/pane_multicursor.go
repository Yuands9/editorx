package editor

import (
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

const maxEditorSelections = 2000

func (p *editorPane) paintSelections() {
	if p.input.Editor == nil {
		return
	}
	p.view.Snapshot.SelectionRanges = p.view.Snapshot.SelectionRanges[:0]
	p.view.Snapshot.ExtraCarets = p.view.Snapshot.ExtraCarets[:0]
	p.input.CopyOverride = p.text.SelectionCount() > 1
	if !p.input.CopyOverride {
		return
	}
	p.view.Snapshot.SelectionRanges = p.text.SelectionSpansInto(p.view.Snapshot.SelectionRanges)
	primary := p.text.ByteOffset(p.text.Caret())
	for _, span := range p.view.Snapshot.SelectionRanges {
		if span.End != primary {
			p.view.Snapshot.ExtraCarets = append(p.view.Snapshot.ExtraCarets, components.CodeEditorCaret{Offset: span.End})
		}
	}
}

func (p *editorPane) copySelections() string {
	value := p.text.String()
	parts := make([]string, 0, p.text.SelectionCount())
	for _, span := range p.text.SelectionSpans() {
		if span.Start != span.End {
			parts = append(parts, value[span.Start:span.End])
		}
	}
	return strings.Join(parts, "\n")
}

func (p *editorPane) selectNextOccurrence(all bool) {
	if _, composing := p.text.Composition(); composing || p.readOnly {
		return
	}
	selection := p.text.Selection()
	if selection.Start == selection.End {
		p.text.SetSelection(text.WordRange(p.text.String(), p.text.Caret()))
		if !all {
			return
		}
	}
	selection = p.text.Selection()
	value := p.text.String()
	needle := value[p.text.ByteOffset(selection.Start):p.text.ByteOffset(selection.End)]
	if needle == "" {
		return
	}
	start := p.text.ByteOffset(selection.End)
	// Search once around the document; already selected occurrences are skipped.
	for _, bounds := range [][2]int{{start, len(value)}, {0, start}} {
		for offset := bounds[0]; offset <= bounds[1]-len(needle) && p.text.SelectionCount() < maxEditorSelections; {
			index := strings.Index(value[offset:bounds[1]], needle)
			if index < 0 {
				break
			}
			from := offset + index
			changed := p.text.AddSelection(text.Range{Start: p.text.RuneIndex(from), End: p.text.RuneIndex(from + len(needle))})
			if changed && !all {
				return
			}
			offset = from + len(needle)
		}
	}
}

func (p *editorPane) addVerticalCaret(direction int) {
	value := p.text.String()
	position := p.text.ByteOffset(p.text.Caret())
	start := strings.LastIndexByte(value[:position], '\n') + 1
	column := utf8.RuneCountInString(value[start:position])
	var next int
	if direction < 0 {
		if start == 0 {
			return
		}
		next = strings.LastIndexByte(value[:start-1], '\n') + 1
	} else {
		end := strings.IndexByte(value[position:], '\n')
		if end < 0 {
			return
		}
		next = position + end + 1
	}
	end := next
	for count := 0; end < len(value) && value[end] != '\n' && count < column; count++ {
		_, size := utf8.DecodeRuneInString(value[end:])
		end += size
	}
	if p.text.SelectionCount() < maxEditorSelections {
		caret := p.text.RuneIndex(end)
		p.text.AddSelection(text.Range{Start: caret, End: caret})
	}
}

func (p *editorPane) splitSelectionLines() {
	selection := p.text.Selection()
	value := p.text.String()
	start, end := p.text.ByteOffset(selection.Start), p.text.ByteOffset(selection.End)
	var ranges []text.Range
	for offset := start; offset <= end && len(ranges) < maxEditorSelections; {
		next := end
		if newline := strings.IndexByte(value[offset:end], '\n'); newline >= 0 {
			next = offset + newline
		}
		ranges = append(ranges, text.Range{Start: p.text.RuneIndex(offset), End: p.text.RuneIndex(next)})
		if next == end {
			break
		}
		offset = next + 1
		if offset == end {
			break
		}
	}
	p.text.SetSelections(ranges, len(ranges)-1)
}

func (p *editorPane) multiPointer(event gfx.PointerEvent) gfx.EventResult {
	if p.readOnly {
		return gfx.EventResult{}
	}
	if _, composing := p.text.Composition(); composing {
		return gfx.EventResult{}
	}
	if event.Kind == gfx.PointerDown && event.Button == gfx.PointerButtonPrimary && event.Modifiers == ui.Alt {
		offset, ok := p.view.Code.OffsetAt(gfx.Point{X: event.X, Y: event.Y})
		if !ok {
			return gfx.EventResult{}
		}
		p.columnAnchor, p.columnDragging = offset, true
		caret := p.text.RuneIndex(offset)
		if p.text.SelectionCount() < maxEditorSelections {
			p.text.AddSelection(text.Range{Start: caret, End: caret})
		}
		return gfx.EventResult{Focus: event.Target, Handled: true, Repaint: true}
	}
	if !p.columnDragging {
		return gfx.EventResult{}
	}
	if event.Kind == gfx.PointerExit {
		p.columnDragging = false
		return gfx.EventResult{}
	}
	if event.Kind == gfx.PointerMove || event.Kind == gfx.PointerUp {
		offset, ok := p.view.Code.OffsetAt(gfx.Point{X: event.X, Y: event.Y})
		if ok && offset != p.columnAnchor {
			ranges := columnSelections(p.text.String(), p.columnAnchor, offset, p.view.Snapshot.TabWidth)
			p.text.SetSelections(ranges, len(ranges)-1)
		}
		if event.Kind == gfx.PointerUp {
			p.columnDragging = false
		}
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	return gfx.EventResult{}
}

// Rectangle columns follow rendered character cells, including tabs and CJK.
// Short rows stop at their end instead of inserting invisible padding.
func columnSelections(value string, anchor, caret, tabSize int) []text.Range {
	if tabSize <= 0 {
		tabSize = 4
	}
	columnAt := func(offset int) (int, int) {
		start := strings.LastIndexByte(value[:offset], '\n') + 1
		column := 0
		for _, character := range value[start:offset] {
			if character == '\t' {
				column += tabSize - column%tabSize
			} else {
				column += text.CellWidth(character)
			}
		}
		return start, column
	}
	first, left := columnAt(anchor)
	last, right := columnAt(caret)
	if first > last {
		first, last = last, first
	}
	if left > right {
		left, right = right, left
	}
	base := utf8.RuneCountInString(value[:first])
	var ranges []text.Range
	for start := first; start <= last && len(ranges) < maxEditorSelections; {
		end := len(value)
		if next := strings.IndexByte(value[start:], '\n'); next >= 0 {
			end = start + next
		}
		column, index, from, to := 0, base, -1, -1
		for _, character := range value[start:end] {
			if from < 0 && column >= left {
				from = index
			}
			if to < 0 && column >= right {
				to = index
			}
			if character == '\t' {
				column += tabSize - column%tabSize
			} else {
				column += text.CellWidth(character)
			}
			index++
		}
		if from < 0 {
			from = index
		}
		if to < 0 {
			to = index
		}
		ranges = append(ranges, text.Range{Start: from, End: to})
		base, start = index+1, end+1
	}
	return ranges
}
