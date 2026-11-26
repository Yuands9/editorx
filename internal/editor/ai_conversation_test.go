package editor

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAIConversationFromTablessHome(t *testing.T) {
	const reply = "可以。下面是 **配置示例**\n\n- 先核对环境变量。\n- 修改后保存文件。\n\n```json\n{\"enabled\":true}\n```"
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []aiMessage
		}
		if r.URL.Path != "/v1/chat/completions" || json.NewDecoder(r.Body).Decode(&request) != nil || len(request.Messages) != 2 ||
			!strings.Contains(request.Messages[1].Content, "配置怎么写") || !strings.Contains(request.Messages[1].Content, "\"files\":[]") {
			http.Error(w, "unexpected conversation request", http.StatusBadRequest)
			return
		}
		writeTestAIResponse(w, reply)
	}))
	defer service.Close()

	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	profiles := aiProfiles{DefaultID: 1, Profiles: []aiSettings{{
		ID: 1, Name: "本地测试", Provider: "ollama", BaseURL: service.URL + "/v1", Model: "demo-editor", Protocol: aiChat,
	}}}
	if err := profiles.save(m.dataDirectory); err != nil {
		t.Fatal(err)
	}
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	click := func(label string) {
		t.Helper()
		check(s.ClickLabel(label))
		check(s.Frame())
	}
	check(s.Resize(900, 560))
	click("主菜单")
	click("menuitem:工具与模板")
	click("menuitem:AI 助手")
	if !m.ai.open || m.active != nil {
		t.Fatal("opening AI from home created a tab")
	}
	if output := os.Getenv("VIVA_PREVIEW_DIR"); output != "" {
		check(s.Screenshot(filepath.Join(output, "ai-conversation-empty.png")))
	}
	click("AI 指令")
	check(s.Type("配置怎么写"))
	click("发送给模型")
	deadline := time.Now().Add(5 * time.Second)
	for m.ai.responding() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
		check(s.Frame())
	}
	if m.ai.responding() || len(m.ai.turns) != 1 || m.ai.turns[0].reply != reply || m.ai.turns[0].notice != "" || m.ai.turns[0].answerBody.markdown == nil || len(m.ai.turns[0].answerBody.markdown.view.Codes()) != 1 {
		t.Fatal("AI answer did not become a formatted conversation turn")
	}
	click("复制问题")
	if s.Driver().Clipboard() != "配置怎么写" {
		t.Fatal("copy did not preserve the sent question")
	}
	click("复制回复")
	if s.Driver().Clipboard() != reply {
		t.Fatal("copy changed the model's reply")
	}
	if output := os.Getenv("VIVA_PREVIEW_DIR"); output != "" {
		check(s.Resize(1440, 900))
		check(s.Screenshot(filepath.Join(output, "ai-conversation-dark.png")))
		m.setLightMode(true)
		check(s.Frame())
		check(s.Screenshot(filepath.Join(output, "ai-conversation-light.png")))
	}
	click("新对话")
	if len(m.ai.turns) != 0 || len(m.ai.history) != 1 || m.ai.prompt.Text() != "" {
		t.Fatal("new conversation lost the previous turn or kept its draft")
	}
	click("对话记录")
	click("配置怎么写")
	if len(m.ai.turns) != 1 || m.ai.turns[0].reply != reply {
		t.Fatal("conversation history did not restore the original reply")
	}
	tab := m.addTab(document{saved: "{}"}, "配置.json")
	check(s.Frame())
	click("添加文件上下文")
	click("checkbox:配置.json")
	click("完成选择")
	if len(m.ai.attachments) != 1 || m.ai.attachments[0].tabID != tab.id {
		t.Fatal("composer did not attach the chosen open file")
	}
	click("移除附件 配置.json")
	check(s.Keys("ctrl+w"))
	if m.active != nil || len(m.tabs) != 0 || !m.ai.open || len(m.ai.turns) != 1 {
		t.Fatal("closing the last tab broke the open AI conversation")
	}
}

