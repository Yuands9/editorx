package editor

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/vivaui/viva/ui"
)

type aiSettingsEditor struct {
	original                                        aiSettings
	name, baseURL, model, key, headers, cliPath     ui.Field
	provider                                        string
	protocol                                        aiProtocol
	auth                                            aiAuth
	direct, clearKey, clearHeaders, viewingSavedKey bool
	advanced, removePending, modelsOpen, scrollTop  bool
	providersOpen                                   bool
	models                                          []string
	probe                                           *ui.EffectState[aiSettingsProbe, int]
	probeLabel, message, problem, suggestedName     string
	passed                                          bool
}

type aiSettingsProbe struct {
	models  []string
	model   string
	elapsed time.Duration
}

func (e *aiSettingsEditor) load(profile aiSettings) {
	*e = aiSettingsEditor{
		original: profile, provider: profile.Provider, protocol: profile.Protocol, auth: profile.Auth, direct: profile.Direct,
		key: ui.Field{Secure: true}, headers: ui.Field{Secure: true},
		advanced: len(profile.Headers) != 0 || profile.Direct || profile.Auth != aiAuthAuto, scrollTop: true,
	}
	e.name.SetText(profile.Name)
	e.suggestedName = profile.Name
	e.baseURL.SetText(profile.BaseURL)
	e.model.SetText(profile.Model)
	e.cliPath.SetText(profile.CLIPath)
}

func (e *aiSettingsEditor) dirty() bool {
	return strings.TrimSpace(e.name.Text()) != e.original.Name || strings.TrimSpace(e.baseURL.Text()) != e.original.BaseURL ||
		strings.TrimSpace(e.model.Text()) != e.original.Model || strings.TrimSpace(e.cliPath.Text()) != e.original.CLIPath ||
		e.provider != e.original.Provider || e.protocol != e.original.Protocol ||
		e.auth != e.original.Auth || e.direct != e.original.Direct || e.enteredKey() != "" || e.headers.Text() != "" || e.clearKey || e.clearHeaders
}

func (e *aiSettingsEditor) suggestName(profiles aiProfiles) {
	if e.original.ID != 0 {
		return
	}
	name := strings.TrimSpace(e.name.Text())
	if name != "" && name != e.suggestedName {
		return
	}
	if _, composing := e.name.GetEditor().Composition(); composing {
		return
	}
	base := strings.TrimSpace(e.model.GetEditor().CommittedString())
	if base == "" || strings.ContainsFunc(base, unicode.IsControl) {
		base = aiProviderLabel(e.provider)
		if e.provider == "custom" || e.provider == "compatible" {
			base = "新配置"
		}
	}
	e.suggestedName = profiles.availableName(base)
	if name != e.suggestedName {
		e.name.SetText(e.suggestedName)
	}
}

func (e *aiSettingsEditor) applyPreset(profiles aiProfiles) {
	preset := aiPresets[e.provider]
	e.baseURL.SetText(preset.baseURL)
	e.model.SetText("")
	e.cliPath.SetText("")
	e.suggestName(profiles)
	e.protocol, e.auth, e.direct = preset.protocol, aiAuthAuto, false
	// Switching a preset never copies typed credentials into another provider.
	e.key = ui.Field{Secure: true}
	e.headers = ui.Field{Secure: true}
	e.clearKey, e.clearHeaders, e.viewingSavedKey = false, false, false
	e.clearProbe(true)
}

func (e *aiSettingsEditor) enteredKey() string {
	if e.viewingSavedKey {
		return ""
	}
	return strings.TrimSpace(e.key.Text())
}

func (e *aiSettingsEditor) toggleKeyVisibility() {
	if !e.key.Secure {
		if e.viewingSavedKey {
			e.key = ui.Field{Secure: true}
			e.viewingSavedKey = false
		} else {
			e.key.Secure = true
		}
		return
	}
	if e.enteredKey() == "" && !e.clearKey && e.provider == e.original.Provider && len(e.original.Key) > 0 {
		key, err := windowsAIKey(e.original.Key, false)
		if err != nil {
			e.problem = "无法解锁已保存的密钥，请重新输入"
			return
		}
		defer clear(key)
		e.key.SetText(string(key))
		e.key.ReadOnly, e.viewingSavedKey = true, true
	}
	e.key.Secure = false
}

