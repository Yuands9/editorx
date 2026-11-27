package editor

import (
	"math"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/markdown"
	"github.com/vivaui/viva/ui"
)

const aiMessageMaxTextWidth = float32(600)

type aiMarkdownMessage struct {
	document markdown.Document
	view     components.MarkdownView
}

type aiMessageBody struct {
	source         string
	font           float32
	preferredWidth float32
	layoutWidth    float32
	plain          components.TextareaLayout
	markdown       *aiMarkdownMessage
	selection      components.TextSelection
	handlers       ui.Handlers
}

func (body *aiMessageBody) update(value string, font float32, formatted bool) {
	if body.handlers.Pointer == nil {
		body.handlers.Pointer = body.pointer
	}
	modeChanged := formatted != (body.markdown != nil)
	if modeChanged {
		body.markdown = nil
		if formatted {
			body.markdown = &aiMarkdownMessage{}
		}
	}
	if body.source != value || modeChanged {
		body.source = value
		body.selection.Clear()
		body.preferredWidth, body.layoutWidth = 0, 0
		if body.markdown != nil {
			body.markdown.document.Parse(value)
		}
	}
	if body.font != font {
		body.font = font
		body.preferredWidth, body.layoutWidth = 0, 0
	}
}

func (body *aiMessageBody) MeasureWidth(ctx *ui.LayoutContext) float32 {
	if body.preferredWidth != 0 {
		return body.preferredWidth
	}
	canvas := ctx.Frame.Surface
	if canvas == nil {
		return aiMessageMaxTextWidth
	}
	width := float32(0)
	if body.markdown != nil {
		width = body.markdownWidth(canvas)
	} else {
		for line := range strings.SplitSeq(body.source, "\n") {
			if len(line) > 1000 {
				width = aiMessageMaxTextWidth
				break
			}
			width = max(width, canvas.TextWidth(line, body.font))
			if width >= aiMessageMaxTextWidth {
				break
			}
		}
	}
	body.preferredWidth = min(aiMessageMaxTextWidth, max(1, float32(math.Ceil(float64(width)))+2))
	return body.preferredWidth
}

func (body *aiMessageBody) markdownWidth(canvas *gfx.Canvas) float32 {
	width := float32(0)
	doc := &body.markdown.document
	for index := range doc.Blocks {
		block := &doc.Blocks[index]
		indent := float32(block.Quote) * 14
		style := gfx.TextStyle{Size: body.font, Weight: gfx.WeightRegular}
		switch block.Kind {
		case markdown.Rule:
			width = max(width, indent+84)
			continue
		case markdown.Code:
			style.Size, style.Mono = max(body.font-1, 9), true
			for line := range strings.SplitSeq(block.Text, "\n") {
				if len(line) > 1000 {
					return aiMessageMaxTextWidth
				}
				width = max(width, indent+canvas.TextWidthStyle(line, style)+20)
				if width >= aiMessageMaxTextWidth {
					return aiMessageMaxTextWidth
				}
			}
			continue
		case markdown.Table:
			cells := doc.Cells(block)
			if block.Columns > 0 {
				columns := make([]float32, block.Columns)
				for index, cell := range cells {
					cellStyle := style
					if index < block.Columns {
						cellStyle.Weight = gfx.WeightSemibold
					}
					column := index % block.Columns
					columns[column] = max(columns[column], aiSpansWidth(canvas, doc.CellSpans(cell), cellStyle)+16)
				}
				tableWidth := indent
				for _, columnWidth := range columns {
					tableWidth += columnWidth
				}
				width = max(width, tableWidth)
			}
			continue
		case markdown.Heading:
			style.Weight = gfx.WeightSemibold
			switch block.Level {
			case 1:
				style.Size *= 1.4
				style.Weight = gfx.WeightBold
			case 2:
				style.Size *= 1.25
			case 3:
				style.Size *= 1.1
			}
		case markdown.Item:
			indent += 24 + float32(block.Level)*20
		case markdown.Paragraph:
			if block.Level > 0 {
				indent += 24 + float32(block.Level-1)*20
			}
		}
		width = max(width, indent+aiSpansWidth(canvas, doc.Spans(block), style))
		if width >= aiMessageMaxTextWidth {
			return aiMessageMaxTextWidth
		}
	}
	return width
}

