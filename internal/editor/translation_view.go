package editor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

var translationIcon = gfx.Icon{ViewBox: 24, Stroke: 1.6, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(2, 6), gfx.LineTo(13, 6), gfx.MoveTo(7.5, 3), gfx.LineTo(7.5, 6),
	gfx.MoveTo(11, 6), gfx.CubicTo(10, 12, 7, 15, 3, 17),
	gfx.MoveTo(4, 9), gfx.CubicTo(5, 12, 7, 14, 10, 15),
	gfx.MoveTo(12, 21), gfx.LineTo(17, 10), gfx.LineTo(22, 21), gfx.MoveTo(14, 17), gfx.LineTo(20, 17),
}}

type translationSuggestion struct {
	entry translationEntry
	scope string
}

type translationMode int

const (
	translationLocalMode translationMode = iota
	translationOfflineMode
)

type translationPanelState struct {
	split                    ui.SplitterState
	managing, projectScope   bool
	mode                     translationMode
	showTerms, localLookup   bool
	hasOutput                bool
	sourceLang, targetLang   translationLanguageChoice
	target                   textEditTarget
	querySource, queryTarget string
	suggestions, terms       []translationSuggestion
	options                  []int
	selected                 int
	output                   ui.Field
	checkedOutput            string
	outputIssue              error
	message, problem         string
	draft                    *translationEntryDraft
	aiMenu                   bool
	aiServices               []aiSettings
	request                  *ui.EffectState[string, int]
	offline                  *offlineTranslationState
	query                    ui.Field
	dictionary               *dictionaryInfo
	dictionaryChecked        bool
	dictionaryBroken         bool
	download                 *ui.EffectState[dictionaryInfo, int]
	downloadProgress         *dictionaryProgress
	dictionaryProblem        string
	removeDictionary         bool
	libraryLoaded            bool
	libraryProject           string
	libraryCount             int
	libraryPreview           []translationEntry
}

type translationLanguageChoice struct {
	code   string
	custom ui.Field
}

var translationLanguages = [...]string{"en", "zh-CN", "zh-TW", "ja", "ko", "fr", "de", "es", "ru", "pt", "custom"}

func translationLanguageLabel(code string) string {
	switch code {
	case "en":
		return "英语"
	case "zh-CN":
		return "简体中文"
	case "zh-TW":
		return "繁體中文"
	case "ja":
		return "日语"
	case "ko":
		return "韩语"
	case "fr":
		return "法语"
	case "de":
		return "德语"
	case "es":
		return "西班牙语"
	case "ru":
		return "俄语"
	case "pt":
		return "葡萄牙语"
	default:
		return "其他语言…"
	}
}

func (choice *translationLanguageChoice) value() string {
	if choice.code == "custom" {
		return choice.custom.Text()
	}
	return choice.code
}

type translationEntryDraft struct {
	project                 string
	sourceLang, targetLang  string
	kind                    string
	source, target, context ui.Field
}

func (m *editorApp) openTranslation(lookup bool) {
	if !m.translationEnabled {
		return
	}
	if m.translation == nil {
		if m.ai.open {
			m.toggleAIPanel()
		}
		m.templatePanel = nil
		m.translation = &translationPanelState{split: m.newAssetSplit(), mode: m.translationMode}
		m.translation.sourceLang.code, m.translation.targetLang.code = "en", "zh-CN"
	}
	if lookup {
		m.lookupTranslation()
	}
}

func (m *editorApp) toggleTranslation() {
	if m.leavePasswordVault() && m.translation != nil {
		return
	}
	if m.translation != nil {
		m.translation = nil
		m.focusActiveSource()
		return
	}
	hasSelection := false
	if editableTextSource(m.active) {
		span := m.active.source.text.Selection()
		hasSelection = span.Start != span.End
	}
	m.openTranslation(hasSelection)
}

func (state *translationPanelState) clearLookup() {
	state.target = textEditTarget{}
	state.request, state.aiMenu, state.aiServices = nil, false, nil
	if state.offline != nil {
		state.offline.request = nil
	}
	state.clearTranslationResult()
}

