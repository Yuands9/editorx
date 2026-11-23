package editor

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

const currentAIConversation = -1

type aiConversationEdit struct {
	open, remove bool
	index        int
	title        string
	field        ui.Field
}

var aiRenameIcon = gfx.Icon{ViewBox: 24, Stroke: 1.65, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(5, 15.5), gfx.LineTo(15.75, 4.75), gfx.QuadTo(17, 3.5, 18.25, 4.75), gfx.LineTo(19.25, 5.75),
	gfx.QuadTo(20.5, 7, 19.25, 8.25), gfx.LineTo(8.5, 19), gfx.LineTo(4, 20), gfx.ClosePath(),
	gfx.MoveTo(14, 6.5), gfx.LineTo(17.5, 10),
}}

var aiDeleteIcon = gfx.Icon{ViewBox: 24, Stroke: 1.65, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(4.5, 6.5), gfx.LineTo(19.5, 6.5),
	gfx.MoveTo(9, 6.5), gfx.LineTo(9, 3.75), gfx.LineTo(15, 3.75), gfx.LineTo(15, 6.5),
	gfx.MoveTo(6.5, 6.5), gfx.LineTo(7.5, 19), gfx.QuadTo(7.6, 20, 8.7, 20), gfx.LineTo(15.3, 20),
	gfx.QuadTo(16.4, 20, 16.5, 19), gfx.LineTo(17.5, 6.5),
	gfx.MoveTo(10, 10), gfx.LineTo(10.4, 16.5), gfx.MoveTo(14, 10), gfx.LineTo(13.6, 16.5),
}}

func (m *editorApp) aiConversationPicker(c ui.C, blocked bool) {
	a := &m.ai
	current := a.currentConversation()
	count := len(a.history)
	if current.hasContent() {
		count++
	}
	button := ui.Button(c, "对话记录").Icon(aiHistoryIcon).IconOnly().Ghost().Size(28, 28).
		Tooltip("对话记录").Disabled(blocked || count == 0)
	if button.Clicked() {
		a.historyMenu = !a.historyMenu
	}
	ui.PopoverWith(c, button, ui.PopoverSpec{
		Side: ui.PopoverBottom, Align: ui.End, Width: min(350, c.Bounds().Width-20), Offset: 6, Padding: 8, Radius: 13, Instant: true,
	}, &a.historyMenu, func() {
		ui.Scroll(c).FillWidth().Height(min(318, float32(count)*46)).Scrollbars().Children(func() {
			ui.Column(c).FillWidth().Gap(2).PadRight(5).Children(func() {
				if current.hasContent() {
					m.aiConversationRow(c.KeyInt(currentAIConversation), currentAIConversation, current)
				}
				for index := len(a.history) - 1; index >= 0; index-- {
					m.aiConversationRow(c.KeyInt(index), index, a.history[index])
				}
			})
		})
	})
}

func (m *editorApp) aiConversationRow(c ui.C, index int, record aiConversation) {
	current := index == currentAIConversation
	name := record.title()
	detail := aiModeLabel(record.mode)
	if current {
		detail += " · 当前"
	}
	if aiPendingTurn(record.turns) != nil {
		detail += " · 正在生成"
	}
	row := ui.Row(c).FillWidth().Height(44).Items(ui.Center).Gap(3).PadRight(4).Radius(9)
	if current {
		row.Background(m.theme.Selection)
	}
	row.Children(func() {
		open := ui.Column(c.Part("open")).Grow(1).MinWidth(0).FillHeight().PadX(8).Gap(1).Justify(ui.Center).Radius(9)
		open.HitWith(ui.ID(c.Part("open")), name, gfx.HitOptions{Role: gfx.RoleButton, Selected: current}).
			Hover(ui.Look{}.Background(m.theme.Hover)).Focus(ui.Look{}.Border(m.theme.Ring)).Tooltip(name)
		open.Children(func() {
			ui.Text(c, name).FillWidth().Ellipsis().FontSize(13)
			ui.Text(c, detail).FontSize(11).Muted()
		})
		if open.Clicked() {
			c.After(func() {
				m.ai.historyMenu = false
				if !current {
					m.switchAIConversation(index)
				}
			})
		}
		if ui.Button(c, "重命名 "+name).Icon(aiRenameIcon).IconOnly().Ghost().Size(25, 27).Shrink(0).Tooltip("重命名对话").Clicked() {
			c.After(func() { m.beginAIConversationEdit(index, name, false) })
		}
		if ui.Button(c, "删除 "+name).Icon(aiDeleteIcon).IconOnly().Ghost().Size(25, 27).Shrink(0).Tooltip("删除对话").Clicked() {
			c.After(func() { m.beginAIConversationEdit(index, name, true) })
		}
	})
}

func (m *editorApp) beginAIConversationEdit(index int, title string, remove bool) {
	m.ai.historyMenu = false
	edit := &m.ai.conversationEdit
	*edit = aiConversationEdit{open: true, index: index, title: title, remove: remove}
	if !remove {
		edit.field.SetText(title)
		edit.field.Select(0, edit.field.GetEditor().RuneCount())
	}
}

func (m *editorApp) aiConversationDialog(c ui.C) {
	a := &m.ai
	edit := &a.conversationEdit
	if !edit.open {
		return
	}
	closeDialog := func() {
		a.conversationEdit = aiConversationEdit{}
		a.focusPrompt = true
	}
	if edit.remove {
		dismissed := ui.ModalWith(c, &edit.open, "删除对话", ui.ModalSpec{Width: 400}, func() {
			ui.Wrap(c, "删除「"+edit.title+"」及其中的消息？").FillWidth()
		}, func() {
			if ui.Button(c, "取消删除对话").Outline().Clicked() {
				c.After(closeDialog)
			}
			if ui.Button(c, "确认删除对话").Destructive().Clicked() {
				c.After(func() {
					if edit.index == currentAIConversation {
						a.resetConversation()
					} else {
						a.history = slices.Delete(a.history, edit.index, edit.index+1)
					}
					closeDialog()
				})
			}
		})
		if dismissed {
			closeDialog()
		}
		return
	}

	submitted := false
	dismissed := ui.ModalWith(c, &edit.open, "重命名对话", ui.ModalSpec{Width: 400}, func() {
		field := ui.TextField(c, &edit.field, "对话名称", "输入对话名称").FillWidth()
		submitted = field.Submitted()
	}, func() {
		if ui.Button(c, "取消重命名对话").Outline().Clicked() {
			c.After(closeDialog)
		}
		name := strings.TrimSpace(edit.field.Text())
		_, composing := edit.field.GetEditor().Composition()
		valid := !composing && name != "" && utf8.RuneCountInString(name) <= 64
		save := ui.Button(c, "保存对话名称").Disabled(!valid).Tooltip("Enter 确认")
		if valid && (save.Clicked() || submitted) {
			c.After(func() {
				if edit.index == currentAIConversation {
					a.name = name
				} else {
					a.history[edit.index].name = name
				}
				closeDialog()
			})
		}
	})
	if dismissed {
		closeDialog()
	}
}
