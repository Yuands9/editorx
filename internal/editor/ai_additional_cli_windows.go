package editor

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

const (
	aiOpenCodeCLI  = "opencode-cli"
	aiCursorCLI    = "cursor-cli"
	aiCodeBuddyCLI = "codebuddy-cli"
	aiQoderCLI     = "qoder-cli"
	aiGrokCLI      = "grok-cli"
	aiPiCLI        = "pi-cli"
)

var otherCLIPrograms = map[string][]string{
	aiOpenCodeCLI: {"opencode", "opencode2"}, aiCursorCLI: {"agent", "cursor-agent"},
	aiCodeBuddyCLI: {"codebuddy"}, aiQoderCLI: {"qodercli"}, aiGrokCLI: {"grok"}, aiPiCLI: {"pi"},
}

func (settings aiSettings) findOtherCLI() (string, error) {
	for _, name := range otherCLIPrograms[settings.Provider] {
		if path, err := exec.LookPath(name + ".exe"); err == nil {
			return path, nil
		}
	}
	var moduleRoots []string
	for _, name := range otherCLIPrograms[settings.Provider] {
		if shim, err := exec.LookPath(name + ".cmd"); err == nil {
			moduleRoots = append(moduleRoots, filepath.Join(filepath.Dir(shim), "node_modules"))
		}
	}
	moduleRoots = append(moduleRoots, filepath.Join(os.Getenv("APPDATA"), "npm", "node_modules"))
	var entries []string
	switch settings.Provider {
	case aiOpenCodeCLI:
		entries = []string{"opencode-ai/bin/opencode.exe", "opencode-windows-x64/bin/opencode.exe", "opencode-ai/node_modules/opencode-windows-x64/bin/opencode.exe", "opencode-ai/bin/opencode"}
	case aiPiCLI:
		entries = []string{"@earendil-works/pi-coding-agent/dist/bundle/cli.js", "@earendil-works/pi-coding-agent/dist/cli.js", "@mariozechner/pi-coding-agent/dist/cli.js"}
	case aiCodeBuddyCLI:
		entries = []string{"@tencent-ai/codebuddy-code/bin/codebuddy.exe", "@tencent-ai/codebuddy-code/bin/codebuddy", "@tencent-ai/codebuddy-code/cli.js", "@tencent-ai/codebuddy-code/dist/cli.js"}
	}
	for _, modules := range moduleRoots {
		for _, entry := range entries {
			path := filepath.Join(modules, filepath.FromSlash(entry))
			if info, err := os.Stat(path); filepath.IsAbs(path) && err == nil && info.Mode().IsRegular() {
				return path, nil
			}
		}
	}
	return "", fmt.Errorf("未找到 %s，请安装后填写 CLI 路径", aiProviderLabel(settings.Provider))
}

func newAITextCommand(ctx context.Context, executable, job string, args []string) (*exec.Cmd, error) {
	ext := strings.ToLower(filepath.Ext(executable))
	if ext != ".exe" {
		node, err := exec.LookPath("node.exe")
		if err != nil {
			return nil, errors.New("运行此 CLI 需要 Node.js")
		}
		args = append([]string{executable}, args...)
		executable = node
	}
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Dir = job
	cmd.Env = append(cmd.Environ(), "TEMP="+job, "TMP="+job)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	cmd.WaitDelay = 2 * time.Second
	return cmd, nil
}