func TestAINewConversationWhileRepliesArePending(t *testing.T) {
	const firstQuestion = "解释第一条问题\n附加说明：配置😀"
	const secondQuestion = "新对话里的问题"
	const firstReply, secondReply = "第一条回复", "第二条回复"
	releaseFirst, releaseSecond, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	requests := make(chan string, 2)
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct{ Messages []aiMessage }
		var request aiContextRequest
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" || json.NewDecoder(r.Body).Decode(&payload) != nil ||
			len(payload.Messages) != 2 || json.Unmarshal([]byte(payload.Messages[1].Content), &request) != nil {
			http.Error(w, "invalid conversation request", http.StatusBadRequest)
			return
		}
		var ready <-chan struct{}
		var reply string
		switch request.Instruction {
		case firstQuestion:
			ready, reply = releaseFirst, firstReply
		case secondQuestion:
			ready, reply = releaseSecond, secondReply
		default:
			http.Error(w, "unexpected question", http.StatusBadRequest)
			return
		}
		requests <- request.Instruction
		select {
		case <-ready:
			writeTestAIResponse(w, reply)
		case <-finished:
		case <-r.Context().Done():
		}
	}))
	defer service.Close()
	defer close(finished)

	m := newTestEditorWithDocument()
	m.dataDirectory = t.TempDir()
	profiles := aiProfiles{DefaultID: 1, Profiles: []aiSettings{{
		ID: 1, Name: "本地验收", Provider: "ollama", BaseURL: service.URL, Protocol: aiChat, Model: "demo-editor",
	}}}
	if err := profiles.save(m.dataDirectory); err != nil {
		t.Fatal(err)
	}

	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	click := func(label string) {
		t.Helper()
		check(s.ClickLabel(label))
	}
	send := func(question string) *aiTurn {
		t.Helper()
		click("AI 指令")
		check(s.Type(question))
		click("发送给模型")
		select {
		case got := <-requests:
			if got != question {
				t.Fatalf("request = %q, want %q", got, question)
			}
		case <-time.After(4 * time.Second):
			t.Fatalf("question did not reach local model: %q", question)
		}
		turn := aiPendingTurn(m.ai.turns)
		if turn == nil {
			t.Fatal("the request is missing from its active conversation")
		}
		return turn
	}
	waitForReply := func(turn *aiTurn, want string) {
		t.Helper()
		deadline := time.Now().Add(4 * time.Second)
		for turn.request != nil && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
			check(s.Frame())
		}
		if turn.request != nil || turn.reply != want || turn.failed {
			t.Fatalf("reply = %q, want %q (status %s)", turn.reply, want, turn.notice)
		}
	}

	check(s.Resize(1100, 690))
	click("AI 助手")
	first := send(firstQuestion)
	click("复制问题")
	if s.Driver().Clipboard() != firstQuestion {
		t.Fatal("copy did not preserve the full question during the request")
	}
	click("新对话")
	if len(m.ai.turns) != 0 || len(m.ai.history) != 1 || aiPendingTurn(m.ai.history[0].turns) != first || m.ai.prompt.Text() != "" {
		t.Fatal("new conversation did not park the original request and reset the composer")
	}
	second := send(secondQuestion)
	if first.request == nil {
		t.Fatal("sending the second question stopped the first request")
	}

	click("对话记录")
	if output := os.Getenv("VIVA_PREVIEW_DIR"); output != "" {
		check(s.Screenshot(filepath.Join(output, "ai-conversations-in-progress.png")))
	}
	click("解释第一条问题")
	if aiPendingTurn(m.ai.turns) != first || len(m.ai.history) != 1 || aiPendingTurn(m.ai.history[0].turns) != second {
		t.Fatal("switching history while both requests were pending lost a conversation")
	}
	close(releaseSecond)
	waitForReply(second, secondReply)
	if aiPendingTurn(m.ai.turns) != first || first.reply != "" {
		t.Fatal("a background reply appeared in the active conversation")
	}
	click("对话记录")
	click(secondQuestion)
	if len(m.ai.turns) != 1 || m.ai.turns[0] != second || !s.HasText(secondReply) {
		t.Fatal("the reply could not be recovered from the second conversation")
	}
	click("AI 指令")
	const draft = "第二个对话的草稿"
	check(s.Type(draft))
	close(releaseFirst)
	waitForReply(first, firstReply)
	if m.ai.prompt.Text() != draft || len(m.ai.turns) != 1 || m.ai.turns[0] != second {
		t.Fatal("finishing the other request changed the current conversation or draft")
	}
	click("对话记录")
	click("解释第一条问题")
	if len(m.ai.turns) != 1 || m.ai.turns[0] != first || !s.HasText(firstReply) {
		t.Fatal("the original conversation lost its background reply")
	}
}

