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

type translationSelection struct {
	textEditTarget
	revision   *largeEditRevision
	start, end int64
}

type translationPanelState struct {
	translationDirection
	translationAIChoice
	split                    ui.SplitterState
	managing, projectScope   bool
	showTerms, localLookup   bool
	hasOutput                bool
	target                   translationSelection
	querySource, queryTarget string
	suggestions, terms       []translationSuggestion
	options                  []int
	selected                 int
	output                   ui.Field
	checkedOutput            string
	outputIssue              error
	message, problem         string
	draft                    *translationEntryDraft
	request                  *ui.EffectState[string, int]
	offline                  *offlineTranslationState
	query                    ui.Field
	dictionary               *dictionaryInfo
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

type translationDirection struct {
	sourceLang, targetLang translationLanguageChoice
}

func defaultTranslationDirection() translationDirection {
	return translationDirection{
		sourceLang: translationLanguageChoice{code: "en"},
		targetLang: translationLanguageChoice{code: "zh-CN"},
	}
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
	m.leavePasswordVault()
	m.tools.gitOpen = false
	if m.translation == nil {
		if m.ai.open {
			m.toggleAIPanel()
		}
		m.templatePanel = nil
		m.translation = &translationPanelState{split: m.newAssetSplit(), translationDirection: defaultTranslationDirection()}
	}
	if lookup {
		m.lookupTranslation()
	}
}

func (m *editorApp) goToTranslation() {
	m.openTranslation(false)
	m.fillTranslationSelection()
}

func (m *editorApp) translationSelectionItem(c ui.C, menu *ui.MenuItems) {
	if menu.Item(m.menuText("去翻译")).Icon(translationIcon).Clicked() {
		c.After(m.goToTranslation)
	}
}

func (tab *editorTab) hasTranslationSelection() bool {
	if tab == nil {
		return false
	}
	if tab.hasLargeEdit() {
		position := tab.largeText.editing.position
		return position.anchor != position.caret
	}
	if tab.largeText == nil && !editableTextSource(tab) {
		return false
	}
	selection := tab.source.text.Selection()
	return selection.Start != selection.End
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
	m.openTranslation(m.active.hasTranslationSelection())
}

func (state *translationPanelState) clearLookup() {
	state.target = translationSelection{}
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

func (direction *translationDirection) languagePair() (string, string, error) {
	source, target := languageCode(direction.sourceLang.value()), languageCode(direction.targetLang.value())
	if !validLanguageCode(source) || !validLanguageCode(target) || source == target {
		return "", "", errors.New(uiText("请输入不同的语言代码，如 en 和 zh-CN"))
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
	if m.fillTranslationSelection() {
		m.findLocalTranslations(m.translation)
	}
}

func (m *editorApp) fillTranslationSelection() bool {
	state := m.translation
	if state == nil {
		return false
	}
	state.clearLookup()
	state.managing, state.message, state.problem, state.draft = false, "", "", nil
	selection, err := captureTranslationSelection(m.active)
	if err != nil {
		state.problem = err.Error()
		return false
	}
	if strings.TrimSpace(selection.selected) == "" {
		state.problem = uiText("请选择需要翻译的文字")
		return false
	}
	state.target = selection
	state.query.SetText(selection.selected)
	source, target, err := state.languagePair()
	if err != nil {
		state.problem = err.Error()
		return false
	}
	state.querySource, state.queryTarget = source, target
	return true
}

func captureTranslationSelection(tab *editorTab) (translationSelection, error) {
	if tab == nil || tab.largeText == nil {
		target, err := captureTextTarget(tab, true, translationTextLimit)
		return translationSelection{textEditTarget: target}, err
	}
	if !tab.hasLargeEdit() {
		target, err := capturePaneTextTarget(tab, true, translationTextLimit)
		target.tab = nil
		return translationSelection{textEditTarget: target}, err
	}
	if tab.largeText.busyEditing() {
		return translationSelection{}, errors.New(uiText("请等待当前大文件操作完成"))
	}
	if _, composing := tab.source.text.Composition(); composing {
		return translationSelection{}, errors.New(uiText("请先完成中文选词"))
	}
	edit := tab.largeText.editing
	start, end := edit.position.rangeOffsets()
	if start == end {
		return translationSelection{}, errors.New(uiText("请先选中一段文本"))
	}
	if end-start > translationTextLimit {
		return translationSelection{}, fmt.Errorf(uiText("选区超过 %d KiB，请缩小选区"), translationTextLimit>>10)
	}
	value, err := edit.revision.read(start, end)
	if err != nil {
		return translationSelection{}, err
	}
	return translationSelection{
		textEditTarget: textEditTarget{tab: tab, selected: strings.ReplaceAll(value, "\r\n", "\n")},
		revision:       edit.revision, start: start, end: end,
	}, nil
}

func (m *editorApp) lookupTypedTranslation() bool {
	state := m.translation
	if state == nil || state.translating() {
		return false
	}
	if err := m.prepareTranslationQuery(state); err != nil {
		state.problem = err.Error()
		return false
	}
	state.managing, state.draft = false, nil
	m.findLocalTranslations(state)
	return true
}

func (m *editorApp) prepareTranslationQuery(state *translationPanelState) error {
	if _, composing := state.query.GetEditor().Composition(); composing {
		return errors.New(uiText("请先完成中文选词"))
	}
	query := state.query.Text()
	if strings.TrimSpace(query) == "" || len(query) > translationTextLimit {
		return errors.New(uiText("请输入 8 KiB 以内的原文"))
	}
	source, target, err := state.languagePair()
	if err != nil {
		return err
	}
	if state.target.selected != query {
		state.target = translationSelection{textEditTarget: textEditTarget{selected: query}}
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
		return errors.New(uiText("原文已修改，请重新翻译"))
	}
	if state.target.tab == nil {
		if state.target.selected == "" {
			return errors.New(uiText("请先查找文本或选区"))
		}
		return nil
	}
	if state.target.revision != nil {
		if active != state.target.tab || !active.hasLargeEdit() {
			return errors.New(uiText("请回到原来的文本标签并重新选择"))
		}
		if active.largeText.busyEditing() {
			return errors.New(uiText("请等待当前大文件操作完成"))
		}
		if _, composing := active.source.text.Composition(); composing {
			return errors.New(uiText("请先完成中文选词"))
		}
		edit := active.largeText.editing
		start, end := edit.position.rangeOffsets()
		if edit.revision != state.target.revision || start != state.target.start || end != state.target.end {
			return errors.New(uiText("原文或选区已变化，请重新选择"))
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
			state.outputIssue = errors.New(uiText("请输入 8 KiB 以内的译文"))
		} else if strings.TrimSpace(translated) != "" {
			state.outputIssue = validateTranslationMarkers(state.target.selected, translated)
		}
	}
	return state.outputIssue
}

func (m *editorApp) applyTranslation() {
	state := m.translation
	if state == nil || state.target.tab == nil {
		return
	}
	state.message, state.problem = "", ""
	if err := state.validateTarget(m.active); err != nil {
		state.problem = err.Error()
		return
	}
	if !state.matchesLanguages() {
		state.problem = uiText("语言方向已变化，请重新翻译")
		return
	}
	if _, composing := state.output.GetEditor().Composition(); composing {
		state.problem = uiText("请先完成中文选词")
		return
	}
	translated := state.output.Text()
	if strings.TrimSpace(translated) == "" {
		state.problem = uiText("请输入译文")
		return
	}
	if err := state.validateOutput(); err != nil {
		state.problem = err.Error()
		return
	}
	if state.target.revision != nil {
		m.active.largeText.problem = ""
		m.active.replaceLargeEdit(state.target.start, state.target.end, translated, false)
		if m.active.largeText.problem != "" {
			state.problem = m.active.largeText.problem
			return
		}
	} else {
		if m.active.format() == "JSON" {
			value, start, end, err := state.target.current(m.active)
			if err == nil {
				translated, err = jsonTranslationReplacement(value, start, end, translated)
			}
			if err != nil {
				state.problem = err.Error()
				return
			}
		}
		m.active.source.text.ReplaceRange(state.target.span, translated)
	}
	state.clearLookup()
	m.status = uiText("已替换选区")
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
				if ui.Button(c.Part("back"), uiText("返回翻译")).Icon(chevronLeftIcon).IconOnly().Ghost().Size(28, 28).Disabled(blocked).Clicked() {
					state.managing, state.draft, state.problem, state.message = false, nil, "", ""
				}
			} else {
				ui.Icon(c, translationIcon).Size(19, 19).Color(m.theme.MutedForeground)
			}
			title := uiText("翻译")
			if state.managing {
				title = uiText("词库管理")
			}
			ui.Text(c, title).Bold().FontSize(15).Grow(1)
			if !state.managing && state.draft == nil && m.active.canTranslateDocument() &&
				ui.Button(c.Part("document"), uiText("翻译当前文章")).Ghost().Height(28).FontSize(12).
					Disabled(blocked || state.translating()).Clicked() {
				tab := m.active
				c.After(func() { m.openDocumentTranslation(tab) })
			}
			if !state.managing && state.draft == nil && ui.Button(c.Part("manage"), uiText("词库管理")).Icon(dictionaryIcon).IconOnly().Ghost().Size(28, 28).Tooltip(uiText("词库管理")).Disabled(blocked || state.translating()).Clicked() {
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
			if ui.Button(c.Part("close"), uiText("关闭翻译")).Icon(closeIcon).IconOnly().Ghost().Size(28, 28).Disabled(blocked).Clicked() {
				c.After(m.toggleTranslation)
			}
		})
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

func (m *editorApp) translationLanguageFields(c ui.C, state *translationPanelState, lookup, blocked bool) {
	translationDirectionFields(c, &state.translationDirection, blocked, func() {
		state.problem, state.message, state.aiMenu = "", "", false
		if lookup && state.localLookup && state.query.Text() == state.target.selected {
			m.lookupTypedTranslation()
		}
	})
}

func translationDirectionFields(c ui.C, direction *translationDirection, blocked bool, afterSwap func()) {
	ui.Row(c).FillWidth().Gap(6).Items(ui.Center).Children(func() {
		translationLanguageField(c.Part("source"), "原文语言", &direction.sourceLang, blocked)
		if ui.Button(c, uiText("互换语言")).Icon(swapLanguagesIcon).IconOnly().Size(28, 28).Tooltip(uiText("互换原文和译文语言")).Ghost().Disabled(blocked).Clicked() {
			c.After(func() {
				direction.sourceLang, direction.targetLang = direction.targetLang, direction.sourceLang
				if afterSwap != nil {
					afterSwap()
				}
			})
		}
		translationLanguageField(c.Part("target"), "译文语言", &direction.targetLang, blocked)
	})
}

func translationLanguageField(c ui.C, label string, choice *translationLanguageChoice, blocked bool) {
	label = uiText(label)
	ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
		ui.Select(c.Part("language"), &choice.code, translationLanguages[:], func(code string) string { return uiText(translationLanguageLabel(code)) }).
			Label(label).FillWidth().ListMaxHeight(290).Disabled(blocked)
		if choice.code == "custom" {
			ui.TextField(c.Part("language-code"), &choice.custom, label+uiText("代码"), uiText("语言代码")).FillWidth().Disabled(blocked)
		}
	})
}