func (settings aiSettings) runTextCLI(ctx context.Context, system string, messages []aiMessage, effort aiReasoning, onDelta func(string)) (aiCompletion, error) {
	executable, err := settings.cliExecutable()
	if err != nil {
		return aiCompletion{}, err
	}
	job, err := settings.newCLIJob()
	if err != nil {
		return aiCompletion{}, err
	}
	defer os.RemoveAll(job)
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	data, err := json.Marshal(messages)
	if err != nil {
		return aiCompletion{}, err
	}
	prompt := system + "\n\n请直接回答以下 JSON 对话的最后一项。\n" + string(data)
	var args, env []string
	switch settings.Provider {
	case aiClaudeCLI, aiCodeBuddyCLI, aiQoderCLI:
		args = []string{"--print", "--output-format", "stream-json", "--input-format", "text",
			"--no-session-persistence", "--setting-sources", "", "--strict-mcp-config",
			"--mcp-config", `{"mcpServers":{}}`, "--tools", ""}
		if settings.Provider == aiClaudeCLI {
			args = append(args, "--verbose", "--include-partial-messages", "--disable-slash-commands")
		}
		if settings.Provider == aiCodeBuddyCLI {
			args = append(args, "--verbose", "--permission-mode", "dontAsk")
		}
		if settings.Provider == aiQoderCLI {
			args = append(args, "--permission-mode", "dont_ask")
		}
	case aiOpenCodeCLI:
		args = []string{"run", "--format", "json"}
		env = []string{"OPENCODE_DB=:memory:", "OPENCODE_DISABLE_PROJECT_CONFIG=1",
			"OPENCODE_DISABLE_AUTOUPDATE=true", "OPENCODE_CONFIG_CONTENT=" + `{"permission":{"*":"deny"},"plugin":[],"mcp":{},"autoupdate":false}`}
	case aiCursorCLI:
		args = []string{"--print", "--mode", "ask", "--output-format", "stream-json", "--stream-partial-output", "--workspace", job}
	case aiGrokCLI:
		promptFile := filepath.Join(job, "prompt.txt")
		if err := os.WriteFile(promptFile, []byte(prompt), 0600); err != nil {
			return aiCompletion{}, err
		}
		authPath := os.Getenv("GROK_AUTH_PATH")
		if authPath == "" {
			grokDirectory := os.Getenv("GROK_HOME")
			if grokDirectory == "" {
				userDirectory, err := os.UserHomeDir()
				if err != nil {
					return aiCompletion{}, err
				}
				grokDirectory = filepath.Join(userDirectory, ".grok")
			}
			authPath = filepath.Join(grokDirectory, "auth.json")
		}
		grokDirectory := filepath.Join(job, ".grok")
		if err := os.Mkdir(grokDirectory, 0700); err != nil {
			return aiCompletion{}, err
		}
		if err := os.WriteFile(filepath.Join(grokDirectory, "config.toml"), []byte("[cli]\nauto_update = false\nuse_leader = false\n"), 0600); err != nil {
			return aiCompletion{}, err
		}
		env = []string{"GROK_HOME=" + grokDirectory, "GROK_AUTH_PATH=" + authPath}
		args = []string{"--no-leader", "--prompt-file", promptFile, "--output-format", "streaming-json",
			"--disable-web-search", "--no-subagents", "--no-plan", "--verbatim",
			"--disallowed-tools", "run_terminal_command,read_file,search_replace,list_dir,grep,kill_command_or_subagent,todo_write,get_command_or_subagent_output,spawn_subagent,scheduler_create,scheduler_delete,scheduler_list,monitor,search_tool,use_tool,workflow,enter_plan_mode,exit_plan_mode,ask_user_question,image_gen,image_edit,image_to_video,reference_to_video,write,bash,shell,edit,Glob,Grep"}
	case aiPiCLI:
		args = []string{"--mode", "json", "--print", "--no-session", "--no-tools", "--no-extensions",
			"--no-skills", "--no-prompt-templates", "--no-context-files", "--no-approve"}
	default:
		return aiCompletion{}, errors.New("无法识别此 CLI 服务商")
	}
	if settings.Model != "" {
		args = append(args, "--model", settings.Model)
	}
	if effort != "" {
		switch settings.Provider {
		case aiClaudeCLI, aiGrokCLI:
			args = append(args, "--effort", string(effort))
		case aiOpenCodeCLI:
			args = append(args, "--variant", string(effort))
		case aiPiCLI:
			args = append(args, "--thinking", string(effort))
		}
	}
	cmd, err := newAITextCommand(ctx, executable, job, args)
	if err != nil {
		return aiCompletion{}, err
	}
	cmd.Env = append(cmd.Env, env...)
	if settings.Provider != aiGrokCLI {
		cmd.Stdin = strings.NewReader(prompt)
	}
	stderr := aiCLICapture{limit: 8192}
	cmd.Stderr = &stderr
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return aiCompletion{}, err
	}
	if err := cmd.Start(); err != nil {
		pipe.Close()
		return aiCompletion{}, err
	}
	reply, streamErr := readCLIText(settings.Provider, pipe, onDelta)
	if streamErr != nil {
		cancel()
	}
	waitErr := cmd.Wait()
	if streamErr != nil {
		return aiCompletion{}, streamErr
	}
	if waitErr != nil {
		if ctx.Err() != nil {
			return aiCompletion{}, ctx.Err()
		}
		return aiCompletion{}, fmt.Errorf("%s 执行失败，请检查 CLI 登录状态、版本和模型 ID", aiProviderLabel(settings.Provider))
	}
	reply.Model = settings.modelLabel(settings.Model)
	return reply, nil
}

