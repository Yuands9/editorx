package main

import (
	"regexp"
	"slices"
	"strings"
)

type markdownHighlightMode uint8

const (
	markdownHighlightStart markdownHighlightMode = iota
	markdownHighlightBody
	markdownHighlightFrontMatter
)

var markdownListPrefix = regexp.MustCompile(`^(?:[-+*]|[0-9]{1,9}[.)])[ \t]+(?:\[[ xX]\][ \t]+)?`)
var markdownLabel = regexp.MustCompile(`^[\pL\pN_.-][\pL\pM\pN_. \t-]*$`)
var markdownInline = regexp.MustCompile(`\x60[^\x60]+\x60|!?\[[^\]\n]+\]\([^)\n]+\)|` + syntaxURL + `|\*\*[^*\n]+\*\*|__[^_\n]+__|~~[^~\n]+~~`)
var markdownRule = regexp.MustCompile(`^(?:-(?:[ \t]*-){2,}|\*(?:[ \t]*\*){2,}|_(?:[ \t]*_){2,})$`)

func (h *syntaxLine) markdown(line string, state configLexState) configLexState {
	line = strings.TrimRight(line, syntaxWhitespace)
	if state.markdownMode == markdownHighlightStart {
		if line == "---" {
			h.add(0, len(line), h.punctuation)
			state.markdownMode = markdownHighlightFrontMatter
			return state
		}
		state.markdownMode = markdownHighlightBody
	}
	if state.markdownMode == markdownHighlightFrontMatter {
		if line == "---" || line == "..." {
			h.add(0, len(line), h.punctuation)
			return configLexState{markdownMode: markdownHighlightBody}
		}
		return h.yaml(line, state)
	}

	quoteEnd, quotes := markdownQuotePrefix(line)
	content := strings.TrimLeft(line[quoteEnd:], " \t")
	start := len(line) - len(content)
	indented := start-quoteEnd > 3 || strings.ContainsRune(line[quoteEnd:start], '\t')
	if state.markdownFence != 0 && uint32(quotes) >= state.markdownFenceDepth {
		if uint32(quotes) == state.markdownFenceDepth && !indented &&
			uint32(len(content)) >= state.markdownFenceLength && strings.Trim(content, string(state.markdownFence)) == "" {
			h.add(start, len(line), h.punctuation)
			return configLexState{markdownMode: markdownHighlightBody}
		}
		if state.markdownFenceSyntax != 0 {
			return h.scanSegment(markdownFenceName(state.markdownFenceSyntax), line[quoteEnd:], state, quoteEnd)
		}
		return state
	}
	if state.markdownFence != 0 {
		state = configLexState{markdownMode: markdownHighlightBody}
	}
	if indented {
		h.add(start, len(line), h.value)
		return state
	}
	if content == "" {
		return state
	}

	prefix := markdownListPrefix.FindString(content)
	start += len(prefix)
	content = content[len(prefix):]
	if content == "" {
		return state
	}
	if fence := content[0]; fence == '`' || fence == '~' {
		info := strings.TrimLeft(content, string(fence))
		count := len(content) - len(info)
		if count >= 3 && (fence == '~' || !strings.ContainsRune(info, '`')) {
			state.markdownFence = fence
			state.markdownFenceDepth, state.markdownFenceLength = uint32(quotes), uint32(count)
			state.markdownFenceSyntax = markdownFenceLanguage(info)
			h.add(start, start+count, h.punctuation)
			h.add(skipSyntaxSpace(line, start+count), len(line), h.keyword)
			return state
		}
	}
	if h.value.Alpha == 0 {
		return state
	}
	if content[0] == '#' {
		text := strings.TrimLeft(content, "#")
		count := len(content) - len(text)
		if count <= 6 && yamlSpaceAfter(content, count) {
			h.add(start, start+count, h.punctuation)
			h.add(skipSyntaxSpace(line, start+count), len(line), h.symbol)
			return state
		}
	}
	if markdownRule.MatchString(content) {
		h.add(start, len(line), h.punctuation)
		return state
	}
	if !h.markdownKeyValue(content, start) {
		for _, match := range markdownInline.FindAllStringIndex(content, -1) {
			left, right := start+match[0], start+match[1]
			switch line[left] {
			case '`':
				h.add(left, right, h.value)
			case '[', '!':
				separator := left + strings.Index(line[left:right], "](")
				h.add(left, separator+1, h.symbol)
				h.add(separator+2, right-1, h.value)
			case 'h':
				h.add(left, left+len(strings.TrimRight(line[left:right], ".,!?;")), h.symbol)
			default:
				h.add(left, right, h.keyword)
			}
		}
	}
	return state
}

