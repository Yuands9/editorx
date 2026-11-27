package editor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/Yuands9/editorx/internal/version"
)

const codexHTTPSProvider = `model_providers.viva-http={name="OpenAI", requires_openai_auth=true, wire_api="responses", supports_websockets=false}`

type codexRPC struct {
	command *exec.Cmd
	cancel  context.CancelFunc
	job     string
	input   io.WriteCloser
	encoder *json.Encoder
	scanner *bufio.Scanner
	id      int
}

type codexRPCMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (settings aiSettings) openCodex(ctx context.Context) (*codexRPC, error) {
	executable, err := settings.cliExecutable()
	if err != nil {
		return nil, err
	}
	job, err := settings.newCLIJob()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	cmd := exec.CommandContext(ctx, executable, "app-server", "--listen", "stdio://",
		"--disable", "apps", "--disable", "plugins", "--disable", "hooks",
		"--disable", "shell_tool", "--disable", "unified_exec", "--disable", "multi_agent", "--disable", "multi_agent_v2",
		"-c", `web_search="disabled"`, "-c", `model_provider="viva-http"`, "-c", codexHTTPSProvider)
	cmd.Dir = job
	cmd.Env = append(cmd.Environ(), "TEMP="+job, "TMP="+job)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	cmd.WaitDelay = 2 * time.Second
	attempt := &codexRPC{job: job, cancel: cancel, command: cmd}
	attempt.input, err = cmd.StdinPipe()
	if err != nil {
		attempt.close()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		attempt.close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		stdout.Close()
		attempt.close()
		return nil, err
	}
	attempt.encoder = json.NewEncoder(attempt.input)
	attempt.scanner = bufio.NewScanner(stdout)
	attempt.scanner.Buffer(make([]byte, 8192), aiResponseLimit*4)
	if err := attempt.call("initialize", map[string]any{
		"clientInfo":   map[string]string{"name": "viva_editor", "title": applicationName, "version": version.Number},
		"capabilities": map[string]bool{"experimentalApi": true},
	}, nil); err != nil {
		attempt.close()
		return nil, err
	}
	if err := attempt.encoder.Encode(map[string]string{"method": "initialized"}); err != nil {
		attempt.close()
		return nil, err
	}
	return attempt, nil
}

func (client *codexRPC) close() {
	if client.input != nil {
		client.input.Close()
	}
	client.cancel()
	if client.command.Process != nil {
		_ = client.command.Wait()
	}
	removeCLIJob(client.job)
}

func (client *codexRPC) next() (codexRPCMessage, error) {
	for client.scanner.Scan() {
		var message codexRPCMessage
		if err := json.Unmarshal(client.scanner.Bytes(), &message); err != nil {
			return codexRPCMessage{}, errors.New(uiText("Codex CLI 返回了无效的 JSON-RPC 消息"))
		}
		if message.Method != "" && len(message.ID) != 0 && !bytes.Equal(message.ID, []byte("null")) {
			// Viva only consumes text and never grants a server request for a tool or approval.
			err := client.encoder.Encode(map[string]any{
				"id": message.ID, "error": map[string]any{"code": -32601, "message": applicationName + " does not provide this action"},
			})
			if err != nil {
				return codexRPCMessage{}, err
			}
			continue
		}
		return message, nil
	}
	if err := client.scanner.Err(); err != nil {
		return codexRPCMessage{}, err
	}
	return codexRPCMessage{}, errors.New(uiText("Codex CLI 已关闭连接"))
}

func (client *codexRPC) send(method string, params any) (int, error) {
	client.id++
	return client.id, client.encoder.Encode(map[string]any{"id": client.id, "method": method, "params": params})
}

func (client *codexRPC) call(method string, params any, result any) error {
	id, err := client.send(method, params)
	if err != nil {
		return err
	}
	for {
		message, err := client.next()
		if err != nil {
			return err
		}
		var responseID int
		if json.Unmarshal(message.ID, &responseID) != nil || responseID != id {
			continue
		}
		if message.Error != nil {
			return codexRequestError(message.Error.Message)
		}
		if result == nil {
			return nil
		}
		return json.Unmarshal(message.Result, result)
	}
}

func codexRequestError(text string) error {
	detail := strings.ToLower(text)
	switch {
	case strings.Contains(detail, "not logged in"), strings.Contains(detail, "unauthorized"), strings.Contains(detail, "authentication"):
		return errors.New(uiText("Codex CLI 的登录已失效，请检查 CLI 的登录状态"))
	case strings.Contains(detail, "rate limit"), strings.Contains(detail, "usage limit"), strings.Contains(detail, "429"):
		return errors.New(uiText("Codex 模型请求达到账号限额，请稍后重试"))
	case strings.Contains(detail, "model"), strings.Contains(detail, "unsupported"):
		return errors.New(uiText("Codex CLI 无法使用当前模型，请从模型列表重新选择"))
	default:
		return errors.New(uiText("Codex CLI 请求失败，请检查本机网络和登录状态"))
	}
}

