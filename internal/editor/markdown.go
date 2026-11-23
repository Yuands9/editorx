package editor

import (
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/markdown"
	"github.com/vivaui/viva/ui"
	uimarkdown "github.com/vivaui/viva/ui/markdown"
)

type heading struct {
	title         string
	level, offset int
	children      bool
}

func (t *editorTab) updateOutline() {
	source := t.source.text.String()
	t.markdown.Source = source
	if t.outlineSource == source {
		return
	}
	t.outlineSource = source
	t.headings = nil
	t.outlineCollapsed = map[int]bool{}
	var parsed markdown.Document
	parsed.Parse(source)
	for i := range parsed.Blocks {
		block := &parsed.Blocks[i]
		if block.Kind != markdown.Heading {
			continue
		}
		var title strings.Builder
		for _, span := range parsed.Spans(block) {
			title.WriteString(span.Text)
		}
		t.headings = append(t.headings, heading{title: title.String(), level: block.Level, offset: block.Start})
	}
	for i := 0; i+1 < len(t.headings); i++ {
		t.headings[i].children = t.headings[i+1].level > t.headings[i].level
	}
}

func (t *editorTab) jumpHeading(h heading) {
	t.source.text.MoveTo(t.source.text.RuneIndex(h.offset), false)
	t.source.focus = t.markdownMode != "预览"
	t.previewJump = h.offset
}

func (m *editorApp) outlinePanel(c ui.C, t *editorTab) {
	t.updateOutline()
	ui.Row(c).FillWidth().Height(48).PadX(18).Items(ui.Center).Children(func() {
		ui.Text(c, "文档大纲").FontSize(14).Grow(1)
		if ui.Button(c, "全部展开").Icon(expandIcon).IconOnly().Ghost().Width(26).Height(28).Tooltip("展开全部标题").Clicked() {
			t.outlineCollapsed = map[int]bool{}
		}
	})
	ui.Slot(c).FillWidth().Height(1).Background(m.theme.Border)
	ui.Slot(c).Height(12)
	if len(t.headings) == 0 {
		ui.Text(c, "添加 Markdown 标题后显示目录").Muted().FontSize(12).Pad(12).FillWidth()
		return
	}
	ui.Scroll(c).FillWidth().Grow(1).Scrollbars().Children(func() {
		hiddenLevel := 0
		for index, h := range t.headings {
			if hiddenLevel > 0 && h.level > hiddenLevel {
				continue
			}
			hiddenLevel = 0
			ui.Row(c.KeyInt(index)).FillWidth().Height(34).PadLeft(8 + float32(h.level-1)*12).Items(ui.Center).Children(func() {
				if h.children {
					chevron, label := chevronDownIcon, "折叠章节 "+h.title
					if t.outlineCollapsed[h.offset] {
						chevron, label = chevronRightIcon, "展开章节 "+h.title
					}
					if ui.Button(c, label).Icon(chevron).IconOnly().Ghost().Width(22).Tooltip(label).Clicked() {
						t.outlineCollapsed[h.offset] = !t.outlineCollapsed[h.offset]
					}
				} else {
					ui.Slot(c).Width(22)
				}
				target := ui.Row(c).Grow(1).MinWidth(0).Height(30).Radius(6).PadX(4).Items(ui.Center).Clip()
				target.HitWith(ui.ID(c), h.title, gfx.HitOptions{Role: gfx.RoleButton}).Tooltip(h.title).Hover(ui.Look{}.Background(m.theme.Hover))
				target.Children(func() { ui.Text(c, h.title).FontSize(14).Grow(1).MinWidth(0).Ellipsis() })
				if target.Clicked() {
					c.After(func() { t.jumpHeading(h) })
				}
			})
			if t.outlineCollapsed[h.offset] {
				hiddenLevel = h.level
			}
		}
	})
}

func (m *editorApp) markdownPreview(c ui.C, t *editorTab) {
	t.markdown.Source = t.source.text.String()
	ui.Column(c).FillHeight().Grow(1).MinWidth(0).PadX(24).Children(func() {
		ui.Text(c, "阅读预览").Muted().FontSize(12).Height(36).PadY(9)
		uimarkdown.View(c, "Markdown 预览", &t.markdown).FillWidth().Grow(1).DrawOver(func(d *ui.DrawContext) {
			t.previewHeight = d.Rect.Height
			offset := t.markdown.Offset
			if t.previewJump >= 0 {
				if y, ok := t.markdown.View.YAtSource(t.markdown.Source, t.previewJump); ok {
					offset = y
				}
				t.previewJump = -1
			}
			offset = max(0, min(offset, max(0, t.markdown.Height()-t.previewHeight)))
			if offset != t.markdown.Offset {
				t.markdown.Offset = offset
				d.Canvas.RequestFrame()
			}
		})
	})
}