func (m *editorApp) translationLookupView(c ui.C, state *translationPanelState, blocked bool) {
	busy := state.translating()
	m.translationLanguageFields(c.Part("languages"), state, true, blocked || busy)
	ui.TextArea(c.Part("query"), &state.query, uiText("查询文本"), uiText("输入或粘贴需要翻译的文字")).FillWidth().Height(96).Disabled(blocked || busy)
	if m.active.hasTranslationSelection() && ui.Button(c.Part("selection"), uiText("查选区")).Icon(fileIcon).Ghost().Height(26).FontSize(12).
		Tooltip(uiText("将编辑器选区带入翻译")).Disabled(blocked || busy).Clicked() {
		c.After(m.lookupTranslation)
	}
	ui.Row(c.Part("actions")).FillWidth().Gap(6).Items(ui.Center).Children(func() {
		m.translationAIAction(c.Part("ai"), state, blocked)
		if state.offline != nil && state.offline.request != nil {
			if ui.Button(c.Part("local"), uiText("取消本地翻译")).Outline().Grow(1).Basis(0).Height(32).FontSize(12).Disabled(blocked).Clicked() {
				c.After(func() { state.offline.request = nil })
			}
			return
		}
		if ui.Button(c.Part("local"), uiText("本地翻译")).Secondary().Grow(1).Basis(0).Height(32).FontSize(12).
			Tooltip(uiText("先查本地词库，再用已安装的离线组件翻译整句")).
			Disabled(blocked || busy || strings.TrimSpace(state.query.Text()) == "").Clicked() {
			c.After(func() {
				if m.lookupTypedTranslation() && !state.hasOutput {
					m.startOfflineTranslation(state)
				}
			})
		}
	})
	m.translationResult(c.Part("result"), state, blocked || busy)
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
		}).FillWidth().Label(uiText("本地译文候选")).Disabled(blocked)
		if state.selected != before {
			state.output.SetText(state.suggestions[state.selected].entry.target)
			state.problem, state.message = "", ""
		}
	}
	if state.localLookup && !state.hasOutput && !state.translating() && state.problem == "" {
		ui.Text(c.Part("no-match"), uiText("未找到本地译文，可用 AI 翻译")).Muted().FontSize(12)
	}
	if state.hasOutput {
		ui.TextArea(c.Part("output"), &state.output, uiText("译文"), uiText("译文")).FillWidth().Height(120).Disabled(blocked)
		issue := state.validateOutput()
		if issue != nil {
			ui.Wrap(c, issue.Error()).FillWidth().Color(m.theme.Danger).FontSize(12)
		}
		empty := strings.TrimSpace(state.output.Text()) == ""
		ui.Row(c).FillWidth().Gap(5).Children(func() {
			if ui.Button(c, uiText("复制")).Icon(copyIcon).Ghost().FontSize(12).Disabled(blocked || empty).Clicked() {
				c.After(func() {
					if err := app.SetClipboardText(state.output.Text()); err != nil {
						state.problem = err.Error()
					}
				})
			}
			if state.target.tab != nil && ui.Button(c, uiText("替换选区")).Outline().FontSize(12).
				Tooltip(uiText("校验占位符后替换，可用 Ctrl+Z 撤销")).Disabled(blocked || empty || issue != nil).Clicked() {
				c.After(m.applyTranslation)
			}
			if ui.Button(c, uiText("保存到库…")).Icon(plusIcon).IconOnly().Ghost().Size(28, 28).Tooltip(uiText("保存到词库")).Disabled(blocked || empty || issue != nil).Clicked() {
				c.After(func() { m.beginTranslationEntry(true) })
			}
		})
	}
	if state.localLookup && len(state.terms) > 0 && (len(state.suggestions) == 0 || len(state.terms) > 1) {
		if ui.Button(c, fmt.Sprintf(uiText("词义参考 · %d"), len(state.terms))).Icon(chevronDownIcon).Ghost().FontSize(12).Disabled(blocked).Clicked() {
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
		ui.Text(c, uiText("个人库")).Muted().FontSize(12)
		return ""
	}
	ui.Checkbox(c, &state.projectScope, uiText("使用项目库 · ")+filepath.Base(project)).Disabled(blocked)
	if state.projectScope {
		return project
	}
	return ""
}

