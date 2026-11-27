package editor

import (
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	uimarkdown "github.com/vivaui/viva/ui/markdown"
)

func (m *editorApp) documentTranslationView(c ui.C, t *editorTab, blocked bool) {
	state := t.documentTranslation
	ui.Row(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
		ui.Column(c.Part("original")).Width(state.split.Pos).MinWidth(0).FillHeight().Children(func() {
			ui.Text(c, uiText("原文")).Muted().FontSize(12).Height(36).PadX(14).PadY(9)
			m.sourceEditor(c, t, blocked).FillWidth().Grow(1)
		})
		ui.Splitter(c.Part("split"), &state.split).Mins(180, 240).SplitterThickness(1).Label(uiText("调整原文和译文宽度"))
		ui.Column(c.Part("translated")).FillHeight().Grow(1).MinWidth(0).PadX(14).PadY(8).Gap(6).Children(func() {
			result := state.result.text.CommittedString()
			ui.Row(c.Part("heading")).FillWidth().Items(ui.Center).Gap(6).Children(func() {
				ui.Text(c, uiText("译文")).Bold().FontSize(13).Grow(1)
				if ui.Button(c.Part("copy"), uiText("复制译文")).Icon(copyIcon).Ghost().Height(28).FontSize(12).
					Disabled(blocked || strings.TrimSpace(result) == "").Clicked() {
					c.After(func() {
						if err := app.SetClipboardText(result); err != nil {
							state.problem = err.Error()
							return
						}
						m.status = uiText("已复制译文")
					})
				}
				if ui.Button(c.Part("close"), uiText("关闭整篇翻译")).Icon(closeIcon).IconOnly().Ghost().Size(28, 28).
					Tooltip(uiText("关闭整篇翻译")).Disabled(blocked).Clicked() {
					c.After(func() { m.closeDocumentTranslation(t) })
				}
			})
			busy := state.request != nil
			translationDirectionFields(c.Part("direction"), &state.translationDirection, blocked || busy, nil)
			ui.Row(c.Part("actions")).FillWidth().Items(ui.Center).Gap(6).Children(func() {
				if busy {
					if ui.Button(c.Part("stop"), uiText("停止翻译")).Outline().Height(30).FontSize(12).Disabled(blocked).Clicked() {
						c.After(state.stop)
					}
					ui.Text(c, uiText("正在翻译…")).Muted().FontSize(12)
					return
				}
				button := ui.Button(c.Part("start"), uiText("AI 翻译整篇")).Height(30).FontSize(12).
					Tooltip(uiText("将当前文章交给所选 AI 服务翻译")).Disabled(blocked || strings.TrimSpace(t.source.text.CommittedString()) == "")
				if button.Clicked() {
					c.After(func() { m.chooseDocumentTranslation(t) })
				}
				m.translationAIMenu(c.Part("services"), button, &state.translationAIChoice, func(settings aiSettings) {
					m.startDocumentTranslation(t, settings)
				})
			})
			if state.problem != "" {
				ui.Wrap(c, state.problem).FillWidth().Muted().FontSize(12)
			}
			from, to, err := state.languagePair()
			if result != "" && (state.original != t.source.text.CommittedString() || err != nil || from != state.source || to != state.target) {
				ui.Wrap(c, uiText("原文或语言方向已变化，可重新翻译。")).FillWidth().Muted().FontSize(12)
			}
			if state.preview != nil {
				uimarkdown.View(c.Part("preview"), "Markdown 译文预览", state.preview).FillWidth().Grow(1).MinHeight(0).
					DrawOver(func(d *ui.DrawContext) { state.previewHeight = d.Rect.Height })
				return
			}
			if result != "" || busy {
				state.result.view.Snapshot.Wrap = true
				state.result.light = m.light
				state.result.declare(c.Part("stream"), "文章译文", !blocked).FillWidth().Grow(1).MinHeight(0)
			}
		})
	})
}

func (state *documentTranslationState) preparePreview(value string) {
	preview := &uimarkdown.State{Source: value, Style: uimarkdown.Style{Size: 16, LineHeight: 27}}
	state.preview = preview
	preview.Scroll = func(event gfx.ScrollEvent) gfx.EventResult {
		preview.Offset = max(0, min(preview.Offset+event.StepY(48), max(0, preview.Height()-state.previewHeight)))
		return gfx.EventResult{Handled: true, Repaint: true}
	}
}