func (e *aiSettingsEditor) clearProbe(models bool) {
	e.message, e.problem, e.passed = "", "", false
	if models {
		e.models, e.modelsOpen = nil, false
	}
}

func (e *aiSettingsEditor) profile(requireModel bool) (aiSettings, error) {
	for _, field := range []*ui.Field{&e.name, &e.baseURL, &e.model, &e.key, &e.headers, &e.cliPath} {
		if editor := field.GetEditor(); editor != nil {
			if _, composing := editor.Composition(); composing {
				return aiSettings{}, errors.New("请先完成中文选词")
			}
		}
	}
	profile := e.original
	profile.Name, profile.BaseURL, profile.Model = strings.TrimSpace(e.name.Text()), strings.TrimSpace(e.baseURL.Text()), strings.TrimSpace(e.model.Text())
	profile.Provider, profile.Protocol, profile.Auth, profile.Direct = e.provider, e.protocol, e.auth, e.direct
	if profile.usesCLI() {
		profile.CLIPath = strings.TrimSpace(e.cliPath.Text())
		profile.BaseURL, profile.Auth, profile.Direct = "", aiAuthNone, false
		profile.Key, profile.Headers = nil, nil
		if profile.Model != "" && !validAIModelID(profile.Model) {
			return aiSettings{}, errors.New("请输入有效的模型 ID")
		}
		if profile.CLIPath != "" {
			if _, err := profile.cliExecutable(); err != nil {
				return aiSettings{}, err
			}
		}
		return profile, nil
	}
	profile.CLIPath = ""
	if requireModel && !validAIModelID(profile.Model) {
		return aiSettings{}, errors.New("请输入有效的模型 ID")
	}
	if !slices.Contains(aiProtocols, profile.Protocol) {
		return aiSettings{}, errors.New("请选择 API 协议")
	}
	base, err := profile.apiBase()
	if err != nil {
		return aiSettings{}, err
	}
	oldBase, _ := e.original.apiBase()
	key, headers := e.enteredKey(), strings.TrimSpace(e.headers.Text())
	if len(key) > 16*1024 || strings.ContainsFunc(key, unicode.IsControl) {
		return aiSettings{}, errors.New("API 密钥超长或包含控制字符")
	}
	if base != oldBase && (len(profile.Key) > 0 && key == "" && !e.clearKey || len(profile.Headers) > 0 && headers == "" && !e.clearHeaders) {
		return aiSettings{}, errors.New("接口地址已改变，请重新填写或清除原密钥和请求头")
	}
	if e.clearKey {
		profile.Key = nil
	}
	if key != "" {
		profile.Key, err = windowsAIKey([]byte(key), true)
		if err != nil {
			return aiSettings{}, err
		}
	}
	if e.clearHeaders {
		profile.Headers = nil
	}
	if headers != "" {
		if _, err := parseAIHeaders(headers); err != nil {
			return aiSettings{}, err
		}
		profile.Headers, err = windowsAIKey([]byte(headers), true)
		if err != nil {
			return aiSettings{}, err
		}
	}
	return profile, nil
}

func (m *editorApp) reloadAIProfiles() error {
	profiles, err := readAIProfiles(m.dataDirectory)
	if err != nil {
		return err
	}
	m.ai.profiles = profiles
	if profiles.index(m.ai.profileID) < 0 {
		m.ai.profileID = 0
	}
	return nil
}

func (m *editorApp) toggleAIPanel() {
	a := &m.ai
	if !a.open {
		m.openAIAt(m.active, "", false)
		return
	}
	a.open, a.focusPrompt = false, false
	a.profileMenu, a.contextMenu, a.historyMenu, a.modeMenu = false, false, false, false
	m.focusActiveSource()
}

