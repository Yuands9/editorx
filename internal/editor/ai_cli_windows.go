package editor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	aiCodexCLI  = "codex-cli"
	aiClaudeCLI = "claude-code-cli"
)

func (settings aiSettings) usesCLI() bool {
	return settings.Provider == aiCodexCLI || settings.Provider == aiClaudeCLI || len(otherCLIPrograms[settings.Provider]) != 0
}

func (settings aiSettings) modelLabel(model string) string {
	if settings.usesCLI() && model == "" {
		return "CLI 默认模型"
	}
	return model
}

func (settings aiSettings) cliExecutable() (string, error) {
	if settings.CLIPath != "" {
		path := settings.CLIPath
		extension := strings.ToLower(filepath.Ext(path))
		canUseScript := len(otherCLIPrograms[settings.Provider]) != 0
		if !filepath.IsAbs(path) || extension != ".exe" && !(canUseScript && (extension == ".js" || extension == ".mjs")) {
			return "", errors.New(uiText("请填写 CLI 的完整可执行文件路径"))
		}
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			return "", errors.New(uiText("找不到指定的 CLI 可执行文件"))
		}
		return path, nil
	}

	if len(otherCLIPrograms[settings.Provider]) != 0 {
		return settings.findOtherCLI()
	}
	name, arch, target := "codex", "x64", "x86_64-pc-windows-msvc"
	if runtime.GOARCH == "arm64" {
		arch, target = "arm64", "aarch64-pc-windows-msvc"
	}
	modules := filepath.Join(os.Getenv("APPDATA"), "npm", "node_modules")
	npmPath := filepath.Join(modules, "@openai", "codex", "node_modules", "@openai", "codex-win32-"+arch, "vendor", target, "bin", "codex.exe")
	if settings.Provider == aiClaudeCLI {
		name = "claude"
		npmPath = filepath.Join(modules, "@anthropic-ai", "claude-code", "bin", "claude.exe")
	}
	if filepath.IsAbs(npmPath) {
		if info, err := os.Stat(npmPath); err == nil && info.Mode().IsRegular() {
			return npmPath, nil
		}
	}
	if path, err := exec.LookPath(name + ".exe"); err == nil {
		return path, nil
	}
	return "", fmt.Errorf(uiText("未找到 %s，请安装后填写可执行文件路径"), aiProviderLabel(settings.Provider))
}