func markdownFenceLanguage(info string) uint8 {
	info = strings.TrimSpace(info)
	if end := strings.IndexAny(info, syntaxWhitespace); end >= 0 {
		info = info[:end]
	}
	switch strings.ToLower(info) {
	case "shell":
		info = "sh"
	case "powershell":
		info = "ps1"
	case "python":
		info = "py"
	case "javascript":
		info = "js"
	case "typescript":
		info = "ts"
	case "c++":
		info = "cpp"
	case "csharp", "c#":
		info = "cs"
	case "dotenv":
		info = "env"
	case "jsonlines", "ndjson":
		info = "jsonl"
	}
	language := documentFormat("fence." + info)
	if index := slices.Index(syntaxLanguages[:], language); index > 0 && language != "Markdown" {
		return uint8(index)
	}
	if index := slices.Index(dataSyntaxLanguages[:], language); index >= 0 {
		return uint8(len(syntaxLanguages) + index)
	}
	for index, syntax := range codeSyntaxes {
		if language == syntax.name || strings.EqualFold(info, syntax.name) {
			return uint8(len(syntaxLanguages) + len(dataSyntaxLanguages) + index)
		}
	}
	return 0
}

func markdownFenceName(id uint8) string {
	index := int(id)
	if index < len(syntaxLanguages) {
		return syntaxLanguages[index]
	}
	index -= len(syntaxLanguages)
	if index < len(dataSyntaxLanguages) {
		return dataSyntaxLanguages[index]
	}
	return codeSyntaxes[index-len(dataSyntaxLanguages)].name
}

func markdownQuotePrefix(line string) (end, depth int) {
	for {
		content := strings.TrimLeft(line[end:], " ")
		indent := len(line) - end - len(content)
		if indent > 3 || !strings.HasPrefix(content, ">") {
			return end, depth
		}
		end += indent + 1
		if end < len(line) && syntaxSpace(line[end]) {
			end++
		}
		depth++
	}
}

func (h *syntaxLine) markdownKeyValue(line string, start int) bool {
	separator := strings.IndexAny(line, ":：")
	if separator <= 0 {
		return false
	}
	width := 1
	if line[separator] != ':' {
		width = len("：")
	}
	key := strings.TrimRight(line[:separator], syntaxWhitespace)
	value := line[separator+width:]
	keyStart := start
	for _, marker := range [...]string{"**", "__", "*", "_", "`"} {
		if !strings.HasPrefix(key, marker) {
			continue
		}
		switch {
		case len(key) > len(marker)*2 && strings.HasSuffix(key, marker):
			key = key[len(marker) : len(key)-len(marker)]
		case len(key) > len(marker) && strings.HasPrefix(value, marker) && yamlSpaceAfter(value, len(marker)):
			key, value = key[len(marker):], value[len(marker):]
		default:
			continue
		}
		keyStart += len(marker)
		break
	}
	if width == 1 && !yamlSpaceAfter(value, 0) || !markdownLabel.MatchString(key) {
		return false
	}
	h.add(keyStart, keyStart+len(key), h.key)
	end := start + len(line)
	value = strings.TrimLeft(value, syntaxWhitespace)
	h.add(end-len(value), end, h.scalar(value))
	return true
}
