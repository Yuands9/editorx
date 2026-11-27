package editor

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/vivaui/viva/markdown"
	"github.com/vivaui/viva/ui"
)

var syntaxLanguages = [...]string{"纯文本", "JSON", "YAML", "INI", "TOML", "XML", "HTML", "Properties", "ENV", "Markdown"}
var dataSyntaxLanguages = [...]string{"JSON Lines", "CSV", "TSV", "日志"}

func supportedSyntaxLanguage(name string) bool {
	return slices.Contains(syntaxLanguages[:], name) || slices.Contains(dataSyntaxLanguages[:], name) || codeSyntaxByName[name] != nil
}

func (m *editorApp) selectLanguage(t *editorTab, language string) {
	previous := t.format()
	if language == "" && t.languageOverride != "" {
		t.detectedSource, t.detectedFormat = "", ""
	}
	t.languageOverride = language
	current := t.format()
	if previous == "JSON" && current != "JSON" {
		t.jsonMenu, t.diffMenus = false, [2]bool{}
		t.result.text.CancelComposition()
		t.comparison.text.CancelComposition()
		t.result.focus, t.result.focused = false, false
		t.comparison.focus, t.comparison.focused = false, false
	}
	if previous == "HTML" && current != "HTML" {
		t.html.close()
	}
	m.focusActiveSource()
}

func suggestOpenedFormat(name, source string) string {
	value := strings.TrimLeft(source, " \t\r\n")
	if value == "" {
		return ""
	}
	format := documentFormat(name)
	if (value[0] == '{' || value[0] == '[') && json.Valid([]byte(value)) {
		if format != "JSON" {
			return "JSON"
		}
		return ""
	}
	if format == "Markdown" || strings.HasPrefix(value, "#!") {
		return ""
	}

	// 在完整行处截取样本，避免打开大文本时为检测解析整篇 Markdown。
	const sampleBytes = 64 << 10
	if len(source) > sampleBytes {
		end := strings.LastIndexByte(source[:sampleBytes], '\n')
		if end < 0 {
			return ""
		}
		source = source[:end]
	}
	var parsed markdown.Document
	parsed.Parse(source)
	// 有既定语法的文件里，注释、列表和普通引号可能与 Markdown 标记相似。
	textLike := format == "纯文本" || format == "日志" || format == "JSON"
	items := 0
	for _, block := range parsed.Blocks {
		if block.Kind == markdown.Code || block.Kind == markdown.Table {
			return "Markdown"
		}
		if block.Kind == markdown.Item {
			items++
		}
		spans := parsed.Spans(&block)
		if textLike && (items >= 2 || len(spans) > 0 && (block.Kind == markdown.Heading || block.Quote > 0)) {
			return "Markdown"
		}
		for _, span := range spans {
			if span.Image != "" || span.Link != "" && span.Text != span.Link ||
				textLike && span.Style&(markdown.Bold|markdown.Strike|markdown.Mono) != 0 {
				return "Markdown"
			}
		}
	}
	return ""
}

func (m *editorApp) fileFormatPromptPending() bool {
	return !m.appSettings.open && m.active != nil && m.active.formatPromptOpen
}

func (m *editorApp) fileFormatDialog(c ui.C) {
	if !m.fileFormatPromptPending() || m.otherInteractionsBlocked() {
		return
	}
	tab := m.active
	format := tab.suggestedFormat
	closePrompt := func() {
		tab.formatPromptOpen, tab.suggestedFormat = false, ""
		m.focusActiveSource()
	}
	description := "「" + tab.title() + uiText("」的内容看起来是 ") + format + uiText("。请选择显示格式。")
	if ui.ModalWith(c, &tab.formatPromptOpen, uiText("检测到文件格式"), ui.ModalSpec{Description: description, Width: 470}, nil, func() {
		if ui.Button(c, uiText("保留原格式")).Outline().Clicked() {
			c.After(closePrompt)
		}
		if ui.Button(c, uiText("以 ")+format+uiText(" 展示")).Clicked() {
			c.After(func() {
				tab.formatPromptOpen, tab.suggestedFormat = false, ""
				m.selectLanguage(tab, format)
			})
		}
	}) {
		closePrompt()
	}
}

// 自动识别只对没有后缀的未保存文档推断正文类型。
func (t *editorTab) detectFormat() string {
	if _, composing := t.source.text.Composition(); composing {
		if t.detectedFormat != "" {
			return t.detectedFormat
		}
		return "纯文本"
	}
	source := t.source.text.String()
	if t.detectedFormat != "" && source == t.detectedSource {
		return t.detectedFormat
	}
	kind := "纯文本"
	value := strings.TrimSpace(source)
	arrayValue := strings.TrimSpace(strings.TrimPrefix(value, "["))
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "<!doctype html") || strings.HasPrefix(lower, "<html>") || strings.HasPrefix(lower, "<html ") {
		kind = "HTML"
	} else if strings.HasPrefix(value, "{") || strings.HasPrefix(value, "[") && (arrayValue == "" || json.Valid([]byte(value)) || strings.ContainsRune("{[\"-0123456789tfn]", rune(arrayValue[0]))) {
		kind = "JSON"
	} else {
		var parsed markdown.Document
		parsed.Parse(source)
		for _, block := range parsed.Blocks {
			if block.Kind != markdown.Paragraph || block.Quote > 0 {
				kind = "Markdown"
				break
			}
			for _, span := range parsed.Spans(&block) {
				if span.Style != 0 || span.Link != "" || span.Image != "" {
					kind = "Markdown"
					break
				}
			}
		}
	}
	// 未命名 JSON 暂时不完整时保留双栏，清空左侧仍可回到起始页。
	if kind == "纯文本" && t.detectedFormat == "JSON" && (value != "" || t.result.focused) {
		kind = "JSON"
	}
	if t.detectedFormat != "" && kind != t.detectedFormat {
		// 类型切换改变面板层级，重新把原生输入挂回同一份文本模型。
		t.source.focus = true
		if kind == "Markdown" && t.markdownMode == "预览" {
			t.markdownMode = "分屏"
		}
	}
	t.detectedSource, t.detectedFormat = source, kind
	return kind
}