func aiSpansWidth(canvas *gfx.Canvas, spans []markdown.Span, style gfx.TextStyle) float32 {
	var width, lineWidth float32
	for _, span := range spans {
		if span.Text == markdown.Newline {
			width, lineWidth = max(width, lineWidth), 0
			continue
		}
		if len(span.Text) > 1000 {
			return aiMessageMaxTextWidth
		}
		spanStyle := style
		if span.Style&markdown.Bold != 0 {
			spanStyle.Weight = gfx.WeightBold
		}
		spanStyle.Italic = span.Style&markdown.Italic != 0
		if span.Style&markdown.Mono != 0 {
			spanStyle.Mono = true
			lineWidth += 8
		}
		lineWidth += canvas.TextWidthStyle(span.Text, spanStyle)
		if lineWidth >= aiMessageMaxTextWidth {
			return aiMessageMaxTextWidth
		}
	}
	return max(width, lineWidth)
}

func (*aiMessageBody) PlaceWidth(ctx *ui.LayoutContext, bounds ui.Rect) {
	ctx.SetWidth(0, bounds.Width)
}

func (body *aiMessageBody) MeasureHeight(ctx *ui.LayoutContext) float32 {
	width := ctx.ContentWidth()
	canvas := ctx.Frame.Surface
	if width <= 0 || canvas == nil {
		return body.font + 7
	}
	if body.markdown != nil {
		if width != body.layoutWidth {
			body.markdown.view.Layout(canvas, components.MarkdownStyle{Size: body.font}, &body.markdown.document, width)
			body.layoutWidth = width
		}
		return body.markdown.view.Height()
	}
	body.plain.Fit(canvas, body.source, body.font, width, true)
	body.layoutWidth = width
	return float32(len(body.plain.Rows)) * (body.font + 7)
}

func (*aiMessageBody) PlaceHeight(ctx *ui.LayoutContext, bounds ui.Rect) {
	ctx.SetHeight(0, bounds.Height)
}

func (body *aiMessageBody) draw(d *ui.DrawContext) {
	part := components.MarkdownSelection{}
	if d.State.Focused {
		part = body.selection.In(0)
	}
	if body.markdown != nil {
		body.markdown.view.Paint(d.Canvas, components.MarkdownPaint{
			Look: components.MarkdownLookFor(*d.Theme), X: d.Rect.X, Y: d.Rect.Y, Clip: d.Rect, Selection: part,
		})
		return
	}
	theme := *d.Theme
	theme.Metrics.FontSizeM = body.font
	start, end := part.Range([]byte(body.source))
	components.PaintTextareaContent(d.Canvas, theme, d.Rect, d.Rect, components.TextareaSnapshot{
		Value: body.source, Layout: &body.plain, ReadOnly: true, SelectionStart: start, SelectionEnd: end,
	})
}

func (body *aiMessageBody) pointAt(point gfx.Point) (components.TextPoint, bool) {
	if body.markdown != nil {
		offset, ok := body.markdown.view.OffsetAt(point)
		return components.TextPoint{Offset: offset}, ok
	}
	offset, ok := body.plain.OffsetAt(point)
	return components.TextPoint{Offset: offset}, ok
}

func (body *aiMessageBody) pointer(event gfx.PointerEvent) gfx.EventResult {
	result := body.selection.Pointer(event, body.pointAt)
	response := gfx.EventResult{Handled: result.Handled, Repaint: result.Changed}
	if result.Handled && event.Kind == gfx.PointerDown {
		response.Focus = event.Target
	}
	return response
}

func (body *aiMessageBody) selectedText() string {
	if body.selection.Empty() {
		return ""
	}
	part := body.selection.In(0)
	if body.markdown != nil {
		return body.markdown.view.SelectedText(part)
	}
	start, end := part.Range([]byte(body.source))
	return body.source[start:end]
}

func (m *editorApp) aiMessageText(c ui.C, body *aiMessageBody, label, value string, font float32, formatted, blocked bool) {
	body.update(value, font, formatted)
	ui.CustomLayout(c, body).MinWidth(0).Shrink(1).Children(func() {
		control := ui.Control(c, label, &body.handlers)
		control.HitWith(control.N().ID, label, gfx.HitOptions{
			Role: gfx.RoleGroup, Cursor: gfx.PointerCursorText, Disabled: blocked,
		}).Draw(body.draw)
		if !blocked && !body.selection.Empty() {
			if control.Shortcut(ui.Ctrl, "c") {
				c.After(func() { m.copyAIText("复制选中内容", body.selectedText()) })
			}
			ui.ContextMenu(c, control, func(menu *ui.MenuItems) {
				if menu.Item(m.menuText("复制选中内容")).Icon(copyIcon).Clicked() {
					c.After(func() { m.copyAIText("复制选中内容", body.selectedText()) })
				}
			})
		}
	})
}