func (m *editorApp) translationManagement(c ui.C, state *translationPanelState, blocked bool) {
	m.offlineComponentsCard(c.Part("offline"), state, blocked)
	m.dictionaryCard(c.Part("dictionary"), state, blocked)
	m.assetCard(c.Part("personal")).Children(func() {
		ui.Text(c, uiText("我的译文")).Bold().FontSize(14)
		project := m.translationScope(c.Part("scope"), state, m.activeTextProject(m.active), blocked)
		m.translationLanguageFields(c.Part("languages"), state, false, blocked)
		ui.Row(c).FillWidth().Gap(5).Children(func() {
			if ui.Button(c, uiText("添加词条")).Icon(plusIcon).Outline().FontSize(12).Disabled(blocked).Clicked() {
				c.After(func() { m.beginTranslationEntry(false) })
			}
			if ui.Button(c, uiText("打开库文件")).Ghost().FontSize(12).Disabled(blocked).Clicked() {
				c.After(func() { m.openTranslationLibrary(project) })
			}
		})
		ui.Row(c).FillWidth().Gap(5).Children(func() {
			if ui.Button(c, uiText("导入 CSV…")).Icon(downloadIcon).Ghost().FontSize(12).Disabled(blocked).Clicked() {
				c.After(func() { m.transferTranslationLibrary(project, true) })
			}
			if ui.Button(c, uiText("导出 CSV…")).Icon(exportIcon).Ghost().FontSize(12).Disabled(blocked).Clicked() {
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
			state.problem = uiText("原文已修改，请重新翻译")
			return
		}
		if source != state.querySource || target != state.queryTarget {
			state.problem = uiText("语言方向已变化，请重新翻译")
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
	m.assetHeading(c, dictionaryIcon, uiText("保存认可的译文"), uiText("个人库跨项目复用，项目库用于团队统一用语。"))
	project := m.translationScope(c.Part("scope"), state, draft.project, blocked)
	ui.Text(c, uiText("添加词条 · ")+draft.sourceLang+" → "+draft.targetLang).Muted().FontSize(12)
	ui.Select(c.Part("kind"), &draft.kind, []string{translationSentence, translationTerm}, func(kind string) string {
		if kind == translationTerm {
			return "术语"
		}
		return "整句译文"
	}).FillWidth().Label(uiText("词条类型")).Disabled(blocked)
	ui.Text(c, uiText("原文")).Muted().FontSize(12)
	ui.TextArea(c.Part("source"), &draft.source, uiText("词条原文"), uiText("原文")).FillWidth().Height(75).Disabled(blocked)
	ui.Text(c, uiText("对应译文")).Muted().FontSize(12)
	ui.TextArea(c.Part("target"), &draft.target, uiText("词条译文"), uiText("译文")).FillWidth().Height(75).Disabled(blocked)
	ui.TextField(c.Part("context"), &draft.context, uiText("用法备注"), uiText("用法备注（选填）")).FillWidth().Disabled(blocked)
	ui.Row(c).FillWidth().Gap(6).Children(func() {
		if ui.Button(c, uiText("取消录入")).Ghost().FontSize(12).Disabled(blocked).Clicked() {
			state.draft, state.problem = nil, ""
		}
		if ui.Button(c, uiText("保存词条")).FontSize(12).Disabled(blocked).Clicked() {
			c.After(func() { m.saveTranslationEntry(project, draft) })
		}
	})
}

func (m *editorApp) saveTranslationEntry(project string, draft *translationEntryDraft) {
	state := m.translation
	for _, field := range []*ui.Field{&draft.source, &draft.target, &draft.context} {
		if _, composing := field.GetEditor().Composition(); composing {
			state.problem = uiText("请先完成中文选词")
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
	state.draft, state.problem, state.message = nil, "", uiText("词条已保存")
	state.libraryLoaded = false
	if added == 0 {
		state.message = uiText("这条译文已在库中")
	}
}

func (m *editorApp) openTranslationLibrary(project string) {
	path, err := m.textAssetPath(project, translationLibraryFile)
	if err == nil {
		_, err = os.Stat(path)
	}
	if errors.Is(err, os.ErrNotExist) {
		m.translation.problem = uiText("库为空，可添加词条或导入 CSV。")
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
	filters := []app.FileFilter{{Name: uiText("翻译库 CSV"), Pattern: "*.csv"}}
	if importing {
		files, err := app.OpenFiles(app.OpenFileOptions{Title: uiText("导入翻译库"), Filters: filters})
		if err == nil && len(files) > 0 {
			var entries []translationEntry
			entries, err = readTranslationLibrary(files[0], false)
			if err == nil {
				var added, duplicates int
				added, duplicates, err = mergeTranslationLibrary(path, entries)
				if err == nil {
					state.message = fmt.Sprintf(uiText("导入 %d 条，跳过 %d 条相同词条"), added, duplicates)
					state.libraryLoaded = false
				}
			}
		}
		if err != nil && !errors.Is(err, app.ErrCanceled) {
			state.problem = uiText("导入失败：") + err.Error()
		}
		return
	}
	entries, err := readTranslationLibrary(path, true)
	if err != nil {
		state.problem = err.Error()
		return
	}
	destination, err := app.SaveFile(app.SaveFileOptions{
		Title: uiText("导出翻译库"), SuggestedName: translationLibraryFile, DefaultExtension: "csv", Filters: filters,
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
		state.problem = uiText("导出失败：") + err.Error()
		return
	}
	state.message = uiText("已导出翻译库")
}
