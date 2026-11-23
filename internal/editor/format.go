package editor

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/vivaui/viva/markdown"
)

var syntaxLanguages = [...]string{"纯文本", "JSON", "YAML", "INI", "TOML", "XML", "HTML", "Properties", "ENV", "Markdown"}
var dataSyntaxLanguages = [...]string{"JSON Lines", "CSV", "TSV", "日志"}

func supportedSyntaxLanguage(name string) bool {
	return slices.Contains(syntaxLanguages[:], name) || slices.Contains(dataSyntaxLanguages[:], name) || codeSyntaxByName[name] != nil
}

func (t *editorTab) syntaxLanguage() string {
	if t.languageOverride != "" {
		return t.languageOverride
	}
	return t.format()
}

// 只对没有后缀的未保存文档推断；明确的文件名始终优先。
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
	if kind == "纯文本" && t.detectedFormat == "JSON" && t.languageOverride == "" && (value != "" || t.result.focused) {
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