func (state *translationPanelState) clearTranslationResult() {
	state.suggestions, state.terms, state.options = nil, nil, nil
	state.output = ui.Field{}
	state.checkedOutput, state.outputIssue = "", nil
	state.selected = 0
	state.showTerms, state.localLookup, state.hasOutput = false, false, false
}

func (state *translationPanelState) translating() bool {
	return state.request != nil || state.offline != nil && state.offline.request != nil
}

func (state *translationPanelState) languagePair() (string, string, error) {
	source, target := languageCode(state.sourceLang.value()), languageCode(state.targetLang.value())
	if !validLanguageCode(source) || !validLanguageCode(target) || source == target {
		return "", "", errors.New("请输入不同的语言代码，如 en 和 zh-CN")
	}
	return source, target, nil
}

func (state *translationPanelState) matchesLanguages() bool {
	source, target, err := state.languagePair()
	return err == nil && source == state.querySource && target == state.queryTarget
}

func (state *translationPanelState) matchesQuery() bool {
	return state.target.selected != "" && state.query.Text() == state.target.selected && state.matchesLanguages()
}

func (m *editorApp) lookupTranslation() {
	state := m.translation
	if state == nil || !m.translationEnabled {
		return
	}
	state.clearLookup()
	state.managing, state.message, state.problem, state.draft = false, "", "", nil
	source, target, err := state.languagePair()
	if err != nil {
		state.problem = err.Error()
		return
	}
	selection, err := captureTextTarget(m.active, true, translationTextLimit)
	if err != nil {
		state.problem = err.Error()
		return
	}
	if strings.TrimSpace(selection.selected) == "" {
		state.problem = "请选择需要翻译的文字"
		return
	}
	state.target = selection
	state.query.SetText(selection.selected)
	state.querySource, state.queryTarget = source, target
	if state.mode == translationLocalMode {
		m.findLocalTranslations(state)
	}
}

func (m *editorApp) lookupTypedTranslation() {
	state := m.translation
	if state == nil || !m.translationEnabled || state.translating() {
		return
	}
	if err := m.prepareTranslationQuery(state); err != nil {
		state.problem = err.Error()
		return
	}
	state.managing, state.draft = false, nil
	m.findLocalTranslations(state)
}

func (m *editorApp) prepareTranslationQuery(state *translationPanelState) error {
	if _, composing := state.query.GetEditor().Composition(); composing {
		return errors.New("请先完成中文选词")
	}
	query := state.query.Text()
	if strings.TrimSpace(query) == "" || len(query) > translationTextLimit {
		return errors.New("请输入 8 KiB 以内的原文")
	}
	source, target, err := state.languagePair()
	if err != nil {
		return err
	}
	if state.target.selected != query {
		state.target = textEditTarget{selected: query}
	}
	if err := state.validateTarget(m.active); err != nil {
		return err
	}
	state.clearTranslationResult()
	state.querySource, state.queryTarget = source, target
	state.problem, state.message, state.aiMenu, state.aiServices = "", "", false, nil
	return nil
}

func (m *editorApp) findLocalTranslations(state *translationPanelState) {
	state.localLookup = true
	m.refreshTranslationDictionary()
	project := m.activeTextProject(m.active)
	var issues []string
	if project != "" {
		if err := m.lookupTranslationScope(state, project, "项目 · "+filepath.Base(project)); err != nil {
			issues = append(issues, "项目库："+err.Error())
		}
	}
	if err := m.lookupTranslationScope(state, "", "个人"); err != nil {
		issues = append(issues, "个人库："+err.Error())
	}
	if err := m.lookupDownloadedDictionary(state); err != nil {
		issues = append(issues, "下载词库："+err.Error())
	}
	state.problem = strings.Join(issues, "\n")
	if len(state.suggestions) == 0 {
		return
	}
	state.options = make([]int, len(state.suggestions))
	for i := range state.options {
		state.options[i] = i
	}
	state.output.SetText(state.suggestions[0].entry.target)
	state.hasOutput = true
}

func (state *translationPanelState) validateTarget(active *editorTab) error {
	if state.query.Text() != state.target.selected {
		return errors.New("原文已修改，请重新翻译")
	}
	if state.target.tab == nil {
		if state.target.selected == "" {
			return errors.New("请先查找文本或选区")
		}
		return nil
	}
	_, _, _, err := state.target.current(active)
	return err
}

