package editor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

const aiResponseLimit = 2 * 1024 * 1024

type aiProtocol string

const (
	aiChat      aiProtocol = "chat/completions"
	aiResponses aiProtocol = "responses"
	aiMessages  aiProtocol = "messages"
)

var aiProtocols = []aiProtocol{aiChat, aiResponses, aiMessages}

func aiProtocolLabel(protocol aiProtocol) string {
	switch protocol {
	case aiResponses:
		return "OpenAI Responses"
	case aiMessages:
		return "Anthropic Messages"
	default:
		return "Chat Completions"
	}
}

type aiAuth string

const (
	aiAuthAuto   aiAuth = ""
	aiAuthBearer aiAuth = "bearer"
	aiAuthAPIKey aiAuth = "x-api-key"
	aiAuthNone   aiAuth = "none"
)

var aiAuthOptions = []aiAuth{aiAuthAuto, aiAuthBearer, aiAuthAPIKey, aiAuthNone}

func aiAuthLabel(auth aiAuth) string {
	switch auth {
	case aiAuthBearer:
		return "Bearer Token"
	case aiAuthAPIKey:
		return "X-API-Key"
	case aiAuthNone:
		return "无认证"
	default:
		return "按协议自动选择"
	}
}

func (settings aiSettings) apiBase() (string, error) {
	address, err := url.Parse(settings.BaseURL)
	if err != nil || address.Hostname() == "" || address.User != nil || address.RawQuery != "" || address.ForceQuery || address.Fragment != "" {
		return "", errors.New(uiText("接口地址无效，需要填写完整 URL"))
	}
	local := aiLocalHost(address.Hostname())
	if address.Scheme != "https" && !(address.Scheme == "http" && local) {
		return "", errors.New(uiText("接口需使用 HTTPS，本机回环地址可使用 HTTP"))
	}
	path := strings.TrimRight(address.EscapedPath(), "/")
	for _, suffix := range []string{"/chat/completions", "/responses", "/messages", "/models"} {
		if base, ok := strings.CutSuffix(path, suffix); ok {
			path = base
			break
		}
	}
	if path == "" {
		path = "/v1"
	}
	return address.Scheme + "://" + strings.ToLower(address.Host) + path, nil
}

func aiLocalHost(host string) bool {
	return strings.EqualFold(host, "localhost") || net.ParseIP(host).IsLoopback()
}

type aiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type aiTextBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type aiCompletion struct {
	Text, Model string
	Truncated   bool
}

