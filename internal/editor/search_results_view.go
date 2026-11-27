package editor

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

const (
	searchResultFontSize    = 12
	searchResultLabelPixels = 2048
)

type searchResultColors struct {
	query, queryText, file, fileText, guide, number, mark, markText gfx.Color
}

func (m *editorApp) resultColors() searchResultColors {
	if m.light {
		return searchResultColors{
			query: gfx.RGB(219, 225, 252), queryText: gfx.RGB(43, 58, 112),
			file: gfx.RGB(228, 239, 235), fileText: gfx.RGB(29, 92, 71), guide: gfx.RGB(190, 201, 219),
			number: gfx.RGB(141, 72, 30), mark: gfx.RGB(255, 234, 160), markText: gfx.RGB(139, 45, 19),
		}
	}
	return searchResultColors{
		query: gfx.RGB(48, 56, 86), queryText: gfx.RGB(214, 224, 255),
		file: gfx.RGB(35, 62, 58), fileText: gfx.RGB(165, 224, 204), guide: gfx.RGB(65, 81, 103),
		number: gfx.RGB(228, 163, 114), mark: gfx.RGB(86, 62, 25), markText: gfx.RGB(255, 219, 139),
	}
}

func searchResultCount(count int) string {
	if count < 1000 {
		return strconv.Itoa(count)
	}
	return fmt.Sprintf("%d,%03d", count/1000, count%1000)
}

func (r *searchResults) summary() string {
	mode := "普通"
	switch r.options.mode {
	case searchEscapes:
		mode = "转义字符"
	case searchRegexp:
		mode = "正则"
	}
	pattern := strings.ReplaceAll(strings.ReplaceAll(r.query, "\r\n", "\n"), "\n", "↵")
	summary := fmt.Sprintf(uiText("搜索 “%s”  ·  %d 个文件匹配 %s 次  ·  共搜索 %d 个文件  ·  %s"),
		pattern, r.files, searchResultCount(r.total), len(r.groups), mode)
	if r.truncated {
		summary += "  ·  仅显示前 10,000 处"
	}
	return summary
}

func (m *editorApp) searchResultsView(c ui.C) {
	r := &m.results
	r.refresh(m)
	colors := m.resultColors()
	face := gfx.MonoFamily(m.editorPrefs.FontFamily)
	ui.Column(c.Part("search-results")).FillWidth().Grow(1).MinHeight(0).Clip().Background(m.theme.Background).
		Draw(func(d *ui.DrawContext) {
			if width, _ := d.Canvas.MonoCellSize(searchResultFontSize, face); width > 0 && width != r.cellWidth {
				r.cellWidth = width
				d.Canvas.RequestFrame()
			}
		}).Children(func() {
		ui.Row(c.Part("toolbar")).FillWidth().Height(29).PadX(10).Gap(6).Items(ui.Center).Background(m.theme.CardElevated).Children(func() {
			ui.Text(c, uiText("搜索结果")).Bold().FontSize(12)
			ui.Text(c, searchResultCount(r.total)+uiText(" 处匹配")).FontSize(11).Muted().Grow(1).MinWidth(0).Ellipsis()
			if ui.Button(c, uiText("复制搜索结果")).Icon(copyIcon).IconOnly().Ghost().Size(24, 24).Tooltip(uiText("复制搜索结果")).Clicked() {
				c.After(m.copySearchResults)
			}
			if ui.Button(c, uiText("清空搜索结果")).Ghost().Height(24).FontSize(11).Clicked() {
				c.After(func() {
					*r = searchResults{split: r.split}
					m.focusActiveSource()
				})
			}
			if ui.Button(c, uiText("关闭搜索结果")).Icon(closeIcon).IconOnly().Ghost().Size(24, 24).Tooltip(uiText("关闭结果面板")).Clicked() {
				r.open = false
				m.focusActiveSource()
			}
		})
		if r.total == 0 {
			ui.Row(c).FillWidth().Height(22).PadX(10).Items(ui.Center).Background(colors.query).Children(func() {
				ui.Text(c, r.summary()).Color(colors.queryText).Bold().FontSize(12).Grow(1).MinWidth(0).Ellipsis()
			})
			ui.Text(c, uiText("没有找到匹配内容")).FontSize(12).Muted().Pad(10)
			return
		}
		cell := r.cellWidth
		if cell == 0 {
			cell = 8
		}
		numberWidth := float32(r.lineDigits+4) * cell
		contentWidth := 40 + numberWidth + float32(r.previewCells)*cell
		clicked := -1
		list := ui.List(c, &r.list, len(r.rows), func(item ui.ListRow) {
			if item.Element.Clicked() {
				clicked = item.Index
			}
			row := r.rows[item.Index]
			item.Element.Height(20).MinWidth(contentWidth).Items(ui.Center).Clip()
			switch {
			case row.group < 0:
				searchResultHeading(item, r.summary(), r.collapsed, 6, colors.query, colors.queryText)
			case row.line < 0:
				group := r.groups[row.group]
				searchResultHeading(item, group.name()+"  ("+searchResultCount(group.count())+" 处匹配)", group.collapsed, 22, colors.file, colors.fileText)
			default:
				line := r.groups[row.group].lines[row.line]
				if item.Selected() {
					item.Element.Background(m.theme.CardElevated)
				} else if item.Element.State().Hovered {
					item.Element.Background(m.theme.Card)
				}
				item.Element.Draw(func(d *ui.DrawContext) {
					d.Canvas.Fill(gfx.Rect{X: d.Rect.X + 15, Y: d.Rect.Y, Width: 1, Height: d.Rect.Height}, colors.guide)
					d.Canvas.Fill(gfx.Rect{X: d.Rect.X + 30, Y: d.Rect.Y, Width: 1, Height: d.Rect.Height}, colors.guide)
					bounds := d.Rect
					bounds.X += 37
					bounds.Width = numberWidth - 7
					style := gfx.TextStyle{Size: searchResultFontSize, Mono: true, MonoFace: face, Color: colors.number}
					d.Canvas.LabelAligned("行 "+strconv.Itoa(line.number), bounds, style, gfx.AlignRight)
					bounds.X += numberWidth + 3
					bounds.Width = max(0, d.Rect.Right()-bounds.X)
					searchResultExcerpt(d, line, bounds, style, colors)
				})
			}
		}).FillWidth().Grow(1).MinHeight(0).Sideways().AlwaysScrollbars().ScrollbarSize(7).
			ScrollbarLabels("纵向滚动搜索结果", "横向滚动搜索结果").Label(uiText("搜索结果列表"))
		if r.resetHorizontal {
			list.ScrollState().ScrollToX(0)
			r.resetHorizontal = false
		}
		index, submitted := r.list.Submitted()
		if clicked >= 0 {
			index, submitted = clicked, true
		} else if !submitted && r.list.Changed() {
			index = r.list.Selected()
			submitted = index >= 0 && r.rows[index].line >= 0
		}
		if submitted {
			c.After(func() { m.chooseSearchResult(index) })
		}
	})
}

