package editor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/vivaui/viva/markdown"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	uimarkdown "github.com/vivaui/viva/ui/markdown"
)

const documentTranslationLimit = 512 << 10

type documentTranslationState struct {
	translationDirection
	translationAIChoice
	open                     bool
	split                    ui.SplitterState
	request                  *ui.EffectState[string, int]
	stream                   *aiReplyStream
	original, source, target string
	result                   *editorPane
	preview                  *uimarkdown.State
	previewHeight            float32
	problem                  string
}

func (t *editorTab) canTranslateDocument() bool {
	return aiTextTab(t) && (t.format() == "Markdown" || t.format() == "纯文本")
}

func (t *editorTab) showingDocumentTranslation() bool {
	return t.documentTranslation != nil && t.documentTranslation.open && t.canTranslateDocument()
}

func (m *editorApp) openDocumentTranslation(t *editorTab) {
	if t != m.active || !t.canTranslateDocument() {
		return
	}
	if t.documentTranslation == nil {
		direction := defaultTranslationDirection()
		if m.translation != nil {
			direction.sourceLang.set(m.translation.sourceLang.value())
			direction.targetLang.set(m.translation.targetLang.value())
		}
		t.documentTranslation = &documentTranslationState{
			translationDirection: direction,
			split:                ui.SplitterState{Ratio: 0.5},
			result:               newEditorPane("", true),
		}
	}
	if m.ai.open {
		m.toggleAIPanel()
	}
	m.translation, m.templatePanel = nil, nil
	t.documentTranslation.open, t.source.focus = true, true
}

func (m *editorApp) closeDocumentTranslation(t *editorTab) {
	state := t.documentTranslation
	state.stop()
	state.open, state.aiMenu, state.aiServices = false, false, nil
	m.focusActiveSource()
}

func (state *documentTranslationState) stop() {
	if state.request != nil {
		state.showResult(state.stream.text())
		state.request, state.stream = nil, nil
		state.problem = uiText("翻译已停止")
	}
}

func (m *editorApp) chooseDocumentTranslation(t *editorTab) {
	state := t.documentTranslation
	if t != m.active || !t.showingDocumentTranslation() || state.request != nil {
		return
	}
	state.problem = ""
	profile, err := state.choose(m.dataDirectory)
	if err != nil {
		state.problem = uiText("无法读取 AI 服务配置：") + err.Error()
		return
	}
	if profile != nil {
		m.startDocumentTranslation(t, *profile)
	}
}

func (m *editorApp) startDocumentTranslation(t *editorTab, settings aiSettings) {
	state := t.documentTranslation
	if t != m.active || !t.showingDocumentTranslation() || state.request != nil {
		return
	}
	state.aiMenu, state.aiServices, state.problem = false, nil, ""
	source, target, err := state.languagePair()
	if err != nil {
		state.problem = err.Error()
		return
	}
	if _, composing := t.source.text.Composition(); composing {
		state.problem = uiText("请先完成中文选词")
		return
	}
	original := t.source.text.CommittedString()
	if strings.TrimSpace(original) == "" {
		state.problem = uiText("请输入需要翻译的文章")
		return
	}
	if len(original) > documentTranslationLimit {
		state.problem = fmt.Sprintf("整篇翻译支持 %d KiB 以内的文章，请拆分后翻译", documentTranslationLimit>>10)
		return
	}
	settings, err = m.translationAISettings(settings)
	if err != nil {
		state.problem = err.Error()
		return
	}
	input := documentTranslationInput{
		content: original, source: source, target: target,
		markdown: t.format() == "Markdown", libraries: m.translationLibraryPaths(t),
	}
	stream := &aiReplyStream{}
	state.stream = stream
	state.original, state.source, state.target = original, source, target
	state.result.setResult("")
	state.preview = nil
	state.request = ui.NewEffect[string, int](func(ctx context.Context) (string, error) {
		return input.translate(ctx, settings, stream, m.invalidator.Invalidate)
	})
}

func (m *editorApp) updateDocumentTranslation(c ui.C, t *editorTab) {
	state := t.documentTranslation
	if state == nil || state.request == nil {
		return
	}
	if !t.canTranslateDocument() {
		state.stop()
		state.open = false
		return
	}
	ui.Effect(c, state.request, 0)
	translated, err, done := state.request.Take()
	if !done {
		state.showResult(state.stream.text())
		return
	}
	if err != nil {
		state.showResult(state.stream.text())
		state.problem = err.Error()
	} else {
		state.showResult(translated)
		if t.format() == "Markdown" {
			state.preparePreview(translated)
		}
	}
	state.request, state.stream = nil, nil
}