func readCLIText(provider string, source io.Reader, onDelta func(string)) (aiCompletion, error) {
	var text strings.Builder
	var final string
	scanner := bufio.NewScanner(source)
	scanner.Buffer(make([]byte, 8192), aiResponseLimit*2)
	for scanner.Scan() {
		var event struct {
			Type, Result, Data string
			IsError            bool            `json:"is_error"`
			Timestamp          json.RawMessage `json:"timestamp_ms"`
			Part               aiTextBlock
			Event              struct {
				Type  string
				Delta aiTextBlock
			}
			Message struct {
				Content []aiTextBlock
			}
			AssistantMessageEvent struct {
				Type, Delta string
			} `json:"assistantMessageEvent"`
		}
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			continue
		}
		if event.IsError || event.Type == "error" {
			return aiCompletion{}, fmt.Errorf("%s 返回了错误，请检查登录状态和模型 ID", aiProviderLabel(provider))
		}
		delta := ""
		switch event.Type {
		case "text":
			if provider == aiOpenCodeCLI {
				delta = event.Part.Text
			}
			if provider == aiGrokCLI {
				delta = event.Data
			}
		case "message_update":
			if event.AssistantMessageEvent.Type == "text_delta" {
				delta = event.AssistantMessageEvent.Delta
			}
		case "stream_event":
			if event.Event.Delta.Type == "text_delta" {
				delta = event.Event.Delta.Text
			}
		case "assistant":
			var message strings.Builder
			for _, content := range event.Message.Content {
				if content.Type == "text" {
					message.WriteString(content.Text)
				}
			}
			if provider == aiCursorCLI && len(event.Timestamp) != 0 && string(event.Timestamp) != "null" {
				delta = message.String()
			} else if message.Len() != 0 {
				final = message.String()
				if text.Len() == 0 {
					delta = final
				}
			}
		case "result":
			if event.Result != "" {
				final = event.Result
			}
		}
		if text.Len()+len(delta) > aiResponseLimit || len(final) > aiResponseLimit {
			return aiCompletion{}, errors.New("CLI 响应超过 2 MB")
		}
		if delta != "" {
			text.WriteString(delta)
			if onDelta != nil {
				onDelta(delta)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return aiCompletion{}, err
	}
	if final == "" {
		final = text.String()
	}
	return aiCompletion{Text: final}, nil
}

func (settings aiSettings) cliModelList(ctx context.Context) ([]string, error) {
	executable, err := settings.cliExecutable()
	if err != nil {
		return nil, err
	}
	job, err := settings.newCLIJob()
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(job)
	args := []string{"--list-models"}
	if settings.Provider == aiOpenCodeCLI {
		args = []string{"models"}
	}
	cmd, err := newAITextCommand(ctx, executable, job, args)
	if err != nil {
		return nil, err
	}
	output := aiCLICapture{limit: aiResponseLimit}
	cmd.Stdout = &output
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("无法获取 %s 的模型列表，请检查 CLI 登录状态和版本", aiProviderLabel(settings.Provider))
	}
	var models []string
	for line := range strings.SplitSeq(output.String(), "\n") {
		line = strings.TrimSpace(line)
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		id := ""
		switch settings.Provider {
		case aiOpenCodeCLI:
			if len(fields) == 1 && strings.Contains(line, "/") {
				id = line
			}
		case aiCursorCLI:
			if name, _, ok := strings.Cut(line, " - "); ok && !strings.EqualFold(name, "auto") {
				id = name
			}
		case aiPiCLI:
			if len(fields) >= 3 && fields[0] != "provider" {
				id = fields[0] + "/" + fields[1]
			}
		case aiQoderCLI:
			line = strings.Trim(line, "│|")
			fields := strings.FieldsFunc(line, func(r rune) bool { return r == '│' || r == '|' || r == '\t' })
			if len(fields) > 1 {
				id = strings.TrimSpace(fields[0])
			} else if strings.HasPrefix(line, "- ") {
				id = strings.TrimSpace(strings.TrimPrefix(line, "- "))
			}
		}
		if validAIModelID(id) && !strings.ContainsAny(id, " \t─┌└") && !strings.EqualFold(id, "model") {
			models = append(models, id)
		}
	}
	slices.Sort(models)
	return slices.Compact(models), nil
}
