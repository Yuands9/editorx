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
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
)

func waitForPartialDocumentTranslation(t *testing.T, session *headless.Session, state *documentTranslationState, expected string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for state.request != nil && time.Now().Before(deadline) {
		if err := session.Frame(); err != nil {
			t.Fatal(err)
		}
		if state.request != nil && state.result.text.CommittedString() == expected {
			return
		}
		time.Sleep(8 * time.Millisecond)
	}
	t.Fatalf("no partial translation, pending=%t, result=%q, problem=%q",
		state.request != nil, state.result.text.CommittedString(), state.problem)
}

func TestDocumentTranslationStreamsInSplit(t *testing.T) {
	const heading = "# Greeting"
	const translatedHeading = "# 问候"
	const paragraph = "An ordinary paragraph about the project."
	const fence = "~~~go\nvar example = \"Greeting untouched\"\n~~~"
	article := heading + "\n\n" +
		strings.Repeat(paragraph+"\n\n", 245) +
		fence + "\n\n## Final section\n"
	translate := strings.NewReplacer(paragraph, "这是一段关于项目的说明。", "Final section", "最后一节")
	expected := translate.Replace(strings.Replace(article, heading, translatedHeading, 1))

	type request struct {
		Stream   bool
		Messages []aiMessage
	}
	requests := make(chan request, 16)
	finishFirst := make(chan struct{})
	canceled := make(chan struct{}, 1)
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var received request
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil || len(received.Messages) != 2 {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		requests <- received
		source := received.Messages[1].Content
		w.Header().Set("Content-Type", "text/event-stream")
		sendDelta := func(value string) {
			payload, _ := json.Marshal(map[string]any{
				"choices": []any{map[string]any{"delta": aiMessage{Content: value}}},
			})
			_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
			w.(http.Flusher).Flush()
		}
		if source == "A separate article to cancel." {
			sendDelta("已收到")
			<-r.Context().Done()
			canceled <- struct{}{}
			return
		}
		if rest, ok := strings.CutPrefix(source, heading); ok {
			sendDelta(translatedHeading)
			select {
			case <-finishFirst:
				source = rest
			case <-r.Context().Done():
				return
			}
		}
		if source != "" {
			sendDelta(translate.Replace(source))
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer service.Close()
	defer service.CloseClientConnections()

	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	project := t.TempDir()
	path := filepath.Join(project, "article.md")
	check(os.WriteFile(path, []byte(strings.Replace(article, "Final section", "Original section", 1)), 0600))
	m.addProjects(project)
	m.load(path)
	tab := m.active
	// The buffer contains a working edit absent from the file on disk.
	tab.source.replace(article)
	tab.markdownMode = "分屏"
	_, _, err := mergeTranslationLibrary(filepath.Join(project, ".editorx", translationLibraryFile), []translationEntry{
		{"en", "zh-cn", translationTerm, "Greeting", "问候", "项目内部备注"},
	})
	check(err)
	_, _, err = mergeTranslationLibrary(filepath.Join(m.dataDirectory, translationLibraryFile), []translationEntry{
		{"en", "zh-cn", translationTerm, "Greeting", "个人问好", ""},
		{"en", "zh-cn", translationTerm, "Unrelated", "无关词条", ""},
	})
	check(err)
	check((aiProfiles{DefaultID: 1, Profiles: []aiSettings{{
		ID: 1, Name: "测试流式服务", Provider: "custom", BaseURL: service.URL, Model: "test", Protocol: aiChat, Auth: aiAuthNone,
	}}}).save(m.dataDirectory))

	s := openTestSession(t, m)
	click := func(label string) { t.Helper(); check(s.ClickLabel("button:" + label)) }
	openTranslation := func() {
		t.Helper()
		if _, visible := s.AccessByRole(gfx.RoleButton, "整篇翻译"); visible {
			t.Fatal("whole document translation is still in the toolbar")
		}
		check(s.RightClickLabel("textfield:代码编辑区"))
		check(s.ClickLabel("menuitem:整篇翻译"))
	}
	openTranslation()
	state := tab.documentTranslation
	if state == nil || !state.open || state.request != nil || len(requests) != 0 {
		t.Fatal("opening the split should show the source without sending a request")
	}
	sourcePane, err := s.Locate("textfield:代码编辑区")
	check(err)
	click("AI 翻译整篇")
	waitForPartialDocumentTranslation(t, s, state, translatedHeading)
	resultPane, err := s.Locate("textfield:文章译文")
	check(err)
	if state.request == nil || sourcePane.Bounds.X >= resultPane.Bounds.X || state.preview != nil {
		t.Fatal("partial translation did not render in the right pane before completion")
	}

	other := m.addTab(document{saved: "A separate article to cancel.", eol: "LF"}, "cancel.txt")
	check(s.Frame())
	close(finishFirst)
	check(s.WaitFor(func(app.Snapshot) bool { return state.request == nil }, 5*time.Second))
	if state.problem != "" || state.preview == nil || state.preview.Source != expected || tab.source.text.CommittedString() != article ||
		other.documentTranslation != nil {
		t.Fatalf("article output lost data or left its tab: %q", state.problem)
	}
	for _, part := range documentTranslationParts(article, true) {
		if part.input == "" {
			continue
		}
		select {
		case got := <-requests:
			if !got.Stream || got.Messages[1].Content != part.input || len(part.input) > translationTextLimit {
				t.Fatalf("unexpected article request length or source: %d", len(got.Messages[1].Content))
			}
			instructions := got.Messages[0].Content
			if strings.HasPrefix(part.input, heading) &&
				(!strings.Contains(instructions, "问候") || strings.Contains(instructions, "个人问好") ||
					strings.Contains(instructions, "项目内部备注") || strings.Contains(instructions, "无关词条")) {
				t.Fatalf("wrong glossary sent: %s", instructions)
			}
		default:
			t.Fatal("one article part was never sent")
		}
	}
	if len(requests) != 0 {
		t.Fatal("Markdown code or other unexpected content was sent")
	}

	openTranslation()
	click("AI 翻译整篇")
	stopped := other.documentTranslation
	waitForPartialDocumentTranslation(t, s, stopped, "已收到")
	click("停止翻译")
	select {
	case <-canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("stopping translation did not cancel the service request")
	}
	if stopped.request != nil || stopped.problem != "翻译已停止" || stopped.result.text.CommittedString() != "已收到" {
		t.Fatal("stopping translation did not retain its partial output")
	}
	m.activate(tab)
	check(s.Frame())
	if preview := os.Getenv("VIVA_PREVIEW_DIR"); preview != "" {
		check(s.Screenshot(filepath.Join(preview, "document-translation.png")))
	}
	click("复制译文")
	check(s.Resize(900, 560))
	if _, err := s.Locate("button:AI 翻译整篇"); err != nil {
		t.Fatal(err)
	}
	click("关闭整篇翻译")
	if state.open || tab.markdownMode != "分屏" || tab.source.text.CommittedString() != article {
		t.Fatal("closing the split did not restore the original reading mode")
	}
}

func TestDocumentTranslationChunksPreserveBoundaries(t *testing.T) {
	content := "  " + strings.Repeat("本", 3500) + "\n\n~~~sh\nprintf 'unchanged'\n~~~\n\n" +
		"{large-" + strings.Repeat("a", translationTextLimit) + "} trailing words.\n"
	var rebuilt strings.Builder
	var translatedParts, literalParts int
	for _, part := range documentTranslationParts(content, true) {
		rebuilt.WriteString(part.before)
		rebuilt.WriteString(part.input)
		rebuilt.WriteString(part.after)
		if part.input != "" {
			translatedParts++
		}
		if strings.HasPrefix(part.before, "~~~") || strings.HasPrefix(part.before, "{large-") {
			literalParts++
		}
		if len(part.input) > translationTextLimit || !utf8.ValidString(part.input) {
			t.Fatal("a chunk exceeds the transport limit or splits a UTF-8 character")
		}
	}
	if rebuilt.String() != content || translatedParts < 3 || literalParts != 2 {
		t.Fatal("chunking lost whitespace, cut a Markdown fence or split a placeholder")
	}
}
