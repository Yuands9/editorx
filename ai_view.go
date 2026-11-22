package main

import (
	"fmt"
	"slices"

	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"github.com/vivaui/viva/ui/codeeditor"
)

var aiSendIcon = gfx.Icon{ViewBox: 24, Stroke: 1.8, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(12, 19), gfx.LineTo(12, 5), gfx.MoveTo(6, 11), gfx.LineTo(12, 5), gfx.LineTo(18, 11),
}}
var aiHistoryIcon = gfx.Icon{ViewBox: 24, Stroke: 1.6, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(5, 7), gfx.CubicTo(9, 1, 20, 3, 20, 12), gfx.CubicTo(20, 22, 6, 22, 4, 15),
	gfx.MoveTo(4, 4), gfx.LineTo(4, 8), gfx.LineTo(8, 8),
	gfx.MoveTo(12, 7), gfx.LineTo(12, 12), gfx.LineTo(15, 14),
}}
var aiStopIcon = gfx.Icon{ViewBox: 24, Stroke: 1.7, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(7, 7), gfx.LineTo(17, 7), gfx.LineTo(17, 17), gfx.LineTo(7, 17), gfx.LineTo(7, 7),
}}

func (m *editorApp) aiPanel(c ui.C, blocked bool) {
	a := &m.ai
	m.workspacePanel(c).Grow(1).MarginRight(workspaceInset).Pad(10).Gap(8).Children(func() {
		ui.Row(c).FillWidth().Height(37).Items(ui.Center).Gap(5).Children(func() {
			m.aiAssistantMark(c, 29)
			ui.Text(c, "AI 助手").Bold().FontSize(15).Grow(1).MinWidth(0).Ellipsis()
			if ui.Button(c, "模型设置").Ghost().Height(28).FontSize(12).Disabled(blocked || a.responding()).Clicked() {
				c.After(m.openAISettings)
			}
			if ui.Button(c, "新对话").Icon(plusIcon).IconOnly().Ghost().Size(28, 28).Tooltip("新对话").Disabled(blocked).Clicked() {
				c.After(a.newConversation)
			}
			m.aiConversationPicker(c.Part("conversations"), blocked)
			if ui.Button(c, "收起 AI 助手").Icon(closeIcon).IconOnly().Ghost().Size(28, 28).Disabled(blocked).Clicked() {
				c.After(m.toggleAIPanel)
			}
		})
		ui.Slot(c).Height(1).FillWidth().Background(m.theme.Border)
		composerMin := float32(110)
		if len(a.attachments) > 0 {
			composerMin += 35
		}
		if a.message != "" {
			composerMin += 23
		}
		ui.Column(c.Part("conversation-split")).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Height(m.aiPromptSplit.Pos).Shrink(0).Clip().Children(func() {
				m.aiTranscript(c.Part("transcript"), blocked)
			})
			ui.Splitter(c, &m.aiPromptSplit).Mins(124, composerMin).SplitterThickness(12).
				Label("调整 AI 输入框高度").DrawOver(func(d *ui.DrawContext) {
				m.drawAIResizeGrip(d, &m.aiPromptSplit, false)
			})
			m.aiComposer(c.Part("composer"), blocked)
		})
	})
}

func (m *editorApp) drawAIResizeGrip(d *ui.DrawContext, split *ui.SplitterState, vertical bool) {
	color := m.theme.MutedForeground
	if split.Dragging() || d.Canvas.Hovered() == split.ID || d.Canvas.FocusVisible() == split.ID {
		color = m.theme.Ring
	}
	x, y := d.Rect.X+d.Rect.Width/2-1.5, d.Rect.Y+d.Rect.Height/2-1.5
	for _, offset := range []float32{-6, 0, 6} {
		dot := gfx.Rect{X: x, Y: y, Width: 3, Height: 3}
		if vertical {
			dot.Y += offset
		} else {
			dot.X += offset
		}
		d.Canvas.Rounded(dot, 1.5, d.Fade(color))
	}
}

