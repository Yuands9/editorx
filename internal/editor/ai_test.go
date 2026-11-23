package editor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/text"
)

func TestAISelectionPreviewAndUndo(t *testing.T) {
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	const source = "私有前文 中文😀待修改 私有后文"
	tab := m.addTab(document{saved: source, eol: "LF"}, "说明.md")
	tab.source.text.SetSelection(text.Range{Start: 5, End: 11})
	requests := make(chan []aiMessage, 1)
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Model    string
			Messages []aiMessage
		}
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" || json.NewDecoder(r.Body).Decode(&request) != nil || request.Model != "test-viva" {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		requests <- request.Messages
		reply := fmt.Sprintf(`{"changes":[{"id":%d,"text":"修改完成"}]}`, tab.id)
		writeTestAIResponse(w, reply)
	}))
	defer service.Close()

	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(s.ClickLabel("AI 助手"))
	check(s.ClickLabel("模型设置"))
	check(s.ClickLabel("新增配置"))
	check(s.ClickLabel("接口基址"))
	check(s.Type(service.URL))
	check(s.ClickLabel("模型 ID"))
	check(s.Type("test-viva"))
	check(s.ClickLabel("保存配置"))
	check(s.ClickLabel("关闭设置"))
	check(s.ClickLabel("选择 AI 模式"))
	check(s.ClickLabel("menuitem:生成修改"))
	check(s.ClickLabel("AI 指令"))
	check(s.Type("润色选中的文字"))
	check(s.ClickLabel("发送给模型"))
	deadline := time.Now().Add(5 * time.Second)
	for m.ai.responding() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
		check(s.Frame())
	}
	select {
	case sent := <-requests:
		if len(sent) != 2 || !strings.Contains(sent[1].Content, "中文😀待修改") || strings.Contains(sent[1].Content, "私有") || !strings.Contains(sent[1].Content, "润色选中的文字") {
			t.Fatalf("unexpected model context: %v", sent)
		}
	default:
		t.Fatal("the UI did not send a model request: " + m.ai.message)
	}
	if len(m.ai.turns) != 1 || len(m.ai.turns[0].changes) != 1 || tab.source.text.String() != source {
		t.Fatal("model response did not become a pending change or modified the file before approval: " + m.ai.message)
	}
	check(s.ClickLabel("查看差异 · 说明.md"))
	if _, err := s.Locate("原文和建议差异"); err != nil {
		t.Fatal(err)
	}
	check(s.ClickLabel("应用修改"))
	if tab.source.text.String() != "私有前文 修改完成 私有后文" || !tab.modified() || m.active != tab {
		t.Fatal("apply changed text outside the selection or left the wrong active tab")
	}
	check(s.Keys("cmd+z"))
	if tab.source.text.String() != source || tab.modified() {
		t.Fatal("one undo did not restore the original text")
	}
}