func (m *editorApp) lookupTranslationScope(state *translationPanelState, project, scope string) error {
	path, err := m.textAssetPath(project, translationLibraryFile)
	if err != nil {
		return err
	}
	source := state.target.selected
	lower := strings.ToLower(source)
	var suggestions, terms, exactTerms []translationSuggestion
	err = scanTranslationLibrary(path, true, func(entry translationEntry) {
		if entry.sourceLang != state.querySource || entry.targetLang != state.queryTarget {
			return
		}
		item := translationSuggestion{entry: entry, scope: scope}
		switch entry.kind {
		case translationSentence:
			if entry.source == source && len(suggestions) < translationMatchLimit {
				suggestions = append(suggestions, item)
			}
		case translationTerm:
			term := strings.ToLower(entry.source)
			if lower == term && len(exactTerms) < translationMatchLimit {
				exactTerms = append(exactTerms, item)
			}
			if len(terms) < translationMatchLimit && termMatches(lower, term) {
				terms = append(terms, item)
			}
		}
	})
	if err != nil {
		return err
	}
	remaining := min(len(exactTerms), translationMatchLimit-len(suggestions))
	state.suggestions = append(state.suggestions, suggestions...)
	state.suggestions = append(state.suggestions, exactTerms[:remaining]...)
	state.terms = append(state.terms, terms...)
	return nil
}

func (state *translationPanelState) validateOutput() error {
	translated := state.output.Text()
	if state.checkedOutput != translated {
		state.checkedOutput = translated
		state.outputIssue = nil
		if len(translated) > translationTextLimit || strings.ContainsRune(translated, 0) {
			state.outputIssue = errors.New("请输入 8 KiB 以内的译文")
		} else if strings.TrimSpace(translated) != "" {
			state.outputIssue = validateTranslationMarkers(state.target.selected, translated)
		}
	}
	return state.outputIssue
}

func (m *editorApp) applyTranslation() {
	state := m.translation
	if state == nil || !m.translationEnabled {
		return
	}
	state.message, state.problem = "", ""
	if err := state.validateTarget(m.active); err != nil {
		state.problem = err.Error()
		return
	}
	value, start, end, err := state.target.current(m.active)
	if err != nil {
		state.problem = err.Error()
		return
	}
	if !state.matchesLanguages() {
		state.problem = "语言方向已变化，请重新翻译"
		return
	}
	if _, composing := state.output.GetEditor().Composition(); composing {
		state.problem = "请先完成中文选词"
		return
	}
	translated := state.output.Text()
	if strings.TrimSpace(translated) == "" {
		state.problem = "请输入译文"
		return
	}
	if err := state.validateOutput(); err != nil {
		state.problem = err.Error()
		return
	}
	if m.active.format() == "JSON" {
		translated, err = jsonTranslationReplacement(value, start, end, translated)
		if err != nil {
			state.problem = err.Error()
			return
		}
	}
	m.active.source.text.ReplaceRange(state.target.span, translated)
	state.clearLookup()
	m.status = "已替换选区"
	m.focusActiveSource()
}

func shortTranslation(value string, limit int) string {
	seen := 0
	for i := range value {
		if seen == limit {
			return value[:i] + "…"
		}
		seen++
	}
	return value
}