func (settings aiSettings) cliArguments(replyFile string, effort aiReasoning) []string {
	var args []string
	switch settings.Provider {
	case aiCodexCLI:
		args = []string{"--ask-for-approval", "never", "exec", "--sandbox", "read-only", "--skip-git-repo-check",
			"--ignore-user-config", "--ignore-rules", "--ephemeral", "--disable", "shell_tool", "--disable", "unified_exec",
			"--disable", "multi_agent_v2", "-c", `web_search="disabled"`, "-c", `model_provider="viva-http"`, "-c", codexHTTPSProvider,
			"--output-last-message", replyFile}
		if effort != "" {
			args = append(args, "-c", "model_reasoning_effort="+strconv.Quote(string(effort)))
		}
	case aiClaudeCLI:
		args = []string{"--print", "--output-format=json", "--tools", "", "--no-session-persistence",
			"--disable-slash-commands", "--setting-sources", "", "--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`}
		if effort != "" {
			args = append(args, "--effort="+string(effort))
		}
	}
	if settings.Model != "" {
		args = append(args, "--model="+settings.Model)
	}
	if settings.Provider == aiCodexCLI {
		args = append(args, "-")
	}
	return args
}

func (settings aiSettings) newCLIJob() (string, error) {
	directory := settings.cliDirectory
	var err error
	if directory == "" {
		directory, err = editorDataDirectory()
		if err != nil {
			return "", err
		}
	}
	workspace := filepath.Join(directory, editorCLICacheDirectory)
	if err := os.MkdirAll(workspace, 0700); err != nil {
		return "", err
	}
	activeEditorCache.Lock()
	defer activeEditorCache.Unlock()
	job, err := os.MkdirTemp(workspace, editorCLICachePattern)
	if err == nil {
		activeEditorCache.paths[editorCacheKey(job)] = struct{}{}
	}
	return job, err
}

func removeCLIJob(job string) {
	_ = os.RemoveAll(job)
	releaseEditorCache(job)
}

func (settings aiSettings) runCLI(ctx context.Context, system string, messages []aiMessage, effort aiReasoning) (aiCompletion, error) {
	executable, err := settings.cliExecutable()
	if err != nil {
		return aiCompletion{}, err
	}
	job, err := settings.newCLIJob()
	if err != nil {
		return aiCompletion{}, err
	}
	defer removeCLIJob(job)

	prompt, err := cliUserPrompt(messages, settings.Provider == aiCodexCLI)
	if err != nil {
		return aiCompletion{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	output := filepath.Join(job, "reply.txt")
	args := settings.cliArguments(output, effort)
	if settings.Provider == aiCodexCLI {
		args = append([]string{"-c", "developer_instructions=" + strconv.Quote(system)}, args...)
	} else {
		prompt = system + "\n\n" + prompt
	}
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Dir = job
	cmd.Env = append(cmd.Environ(), "TEMP="+job, "TMP="+job)
	cmd.Stdin = strings.NewReader(prompt)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	cmd.WaitDelay = 2 * time.Second
	stderr := aiCLICapture{limit: 8192}
	stdout := aiCLICapture{limit: aiResponseLimit}
	cmd.Stderr = &stderr
	if settings.Provider == aiClaudeCLI {
		cmd.Stdout = &stdout
	}
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return aiCompletion{}, ctx.Err()
		}
		detail := strings.ToLower(stderr.String() + " " + stdout.String())
		if strings.Contains(detail, "not logged in") || strings.Contains(detail, "unauthorized") ||
			strings.Contains(detail, "authentication required") {
			return aiCompletion{}, fmt.Errorf(uiText("请先在终端登录 %s"), aiProviderLabel(settings.Provider))
		}
		if strings.Contains(detail, "unexpected argument") || strings.Contains(detail, "unknown option") {
			return aiCompletion{}, fmt.Errorf(uiText("当前 %s 版本不支持所需的命令选项，请更新 CLI"), aiProviderLabel(settings.Provider))
		}
		return aiCompletion{}, fmt.Errorf(uiText("%s 执行失败（%w），请检查登录状态和模型 ID"), aiProviderLabel(settings.Provider), err)
	}

	reply := aiCompletion{Model: settings.modelLabel(settings.Model)}
	if settings.Provider == aiCodexCLI {
		file, err := os.Open(output)
		if errors.Is(err, os.ErrNotExist) {
			return aiCompletion{}, errors.New(uiText("Codex CLI 没有生成回复"))
		}
		if err != nil {
			return aiCompletion{}, err
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, aiResponseLimit+1))
		if err != nil {
			return aiCompletion{}, err
		}
		reply.Truncated, reply.Text = len(data) > aiResponseLimit, string(data)
		return reply, nil
	}
	if stdout.overflow {
		reply.Truncated = true
		return reply, nil
	}
	var result struct {
		Result  string `json:"result"`
		IsError bool   `json:"is_error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || result.IsError {
		return aiCompletion{}, errors.New(uiText("Claude Code CLI 未返回有效回复，请检查登录状态和模型 ID"))
	}
	reply.Text = result.Result
	return reply, nil
}

func cliUserPrompt(messages []aiMessage, rawSingleMessage bool) (string, error) {
	if rawSingleMessage && len(messages) == 1 && messages[0].Role == "user" {
		return messages[0].Content, nil
	}
	history, err := json.Marshal(messages)
	if err != nil {
		return "", err
	}
	return "以下 JSON 数组是对话记录，最后一项是当前请求。请直接给出回复。\n" + string(history), nil
}

type aiCLICapture struct {
	bytes.Buffer
	limit    int
	overflow bool
}

func (capture *aiCLICapture) Write(data []byte) (int, error) {
	count := min(len(data), capture.limit-capture.Len())
	if count < len(data) {
		capture.overflow = true
	}
	_, _ = capture.Buffer.Write(data[:count])
	return len(data), nil
}
