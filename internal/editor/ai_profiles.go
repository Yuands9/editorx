package editor

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

type aiSettings struct {
	ID           int        `json:"id"`
	Name         string     `json:"name"`
	Provider     string     `json:"provider"`
	BaseURL      string     `json:"base_url"`
	Model        string     `json:"model"`
	CLIPath      string     `json:"cli_path,omitempty"`
	Protocol     aiProtocol `json:"protocol"`
	Auth         aiAuth     `json:"auth,omitempty"`
	Direct       bool       `json:"direct,omitempty"`
	Key          []byte     `json:"encrypted_key,omitempty"`
	Headers      []byte     `json:"encrypted_headers,omitempty"`
	cliDirectory string
}

type aiProviderPreset struct {
	label, baseURL string
	protocol       aiProtocol
}

var aiPresetIDs = []string{
	"anthropic", "openai", "gemini", "deepseek", "kimi", "qwen", "zhipu", "minimax",
	aiClaudeCLI, aiCodexCLI, aiOpenCodeCLI, aiCursorCLI, aiCodeBuddyCLI, aiQoderCLI, aiGrokCLI, aiPiCLI, "custom",
}

var aiPresets = map[string]aiProviderPreset{
	"anthropic":            {"Claude", "https://api.anthropic.com/v1", aiMessages},
	"openai":               {"OpenAI", "https://api.openai.com/v1", aiResponses},
	"gemini":               {"Gemini", "https://generativelanguage.googleapis.com/v1beta/openai", aiChat},
	"deepseek":             {"DeepSeek", "https://api.deepseek.com/v1", aiChat},
	"kimi":                 {"Kimi", "https://api.moonshot.cn/v1", aiChat},
	"qwen":                 {"通义千问", "https://dashscope.aliyuncs.com/compatible-mode/v1", aiChat},
	"zhipu":                {"智谱", "https://open.bigmodel.cn/api/paas/v4", aiChat},
	"minimax":              {"MiniMax", "https://api.minimax.io/v1", aiChat},
	aiClaudeCLI:            {"Claude Code CLI", "", aiMessages},
	aiOpenCodeCLI:          {"OpenCode CLI", "", aiChat},
	aiCursorCLI:            {"Cursor CLI", "", aiChat},
	aiCodeBuddyCLI:         {"CodeBuddy Code CLI", "", aiMessages},
	aiQoderCLI:             {"Qoder CLI", "", aiMessages},
	aiGrokCLI:              {"Grok CLI", "", aiResponses},
	aiPiCLI:                {"Pi Coding Agent", "", aiChat},
	aiCodexCLI:             {"Codex CLI", "", aiResponses},
	"custom":               {"自定义 API", "", aiChat},
	"compatible":           {"自定义 API", "", aiChat},
	"anthropic-compatible": {"自定义 API", "", aiMessages},
	"ollama":               {"Ollama", "http://127.0.0.1:11434/v1", aiChat},
}

func aiProviderLabel(id string) string {
	if preset, exists := aiPresets[id]; exists {
		return preset.label
	}
	return "自定义服务"
}

func aiServiceModelLabel(profile aiSettings) string {
	name := profile.Name
	if strings.TrimSpace(name) == "" {
		name = aiProviderLabel(profile.Provider)
	}
	if profile.Model == "" {
		return name
	}
	return name + " · " + profile.Model
}

type aiProfiles struct {
	DefaultID   int          `json:"default_id,omitempty"`
	DefaultMode int          `json:"default_mode,omitempty"`
	ChatFont    int          `json:"chat_font_size,omitempty"`
	Profiles    []aiSettings `json:"profiles"`
}

func readAIProfiles(directory string) (aiProfiles, error) {
	data, err := os.ReadFile(filepath.Join(directory, "ai-settings.json"))
	if errors.Is(err, os.ErrNotExist) {
		return aiProfiles{}, nil
	}
	if err != nil {
		return aiProfiles{}, err
	}
	var file struct {
		aiProfiles
		aiSettings
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return aiProfiles{}, errors.New(uiText("AI 设置文件无法读取"))
	}
	if file.Profiles == nil && file.BaseURL != "" {
		// 旧版只有一套 Chat Completions 设置。沿用其地址、模型和密文。
		legacy := file.aiSettings
		legacy.ID, legacy.Name, legacy.Provider, legacy.Protocol = 1, "原有配置", "compatible", aiChat
		return aiProfiles{DefaultID: legacy.ID, Profiles: []aiSettings{legacy}}, nil
	}
	if file.DefaultMode < aiAsk || file.DefaultMode > aiDraft {
		file.DefaultMode = aiAsk
	}
	if file.ChatFont < 12 || file.ChatFont > 20 {
		file.ChatFont = 0
	}
	return file.aiProfiles, nil
}

func (profiles aiProfiles) save(directory string) error {
	data, err := json.MarshalIndent(profiles, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	return writeFile(filepath.Join(directory, "ai-settings.json"), data)
}

func (profiles aiProfiles) index(id int) int {
	return slices.IndexFunc(profiles.Profiles, func(profile aiSettings) bool { return profile.ID == id })
}

func (profiles aiProfiles) active(id int) aiSettings {
	if index := profiles.index(id); index >= 0 {
		return profiles.Profiles[index]
	}
	if index := profiles.index(profiles.DefaultID); index >= 0 {
		return profiles.Profiles[index]
	}
	if len(profiles.Profiles) != 0 {
		return profiles.Profiles[0]
	}
	return aiSettings{}
}

func validAIModelID(model string) bool {
	return model != "" && len(model) <= 256 && !strings.ContainsFunc(model, unicode.IsControl)
}

const aiProfileNameLimit = 40

func (profiles aiProfiles) availableName(name string) string {
	characters := []rune(name)
	for suffix := 1; ; suffix++ {
		var ending string
		if suffix > 1 {
			ending = " " + strconv.Itoa(suffix)
		}
		candidate := strings.TrimSpace(string(characters[:min(len(characters), aiProfileNameLimit-len(ending))])) + ending
		if !slices.ContainsFunc(profiles.Profiles, func(p aiSettings) bool { return strings.EqualFold(p.Name, candidate) }) {
			return candidate
		}
	}
}
