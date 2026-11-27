package editor

import (
	"slices"
	"strings"

	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

var aiModes = []int{aiAsk, aiEdit, aiDraft}
var aiFontSizes = []int{12, 14, 16, 18, 20}

type aiTurn struct {
	prompt, provider, model, reply, notice string
	failed                                 bool
	stream                                 *aiReplyStream
	partial                                string
	request                                *ui.EffectState[aiResult, int]
	changes                                []aiChange
	questionBody, answerBody               aiMessageBody
}

type aiConversation struct {
	name        string
	mode        int
	effort      aiReasoning
	turns       []*aiTurn
	draft       string
	attachments []aiAttachment
}

func aiPendingTurn(turns []*aiTurn) *aiTurn {
	if len(turns) == 0 {
		return nil
	}
	last := turns[len(turns)-1]
	if last.request == nil {
		return nil
	}
	return last
}

func (a *aiAssistant) responding() bool {
	return aiPendingTurn(a.turns) != nil
}

func (a *aiAssistant) hasPendingRequest() bool {
	return a.responding() || slices.ContainsFunc(a.history, func(record aiConversation) bool {
		return aiPendingTurn(record.turns) != nil
	})
}

func aiModeLabel(mode int) string {
	switch mode {
	case aiEdit:
		return "生成修改"
	case aiDraft:
		return "新建草稿"
	default:
		return "问答"
	}
}

func (profiles aiProfiles) chatFontSize() int {
	if profiles.ChatFont == 0 {
		return 14
	}
	return profiles.ChatFont
}

func (a *aiAssistant) currentConversation() aiConversation {
	record := a.aiConversation
	if editor := a.prompt.GetEditor(); editor != nil {
		record.draft = editor.CommittedString()
	}
	return record
}

func (h aiConversation) hasContent() bool {
	return h.name != "" || len(h.turns) != 0 || strings.TrimSpace(h.draft) != "" ||
		slices.ContainsFunc(h.attachments, func(item aiAttachment) bool { return !item.automatic })
}

func (a *aiAssistant) rememberConversation() {
	record := a.currentConversation()
	if record.hasContent() {
		record.attachments = slices.Clone(record.attachments)
		a.history = append(a.history, record)
	}
}

func (a *aiAssistant) newConversation() {
	a.rememberConversation()
	a.resetConversation()
}

func (a *aiAssistant) resetConversation() {
	context := slices.DeleteFunc(a.attachments, func(item aiAttachment) bool { return !item.automatic })
	a.aiConversation = aiConversation{mode: a.profiles.DefaultMode, attachments: context}
	a.prompt.Bind(text.NewEditor(""))
	a.message, a.scrollEnd, a.focusPrompt = "", true, true
}

func (m *editorApp) switchAIConversation(index int) {
	a := &m.ai
	conversation := a.history[index]
	a.history = slices.Delete(a.history, index, index+1)
	a.rememberConversation()
	a.aiConversation = conversation
	a.attachments = slices.DeleteFunc(conversation.attachments, func(item aiAttachment) bool {
		return item.tabID != 0 && !aiTextTab(m.aiTab(item.tabID))
	})
	a.prompt.Bind(text.NewEditor(conversation.draft))
	a.message, a.scrollEnd, a.focusPrompt = "", true, true
}

func (a *aiAssistant) stopRequest() {
	if turn := aiPendingTurn(a.turns); turn != nil {
		if turn.stream != nil {
			turn.reply = turn.stream.text()
		}
		turn.stream, turn.partial = nil, ""
		turn.request, turn.notice = nil, uiText("请求已停止")
	}
}

func (a *aiAssistant) conversationMessages() []aiMessage {
	messages := make([]aiMessage, 0, len(a.turns)*2+1)
	for _, turn := range a.turns {
		if turn.reply == "" || turn.failed || turn.notice == "请求已停止" {
			continue
		}
		messages = append(messages,
			aiMessage{Role: "user", Content: turn.prompt},
			aiMessage{Role: "assistant", Content: turn.reply})
	}
	return messages
}

func (h aiConversation) title() string {
	if h.name != "" {
		return h.name
	}
	title := h.draft
	if len(h.turns) > 0 {
		title = h.turns[0].prompt
	}
	line, _, _ := strings.Cut(strings.TrimSpace(title), "\n")
	if line == "" {
		return "未发送的对话"
	}
	if chars := []rune(line); len(chars) > 28 {
		return string(chars[:28]) + "…"
	}
	return line
}
