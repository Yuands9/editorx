package editor

import (
	"fmt"
	"strings"

	"github.com/vivaui/viva/ui"
)

func (m *editorApp) translationEntriesPreview(c ui.C, state *translationPanelState, blocked bool) {
	project := ""
	if state.projectScope {
		project = m.activeTextProject(m.active)
	}
	if !state.libraryLoaded || state.libraryProject != project {
		c.After(func() {
			state.libraryLoaded, state.libraryProject = true, project
			state.libraryCount, state.libraryPreview = 0, nil
			path, err := m.textAssetPath(project, translationLibraryFile)
			if err == nil {
				err = scanTranslationLibrary(path, true, func(entry translationEntry) {
					state.libraryCount++
					if len(state.libraryPreview) < 8 {
						state.libraryPreview = append(state.libraryPreview, entry)
					}
				})
			}
			if err != nil {
				state.problem = "读取我的译文失败：" + err.Error()
			}
		})
		return
	}
	ui.Text(c, fmt.Sprintf("已保存的译文 · %d 条", state.libraryCount)).Bold().FontSize(13)
	if state.libraryCount == 0 {
		ui.Wrap(c, "这里还没有保存的译文。查词或翻译后点「保存到库」，也可以直接添加词条。").FillWidth().Pad(12).Radius(10).Background(m.theme.Muted).Muted().FontSize(12)
		return
	}
	for i, entry := range state.libraryPreview {
		m.assetCard(c.KeyInt(i)).Children(func() {
			kind := "整句"
			if entry.kind == translationTerm {
				kind = "术语"
			}
			ui.Row(c).FillWidth().Gap(6).Items(ui.Center).Children(func() {
				m.assetBadge(c, kind+" · "+entry.sourceLang+" → "+entry.targetLang)
				if ui.Button(c, "使用").Outline().FontSize(12).Disabled(blocked).Clicked() {
					c.After(func() {
						m.selectTranslationMode(translationLocalMode)
						state.target = textEditTarget{}
						state.query.SetText(entry.source)
						state.sourceLang.set(entry.sourceLang)
						state.targetLang.set(entry.targetLang)
						m.lookupTypedTranslation()
						state.output.SetText(entry.target)
						state.hasOutput = true
					})
				}
			})
			ui.Wrap(c, shortTranslation(entry.source, 100)).FillWidth().FontSize(12)
			ui.Wrap(c, shortTranslation(entry.target, 180)).FillWidth().FontSize(13).Color(m.theme.Ring)
		})
	}
	if state.libraryCount > len(state.libraryPreview) {
		ui.Wrap(c, "显示前 8 条；可在翻译页查找原文，或打开库文件查看全部。").FillWidth().Muted().FontSize(11)
	}
}

func (choice *translationLanguageChoice) set(code string) {
	for _, option := range translationLanguages {
		if strings.EqualFold(code, option) {
			choice.code = option
			return
		}
	}
	choice.code = "custom"
	choice.custom.SetText(code)
}