func (settings aiSettings) codexModels(ctx context.Context) ([]string, error) {
	client, err := settings.openCodex(ctx)
	if err != nil {
		return nil, err
	}
	defer client.close()
	params := map[string]any{"limit": 100, "includeHidden": false, "modelProvider": "openai"}
	var models []string
	seen := make(map[string]bool)
	for range 5 {
		var page struct {
			Data []struct {
				Model  string `json:"model"`
				ID     string `json:"id"`
				Hidden bool   `json:"hidden"`
			} `json:"data"`
			NextCursor string `json:"nextCursor"`
		}
		if err := client.call("model/list", params, &page); err != nil {
			return nil, err
		}
		for _, item := range page.Data {
			name := item.Model
			if name == "" {
				name = item.ID
			}
			if !item.Hidden && validAIModelID(name) && !seen[name] {
				models = append(models, name)
				seen[name] = true
			}
		}
		if page.NextCursor == "" {
			break
		}
		params["cursor"] = page.NextCursor
	}
	return models, nil
}

func (settings aiSettings) runCodex(ctx context.Context, system string, messages []aiMessage, effort aiReasoning, onDelta func(string)) (aiCompletion, bool, error) {
	client, err := settings.openCodex(ctx)
	if err != nil {
		return aiCompletion{}, false, err
	}
	defer client.close()
	var config struct {
		Config struct {
			MCPServers map[string]json.RawMessage `json:"mcp_servers"`
		} `json:"config"`
	}
	if err := client.call("config/read", map[string]any{"cwd": client.job, "includeLayers": false}, &config); err != nil {
		return aiCompletion{}, false, err
	}
	// The app server uses the user's login. Disable configured servers on our own ephemeral thread.
	disabledServers := make(map[string]map[string]bool, len(config.Config.MCPServers))
	for name := range config.Config.MCPServers {
		disabledServers[name] = map[string]bool{"enabled": false}
	}
	start := map[string]any{
		"cwd": client.job, "ephemeral": true, "approvalPolicy": "never", "sandbox": "read-only",
		"developerInstructions": system,
		"config":                map[string]any{"mcp_servers": disabledServers},
	}
	if settings.Model != "" {
		start["model"] = settings.Model
	}
	var thread struct {
		Model  string `json:"model"`
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := client.call("thread/start", start, &thread); err != nil {
		return aiCompletion{}, false, err
	}
	if thread.Thread.ID == "" {
		return aiCompletion{}, false, errors.New(uiText("Codex CLI 未创建对话"))
	}
	prompt, err := cliUserPrompt(messages, true)
	if err != nil {
		return aiCompletion{}, false, err
	}
	params := map[string]any{
		"threadId": thread.Thread.ID,
		"input": []map[string]any{{
			"type": "text", "text": prompt,
			"text_elements": []string{},
		}},
	}
	if effort != "" {
		params["effort"] = effort
	}
	id, err := client.send("turn/start", params)
	if err != nil {
		return aiCompletion{}, true, err
	}
	reply, err := client.readTurn(id, onDelta)
	reply.Model = thread.Model
	if reply.Model == "" {
		reply.Model = settings.modelLabel(settings.Model)
	}
	return reply, true, err
}

func (client *codexRPC) readTurn(id int, onDelta func(string)) (aiCompletion, error) {
	var reply aiCompletion
	var partial strings.Builder
	for {
		message, err := client.next()
		if err != nil {
			return aiCompletion{}, err
		}
		var responseID int
		if json.Unmarshal(message.ID, &responseID) == nil && responseID == id && message.Error != nil {
			return aiCompletion{}, codexRequestError(message.Error.Message)
		}
		switch message.Method {
		case "item/agentMessage/delta":
			var delta struct {
				Delta string `json:"delta"`
			}
			if err := json.Unmarshal(message.Params, &delta); err != nil {
				return aiCompletion{}, err
			}
			if partial.Len()+len(delta.Delta) > aiResponseLimit {
				return aiCompletion{}, errors.New(uiText("Codex 模型响应超过 2 MB"))
			}
			partial.WriteString(delta.Delta)
			if onDelta != nil {
				onDelta(delta.Delta)
			}
		case "item/completed":
			var completed struct {
				Item struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"item"`
			}
			if json.Unmarshal(message.Params, &completed) == nil && completed.Item.Type == "agentMessage" {
				reply.Text = completed.Item.Text
			}
		case "turn/completed":
			var completed struct {
				Turn struct {
					Status string `json:"status"`
					Error  *struct {
						Message string `json:"message"`
					} `json:"error"`
				} `json:"turn"`
			}
			if err := json.Unmarshal(message.Params, &completed); err != nil {
				return aiCompletion{}, err
			}
			if completed.Turn.Error != nil {
				return aiCompletion{}, codexRequestError(completed.Turn.Error.Message)
			}
			if completed.Turn.Status != "completed" {
				return aiCompletion{}, fmt.Errorf("%s", uiText("Codex CLI 没有完成回复"))
			}
			if reply.Text == "" {
				reply.Text = partial.String()
			}
			reply.Truncated = len(reply.Text) > aiResponseLimit
			return reply, nil
		}
	}
}