func (m *editorApp) openAIAt(tab *editorTab, path string, directory bool) {
	a := &m.ai
	if !a.open {
		if err := m.reloadAIProfiles(); err != nil {
			a.message = err.Error()
		}
		if !a.conversationStarted {
			a.mode, a.conversationStarted = a.profiles.DefaultMode, true
		}
	}
	m.setAIDefaultContext(tab, path, directory)
	a.open, a.focusPrompt = true, true
	a.profileMenu, a.contextMenu, a.historyMenu, a.modeMenu = false, false, false, false
}

func (m *editorApp) openAISettings() {
	m.showSettings(settingsAI)
	m.appSettings.returnToAI = m.ai.open
}

func (m *editorApp) reloadAISettingsPage() {
	a := &m.ai
	err := m.reloadAIProfiles()
	profile := a.profiles.active(a.profileID)
	if profile.ID == 0 {
		profile = aiSettings{Name: "新配置", Provider: "custom", Protocol: aiChat}
	}
	a.settings.load(profile)
	if err != nil {
		a.settings.problem = err.Error()
	}
	a.settingsEditing, a.profileMenu = false, false
}

func (m *editorApp) editAISettings(profile aiSettings) {
	m.ai.settings.load(profile)
	m.ai.settingsEditing = true
}

func (m *editorApp) saveAIChatPreferences(mode, size int) {
	a := &m.ai
	profiles, err := readAIProfiles(m.dataDirectory)
	if err == nil {
		if mode != a.profiles.DefaultMode {
			profiles.DefaultMode = mode
		}
		if size != a.profiles.chatFontSize() {
			profiles.ChatFont = size
		}
		err = profiles.save(m.dataDirectory)
	}
	if err != nil {
		a.settings.problem = err.Error()
		return
	}
	a.profiles = profiles
	if m.desktop != nil {
		for _, window := range m.desktop.store.windows {
			if !window.windowClosed {
				window.ai.profiles.DefaultMode = profiles.DefaultMode
				window.ai.profiles.ChatFont = profiles.ChatFont
				window.invalidator.Invalidate()
			}
		}
	}
	a.settings.message, a.settings.problem, a.settings.passed = "对话偏好已保存", "", true
}

// Read the catalog again before writing so changes from another window are
// preserved and a removed or replaced configuration cannot be saved over.
func (m *editorApp) editableAIProfiles() (aiProfiles, int, error) {
	profiles, err := readAIProfiles(m.dataDirectory)
	if err != nil {
		return aiProfiles{}, -1, err
	}
	original := m.ai.settings.original
	index := profiles.index(original.ID)
	if original.ID != 0 && (index < 0 || !reflect.DeepEqual(profiles.Profiles[index], original)) {
		return aiProfiles{}, -1, errors.New("此配置已在其他窗口修改，请重新打开设置")
	}
	return profiles, index, nil
}

func (m *editorApp) applyAIProfileChanges() bool {
	if !m.ai.settingsEditing || !m.ai.settings.dirty() {
		return true
	}
	m.saveAISettings()
	return m.ai.settings.problem == ""
}

func (m *editorApp) saveAISettings() {
	a, e := &m.ai, &m.ai.settings
	profiles, index, err := m.editableAIProfiles()
	if err != nil {
		e.problem = err.Error()
		return
	}
	e.suggestName(profiles)
	profile, err := e.profile(true)
	if err != nil {
		e.problem = err.Error()
		return
	}
	if profile.Name == "" || utf8.RuneCountInString(profile.Name) > aiProfileNameLimit || strings.ContainsFunc(profile.Name, unicode.IsControl) {
		e.problem = fmt.Sprintf("请输入有效的配置名称，最多 %d 个字符", aiProfileNameLimit)
		return
	}
	for _, other := range profiles.Profiles {
		if other.ID != profile.ID && strings.EqualFold(other.Name, profile.Name) {
			e.problem = "已有同名配置，请换一个名称"
			return
		}
	}
	if index < 0 {
		for _, other := range profiles.Profiles {
			profile.ID = max(profile.ID, other.ID)
		}
		profile.ID++
		profiles.Profiles = append(profiles.Profiles, profile)
	} else {
		profiles.Profiles[index] = profile
	}
	if profiles.index(profiles.DefaultID) < 0 {
		profiles.DefaultID = profile.ID
	}
	if err := profiles.save(m.dataDirectory); err != nil {
		e.problem = err.Error()
		return
	}
	a.profiles = profiles
	models, message, passed := e.models, e.message, e.passed
	e.load(profile)
	e.models, e.scrollTop = models, false
	e.message, e.passed = "配置已保存", true
	if passed {
		e.message = message + " · 已保存"
	}
	a.message = "模型配置已保存"
}