func (m *editorApp) aiTranscript(c ui.C, blocked bool) {
	a := &m.ai
	if len(a.turns) == 0 {
		ui.Column(c).FillWidth().Grow(1).MinHeight(0).Center().Gap(10).Children(func() {
			m.aiAssistantMark(c, 58)
			ui.Text(c, "问一问，或开始写作").FontSize(15).Bold()

		})
		return
	}
	scroll := ui.Scroll(c).Grow(1).MinHeight(0).FillWidth().Scrollbars()
	if a.scrollEnd || a.scrollStream && scroll.ScrollState().AtEnd() {
		scroll.ScrollState().ScrollToEnd()
	}
	a.scrollEnd, a.scrollStream = false, false
	scroll.Children(func() {
		ui.Column(c).FillWidth().PadRight(5).Gap(16).Children(func() {
			for i, turn := range a.turns {
				ui.Column(c.KeyInt(i)).FillWidth().Gap(8).Children(func() {
					m.aiTurnView(c, turn, blocked)
				})
			}
		})
	})
}

func (m *editorApp) aiTurnView(c ui.C, turn *aiTurn, blocked bool) {
	fontSize := float32(m.ai.profiles.chatFontSize())
	tint := m.theme.Ring
	tint.Alpha = .24
	ui.Row(c).FillWidth().PadLeft(24).Justify(ui.End).Children(func() {
		ui.Row(c).MinWidth(0).Shrink(1).Items(ui.Start).PadLeft(12).PadRight(8).PadY(8).Gap(6).
			Radius(14).Background(tint.Over(m.theme.Background)).Children(func() {
			m.aiMessageText(c, &turn.questionBody, "问题正文", turn.prompt, fontSize, false, blocked)
			m.aiCopyButton(c, "复制问题", turn.prompt, blocked)
		})
	})
	ui.Row(c).FillWidth().PadRight(16).Children(func() {
		ui.Column(c).MinWidth(0).Shrink(1).Gap(6).Pad(12).Radius(14).Background(m.theme.Card).Children(func() {
			ui.Row(c).FillWidth().Items(ui.Center).Gap(6).Children(func() {
				m.aiProviderMark(c, turn.provider, 16)
				ui.Text(c, turn.model).Muted().FontSize(12).Grow(1).MinWidth(0).Ellipsis()
				if turn.reply != "" {
					m.aiCopyButton(c, "复制回复", turn.reply, blocked)
				}
			})
			if turn.request != nil && turn.partial == "" {
				ui.Text(c, "正在生成…").Muted().FontSize(12)
			}
			if turn.notice != "" {
				color := m.theme.MutedForeground
				if turn.failed {
					color = m.theme.Danger
				}
				ui.Wrap(c, turn.notice).FillWidth().Color(color).FontSize(fontSize)
			}
			answer := turn.reply
			if turn.request != nil {
				answer = turn.partial
			}
			if answer != "" {
				m.aiMessageText(c, &turn.answerBody, "回复正文", answer, fontSize, turn.request == nil, blocked)
			}
			for i, change := range turn.changes {
				label := "查看差异 · " + change.target.file.Name
				if change.target.file.ID == 0 {
					label = "预览草稿"
				}
				if ui.Button(c.KeyInt(i), label).Outline().Height(32).FillWidth().FontSize(12).Disabled(blocked).Clicked() {
					c.After(func() { m.ai.showReview(turn, i) })
				}
			}
		})
	})
}

func (m *editorApp) aiCopyButton(c ui.C, label, value string, blocked bool) {
	button := ui.Button(c, label).Icon(copyIcon).IconOnly().Ghost().Size(24, 24).Shrink(0).Tooltip(label).Disabled(blocked)
	if button.Clicked() {
		c.After(func() { m.copyAIText(label, value) })
	}
}

func (m *editorApp) copyAIText(label, value string) {
	if value == "" {
		return
	}
	if err := app.SetClipboardText(value); err != nil {
		m.ai.message = err.Error()
		return
	}
	m.ai.message = "已" + label
}