func TestAIComposerSelectsModelsWithinCurrentProfile(t *testing.T) {
	requests := make(chan string, 4)
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/models":
			requests <- "list"
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":[{"id":"saved-model"},{"id":"service-fast"},{"id":"service-pro"}]}`)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/chat/completions":
			var request struct{ Model string }
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				http.Error(w, "invalid request", http.StatusBadRequest)
				return
			}
			requests <- request.Model
			writeTestAIResponse(w, "回复")
		default:
			http.NotFound(w, r)
		}
	}))
	defer service.Close()

	m := newTestEditorWithDocument()
	m.dataDirectory = t.TempDir()
	profiles := aiProfiles{DefaultID: 1, Profiles: []aiSettings{{
		ID: 1, Name: "本地模型", Provider: "ollama", BaseURL: service.URL, Protocol: aiChat, Model: "saved-model",
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
	expectRequest := func(want string) {
		t.Helper()
		select {
		case got := <-requests:
			if got != want {
				t.Fatalf("request = %q, want %q", got, want)
			}
		default:
			t.Fatalf("missing request %q", want)
		}
	}
	wait := func(pending func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for pending() && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
			check(s.Frame())
		}
		if pending() {
			t.Fatal("model request did not finish")
		}
	}
	click := func(label string) { check(s.ClickLabel(label)) }
	click("AI 助手")
	click("AI 指令")
	const draft = "请解释这条配置"
	check(s.Type(draft))
	click("切换模型配置")
	if len(requests) != 0 {
		t.Fatal("opening the picker made a request")
	}
	click("获取模型")
	wait(func() bool { return m.ai.modelPicker.list(m.ai.profiles.active(1)).request != nil })
	expectRequest("list")
	click("选择模型 service-fast")
	if m.ai.profileMenu || m.ai.prompt.Text() != draft || !s.HasText("service-fast") {
		t.Fatal("selecting the model did not update the composer or preserve the draft")
	}
	click("发送给模型")
	wait(func() bool { return m.ai.responding() })
	expectRequest("service-fast")

	click("切换模型配置")
	click("手动输入 ID")
	check(s.Type("manual-model"))
	check(s.Keys("enter"))
	if m.ai.profileMenu || m.ai.selectedModel(m.ai.profiles.active(1)) != "manual-model" {
		t.Fatal("manual model ID was not confirmed on Enter")
	}
	check(s.Type("请继续"))
	click("发送给模型")
	wait(func() bool { return m.ai.responding() })
	expectRequest("manual-model")

	click("切换模型配置")
	click("选择配置 本地模型")
	settings, err := readAIProfiles(m.dataDirectory)
	check(err)
	if m.ai.selectedModel(m.ai.profiles.active(1)) != "saved-model" || settings.active(0).Model != "saved-model" {
		t.Fatal("switching changed the saved model or prevented returning to it")
	}
}

func TestAIMultiFileScopeAndValidation(t *testing.T) {
	m := newEditorApp()
	config := m.addTab(document{saved: `{"name":"旧值"}`, eol: "LF"}, "配置.json")
	page := m.addTab(document{saved: "<html><p>旧值</p></html>", eol: "LF"}, "页面.html")
	m.ai.attachments = []aiAttachment{{tabID: config.id}, {tabID: page.id}}
	snapshot, err := m.captureAIContext()
	if err != nil {
		t.Fatal(err)
	}
	targets, err := snapshot.targets(context.Background())
	if err != nil || len(targets) != 2 || targets[0].file.Text != config.doc.saved || targets[1].file.Text != page.doc.saved {
		t.Fatalf("explicit scope: %+v, %v", targets, err)
	}
	reply := fmt.Sprintf(`{"changes":[{"id":%d,"text":"不合法"},{"id":%d,"text":"<html><p>新值</p></html>"}]}`, config.id, page.id)
	result, err := parseAIChanges(reply, aiEdit, targets)
	if err != nil || len(result.changes) != 2 || result.changes[0].error == "" || result.changes[1].error != "" {
		t.Fatalf("unexpected parsed edits: %+v, %v", result.changes, err)
	}
	if err = m.applyAIChange(&result.changes[0]); err == nil || config.modified() {
		t.Fatal("an invalid JSON result was applied")
	}
	page.source.replace("<html><p>手改</p></html>")
	if err = m.applyAIChange(&result.changes[1]); err == nil || page.source.text.String() != "<html><p>手改</p></html>" {
		t.Fatal("an old suggestion overwrote a newer source edit")
	}
	page.source.replace(page.doc.saved)
	if err = m.applyAIChange(&result.changes[1]); err != nil || page.source.text.String() != "<html><p>新值</p></html>" || config.modified() {
		t.Fatal("applying the HTML edit did not stay within its file")
	}
	for _, reply := range []string{
		`{"changes":[{"id":0,"text":"unauthorized"}]}`,
		`{"changes":[{"text":"no id"}]}`,
		`{"changes":[{"id":2,"text":"one"},{"id":2,"text":"duplicate"}]}`,
	} {
		if _, err := parseAIChanges(reply, aiEdit, targets); err == nil {
			t.Fatalf("accepted invalid proposal %s", reply)
		}
	}
	if err := validateAIChange(aiTarget{file: aiFileContext{Format: "XML"}}, "<a/><b/>"); err == nil {
		t.Fatal("accepted XML with two root elements")
	}
}

func TestAIDraftAndEncryptedModelSettings(t *testing.T) {
	m := newTestEditorWithDocument()
	original := m.active
	result, err := parseAIChanges(`{"changes":[{"id":0,"text":"# 会议纪要\n\n- 下一步"}]}`, aiDraft, nil)
	if err != nil || len(result.changes) != 1 {
		t.Fatalf("draft: %v", err)
	}
	if err = m.applyAIChange(&result.changes[0]); err != nil || m.active == original || !m.active.modified() || m.active.format() != "Markdown" || original.modified() {
		t.Fatal("draft did not appear in a new unsaved Markdown tab")
	}
	m.dataDirectory = t.TempDir()
	m.openAISettings()
	m.ai.settings.baseURL.SetText("https://example.test/v1")
	m.ai.settings.model.SetText("sample")
	const token = "fake-test-credential"
	m.ai.settings.key.SetText(token)
	m.saveAISettings()
	if m.ai.settings.original.ID == 0 {
		t.Fatal(m.ai.settings.problem)
	}
	data, err := os.ReadFile(filepath.Join(m.dataDirectory, "ai-settings.json"))
	if err != nil || strings.Contains(string(data), token) {
		t.Fatalf("could not read encrypted settings or found plaintext key: %v", err)
	}
	profiles, err := readAIProfiles(m.dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	decrypted, err := windowsAIKey(profiles.active(0).Key, false)
	if err != nil || string(decrypted) != token {
		t.Fatal("saved API key did not round-trip through Windows encryption")
	}
	m.openAISettings()
	m.ai.settings.baseURL.SetText("https://different.test/v1")
	m.saveAISettings()
	if !m.appSettings.open || m.appSettings.section != settingsAI || m.ai.settings.problem == "" {
		t.Fatal("old credentials were reused with a different endpoint")
	}
}

func writeTestAIResponse(w http.ResponseWriter, reply string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"choices": []map[string]any{{"message": aiMessage{Role: "assistant", Content: reply}, "finish_reason": "stop"}},
	})
}