func (m *editorApp) setDefaultAISettings() {
	a, e := &m.ai, &m.ai.settings
	profiles, index, err := m.editableAIProfiles()
	if err != nil {
		e.problem = err.Error()
		return
	}
	if index < 0 {
		return
	}
	profiles.DefaultID = e.original.ID
	if err := profiles.save(m.dataDirectory); err != nil {
		e.problem = err.Error()
		return
	}
	a.profiles, a.profileID = profiles, 0
	e.problem, e.message, e.passed = "", "已设为默认配置", true
}

func (m *editorApp) removeAISettings() {
	a, e := &m.ai, &m.ai.settings
	profiles, index, err := m.editableAIProfiles()
	if err != nil {
		e.problem = err.Error()
		return
	}
	if index < 0 {
		return
	}
	profiles.Profiles = slices.Delete(profiles.Profiles, index, index+1)
	if profiles.DefaultID == e.original.ID {
		profiles.DefaultID = profiles.active(0).ID
	}
	if err := profiles.save(m.dataDirectory); err != nil {
		e.problem = err.Error()
		return
	}
	m.reloadAISettingsPage()
	a.settings.message, a.settings.passed = "配置已删除", true
}

func (m *editorApp) startAISettingsProbe(discover bool) {
	e := &m.ai.settings
	profile, err := e.profile(!discover)
	if err != nil {
		e.problem = err.Error()
		return
	}
	profile.cliDirectory = m.dataDirectory
	e.clearProbe(discover)
	e.probeLabel = "正在测试连接"
	if discover {
		e.probeLabel = "正在获取模型"
	}
	e.probe = ui.NewEffect[aiSettingsProbe, int](func(ctx context.Context) (aiSettingsProbe, error) {
		ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
		defer cancel()
		start := time.Now()
		if discover {
			models, err := profile.models(ctx)
			return aiSettingsProbe{models: models, elapsed: time.Since(start)}, err
		}
		reply, err := profile.infer(ctx, "Reply briefly in plain text.", []aiMessage{{Role: "user", Content: "Reply with OK."}}, "", 128, nil)
		if err != nil {
			return aiSettingsProbe{}, err
		}
		// A reasoning model may reach the probe output limit before any visible text.
		if !reply.Truncated && strings.TrimSpace(reply.Text) == "" {
			return aiSettingsProbe{}, errors.New("服务已连接，但该模型没有返回文本")
		}
		model := reply.Model
		if model == "" {
			model = profile.modelLabel(profile.Model)
		}
		return aiSettingsProbe{model: model, elapsed: time.Since(start)}, nil
	})
}

func (e *aiSettingsEditor) updateProbe(c ui.C) {
	if e.probe == nil {
		return
	}
	ui.Effect(c, e.probe, 0)
	result, err, ready := e.probe.Take()
	if !ready {
		return
	}
	e.probe = nil
	if err != nil {
		e.problem = err.Error()
		return
	}
	e.passed = true
	if result.model != "" {
		e.message = fmt.Sprintf("连接成功 · %d ms · %s", max(1, result.elapsed.Milliseconds()), result.model)
		return
	}
	e.models = result.models
	if len(e.models) == 0 {
		e.message = "服务未返回模型，可直接填写模型 ID"
		return
	}
	e.message = fmt.Sprintf("已获取 %d 个模型，可从列表选择", len(e.models))
}