var (
	aiHTTPClient = &http.Client{
		Timeout:       90 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	aiDirectHTTPClient = func() *http.Client {
		client := *aiHTTPClient
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = nil
		client.Transport = transport
		return &client
	}()
)

type aiHTTPError struct{ status int }

func (err aiHTTPError) Error() string {
	switch err.status {
	case 400, 422:
		return fmt.Sprintf("服务拒绝了请求（HTTP %d），请检查模型、API 协议或模型允许的上下文长度", err.status)
	case 401:
		return "认证失败（HTTP 401），请检查 API 密钥和认证方式"
	case 402:
		return "账户余额或配额不足（HTTP 402）"
	case 403:
		return "访问被拒绝（HTTP 403），请检查密钥权限和账号配置"
	case 404:
		return "接口或模型不存在（HTTP 404），请检查地址、协议和模型名称"
	case 413:
		return "请求超过服务商允许的大小（HTTP 413）"
	case 429:
		return "服务限流或额度用尽（HTTP 429），请稍后重试"
	case 502, 503, 504:
		return fmt.Sprintf("服务或网关暂不可用（HTTP %d）", err.status)
	default:
		return fmt.Sprintf("模型服务返回 HTTP %d", err.status)
	}
}

func (settings aiSettings) openResponse(ctx context.Context, method, operation string, body []byte, streaming bool) (*http.Response, error) {
	base, err := settings.apiBase()
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, method, base+"/"+operation, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New(uiText("无法创建模型请求，请检查接口地址"))
	}
	headerBytes, err := windowsAIKey(settings.Headers, false)
	if err != nil {
		return nil, errors.New(uiText("无法解锁自定义请求头，请在模型设置中重新输入"))
	}
	defer clear(headerBytes)
	if len(headerBytes) != 0 {
		headers, err := parseAIHeaders(string(headerBytes))
		if err != nil {
			return nil, err
		}
		request.Header = headers
	}
	request.Header.Set("Accept", "application/json")
	if streaming {
		request.Header.Set("Accept", "text/event-stream, application/json")
	}
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/json")
	}
	if settings.Protocol == aiMessages {
		request.Header.Set("anthropic-version", "2023-06-01")
	}
	if settings.Auth != aiAuthNone {
		key, err := windowsAIKey(settings.Key, false)
		if err != nil {
			return nil, err
		}
		defer clear(key)
		if len(key) != 0 {
			if settings.Auth == aiAuthAPIKey || (settings.Auth == aiAuthAuto && settings.Protocol == aiMessages) {
				request.Header.Set("X-Api-Key", string(key))
			} else {
				request.Header.Set("Authorization", "Bearer "+string(key))
			}
		}
	}
	client := aiHTTPClient
	if settings.Direct || aiLocalHost(request.URL.Hostname()) {
		client = aiDirectHTTPClient
	}
	response, err := client.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, errors.New(uiText("模型请求超时，请检查地址、网络或代理"))
		}
		return nil, errors.New(uiText("无法连接模型服务，请检查地址、网络或代理"))
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, aiHTTPError{status: response.StatusCode}
	}
	return response, nil
}

func readAIResponse(reader io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, aiResponseLimit+1))
	if err != nil {
		return nil, errors.New(uiText("读取模型响应失败"))
	}
	if len(data) > aiResponseLimit {
		return nil, errors.New(uiText("模型响应超过 2 MB"))
	}
	return data, nil
}

func (settings aiSettings) canListModels() bool {
	switch settings.Provider {
	case aiCodexCLI, aiOpenCodeCLI, aiCursorCLI, aiQoderCLI, aiPiCLI:
		return true
	default:
		return !settings.usesCLI()
	}
}

func (settings aiSettings) models(ctx context.Context) ([]string, error) {
	if settings.Provider == aiCodexCLI {
		return settings.codexModels(ctx)
	}
	if settings.usesCLI() {
		if settings.canListModels() {
			return settings.cliModelList(ctx)
		}
		return nil, errors.New(uiText("CLI 可使用默认模型或手动填写模型 ID"))
	}
	response, err := settings.openResponse(ctx, http.MethodGet, "models", nil, false)
	if err != nil {
		var responseErr aiHTTPError
		if errors.As(err, &responseErr) && (responseErr.status == 404 || responseErr.status == 405 || responseErr.status == 501) {
			return nil, errors.New(uiText("此服务未提供模型列表，可直接填写模型 ID"))
		}
		return nil, err
	}
	defer response.Body.Close()
	data, err := readAIResponse(response.Body)
	if err != nil {
		return nil, err
	}
	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &result); err != nil || result.Data == nil {
		return nil, errors.New(uiText("模型列表格式不兼容，可直接填写模型 ID"))
	}
	models := make([]string, 0, len(result.Data))
	for _, model := range result.Data {
		if id := strings.TrimSpace(model.ID); validAIModelID(id) {
			models = append(models, id)
		}
	}
	slices.Sort(models)
	return slices.Compact(models), nil
}

func (settings aiSettings) complete(ctx context.Context, system string, messages []aiMessage, effort aiReasoning, onDelta func(string)) (string, error) {
	reply, err := settings.infer(ctx, system, messages, effort, 0, onDelta)
	if err != nil {
		return "", err
	}
	if reply.Truncated {
		return "", errors.New(uiText("模型输出未完成，请缩小修改范围"))
	}
	if strings.TrimSpace(reply.Text) == "" {
		return "", errors.New(uiText("模型没有返回文本，请检查模型能力或修改指令"))
	}
	return reply.Text, nil
}