func searchResultHeading(item ui.ListRow, label string, collapsed bool, indent float32, background, foreground gfx.Color) {
	icon := chevronDownIcon
	if collapsed {
		icon = chevronRightIcon
	}
	item.Element.Height(22).Background(background).PadX(6).Gap(5)
	if item.Focused() {
		item.Element.Border(1, foreground)
	}
	if indent > 6 {
		ui.Slot(item.C).Width(indent - 6)
	}
	ui.Icon(item.C, icon).Width(12).Height(12).Color(foreground)
	maxWidth := searchResultLabelPixels / item.C.Canvas().PixelScale()
	ui.Text(item.C, label).Color(foreground).Bold().FontSize(searchResultFontSize).MaxWidth(maxWidth).MinWidth(0).Ellipsis()
}

func searchResultExcerpt(d *ui.DrawContext, line searchResultLine, bounds gfx.Rect, style gfx.TextStyle, colors searchResultColors) {
	canvas := d.Canvas
	viewport := d.Frame.Bounds()
	left, right := viewport.X, viewport.Right()
	for _, clip := range canvas.Clips() {
		left = max(left, clip.Bounds.X)
		right = min(right, clip.Bounds.Right())
	}
	if left >= right {
		return
	}
	draw := func(value string, color gfx.Color, marked bool) {
		if value == "" || bounds.X >= right {
			return
		}
		value = strings.ReplaceAll(value, "\t", "    ")
		style.Color = color
		rect := bounds
		rect.Width = canvas.TextWidthStyle(value, style)
		bounds.X += rect.Width
		if rect.Right() <= left {
			return
		}
		if marked {
			start, end := max(rect.X, left), min(rect.Right(), right)
			canvas.Fill(gfx.Rect{X: start, Y: rect.Y + 2, Width: end - start, Height: rect.Height - 4}, colors.mark)
		}
		drawSearchResultLabel(canvas, value, rect, style, left, right)
	}
	if line.clippedLeft {
		draw("…", d.Theme.Muted, false)
	}
	at := 0
	for _, mark := range line.marks {
		from := max(at, mark.Start)
		draw(line.excerpt[at:from], d.Theme.Foreground, false)
		if mark.End > from {
			draw(line.excerpt[from:mark.End], colors.markText, true)
		} else {
			draw("│", colors.markText, true)
		}
		at = max(from, mark.End)
	}
	draw(line.excerpt[at:], d.Theme.Foreground, false)
	if line.clippedRight {
		draw("…", d.Theme.Muted, false)
	}
	if count := line.end - line.first; count > len(line.marks) {
		draw("  · 本行 "+searchResultCount(count)+" 处", d.Theme.Muted, false)
	}
}

// The Windows renderer allocates a texture for the full width of a fixed-pitch
// label before applying its clip. Keep each visible label bounded in device pixels.
func drawSearchResultLabel(canvas *gfx.Canvas, value string, bounds gfx.Rect, style gfx.TextStyle, left, right float32) {
	maxWidth := searchResultLabelPixels / canvas.PixelScale()
	if bounds.Width <= maxWidth {
		canvas.Label(value, bounds, style)
		return
	}
	cell, _ := canvas.MonoCellSize(style.Size, style.MonoFace)
	maxCells := max(2, int(maxWidth/cell))
	for value != "" && bounds.X < right {
		end, cells := searchResultLabelPart(value, maxCells)
		bounds.Width = float32(cells) * cell
		if bounds.Right() > left {
			canvas.Label(value[:end], bounds, style)
		}
		bounds.X += bounds.Width
		value = value[end:]
	}
}

func searchResultLabelPart(value string, maxCells int) (end, cells int) {
	for end < len(value) {
		next := text.NextGraphemeBoundary(value, end)
		clusterCells := 0
		for _, ch := range value[end:next] {
			clusterCells += text.CellWidth(ch)
		}
		if cells+clusterCells > maxCells {
			if end > 0 {
				break
			}
			// A cluster can itself be wider than a texture tile. In that case
			// split it at a UTF-8 character boundary to guarantee progress.
			for at, ch := range value[:next] {
				width := text.CellWidth(ch)
				if cells+width > maxCells {
					return at, cells
				}
				cells += width
			}
		}
		end, cells = next, cells+clusterCells
	}
	return end, cells
}