func (m *editorApp) translationPanel(c ui.C, blocked bool) {
	state := m.translation
	m.workspacePanel(c).Grow(1).MarginRight(workspaceInset).Pad(12).Gap(10).Children(func() {
		ui.Row(c).FillWidth().Items(ui.Center).Gap(8).Children(func() {
			if state.managing || state.draft != nil {
				if ui.Button(c, "返回翻译").Icon(chevronLeftIcon).IconOnly().Ghost().Size(28, 28).Disabled(blocked).Clicked() {
					state.managing, state.draft, state.problem, state.message = false, nil, "", ""
				}
			} else {
				ui.Icon(c, translationIcon).Size(19, 19).Color(m.theme.MutedForeground)
			}
			title := "翻译"
			if state.managing {
				title = "词库管理"
			}
			ui.Text(c, title).Bold().FontSize(15).Grow(1)
			if !state.managing && state.draft == nil && ui.Button(c, "词库管理").Icon(dictionaryIcon).IconOnly().Ghost().Size(28, 28).Tooltip("词库管理").Disabled(blocked || state.translating()).Clicked() {
				state.aiMenu, state.aiServices = false, nil
				state.managing, state.problem, state.message = true, "", ""
				c.After(func() {
					state.libraryLoaded = false
					m.refreshTranslationDictionary()
					if state.offline != nil {
						m.refreshOfflineComponents(state.offline)
					}
				})
			}
			if ui.Button(c, "关闭翻译").Icon(closeIcon).IconOnly().Ghost().Size(28, 28).Disabled(blocked).Clicked() {
				c.After(m.toggleTranslation)
			}
		})
		if !state.managing && state.draft == nil {
			ui.Row(c).FillWidth().Pad(3).Gap(4).Radius(9).Background(m.theme.Muted).Children(func() {
				for _, option := range []struct {
					label string
					mode  translationMode
				}{{"本地词库", translationLocalMode}, {"离线整句", translationOfflineMode}} {
					button := ui.Button(c.Key(option.label), option.label).Ghost().Grow(1).Basis(0).FontSize(12).Disabled(blocked || state.translating())
					if state.mode == option.mode {
						button.Secondary()
					}
					if button.Clicked() && state.mode != option.mode {
						c.After(func() { m.selectTranslationMode(option.mode) })
					}
				}
			})
		}
		scroll := c.Part("lookup-scroll")
		if state.managing || state.draft != nil {
			scroll = c.Part("manage-scroll")
		}
		if state.draft != nil {
			scroll = c.Part("entry-scroll")
		}
		ui.Scroll(scroll).FillWidth().Grow(1).MinHeight(0).Scrollbars().Children(func() {
			ui.Column(c).FillWidth().PadRight(6).Gap(10).Children(func() {
				if state.problem != "" {
					ui.Wrap(c, state.problem).FillWidth().FontSize(12).Color(m.theme.Danger)
				}
				if state.draft != nil {
					m.translationEntryForm(c.Part("entry"), state, blocked)
				} else if state.managing {
					m.translationManagement(c.Part("manage"), state, blocked)
				} else {
					m.translationLookupView(c.Part("lookup"), state, blocked)
				}
				if state.message != "" {
					ui.Wrap(c, state.message).FillWidth().FontSize(12).Color(m.theme.Success)
				}
			})
		})
	})
}

func (m *editorApp) selectTranslationMode(mode translationMode) {
	state := m.translation
	state.mode, m.translationMode = mode, mode
	state.managing, state.problem, state.message = false, "", ""
	state.aiMenu, state.aiServices = false, nil
	if mode == translationLocalMode {
		m.refreshTranslationDictionary()
	} else if state.offline != nil {
		m.refreshOfflineComponents(state.offline)
	}
}

func (m *editorApp) translationLanguageFields(c ui.C, state *translationPanelState, lookup, blocked bool) {
	ui.Row(c).FillWidth().Gap(6).Items(ui.Center).Children(func() {
		translationLanguageField(c.Part("source"), "原文语言", &state.sourceLang, blocked)
		if ui.Button(c, "互换语言").Icon(swapLanguagesIcon).IconOnly().Size(28, 28).Tooltip("互换原文和译文语言").Ghost().Disabled(blocked).Clicked() {
			c.After(func() {
				state.sourceLang, state.targetLang = state.targetLang, state.sourceLang
				state.problem, state.message, state.aiMenu = "", "", false
				if lookup && state.localLookup && state.query.Text() == state.target.selected {
					m.lookupTypedTranslation()
				}
			})
		}
		translationLanguageField(c.Part("target"), "译文语言", &state.targetLang, blocked)
	})
}

func translationLanguageField(c ui.C, label string, choice *translationLanguageChoice, blocked bool) {
	ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
		ui.Select(c.Part("language"), &choice.code, translationLanguages[:], translationLanguageLabel).
			Label(label).FillWidth().ListMaxHeight(290).Disabled(blocked)
		if choice.code == "custom" {
			ui.TextField(c.Part("language-code"), &choice.custom, label+"代码", "语言代码").FillWidth().Disabled(blocked)
		}
	})
}