func (settings aiSettings) infer(ctx context.Context, system string, messages []aiMessage, effort aiReasoning, probeLimit int, onDelta func(string)) (aiCompletion, error) {
	if settings.Provider == aiCodexCLI {
		reply, sent, err := settings.runCodex(ctx, system, messages, effort, onDelta)
		if err == nil || sent || ctx.Err() != nil {
			return reply, err
		}
		// Older Codex installations can still complete a reply with exec.
	}
	if len(otherCLIPrograms[settings.Provider]) != 0 || settings.Provider == aiClaudeCLI && onDelta != nil {
		return settings.runTextCLI(ctx, system, messages, effort, onDelta)
	}
	if settings.usesCLI() {
		return settings.runCLI(ctx, system, messages, effort)
	}
	if settings.Model == "" {
		return aiCompletion{}, errors.New(uiText("请输入模型 ID"))
	}
	protocol := settings.Protocol
	if protocol == "" {
		protocol = aiChat
	}
	var outputConfig *aiEffortConfig
	var thinking *aiThinkingConfig
	if effort != "" {
		outputConfig = &aiEffortConfig{Effort: effort}
		if settings.isDeepSeek() {
			thinking = &aiThinkingConfig{Type: "enabled"}
		}
	}
	var payload any
	streaming := onDelta != nil
	switch protocol {
	case aiChat:
		body := struct {
			Stream        bool              `json:"stream,omitempty"`
			Model         string            `json:"model"`
			Messages      []aiMessage       `json:"messages"`
			MaxTokens     int               `json:"max_tokens,omitempty"`
			CompletionCap int               `json:"max_completion_tokens,omitempty"`
			Effort        aiReasoning       `json:"reasoning_effort,omitempty"`
			Thinking      *aiThinkingConfig `json:"thinking,omitempty"`
		}{Stream: streaming, Model: settings.Model, Messages: append([]aiMessage{{Role: "system", Content: system}}, messages...), Effort: effort, Thinking: thinking}
		if settings.Provider == "openai" {
			body.CompletionCap = probeLimit
		} else {
			body.MaxTokens = probeLimit
		}
		payload = body
	case aiResponses:
		var input any = messages
		if len(messages) == 1 {
			input = messages[0].Content
		}
		payload = struct {
			Stream          bool            `json:"stream,omitempty"`
			Model           string          `json:"model"`
			Instructions    string          `json:"instructions"`
			Input           any             `json:"input"`
			MaxOutputTokens int             `json:"max_output_tokens,omitempty"`
			Store           bool            `json:"store"`
			Reasoning       *aiEffortConfig `json:"reasoning,omitempty"`
		}{Stream: streaming, Model: settings.Model, Instructions: system, Input: input, MaxOutputTokens: probeLimit, Reasoning: outputConfig}
	case aiMessages:
		limit := probeLimit
		if limit == 0 {
			limit = 8192
		}
		payload = struct {
			Stream       bool              `json:"stream,omitempty"`
			Model        string            `json:"model"`
			System       string            `json:"system"`
			Messages     []aiMessage       `json:"messages"`
			MaxTokens    int               `json:"max_tokens"`
			OutputConfig *aiEffortConfig   `json:"output_config,omitempty"`
			Thinking     *aiThinkingConfig `json:"thinking,omitempty"`
		}{Stream: streaming, Model: settings.Model, System: system, Messages: messages, MaxTokens: limit, OutputConfig: outputConfig, Thinking: thinking}
	default:
		return aiCompletion{}, errors.New(uiText("无法识别此配置的 API 协议"))
	}
	reply, err := settings.requestAICompletion(ctx, protocol, payload, onDelta)
	var responseErr aiHTTPError
	if effort != "" && errors.As(err, &responseErr) && (responseErr.status == 400 || responseErr.status == 422) {
		return aiCompletion{}, fmt.Errorf(uiText("%w。可将思考水平切回「服务默认」后重试"), err)
	}
	return reply, err
}

