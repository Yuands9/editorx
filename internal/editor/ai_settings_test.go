package editor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
)

func TestAIProfileManagementAndConnectionProbe(t *testing.T) {
	m := newTestEditorWithDocument()
	m.dataDirectory = t.TempDir()
	oldKey, err := windowsAIKey([]byte("legacy-test-key"), true)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := json.Marshal(map[string]any{
		"base_url": "https://example.test/v1", "model": "old-model", "encrypted_key": oldKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(m.dataDirectory, "ai-settings.json")
	if err = os.WriteFile(path, legacy, 0600); err != nil {
		t.Fatal(err)
	}

	requests := make(chan string, 8)
	const key = "fake-connection-test-key"
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+key {
			http.Error(w, "no authorization", http.StatusUnauthorized)
			return
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/models":
			requests <- "models"
			fmt.Fprint(w, `{"data":[{"id":"case-z"},{"id":"case-chat"},{"id":"case-chat"}]}`)
		case "POST /v1/chat/completions":
			var body struct {
				Model    string
				Messages []aiMessage
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || body.Model != "case-chat" || len(body.Messages) != 2 || body.Messages[1].Content != "Reply with OK." {
				http.Error(w, "not an isolated probe", http.StatusBadRequest)
				return
			}
			requests <- "probe"
			fmt.Fprint(w, `{"model":"case-chat-routed","choices":[{"message":{"content":"OK"},"finish_reason":"stop"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()

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
	waitProbe := func() {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for m.ai.settings.probe != nil && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
			check(s.Frame())
		}
		if m.ai.settings.probe != nil || m.ai.settings.problem != "" {
			t.Fatal("settings probe did not succeed: " + m.ai.settings.problem)
		}
	}
	check(s.Resize(900, 560))
	click("AI 助手")
	click("模型设置")
	if m.ai.settings.original.Model != "old-model" || !slices.Equal(m.ai.settings.original.Key, oldKey) {
		t.Fatal("opening the settings lost the existing default or encrypted key")
	}
	click("新增配置")
	click("combobox:服务商预设")
	click("option:DeepSeek")
	if m.ai.settings.baseURL.Text() != "https://api.deepseek.com/v1" || m.ai.settings.protocol != aiChat {
		t.Fatal("selecting a provider did not populate the preset")
	}
	click("配置名称")
	check(s.Keys("home"))
	check(s.Keys("shift+end"))
	check(s.Type("本机验收模型"))
	click("接口基址")
	check(s.Keys("home"))
	check(s.Keys("shift+end"))
	check(s.Type(provider.URL + "/v1/chat/completions"))
	click("API 密钥")
	check(s.Type(key))
	click("button:编辑器")
	if m.appSettings.section != settingsEditor {
		t.Fatal("model form prevented navigation to another settings section")
	}
	drafts, err := readAIProfiles(m.dataDirectory)
	check(err)
	if len(drafts.Profiles) != 1 {
		t.Fatal("leaving the form unexpectedly saved an incomplete configuration")
	}
	click("button:AI")
	if !m.ai.settingsEditing || m.ai.settings.name.Text() != "本机验收模型" || m.ai.settings.enteredKey() != key {
		t.Fatal("navigation lost an unsaved model configuration")
	}
	click("获取模型")
	waitProbe()
	if !slices.Equal(m.ai.settings.models, []string{"case-chat", "case-z"}) {
		t.Fatal("discovery did not return a unique selectable model list")
	}
	click("选择模型")
	click("menuitem:case-chat")
	click("测试连接")
	waitProbe()
	if !strings.Contains(m.ai.settings.message, "连接成功") || !strings.Contains(m.ai.settings.message, "case-chat-routed") {
		t.Fatal("probe did not display the resolved model")
	}
	click("保存配置")
	if m.ai.settings.original.ID == 0 || m.ai.settings.problem != "" {
		t.Fatal("new profile could not be saved: " + m.ai.settings.problem)
	}
	click("设为默认 本机验收模型")
	check(s.Resize(1180, 740))
	click("combobox:对话字号")
	click("option:16 px")
	click("combobox:新对话的默认模式")
	click("option:新建草稿")
	click("编辑配置 本机验收模型")
	if _, err := s.Locate("combobox:对话字号"); err == nil {
		t.Fatal("global AI preferences appeared inside a provider profile")
	}
	field, err := s.Locate("API 密钥")
	check(err)
	eye, err := s.Locate("button:显示密钥")
	check(err)
	if !m.ai.settings.key.Secure || eye.Bounds.X < field.Bounds.X || eye.Bounds.X+eye.Bounds.Width > field.Bounds.X+field.Bounds.Width {
		t.Fatal("the saved key toggle is outside the masked input")
	}
	if _, err := s.Locate("button:复制 API 密钥"); err == nil {
		t.Fatal("the hidden key offered a copy action")
	}
	clearKey, err := s.Locate("清除已保存的密钥")
	check(err)
	click("API 密钥")
	check(s.Driver().Key(gfx.KeyTab, 0))
	if s.Driver().Snapshot().Focused != clearKey.ID {
		t.Fatal("Tab from the API key did not skip the visibility button")
	}
	check(s.Driver().Key(gfx.KeyTab, gfx.ModifierShift))
	if s.Driver().Snapshot().Focused != field.ID {
		t.Fatal("Shift+Tab did not return directly to the API key")
	}
	click("button:显示密钥")
	if m.ai.settings.key.Secure || m.ai.settings.key.Text() != key || m.ai.settings.dirty() {
		t.Fatal("revealing the saved key changed the configuration")
	}
	field, err = s.Locate("API 密钥")
	check(err)
	if !paintedTextInBounds(s.Snapshot().Texts, key, field.Bounds) {
		t.Fatal("the revealed key was not painted inside its input")
	}
	click("button:复制 API 密钥")
	if s.Driver().Clipboard() != key {
		t.Fatal("the copy button did not copy the saved key")
	}

	s.Driver().SetClipboard("copy-before-shortcut")
	click("API 密钥")
	handled, err := s.Driver().ShortcutChord(gfx.ModifierControl, 'a')
	check(err)
	selection := m.ai.settings.key.GetEditor().Selection()
	if !handled || selection.Start != 0 || selection.End != len(key) {
		t.Fatal("Ctrl+A did not select the saved key")
	}
	handled, err = s.Driver().ShortcutChord(gfx.ModifierControl, 'c')
	check(err)
	if !handled || s.Driver().Clipboard() != key || m.ai.settings.dirty() {
		t.Fatal("Ctrl+C did not copy the saved key without editing the configuration")
	}

	click("button:隐藏密钥")
	if !m.ai.settings.key.Secure || m.ai.settings.key.Text() != "" {
		t.Fatal("hiding the key did not clear the revealed value")
	}
	if _, err := s.Locate("button:复制 API 密钥"); err == nil {
		t.Fatal("the copy action remained visible after hiding the key")
	}
	click("button:模型配置")
	check(s.Resize(900, 560))
	directory := os.Getenv("VIVA_PREVIEW_DIR")
	if directory != "" {
		check(s.Screenshot(filepath.Join(directory, "ai-configurations-small.png")))
		check(s.Resize(1440, 900))
		check(s.Screenshot(filepath.Join(directory, "ai-configurations-dark.png")))
		m.setLightMode(true)
		check(s.Frame())
		check(s.Screenshot(filepath.Join(directory, "ai-configurations-light.png")))
		m.setLightMode(false)
		check(s.Resize(900, 560))
	}
	click("编辑配置 本机验收模型")
	modelRow, err := s.Locate("模型 ID")
	check(err)
	click("高级设置")
	check(s.Scroll(gfx.ScrollEvent{X: modelRow.Bounds.X + 80, Y: modelRow.Bounds.Y + 12, DeltaY: 440, Precise: true}))
	click("switch:网络直连")
	if !m.ai.settings.direct {
		t.Fatal("the network switch did not enable direct connections")
	}
	click("自定义请求头 · JSON")
	check(s.Type(`{"X-Draft":"uncommitted"}`))
	headers, err := s.Locate("自定义请求头 · JSON")
	check(err)
	save, err := s.Locate("保存配置")
	check(err)
	if headers.Bounds.Y <= save.Bounds.Y+save.Bounds.Height {
		t.Fatal("advanced fields overlap the header actions at minimum window size")
	}
	if directory != "" {
		check(s.Resize(1440, 900))
		check(s.Screenshot(filepath.Join(directory, "ai-profile-edit-advanced.png")))
	}
	click("关闭设置")
	profiles, err := readAIProfiles(m.dataDirectory)
	check(err)
	if len(profiles.Profiles) != 2 || profiles.active(0).Name != "本机验收模型" || !slices.Equal(profiles.Profiles[0].Key, oldKey) ||
		len(profiles.active(0).Headers) != 0 || profiles.active(0).Direct ||
		profiles.ChatFont != 16 || profiles.DefaultMode != aiDraft {
		t.Fatal("new configuration or updated default overwrote the migrated configuration")
	}
	disk, err := os.ReadFile(path)
	check(err)
	if strings.Contains(string(disk), key) || strings.Contains(string(disk), "legacy-test-key") {
		t.Fatal("API key persisted as plaintext")
	}
	if len(requests) != 2 || <-requests != "models" || <-requests != "probe" {
		t.Fatal("expected discovery and a short connection test")
	}

	click("切换模型配置")
	modelOption, err := s.Locate("选择配置 原有配置")
	check(err)
	if directory != "" {
		check(s.Screenshot(filepath.Join(directory, "ai-picker-migrated-profile.png")))
	}
	click("选择配置 原有配置")
	if m.ai.profileID != profiles.Profiles[0].ID {
		t.Fatalf("sidebar did not switch the current window to the chosen profile: open=%t, selected=%d, expected=%d, row=%+v", m.ai.profileMenu, m.ai.profileID, profiles.Profiles[0].ID, modelOption.Bounds)
	}
	click("模型设置")
	if m.ai.settings.headers.Text() != `{"X-Draft":"uncommitted"}` {
		t.Fatal("closing settings lost the unsaved profile")
	}
	click("button:模型配置")
	click("放弃修改")
	click("删除配置 原有配置")
	click("取消删除")
	if len(m.ai.profiles.Profiles) != 2 {
		t.Fatal("canceling deletion removed a profile")
	}
	click("删除配置 原有配置")
	click("确认删除配置")
	if len(m.ai.profiles.Profiles) != 1 || m.ai.profiles.active(m.ai.profileID).Name != "本机验收模型" {
		t.Fatal("deleting the selected profile did not fall back to the default")
	}
}

func TestAIProtocolRequestsAndEncryptedHeaders(t *testing.T) {
	type sample struct {
		protocol                                      aiProtocol
		authHeader, auth                              string
		requestField, effortField, effortValue, reply string
	}
	for _, sample := range []sample{
		{aiChat, "Authorization", "Bearer test-key", "messages", "reasoning_effort", `"high"`, `{"choices":[{"message":{"content":"中文 OK"},"finish_reason":"stop"}]}`},
		{aiResponses, "Authorization", "Bearer test-key", "input", "reasoning", `{"effort":"high"}`, `{"status":"completed","output":[{"type":"reasoning"},{"type":"message","content":[{"type":"output_text","text":"中文 OK"}]}]}`},
		{aiMessages, "X-Api-Key", "test-key", "system", "output_config", `{"effort":"high"}`, `{"content":[{"type":"thinking"},{"type":"text","text":"中文 OK"}],"stop_reason":"end_turn"}`},
	} {
		t.Run(string(sample.protocol), func(t *testing.T) {
			key, err := windowsAIKey([]byte("test-key"), true)
			if err != nil {
				t.Fatal(err)
			}
			headers, err := windowsAIKey([]byte(`{"X-Test-Tenant":"isolated"}`), true)
			if err != nil {
				t.Fatal(err)
			}
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]json.RawMessage
				if r.Method != http.MethodPost || r.URL.Path != "/gateway/"+string(sample.protocol) ||
					r.Header.Get(sample.authHeader) != sample.auth || r.Header.Get("X-Test-Tenant") != "isolated" ||
					json.NewDecoder(r.Body).Decode(&body) != nil || len(body[sample.requestField]) == 0 || string(body[sample.effortField]) != sample.effortValue {
					http.Error(w, "unexpected request", http.StatusBadRequest)
					return
				}
				field := "messages"
				if sample.protocol == aiResponses {
					field = "input"
				}
				var history []aiMessage
				if err := json.Unmarshal(body[field], &history); err != nil || len(history) < 3 ||
					history[len(history)-2] != (aiMessage{Role: "assistant", Content: "前一轮的答复"}) ||
					history[len(history)-1] != (aiMessage{Role: "user", Content: "测试请求"}) {
					http.Error(w, "missing conversation messages", http.StatusBadRequest)
					return
				}
				if sample.protocol == aiMessages && (r.Header.Get("Anthropic-Version") == "" || len(body["max_tokens"]) == 0) {
					http.Error(w, "missing Anthropic headers or required limit", http.StatusBadRequest)
					return
				}
				fmt.Fprint(w, sample.reply)
			}))
			defer provider.Close()
			profile := aiSettings{BaseURL: provider.URL + "/gateway", Protocol: sample.protocol, Model: "case-model", Key: key, Headers: headers, Direct: true}
			messages := []aiMessage{{Role: "user", Content: "前一轮的问题"}, {Role: "assistant", Content: "前一轮的答复"}, {Role: "user", Content: "测试请求"}}
			answer, err := profile.complete(context.Background(), "要求", messages, aiReasoningHigh, nil)
			if err != nil || answer != "中文 OK" {
				t.Fatalf("unexpected protocol response: %q, %v", answer, err)
			}
		})
	}
	for _, source := range []string{`{"Accept":"text/html"}`, `{"X-Example":"bad\r\nvalue"}`, `{"Bad Header":"a"}`} {
		if _, err := parseAIHeaders(source); err == nil {
			t.Fatal("accepted an invalid or reserved HTTP header")
		}
	}
}

func TestAISavedSettingsAreNotOverwrittenByAnotherWindow(t *testing.T) {
	directory := t.TempDir()
	m := newEditorApp()
	m.dataDirectory = directory
	m.openAISettings()
	m.ai.settings.name.SetText("配置一")
	m.ai.settings.baseURL.SetText("https://example.test/v1")
	m.ai.settings.model.SetText("first")
	m.saveAISettings()
	if m.ai.settings.original.ID == 0 {
		t.Fatal(m.ai.settings.problem)
	}
	other := newEditorApp()
	other.dataDirectory = directory
	other.openAISettings()
	other.ai.settings.model.SetText("updated")
	other.saveAISettings()
	m.ai.settings.model.SetText("stale")
	m.saveAISettings()
	if !strings.Contains(m.ai.settings.problem, "其他窗口修改") {
		t.Fatal("a stale editor silently overwrote a saved update")
	}
	profiles, err := readAIProfiles(directory)
	if err != nil || profiles.active(0).Model != "updated" {
		t.Fatal("other window's update was lost")
	}
}

// Changing providers introduced a suffix on the autosuggested label. It must
// still behave as an automatic name through subsequent provider/model changes.
func TestAINewProfileNameTracksModelUntilCustomized(t *testing.T) {
	m := newTestEditorWithDocument()
	m.dataDirectory = t.TempDir()
	profiles := aiProfiles{DefaultID: 1, Profiles: []aiSettings{
		{ID: 1, Name: "DeepSeek", Provider: "deepseek", Model: "old", BaseURL: "https://example.test/v1", Protocol: aiChat},
		{ID: 2, Name: "deepseek-chat", Provider: "deepseek", Model: "deepseek-chat", BaseURL: "https://example.test/v1", Protocol: aiChat},
	}}
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
	preset := func(label string) {
		t.Helper()
		click("combobox:服务商预设")
		if output := os.Getenv("VIVA_PREVIEW_DIR"); output != "" {
			check(s.Screenshot(filepath.Join(output, "ai-provider-presets.png")))
		}
		click("option:" + label)
	}
	replace := func(label, value string) {
		t.Helper()
		click(label)
		check(s.Keys("cmd+a"))
		check(s.Type(value))
		check(s.Frame())
	}
	click("AI 助手")
	click("模型设置")
	click("新增配置")
	preset("DeepSeek")
	e := &m.ai.settings
	if e.name.Text() != "DeepSeek 2" {
		t.Fatalf("expected the available provider name, got %q", e.name.Text())
	}
	preset("OpenAI")
	if e.name.Text() != "OpenAI" {
		t.Fatalf("name remained stale after changing providers: %q", e.name.Text())
	}
	preset("DeepSeek")
	replace("模型 ID", "deepseek-chat")
	if e.name.Text() != "deepseek-chat 2" {
		t.Fatalf("model name or duplicate handling did not update: %q", e.name.Text())
	}
	replace("模型 ID", "deepseek-reasoner")
	if e.name.Text() != "deepseek-reasoner" {
		t.Fatalf("automatic name did not follow the next model: %q", e.name.Text())
	}
	replace("配置名称", "日常代码助手")
	replace("模型 ID", "deepseek-chat")
	if e.name.Text() != "日常代码助手" {
		t.Fatalf("custom name was overwritten: %q", e.name.Text())
	}
	check(s.Keys("enter"))
	check(s.Frame())
	if m.ai.settingsEditing {
		t.Fatal("Enter did not close the saved configuration form")
	}
	persisted, err := readAIProfiles(m.dataDirectory)
	check(err)
	if len(persisted.Profiles) != 3 || persisted.Profiles[2].Name != "日常代码助手" || persisted.Profiles[2].Model != "deepseek-chat" {
		t.Fatal("new configuration did not persist the current name and model")
	}
}