func (m *editorApp) aiComposer(c ui.C, blocked bool) {
	a := &m.ai
	busy := a.responding()
	ui.Column(c).FillWidth().Grow(1).Basis(0).MinHeight(0).Pad(9).Gap(5).Radius(14).Border(1, m.theme.Border).Background(m.theme.Card).Children(func() {
		m.aiAttachmentChips(c.Part("attachments"), blocked)
		if a.message != "" {
			status := ui.Row(c).FillWidth().Height(18).Items(ui.Center)
			status.HitWith(ui.ID(c), a.message, gfx.HitOptions{Role: gfx.RoleGroup, SkipFocus: true}).Tooltip(a.message)
			status.Children(func() { ui.Text(c, a.message).FillWidth().FontSize(12).Muted().Ellipsis() })
		}
		hint := "输入你想问的问题"
		switch a.mode {
		case aiEdit:
			hint = "说明要怎么修改选区或附件"
		case aiDraft:
			hint = "描述你想起草的内容"
		}
		a.prompt.Hint = "Enter 发送，Shift+Enter 换行"
		input := ui.TextArea(c, &a.prompt, "AI 指令", hint).
			FillWidth().Grow(1).Basis(0).MinHeight(48).FontSize(float32(a.profiles.chatFontSize())).
			Background(m.theme.Background).Border(0, gfx.Color{}).Shadow(gfx.ShadowNone).Disabled(blocked)
		if a.focusPrompt && !blocked {
			input.AutoFocus()
			a.focusPrompt = false
		}
		ui.Row(c).FillWidth().Items(ui.Center).Justify(ui.Between).Gap(5).Children(func() {
			ui.Row(c.Part("left-controls")).Items(ui.Center).Gap(5).Shrink(0).Children(func() {
				m.aiContextPicker(c.Part("context"), blocked || busy)
				m.aiModePicker(c.Part("mode"), blocked || busy)
			})
			ui.Row(c.Part("right-controls")).Items(ui.Center).Gap(5).MinWidth(0).Children(func() {
				m.aiProfilePicker(c.Part("profile"), blocked || busy)
				if busy {
					if ui.Button(c, "停止请求").Icon(aiStopIcon).IconOnly().Outline().Size(36, 36).Shrink(0).Radius(18).Tooltip("停止请求").Disabled(blocked).Clicked() {
						c.After(a.stopRequest)
					}
				} else if ui.Button(c, "发送给模型").Icon(aiSendIcon).IconOnly().Size(36, 36).Shrink(0).Radius(18).Tooltip("Enter 发送 · Shift+Enter 换行").
					ButtonRecipe(m.aiSendRecipe).
					Disabled(blocked || strings.TrimSpace(a.prompt.Text()) == "").Clicked() {
					c.After(m.startAI)
				}
			})
		})
	})
}

func (m *editorApp) aiReviewDialog(c ui.C) {
	a := &m.ai
	if !a.reviewOpen {
		return
	}
	i, turn := a.reviewIndex, a.reviewTurn
	change := &turn.changes[i]
	target := change.target
	label := target.file.Name + " · " + target.file.Scope
	apply := "应用修改"
	if target.file.ID == 0 {
		label, apply = "草稿内容", "写入新标签"
	}
	tab := m.aiTab(target.file.ID)
	if target.file.ID < 0 {
		tab = m.tabAtPath(target.path)
	}
	if tab != nil && target.file.Format == "HTML" && tab.htmlMode != "编辑" {
		apply = "应用并刷新预览"
	}
	dismissed := ui.ModalWith(c, &a.reviewOpen, "AI 修改预览", ui.ModalSpec{
		Width: min(940, c.Bounds().Width-40), Height: min(650, c.Bounds().Height-42),
	}, func() {
		ui.Row(c).FillWidth().Items(ui.Center).Gap(8).Children(func() {
			ui.Text(c, label).Grow(1).MinWidth(0).Ellipsis().FontSize(14).Bold()
			stats := change.diff.Stats
			ui.Text(c, fmt.Sprintf("−%d  +%d", stats.Removed, stats.Added)).Muted()
		})
		if change.error != "" {
			ui.Wrap(c, change.error).FillWidth().FontSize(12)
		}
		codeeditor.DiffView(c, "原文和建议差异", &a.diff).FillWidth().Grow(1).MinHeight(0)
	}, func() {
		if ui.Button(c, "稍后审阅").Outline().Clicked() {
			a.closeReview()
		}
		if ui.Button(c, "丢弃建议").Outline().Clicked() {
			c.After(func() { a.finishReview(i) })
		}
		if ui.Button(c, apply).Disabled(change.error != "").Clicked() {
			c.After(func() {
				if err := m.applyAIChange(change); err != nil {
					a.message = err.Error()
					a.closeReview()
					return
				}
				a.message = m.status
				a.finishReview(i)
			})
		}
	})
	if dismissed {
		a.closeReview()
	}
}

func (a *aiAssistant) finishReview(index int) {
	turn := a.reviewTurn
	turn.changes = slices.Delete(turn.changes, index, index+1)
	if len(turn.changes) == 0 {
		turn.notice = "建议已审阅"
	}
	a.closeReview()
}

func (a *aiAssistant) closeReview() {
	a.reviewOpen = false
	a.reviewTurn = nil
	a.diff = codeeditor.Diff{}
}