func (m *editorApp) translationLookupView(c ui.C, state *translationPanelState, blocked bool) {
	busy := state.translating()
	m.translationLanguageFields(c.Part("languages"), state, state.mode == translationLocalMode, blocked || busy)
	ui.TextArea(c.Part("query"), &state.query, "查询文本", "输入或粘贴需要翻译的文字").FillWidth().Height(96).Disabled(blocked || busy)
	if editableTextSource(m.active) {
		selection := m.active.source.text.Selection()
		if selection.Start != selection.End && ui.Button(c.Part("selection"), "查选区").Icon(fileIcon).Ghost().Height(26).FontSize(12).
			Tooltip("将编辑器选区带入翻译").Disabled(blocked || busy).Clicked() {
			c.After(m.lookupTranslation)
		}
	}
	if state.mode == translationOfflineMode {
		m.offlineTranslationControls(c.Part("offline"), state, blocked)
	} else {
		ui.Row(c.Part("actions")).FillWidth().Gap(6).Items(ui.Center).Children(func() {
			if ui.Button(c.Part("local"), "查找").Grow(1).Basis(0).Height(32).FontSize(12).
				Tooltip("查找本地词库和已保存译文").
				Disabled(blocked || busy || strings.TrimSpace(state.query.Text()) == "").Clicked() {
				c.After(m.lookupTypedTranslation)
			}
			m.translationAIAction(c.Part("ai"), state, blocked)
		})
	}
	m.translationResult(c.Part("result"), state, blocked || busy)
	if state.mode == translationLocalMode {
		source, target, _ := state.languagePair()
		if source == "en" && target == "zh-cn" {
			if !state.dictionaryChecked {
				m.refreshTranslationDictionary()
			}
			m.dictionaryDownloadControls(c.Part("download"), state, blocked)
		}
	}
}

func (m *editorApp) translationResult(c ui.C, state *translationPanelState, blocked bool) {
	if !state.matchesQuery() {
		return
	}
	if state.localLookup && len(state.suggestions) > 1 {
		before := state.selected
		ui.Select(c.Part("matches"), &state.selected, state.options, func(index int) string {
			item := state.suggestions[index]
			return item.scope + " · " + shortTranslation(item.entry.target, 26)
		}).FillWidth().Label("本地译文候选").Disabled(blocked)
		if state.selected != before {
			state.output.SetText(state.suggestions[state.selected].entry.target)
			state.problem, state.message = "", ""
		}
	}
	if state.localLookup && !state.hasOutput && len(state.terms) == 0 {
		ui.Text(c.Part("no-match"), "未找到匹配译文").Muted().FontSize(12)
	}
	if state.hasOutput {
		ui.TextArea(c.Part("output"), &state.output, "译文", "译文").FillWidth().Height(120).Disabled(blocked)
		issue := state.validateOutput()
		if issue != nil {
			ui.Wrap(c, issue.Error()).FillWidth().Color(m.theme.Danger).FontSize(12)
		}
		empty := strings.TrimSpace(state.output.Text()) == ""
		ui.Row(c).FillWidth().Gap(5).Children(func() {
			if ui.Button(c, "复制").Icon(copyIcon).Ghost().FontSize(12).Disabled(blocked || empty).Clicked() {
				c.After(func() {
					if err := app.SetClipboardText(state.output.Text()); err != nil {
						state.problem = err.Error()
					}
				})
			}
			if state.target.tab != nil && ui.Button(c, "替换选区").Outline().FontSize(12).
				Tooltip("校验占位符后替换，可用 Ctrl+Z 撤销").Disabled(blocked || empty || issue != nil).Clicked() {
				c.After(m.applyTranslation)
			}
			if ui.Button(c, "保存到库…").Icon(plusIcon).IconOnly().Ghost().Size(28, 28).Tooltip("保存到词库").Disabled(blocked || empty || issue != nil).Clicked() {
				c.After(func() { m.beginTranslationEntry(true) })
			}
		})
	}
	if state.localLookup && len(state.terms) > 0 && (len(state.suggestions) == 0 || len(state.terms) > 1) {
		if ui.Button(c, fmt.Sprintf("词义参考 · %d", len(state.terms))).Icon(chevronDownIcon).Ghost().FontSize(12).Disabled(blocked).Clicked() {
			state.showTerms = !state.showTerms
		}
		if state.showTerms {
			for i, item := range state.terms {
				ui.Column(c.KeyInt(i)).FillWidth().Gap(3).PadY(5).Children(func() {
					ui.Text(c, item.entry.source).Bold().FontSize(13)
					ui.Wrap(c, shortTranslation(item.entry.target, 220)).FillWidth().FontSize(12)
				})
			}
		}
	}
}

