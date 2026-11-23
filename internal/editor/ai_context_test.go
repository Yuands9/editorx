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
	"unicode/utf8"

	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/text"
)

type aiContextRequest struct {
	Instruction string
	Files       []aiFileContext
	Folders     []string
}

func serveAIContextTest(t *testing.T, m *editorApp) <-chan aiContextRequest {
	t.Helper()
	requests := make(chan aiContextRequest, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct{ Messages []aiMessage }
		var request aiContextRequest
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" || json.NewDecoder(r.Body).Decode(&payload) != nil ||
			len(payload.Messages) < 2 || json.Unmarshal([]byte(payload.Messages[len(payload.Messages)-1].Content), &request) != nil {
			http.Error(w, "invalid context request", http.StatusBadRequest)
			return
		}
		requests <- request
		writeTestAIResponse(w, "已收到文件上下文")
	}))
	t.Cleanup(server.Close)
	profiles := aiProfiles{DefaultID: 1, Profiles: []aiSettings{{
		ID: 1, Name: "本地验收", Provider: "compatible", BaseURL: server.URL, Model: "test-context", Protocol: aiChat,
	}}}
	if err := profiles.save(m.dataDirectory); err != nil {
		t.Fatal(err)
	}
	return requests
}

func waitForAIContext(t *testing.T, m *editorApp, session *headless.Session, requests <-chan aiContextRequest) aiContextRequest {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for m.ai.responding() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
		if err := session.Frame(); err != nil {
			t.Fatal(err)
		}
	}
	if m.ai.responding() || len(m.ai.turns) == 0 || m.ai.turns[len(m.ai.turns)-1].failed {
		t.Fatalf("model request did not complete: turns=%d status=%s", len(m.ai.turns), m.ai.message)
	}
	select {
	case request := <-requests:
		return request
	default:
		t.Fatal("no request reached the local model")
	}
	return aiContextRequest{}
}

func TestAIEnterSendsLargeDefaultFileAndSelection(t *testing.T) {
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	directory := t.TempDir()
	project := filepath.Join(directory, "订单项目")
	check(os.MkdirAll(project, 0700))
	const prefix, suffix = "{\"merchants\":\"", "\",\"tail\":\"文件尾\"}\n"
	content := strings.Repeat("商家😀", 15000)
	source := prefix + content + suffix
	file := filepath.Join(project, "账单.json")
	check(os.WriteFile(file, []byte(source), 0600))
	config := filepath.Join(project, "规则.ini")
	check(os.WriteFile(config, []byte("merchant=all"), 0600))
	m := newEditorApp()
	m.dataDirectory = filepath.Join(directory, "user-data")
	m.addProjects(project)
	m.load(file)
	tab := m.active
	requests := serveAIContextTest(t, m)
	s := openTestSession(t, m)
	check(s.Resize(1300, 800))
	check(s.ClickLabel("AI 助手"))
	check(s.Frame())
	if m.aiAttachmentIndex(aiAttachment{tabID: tab.id}) < 0 ||
		m.aiAttachmentIndex(aiAttachment{path: project, directory: true}) < 0 {
		t.Fatal("opening the assistant did not preselect the file and its project")
	}
	_, err := s.Locate("移除附件 账单.json")
	check(err)
	_, err = s.Locate("移除附件 订单项目")
	check(err)
	check(s.Type("请计算"))
	check(s.Compose("shangjia"))
	check(s.Keys("enter"))
	if len(m.ai.turns) != 0 || m.ai.responding() || !strings.HasSuffix(m.ai.prompt.Text(), "shangjia") {
		t.Fatal("IME candidate Enter was sent as a request or was not committed")
	}
	check(s.Keys("cmd+a"))
	check(s.Type("请计算"))
	check(s.Keys("shift+enter"))
	check(s.Type("商家数"))
	if m.ai.prompt.Text() != "请计算\n商家数" {
		t.Fatalf("Shift+Enter did not create a line break: %q", m.ai.prompt.Text())
	}
	check(s.Keys("enter"))
	request := waitForAIContext(t, m, s, requests)
	formatted, _, err := transformJSON(source, "格式化", 4)
	check(err)
	if request.Instruction != "请计算\n商家数" || !slices.Equal(request.Folders, []string{"订单项目"}) ||
		len(request.Files) != 2 || request.Files[0].Text != formatted || request.Files[0].Path != "订单项目/账单.json" ||
		request.Files[1].Text != "merchant=all" {
		t.Fatal("the default request truncated or duplicated the large file, or omitted the project")
	}
	if len(m.ai.attachments) != 2 || m.ai.prompt.Text() != "" {
		t.Fatal("sending lost the default context or failed to clear the draft")
	}

	start := strings.Index(formatted, content)
	tab.source.text.SetSelection(text.Range{Start: utf8.RuneCountInString(formatted[:start]), End: utf8.RuneCountInString(formatted[:start+len(content)])})
	largePrompt := strings.Repeat("复核", 3200)
	m.ai.prompt.SetText(largePrompt)
	check(s.Frame())
	check(s.Keys("enter"))
	request = waitForAIContext(t, m, s, requests)
	if request.Instruction != largePrompt || len(request.Files) != 2 || request.Files[0].Scope != "选区" ||
		request.Files[0].Text != content || request.Files[0].ID != tab.id {
		t.Fatal("the large selected text or prompt was truncated, or the folder replaced the selection with the whole file")
	}
}