func (state *documentTranslationState) showResult(value string) {
	editor := state.result.text
	if previous := editor.CommittedString(); value == previous {
		return
	} else if !strings.HasPrefix(value, previous) {
		state.result.setResult(value)
	} else {
		selection := editor.Selection()
		if editor.Caret() == selection.Start {
			selection.Start, selection.End = selection.End, selection.Start
		}
		end := editor.RuneCount()
		editor.ReplaceRange(text.Range{Start: end, End: end}, value[len(previous):])
		editor.ClearHistory()
		editor.SetSelection(selection)
	}
}

type documentTranslationInput struct {
	content, source, target string
	markdown                bool
	libraries               []string
}

func (input documentTranslationInput) translate(ctx context.Context, settings aiSettings, stream *aiReplyStream, invalidate func()) (string, error) {
	parts := documentTranslationParts(input.content, input.markdown)
	terms := readTranslationTerms(input.libraries, input.source, input.target)
	var result strings.Builder
	publish := func(delta string) {
		if stream.append(delta) {
			invalidate()
		}
	}
	limitError := errors.New(uiText("译文超过 2 MiB，已保留已输出的内容"))
	for index, part := range parts {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if result.Len()+len(part.before)+len(part.after) > aiResponseLimit {
			return "", limitError
		}
		result.WriteString(part.before)
		publish(part.before)
		if part.input == "" {
			continue
		}

		system := translationInstructions(input.source, input.target, part.input, input.markdown, terms)
		partContext, cancel := context.WithTimeout(ctx, 90*time.Second)
		delivered := 0
		overLimit := false
		translated, err := settings.complete(partContext, system, []aiMessage{{Role: "user", Content: part.input}}, "", func(delta string) {
			delivered += len(delta)
			if result.Len()+delivered+len(part.after) > aiResponseLimit {
				overLimit = true
				cancel()
				return
			}
			publish(delta)
		})
		timedOut := errors.Is(partContext.Err(), context.DeadlineExceeded)
		cancel()
		if overLimit {
			return "", limitError
		}
		if timedOut {
			return "", fmt.Errorf(uiText("第 %d 段翻译超时，请重试"), index+1)
		}
		if err != nil {
			return "", fmt.Errorf(uiText("第 %d 段翻译失败：%w"), index+1, err)
		}
		translated = strings.TrimSpace(translated)
		if !utf8.ValidString(translated) || strings.ContainsRune(translated, 0) {
			return "", fmt.Errorf(uiText("第 %d 段返回了无效的译文"), index+1)
		}
		if result.Len()+len(translated)+len(part.after) > aiResponseLimit {
			return "", limitError
		}
		result.WriteString(translated)
		result.WriteString(part.after)
		// The final answer can differ from the deltas, and some services return JSON.
		stream.replace(result.String())
		invalidate()
	}
	return result.String(), nil
}

type documentTranslationPart struct {
	before, input, after string
}

func documentTranslationParts(source string, isMarkdown bool) []documentTranslationPart {
	var parts []documentTranslationPart
	start := 0
	if isMarkdown {
		var document markdown.Document
		document.Parse(source)
		for _, block := range document.Blocks {
			if block.Kind == markdown.Code {
				parts = appendDocumentTranslationParts(parts, source[start:block.Start])
				parts = append(parts, documentTranslationPart{before: source[block.Start:block.End]})
				start = block.End
			}
		}
	}
	return appendDocumentTranslationParts(parts, source[start:])
}

func appendDocumentTranslationParts(parts []documentTranslationPart, source string) []documentTranslationPart {
	markers := translationMarkerPattern().FindAllStringIndex(source, -1)
	for start := 0; start < len(source); {
		end := start + documentTranslationChunkEnd(source[start:])
		for len(markers) > 0 && markers[0][1] <= end {
			markers = markers[1:]
		}
		if len(markers) > 0 && markers[0][0] < end {
			if markers[0][0] == start {
				end = markers[0][1]
				parts = append(parts, documentTranslationPart{before: source[start:end]})
				start = end
				continue
			}
			end = markers[0][0]
		}
		fragment := source[start:end]
		input := strings.TrimSpace(fragment)
		before := len(fragment) - len(strings.TrimLeftFunc(fragment, unicode.IsSpace))
		parts = append(parts, documentTranslationPart{before: fragment[:before], input: input, after: fragment[before+len(input):]})
		start = end
	}
	return parts
}

func documentTranslationChunkEnd(value string) int {
	if len(value) <= translationTextLimit {
		return len(value)
	}
	end := translationTextLimit
	for !utf8.RuneStart(value[end]) {
		end--
	}
	for _, delimiter := range []string{"\n\n", "\n", " "} {
		if index := strings.LastIndex(value[:end], delimiter); index >= end/2 {
			return index + len(delimiter)
		}
	}
	return end
}