func TestAIHistoryEditingAndResumeAfterRestart(t *testing.T) {
	const originalQuestion = "部署遇到的问题"
	const originalReply = "请检查 JAVA_HOME 的配置。"
	const title = "部署排查记录"
	const followUp = "可以给我一个示例吗？"
	type requestCapture struct {
		Messages []aiMessage
		Effort   aiReasoning `json:"reasoning_effort"`
	}
	requests := make(chan requestCapture, 2)
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request requestCapture
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" || json.NewDecoder(r.Body).Decode(&request) != nil {
			http.Error(w, "unexpected continuation request", http.StatusBadRequest)
			return
		}
		requests <- request
		writeTestAIResponse(w, originalReply)
	}))
	defer service.Close()

	m := newTestEditorWithDocument()
	store, err := openSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.attach(m)
	profiles := aiProfiles{DefaultID: 1, Profiles: []aiSettings{{
		ID: 1, Name: "本机模型", Provider: "compatible", Model: "test-chat", Protocol: aiChat, BaseURL: service.URL,
	}}}
	if err = profiles.save(store.directory); err != nil {
		t.Fatal(err)
	}
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	click := func(label string) {
		t.Helper()
		check(s.ClickLabel(label))
	}
	waitRequest := func() requestCapture {
		t.Helper()
		deadline := time.Now().Add(4 * time.Second)
		for m.ai.responding() && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
			check(s.Frame())
		}
		select {
		case request := <-requests:
			return request
		default:
			t.Fatal("no request reached the local model")
			return requestCapture{}
		}
	}
	check(s.Resize(1140, 700))
	click("AI 助手")
	check(s.Type(originalQuestion))
	click("发送给模型")
	initial := waitRequest()
	if initial.Effort != "" || len(initial.Messages) != 2 || m.ai.turns[0].reply != originalReply {
		t.Fatal("first request did not use the provider default")
	}

	click("对话记录")
	click("重命名 " + originalQuestion)
	click("对话名称")
	check(s.Keys("home"))
	check(s.Keys("shift+end"))
	check(s.Type(title))
	check(s.Keys("enter"))
	if m.ai.name != title || m.ai.conversationEdit.open {
		t.Fatal("Enter did not rename the current conversation")
	}

	click("切换模型配置")
	click("思考水平")
	if output := os.Getenv("VIVA_PREVIEW_DIR"); output != "" {
		check(s.Screenshot(filepath.Join(output, "ai-reasoning-picker.png")))
	}
	click("radio:高")
	if m.ai.effort != aiReasoningHigh {
		t.Fatal("selecting high did not update the conversation")
	}
	check(s.Keys("escape"))
	click("新对话")
	check(s.Type("要移除的草稿"))
	click("新对话")
	click("对话记录")
	if output := os.Getenv("VIVA_PREVIEW_DIR"); output != "" {
		check(s.Screenshot(filepath.Join(output, "ai-conversation-history.png")))
	}
	click("删除 要移除的草稿")
	click("取消删除对话")
	if len(m.ai.history) != 2 {
		t.Fatal("canceling the delete changed history")
	}
	click("对话记录")
	click("删除 要移除的草稿")
	click("确认删除对话")
	if len(m.ai.history) != 1 || m.ai.history[0].name != title {
		t.Fatal("deleting history removed or changed the wrong conversation")
	}
	click("对话记录")
	click(title)
	check(s.Type(followUp))

	closed := false
	m.requestClose(func() { closed = true })
	if !closed {
		t.Fatal("window did not save the conversation on close")
	}
	restored, err := openSessionStore(store.directory)
	check(err)
	m = restored.windows[0]
	s = openTestSession(t, m)
	if !m.ai.open || m.ai.name != title || m.ai.effort != aiReasoningHigh || m.ai.prompt.Text() != followUp ||
		len(m.ai.history) != 0 || len(m.ai.turns) != 1 || m.ai.turns[0].reply != originalReply {
		t.Fatal("window did not restore the named conversation, reply, draft and effort")
	}
	click("发送给模型")
	continued := waitRequest()
	if continued.Effort != aiReasoningHigh || len(continued.Messages) != 4 ||
		continued.Messages[1] != (aiMessage{Role: "user", Content: originalQuestion}) ||
		continued.Messages[2] != (aiMessage{Role: "assistant", Content: originalReply}) ||
		!strings.Contains(continued.Messages[3].Content, followUp) {
		t.Fatalf("the resumed request lost its messages or effort: %+v", continued)
	}
}

func TestAIHistoryRestoresReviewAgainstOriginalDocument(t *testing.T) {
	m := newTestEditorWithDocument()
	store, err := openSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.attach(m)
	m.closeTab(m.active)
	const source = "APP_NAME=Viva\nPORT=8080\n"
	tab := m.addTab(document{saved: source, eol: "LF"}, ".env")
	target, err := aiTabTarget(tab, false)
	if err != nil {
		t.Fatal(err)
	}
	const replacement = "APP_NAME=Viva\nPORT=8081\n"
	response := fmt.Sprintf(`{"changes":[{"id":%d,"text":%q}]}`, tab.id, replacement)
	suggestion, err := parseAIChanges(response, aiEdit, []aiTarget{target})
	if err != nil {
		t.Fatal(err)
	}
	m.ai.mode, m.ai.open = aiEdit, true
	m.ai.turns = []*aiTurn{{prompt: "调整端口", changes: suggestion.changes}}
	m.ai.attachments = []aiAttachment{{tabID: tab.id}}
	if err = store.save(); err != nil {
		t.Fatal(err)
	}
	restored, err := openSessionStore(store.directory)
	if err != nil {
		t.Fatal(err)
	}
	next := restored.windows[0]
	if next.active == nil || next.active.id == tab.id || len(next.ai.attachments) != 1 ||
		next.ai.attachments[0].tabID != next.active.id {
		t.Fatal("the saved attachment was not remapped to the restored tab")
	}
	turn := next.ai.turns[0]
	next.ai.showReview(turn, 0)
	change := &turn.changes[0]
	if len(change.diff.Rows) == 0 || change.target.file.Text != source || next.applyAIChange(change) != nil ||
		next.active.source.text.String() != replacement {
		t.Fatal("the restored review could not apply against its original document")
	}
	if err = next.applyAIChange(change); err == nil {
		t.Fatal("the restored proposal applied a second time to changed content")
	}
}