func (m *editorApp) translationScope(c ui.C, state *translationPanelState, project string, blocked bool) string {
	if project == "" {
		state.projectScope = false
		ui.Text(c, "个人库").Muted().FontSize(12)
		return ""
	}
	ui.Checkbox(c, &state.projectScope, "使用项目库 · "+filepath.Base(project)).Disabled(blocked)
	if state.projectScope {
		return project
	}
	return ""
}

func (m *editorApp) translationManagement(c ui.C, state *translationPanelState, blocked bool) {
	m.offlineComponentsCard(c.Part("offline"), state, blocked)
	m.dictionaryCard(c.Part("dictionary"), state, blocked)
	m.assetCard(c.Part("personal")).Children(func() {
		ui.Text(c, "我的译文").Bold().FontSize(14)
		project := m.translationScope(c.Part("scope"), state, m.activeTextProject(m.active), blocked)
		m.translationLanguageFields(c.Part("languages"), state, false, blocked)
		ui.Row(c).FillWidth().Gap(5).Children(func() {
			if ui.Button(c, "添加词条").Icon(plusIcon).Outline().FontSize(12).Disabled(blocked).Clicked() {
				c.After(func() { m.beginTranslationEntry(false) })
			}
			if ui.Button(c, "打开库文件").Ghost().FontSize(12).Disabled(blocked).Clicked() {
				c.After(func() { m.openTranslationLibrary(project) })
			}
		})
		ui.Row(c).FillWidth().Gap(5).Children(func() {
			if ui.Button(c, "导入 CSV…").Icon(downloadIcon).Ghost().FontSize(12).Disabled(blocked).Clicked() {
				c.After(func() { m.transferTranslationLibrary(project, true) })
			}
			if ui.Button(c, "导出 CSV…").Icon(exportIcon).Ghost().FontSize(12).Disabled(blocked).Clicked() {
				c.After(func() { m.transferTranslationLibrary(project, false) })
			}
		})
	})
	m.translationEntriesPreview(c.Part("entries"), state, blocked)
}

func (m *editorApp) beginTranslationEntry(fromLookup bool) {
	state := m.translation
	source, target, err := state.languagePair()
	if err != nil {
		state.problem = err.Error()
		return
	}
	draft := &translationEntryDraft{
		project: m.activeTextProject(m.active), sourceLang: source, targetLang: target, kind: translationSentence,
	}
	if fromLookup {
		if state.query.Text() != state.target.selected {
			state.problem = "原文已修改，请重新翻译"
			return
		}
		if source != state.querySource || target != state.queryTarget {
			state.problem = "语言方向已变化，请重新翻译"
			return
		}
		draft.source.SetText(state.target.selected)
		draft.target.SetText(state.output.Text())
		if state.localLookup && len(state.suggestions) > 0 {
			draft.kind = state.suggestions[state.selected].entry.kind
		}
	}
	state.draft, state.problem, state.message = draft, "", ""
}