func TestAIWorkspaceEntryAndMultiSelectAttachments(t *testing.T) {
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	directory := t.TempDir()
	root := filepath.Join(directory, "资源项目")
	subfolder := filepath.Join(root, "服务配置")
	check(os.MkdirAll(subfolder, 0700))
	config := filepath.Join(subfolder, "应用.yaml")
	check(os.WriteFile(config, []byte("host: saved\n"), 0600))
	readme := filepath.Join(subfolder, "用法.md")
	check(os.WriteFile(readme, []byte("# 用法\n"), 0600))
	rootFile := filepath.Join(root, "根目录.txt")
	check(os.WriteFile(rootFile, []byte("根目录"), 0600))
	check(os.WriteFile(filepath.Join(subfolder, "图片.bin"), []byte{0, 0xff}, 0600))
	m := newEditorApp()
	m.dataDirectory = filepath.Join(directory, "user-data")
	m.addProjects(root)
	m.load(config)
	m.active.source.replace("host: unsaved\n")
	first := m.addTab(document{saved: "first"}, "临时文件甲.txt")
	second := m.addTab(document{saved: "second"}, "临时文件乙.txt")
	requests := serveAIContextTest(t, m)
	s := openTestSession(t, m)
	check(s.Resize(1320, 810))
	click := func(label string) {
		t.Helper()
		check(s.ClickLabel(label))
		check(s.Frame())
	}
	click("服务配置")
	check(s.RightClickLabel("用法.md"))
	click("menuitem:在 AI 助手中打开")
	if len(m.ai.attachments) != 2 || m.ai.attachments[0].path != readme || m.ai.attachments[1].path != root || m.active != second {
		t.Fatal("opening from a file row did not choose that file and its project")
	}
	check(s.RightClickLabel("服务配置"))
	click("menuitem:在 AI 助手中打开")
	if len(m.ai.attachments) != 1 || !m.ai.attachments[0].directory || m.ai.attachments[0].path != subfolder || m.active != second {
		t.Fatal("opening from a folder row selected an unrelated file or folder")
	}
	check(s.RightClickLabel("资源项目"))
	click("menuitem:在 AI 助手中打开")
	if len(m.ai.attachments) != 1 || m.ai.attachments[0].path != root {
		t.Fatal("project action did not switch the contextual folder")
	}
	check(s.RightClickLabel("服务配置"))
	click("menuitem:在 AI 助手中打开")

	click("添加文件上下文")
	click("checkbox:临时文件甲.txt")
	click("checkbox:临时文件乙.txt")
	if !m.ai.contextMenu || m.aiAttachmentIndex(aiAttachment{tabID: first.id}) < 0 ||
		m.aiAttachmentIndex(aiAttachment{tabID: second.id}) < 0 {
		t.Fatal("open-file picker did not allow multiple selections in one visit")
	}
	click("完成选择")
	paths := make([]string, 0, 8)
	for i := 0; i < 7; i++ {
		path := filepath.Join(directory, fmt.Sprintf("批量-%d.txt", i))
		check(os.WriteFile(path, []byte(fmt.Sprintf("file %d", i)), 0600))
		paths = append(paths, path)
	}
	click("添加文件上下文")
	check(s.Driver().QueueOpenFiles(append(paths, readme), nil))
	click("选择文件…")
	if len(m.tabs) != 3 || len(m.ai.attachments) != 11 {
		t.Fatal("native picker opened editor tabs, skipped a selected file, or retained the old six-file limit")
	}
	snapshot, err := m.captureAIContext()
	check(err)
	targets, err := snapshot.targets(context.Background())
	check(err)
	if len(targets) != 11 || !slices.ContainsFunc(targets, func(target aiTarget) bool {
		return target.path == config && target.file.Text == "host: unsaved\n" && target.file.Path == "服务配置/应用.yaml"
	}) {
		t.Fatal("file and folder contexts duplicated files, lost an open draft, or included nontext/outside content")
	}
	click("AI 指令")
	check(s.Type("解释这些文件"))
	check(s.Keys("enter"))
	request := waitForAIContext(t, m, s, requests)
	if len(request.Files) != len(targets) || !slices.Equal(request.Folders, []string{"服务配置"}) {
		t.Fatal("the model did not receive the full batch of files")
	}
	for i, target := range targets {
		if request.Files[i] != target.file {
			t.Fatalf("the sent context for %s differs from the selected text", target.file.Name)
		}
	}
	index := slices.IndexFunc(targets, func(target aiTarget) bool { return target.path == readme })
	if index < 0 || targets[index].file.ID >= 0 {
		t.Fatal("an unopened file was not available for review")
	}
	result, err := parseAIChanges(fmt.Sprintf("{\"changes\":[{\"id\":%d,\"text\":\"# 新用法\\n\"}]}", targets[index].file.ID), aiEdit, targets)
	check(err)
	check(os.WriteFile(readme, []byte("# 外部已改\n"), 0600))
	if m.applyAIChange(&result.changes[0]) == nil || len(m.tabs) != 3 {
		t.Fatal("disk change after the request was not detected")
	}
	check(os.WriteFile(readme, []byte("# 用法\n"), 0600))
	check(m.applyAIChange(&result.changes[0]))
	disk, err := os.ReadFile(readme)
	check(err)
	if m.active.doc.path != readme || m.active.source.text.String() != "# 新用法\n" || !m.active.modified() || string(disk) != "# 用法\n" {
		t.Fatal("applying a suggestion did not open an unsaved, reviewable change")
	}
}
