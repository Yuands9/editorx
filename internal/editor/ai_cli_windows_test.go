package editor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestAICommandLineProvidersThroughEditor(t *testing.T) {
	working := t.TempDir()
	source := filepath.Join(working, "fixture.go")
	executable := filepath.Join(working, "CLI 测试.exe")
	if err := os.WriteFile(source, []byte(`package main

import (
 "encoding/json"
 "io"
 "os"
 "slices"
 "strings"
 "time"
)
func appServer() {
 decoder := json.NewDecoder(os.Stdin)
 encoder := json.NewEncoder(os.Stdout)
 type request struct {
  ID int
  Method string
  Params json.RawMessage
 }
 response := func(id int, result any) { _ = encoder.Encode(map[string]any{"id":id,"result":result}) }
 notify := func(method string, params any) { _ = encoder.Encode(map[string]any{"method":method,"params":params}) }
 for {
  var req request
  if decoder.Decode(&req) != nil { return }
  switch req.Method {
  case "initialize":
   response(req.ID, map[string]any{})
  case "config/read":
   response(req.ID, map[string]any{"config":map[string]any{"mcp_servers":map[string]any{"fixture":map[string]bool{"enabled":true}}}})
  case "model/list":
   response(req.ID, map[string]any{"data":[]map[string]string{{"id":"viva-fast","model":"viva-fast"}}})
  case "thread/start":
   params := string(req.Params)
   if !strings.Contains(params, "viva-fast") || !strings.Contains(params, "\"fixture\":{\"enabled\":false}") || !strings.Contains(params, "\"sandbox\":\"read-only\"") { os.Exit(9) }
   response(req.ID, map[string]any{"thread":map[string]string{"id":"test-thread"},"model":"viva-fast"})
  case "turn/start":
   if !strings.Contains(string(req.Params), "请解释配置") { os.Exit(8) }
   response(req.ID, map[string]any{})
   notify("item/agentMessage/delta", map[string]string{"delta":"Codex 的"})
   time.Sleep(130*time.Millisecond)
   notify("item/agentMessage/delta", map[string]string{"delta":"回复"})
   notify("item/completed", map[string]any{"item":map[string]string{"type":"agentMessage","text":"Codex 的回复"}})
   notify("turn/completed", map[string]any{"turn":map[string]string{"status":"completed"}})
  }
 }
}

func additionalCLI(args []string, model string) {
 encoder := json.NewEncoder(os.Stdout)
 emit := func(value any) { _ = encoder.Encode(value) }
 var input []byte
 var err error
 switch model {
 case "grok-cli":
  at := slices.Index(args, "--prompt-file")
  if at < 0 || !slices.Contains(args, "streaming-json") || os.Getenv("GROK_HOME") == "" { os.Exit(10) }
  input, err = os.ReadFile(args[at+1])
 case "opencode-cli":
  if !slices.Contains(args, "run") || os.Getenv("OPENCODE_DISABLE_PROJECT_CONFIG") != "1" { os.Exit(11) }
  input, err = io.ReadAll(os.Stdin)
 case "cursor-cli":
  if !slices.Contains(args, "ask") || !slices.Contains(args, "--stream-partial-output") { os.Exit(12) }
  input, err = io.ReadAll(os.Stdin)
 case "pi-cli":
  if !slices.Contains(args, "--no-tools") || !slices.Contains(args, "--no-session") { os.Exit(13) }
  input, err = io.ReadAll(os.Stdin)
 case "codebuddy-cli", "qoder-cli":
  if !slices.Contains(args, "--strict-mcp-config") || !slices.Contains(args, "--tools") { os.Exit(14) }
  input, err = io.ReadAll(os.Stdin)
 default:
  os.Exit(15)
 }
 if err != nil || !strings.Contains(string(input), "请解释配置") { os.Exit(16) }
 for _, chunk := range []string{"测试", "回复"} {
  switch model {
  case "grok-cli":
   emit(map[string]any{"type":"text","data":chunk})
  case "opencode-cli":
   emit(map[string]any{"type":"text","part":map[string]string{"type":"text","text":chunk}})
  case "pi-cli":
   emit(map[string]any{"type":"message_update","assistantMessageEvent":map[string]string{"type":"text_delta","delta":chunk}})
  case "cursor-cli":
   emit(map[string]any{"type":"assistant","timestamp_ms":1,"message":map[string]any{"content":[]map[string]string{{"type":"text","text":chunk}}}})
  default:
   emit(map[string]any{"type":"stream_event","event":map[string]any{"type":"content_block_delta","delta":map[string]string{"type":"text_delta","text":chunk}}})
  }
 }
}

func main() {
 args := os.Args[1:]
 if at := slices.Index(args,"--model"); at >= 0 && at+1 < len(args) {
  additionalCLI(args, args[at+1])
  return
 }
 if slices.Contains(args, "app-server") {
  appServer()
  return
 }
 text, err := io.ReadAll(os.Stdin)
 if err != nil || !strings.Contains(string(text), "请解释配置") {
  os.Exit(2)
 }
 directory, err := os.Getwd()
 if err != nil || directory != os.Getenv("TEMP") {
  os.Exit(3)
 }
 if slices.Contains(args, "exec") {
  sandbox, output := slices.Index(args, "--sandbox"), slices.Index(args, "--output-last-message")
  if sandbox < 0 || args[sandbox+1] != "read-only" || output < 0 || args[len(args)-1] != "-" || !slices.Contains(args, "--ephemeral") {
   os.Exit(4)
  }
  if os.WriteFile(args[output+1], []byte("Codex 的回复"), 0600) != nil {
   os.Exit(5)
  }
  return
 }
 tools := slices.Index(args, "--tools")
 if tools < 0 || args[tools+1] != "" || !slices.Contains(args, "--strict-mcp-config") || !slices.Contains(args, "--model=haiku") {
  os.Exit(6)
 }
 if json.NewEncoder(os.Stdout).Encode(map[string]string{"result":"Claude 的回复"}) != nil {
  os.Exit(7)
 }
}
`), 0600); err != nil {
		t.Fatal(err)
	}
	build := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go.exe"), "build", "-o", executable, source)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building CLI fixture: %v, %s", err, output)
	}
	m := newTestEditorWithDocument()
	m.dataDirectory = working
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
	check(s.Resize(1200, 760))
	click("AI 助手")
	click("模型设置")
	click("新增配置")
	click("combobox:服务商预设")
	for _, removed := range []string{"OpenAI 兼容", "Anthropic 兼容", "Ollama · 本机", "Ollama"} {
		if _, err := s.Locate("option:" + removed); err == nil {
			t.Fatalf("obsolete preset still offered: %s", removed)
		}
	}
	click("option:Codex CLI")
	if _, err := s.Locate("接口基址"); err == nil {
		t.Fatal("CLI configuration still displays the HTTP endpoint editor")
	}
	click("CLI 路径")
	check(s.Type(executable))
	click("保存配置")
	click("关闭设置")
	if !s.HasText("CLI 默认模型") {
		t.Fatal("the model picker did not show the CLI default")
	}
	click("切换模型配置")
	catalogDeadline := time.Now().Add(8 * time.Second)
	for !s.HasText("viva-fast") && time.Now().Before(catalogDeadline) {
		time.Sleep(10 * time.Millisecond)
		check(s.Frame())
	}
	click("选择模型 viva-fast")
	click("AI 指令")
	check(s.Type("请解释配置"))
	click("发送给模型")
	deadline := time.Now().Add(8 * time.Second)
	partialSeen := false
	for m.ai.responding() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		check(s.Frame())
		partialSeen = partialSeen || m.ai.turns[0].partial == "Codex 的"
	}
	if !partialSeen {
		t.Fatal("the first reply delta was not visible during the pending turn")
	}
	if len(m.ai.turns) != 1 || m.ai.turns[0].reply != "Codex 的回复" {
		t.Fatalf("Codex prompt did not use the saved CLI: %+v", m.ai.turns)
	}
	saved, err := readAIProfiles(working)
	check(err)
	profile := saved.active(0)
	if profile.Provider != aiCodexCLI || profile.CLIPath != executable || profile.BaseURL != "" {
		t.Fatal("the CLI config did not round-trip")
	}
	profile.Provider, profile.Model, profile.cliDirectory = aiClaudeCLI, "haiku", working
	reply, err := profile.complete(context.Background(), "编辑助手", []aiMessage{{Role: "user", Content: "请解释配置"}}, aiReasoningHigh, nil)
	check(err)
	if strings.TrimSpace(reply) != "Claude 的回复" {
		t.Fatalf("unexpected Claude Code response %q", reply)
	}

	for _, provider := range []string{aiOpenCodeCLI, aiCursorCLI, aiCodeBuddyCLI, aiQoderCLI, aiGrokCLI, aiPiCLI} {
		t.Run(provider, func(t *testing.T) {
			profile.Provider, profile.Model = provider, provider
			var deltas []string
			got, err := profile.complete(context.Background(), "编辑助手", []aiMessage{{Role: "user", Content: "请解释配置"}}, "", func(delta string) {
				deltas = append(deltas, delta)
			})
			if err != nil || got != "测试回复" || !slices.Equal(deltas, []string{"测试", "回复"}) {
				t.Fatalf("headless program did not deliver two text events: %q, %q (%v)", got, deltas, err)
			}
		})
	}
}
