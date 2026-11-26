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
	"strings"
	"sync"
	"time"
)

const workflowSettingsFile = "workflow-settings.json"
const toolOutputLimit = 1 << 20

type customEditorTool struct {
	Name           string   `json:"name"`
	Executable     string   `json:"executable"`
	Arguments      []string `json:"arguments,omitempty"`
	Directory      string   `json:"directory,omitempty"`
	SelectionInput bool     `json:"selectionInput,omitempty"`
}
type workflowPreferences struct {
	Macros []editorMacro      `json:"macros,omitempty"`
	Tools  []customEditorTool `json:"tools,omitempty"`
}

func readWorkflowPreferences(directory string) (workflowPreferences, error) {
	var preferences workflowPreferences
	file, err := os.Open(filepath.Join(directory, workflowSettingsFile))
	if errors.Is(err, os.ErrNotExist) {
		return preferences, nil
	}
	if err != nil {
		return preferences, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (2<<20)+1))
	if err != nil {
		return preferences, err
	}
	if len(data) > 2<<20 {
		return preferences, errors.New("宏与工具配置超过 2 MiB")
	}
	if err := json.Unmarshal(data, &preferences); err != nil {
		return preferences, err
	}
	return preferences, preferences.validate()
}

func (preferences workflowPreferences) validate() error {
	if len(preferences.Tools) > 30 || len(preferences.Macros) > 30 {
		return errors.New("最多保存 30 个宏和 30 个工具")
	}
	for _, macro := range preferences.Macros {
		if strings.TrimSpace(macro.Name) == "" || len(macro.Name) > 120 || len(macro.Steps) == 0 || len(macro.Steps) > macroStepLimit {
			return errors.New("宏名称或步数无效")
		}
		bytes := 0
		for _, step := range macro.Steps {
			bytes += len(step.Text)
		}
		if bytes > macroTextLimit {
			return errors.New("宏文本超过 256 KiB")
		}
	}
	for _, tool := range preferences.Tools {
		if strings.TrimSpace(tool.Name) == "" || len(tool.Name) > 120 || strings.TrimSpace(tool.Executable) == "" {
			return errors.New("请填写工具名称和程序")
		}
		if len(tool.Arguments) > 200 || len(strings.Join(tool.Arguments, "")) > 64<<10 {
			return errors.New("工具参数过长")
		}
	}
	return nil
}

func (preferences workflowPreferences) save(directory string) error {
	if err := preferences.validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(preferences, "", "  ")
	if err != nil {
		return err
	}
	if len(data) > 2<<20 {
		return errors.New("宏与工具配置超过 2 MiB，请删除不需要的条目")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	return writeFile(filepath.Join(directory, workflowSettingsFile), data)
}

type toolContext struct{ file, directory, project, selection string }

func (m *editorApp) currentToolContext() toolContext {
	ctx := toolContext{}
	if tab := m.active; historyEditableTab(tab) {
		ctx.file = tab.doc.path
		if ctx.file != "" {
			ctx.directory = filepath.Dir(ctx.file)
		}
		selection := tab.source.text.Selection()
		ctx.selection = tab.source.text.String()[tab.source.text.ByteOffset(selection.Start):tab.source.text.ByteOffset(selection.End)]
	}
	if m.workspace != nil && len(m.workspace.roots) > 0 {
		ctx.project = m.workspace.roots[0].path
	}
	return ctx
}

func toolArguments(tool customEditorTool, values toolContext) (string, []string, error) {
	expand := func(value string) (string, error) {
		for _, variable := range []struct{ key, value string }{{"{file}", values.file}, {"{dir}", values.directory}, {"{project}", values.project}, {"{selection}", values.selection}} {
			if strings.Contains(value, variable.key) && variable.value == "" && variable.key != "{selection}" {
				return "", fmt.Errorf("%s 当前不可用，请先保存文件或添加项目", variable.key)
			}
		}
		// One replacement pass: document text containing placeholders is literal.
		return strings.NewReplacer("{file}", values.file, "{dir}", values.directory, "{project}", values.project, "{selection}", values.selection).Replace(value), nil
	}
	directory, err := expand(tool.Directory)
	if err != nil {
		return "", nil, err
	}
	if directory == "" {
		directory = values.directory
		if directory == "" {
			directory = values.project
		}
	}
	args := make([]string, len(tool.Arguments))
	for i, argument := range tool.Arguments {
		args[i], err = expand(argument)
		if err != nil {
			return "", nil, err
		}
	}
	return directory, args, nil
}

type toolCapture struct {
	mu        sync.Mutex
	buffer    bytes.Buffer
	truncated bool
}

func (capture *toolCapture) Write(data []byte) (int, error) {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	n := min(len(data), toolOutputLimit-capture.buffer.Len())
	capture.buffer.Write(data[:n])
	capture.truncated = capture.truncated || n < len(data)
	return len(data), nil
}

type toolRunResult struct {
	output    string
	truncated bool
}

func runCustomTool(ctx context.Context, tool customEditorTool, values toolContext) (toolRunResult, error) {
	directory, args, err := toolArguments(tool, values)
	if err != nil {
		return toolRunResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, tool.Executable, args...)
	command.Dir, command.WaitDelay = directory, 2*time.Second
	configureCustomTool(command)
	if tool.SelectionInput {
		command.Stdin = strings.NewReader(values.selection)
	}
	capture := &toolCapture{}
	command.Stdout, command.Stderr = capture, capture
	err = command.Run()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return toolRunResult{output: strings.ToValidUTF8(capture.buffer.String(), "�"), truncated: capture.truncated}, err
}