func (settings aiSettings) requestAICompletion(ctx context.Context, protocol aiProtocol, payload any, onDelta func(string)) (aiCompletion, error) {
	request, err := json.Marshal(payload)
	if err != nil {
		return aiCompletion{}, err
	}
	streaming := onDelta != nil
	response, err := settings.openResponse(ctx, http.MethodPost, string(protocol), request, streaming)
	if err != nil {
		return aiCompletion{}, err
	}
	defer response.Body.Close()
	mediaType, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if streaming && mediaType == "text/event-stream" {
		return readAIStream(protocol, response.Body, onDelta)
	}
	data, err := readAIResponse(response.Body)
	if err != nil {
		return aiCompletion{}, err
	}
	return decodeAICompletion(protocol, data)
}

func decodeAICompletion(protocol aiProtocol, data []byte) (aiCompletion, error) {
	var result struct {
		Model      string        `json:"model"`
		Status     string        `json:"status"`
		StopReason string        `json:"stop_reason"`
		Content    []aiTextBlock `json:"content"`
		Choices    []struct {
			Message      aiMessage `json:"message"`
			FinishReason string    `json:"finish_reason"`
		} `json:"choices"`
		Output []struct {
			Type    string        `json:"type"`
			Content []aiTextBlock `json:"content"`
		} `json:"output"`
	}
	badResponse := fmt.Errorf(uiText("模型响应与 %s 格式不兼容"), aiProtocolLabel(protocol))
	if err := json.Unmarshal(data, &result); err != nil {
		return aiCompletion{}, badResponse
	}
	reply := aiCompletion{Model: result.Model}
	var output strings.Builder
	switch protocol {
	case aiChat:
		if len(result.Choices) == 0 {
			return aiCompletion{}, badResponse
		}
		reply.Text, reply.Truncated = result.Choices[0].Message.Content, result.Choices[0].FinishReason == "length"
		return reply, nil
	case aiResponses:
		if result.Output == nil || (result.Status != "" && result.Status != "completed" && result.Status != "incomplete") {
			return aiCompletion{}, badResponse
		}
		reply.Truncated = result.Status == "incomplete"
		for _, item := range result.Output {
			if item.Type != "message" {
				continue
			}
			for _, part := range item.Content {
				if part.Type == "output_text" {
					output.WriteString(part.Text)
				}
			}
		}
	case aiMessages:
		if result.Content == nil {
			return aiCompletion{}, badResponse
		}
		reply.Truncated = result.StopReason == "max_tokens"
		for _, part := range result.Content {
			if part.Type == "text" {
				output.WriteString(part.Text)
			}
		}
	}
	reply.Text = output.String()
	return reply, nil
}

func parseAIHeaders(source string) (http.Header, error) {
	if len(source) > 16*1024 {
		return nil, errors.New(uiText("自定义请求头超过 16 KB"))
	}
	var values map[string]string
	if err := json.Unmarshal([]byte(source), &values); err != nil || values == nil {
		return nil, errors.New(uiText("请求头须为名称和值均为字符串的 JSON 对象"))
	}
	headers := make(http.Header, len(values))
	for name, value := range values {
		if name == "" || strings.ContainsFunc(name, func(c rune) bool {
			return !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'*+-.^_|~", c))
		}) || strings.ContainsFunc(value, func(c rune) bool { return c < 32 && c != '\t' || c == 127 }) {
			return nil, errors.New(uiText("请求头的名称或值含有无效字符"))
		}
		switch strings.ToLower(name) {
		case "authorization", "proxy-authorization", "x-api-key", "anthropic-version", "accept", "content-type",
			"host", "content-length", "transfer-encoding", "connection", "upgrade", "trailer":
			return nil, fmt.Errorf(uiText("请求头 %s 由认证或请求协议管理"), name)
		}
		headers.Set(name, value)
	}
	return headers, nil
}