func (m *editorApp) translationEntryForm(c ui.C, state *translationPanelState, blocked bool) {
	draft := state.draft
	m.assetHeading(c, dictionaryIcon, "保存认可的译文", "个人库跨项目复用，项目库用于团队统一用语。")
	project := m.translationScope(c.Part("scope"), state, draft.project, blocked)
	ui.Text(c, "添加词条 · "+draft.sourceLang+" → "+draft.targetLang).Muted().FontSize(12)
	ui.Select(c.Part("kind"), &draft.kind, []string{translationSentence, translationTerm}, func(kind string) string {
		if kind == translationTerm {
			return "术语"
		}
		return "整句译文"
	}).FillWidth().Label("词条类型").Disabled(blocked)
	ui.Text(c, "原文").Muted().FontSize(12)
	ui.TextArea(c.Part("source"), &draft.source, "词条原文", "原文").FillWidth().Height(75).Disabled(blocked)
	ui.Text(c, "对应译文").Muted().FontSize(12)
	ui.TextArea(c.Part("target"), &draft.target, "词条译文", "译文").FillWidth().Height(75).Disabled(blocked)
	ui.TextField(c.Part("context"), &draft.context, "用法备注", "用法备注（选填）").FillWidth().Disabled(blocked)
	ui.Row(c).FillWidth().Gap(6).Children(func() {
		if ui.Button(c, "取消录入").Ghost().FontSize(12).Disabled(blocked).Clicked() {
			state.draft, state.problem = nil, ""
		}
		if ui.Button(c, "保存词条").FontSize(12).Disabled(blocked).Clicked() {
			c.After(func() { m.saveTranslationEntry(project, draft) })
		}
	})
}

func (m *editorApp) saveTranslationEntry(project string, draft *translationEntryDraft) {
	state := m.translation
	for _, field := range []*ui.Field{&draft.source, &draft.target, &draft.context} {
		if _, composing := field.GetEditor().Composition(); composing {
			state.problem = "请先完成中文选词"
			return
		}
	}
	entry := translationEntry{draft.sourceLang, draft.targetLang, draft.kind, draft.source.Text(), draft.target.Text(), draft.context.Text()}
	if err := entry.validate(); err != nil {
		state.problem = err.Error()
		return
	}
	if err := validateTranslationMarkers(entry.source, entry.target); err != nil {
		state.problem = err.Error()
		return
	}
	path, err := m.textAssetPath(project, translationLibraryFile)
	if err != nil {
		state.problem = err.Error()
		return
	}
	added, _, err := mergeTranslationLibrary(path, []translationEntry{entry})
	if err != nil {
		state.problem = err.Error()
		return
	}
	state.draft, state.problem, state.message = nil, "", "词条已保存"
	state.libraryLoaded = false
	if added == 0 {
		state.message = "这条译文已在库中"
	}
}

func (m *editorApp) openTranslationLibrary(project string) {
	path, err := m.textAssetPath(project, translationLibraryFile)
	if err == nil {
		_, err = os.Stat(path)
	}
	if errors.Is(err, os.ErrNotExist) {
		m.translation.problem = "库为空，可添加词条或导入 CSV。"
		return
	}
	if err != nil {
		m.translation.problem = err.Error()
		return
	}
	m.load(path)
}

func (m *editorApp) transferTranslationLibrary(project string, importing bool) {
	state := m.translation
	state.problem, state.message = "", ""
	path, err := m.textAssetPath(project, translationLibraryFile)
	if err != nil {
		state.problem = err.Error()
		return
	}
	filters := []app.FileFilter{{Name: "翻译库 CSV", Pattern: "*.csv"}}
	if importing {
		files, err := app.OpenFiles(app.OpenFileOptions{Title: "导入翻译库", Filters: filters})
		if err == nil && len(files) > 0 {
			var entries []translationEntry
			entries, err = readTranslationLibrary(files[0], false)
			if err == nil {
				var added, duplicates int
				added, duplicates, err = mergeTranslationLibrary(path, entries)
				if err == nil {
					state.message = fmt.Sprintf("导入 %d 条，跳过 %d 条相同词条", added, duplicates)
					state.libraryLoaded = false
				}
			}
		}
		if err != nil && !errors.Is(err, app.ErrCanceled) {
			state.problem = "导入失败：" + err.Error()
		}
		return
	}
	entries, err := readTranslationLibrary(path, true)
	if err != nil {
		state.problem = err.Error()
		return
	}
	destination, err := app.SaveFile(app.SaveFileOptions{
		Title: "导出翻译库", SuggestedName: translationLibraryFile, DefaultExtension: "csv", Filters: filters,
	})
	if errors.Is(err, app.ErrCanceled) {
		return
	}
	if err == nil {
		var data []byte
		data, err = encodeTranslationLibrary(entries)
		if err == nil {
			err = writeFile(destination, data)
		}
	}
	if err != nil {
		state.problem = "导出失败：" + err.Error()
		return
	}
	state.message = "已导出翻译库"
}
