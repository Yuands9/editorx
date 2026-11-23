package editor

import (
	"regexp"
	"strings"
)

var markupEntity = regexp.MustCompile(`&(?:[A-Za-z][A-Za-z0-9]*|#[0-9]+|#[xX][0-9A-Fa-f]+);`)

type xmlHighlightMode uint8

const (
	xmlHighlightText xmlHighlightMode = iota
	xmlHighlightTag
	xmlHighlightInstruction
	xmlHighlightComment
	xmlHighlightCDATA
	xmlHighlightDirective
	htmlHighlightRawText
)

const (
	htmlRawTitle = iota + 1
	htmlRawTextarea
	htmlRawScript
	htmlRawStyle
)

var htmlRawTags = [...]string{
	htmlRawTitle: "title", htmlRawTextarea: "textarea",
	htmlRawScript: "script", htmlRawStyle: "style",
	"iframe", "xmp", "noembed", "noframes",
}

func (h *syntaxLine) markup(line string, state configLexState, html bool) configLexState {
	for at := 0; at < len(line); {
		if state.xmlMode == htmlHighlightRawText {
			end := at + htmlRawTextEnd(line[at:], htmlRawTags[state.htmlRawTag])
			switch state.htmlRawTag {
			case htmlRawTitle, htmlRawTextarea:
				h.markupValue(line, at, end)
			case htmlRawScript:
				state = h.scanSegment("JavaScript", line[at:end], state, at)
			case htmlRawStyle:
				state = h.scanSegment("CSS", line[at:end], state, at)
			}
			at = end
			if at == len(line) {
				break
			}
			state.xmlMode, state.htmlRawTag = xmlHighlightText, 0
			state.codeRegion, state.codeCSSValue = codeOutsideRegion, false
		}

		start := at
		if state.xmlMode == xmlHighlightText && line[at] == '<' && (!html || htmlTagStart(line[at+1:])) {
			state.xmlValue, state.xmlName = false, true
			switch {
			case strings.HasPrefix(line[at:], "<!--"):
				state.xmlMode, at = xmlHighlightComment, at+4
			case strings.HasPrefix(line[at:], "<![CDATA["):
				state.xmlMode, at = xmlHighlightCDATA, at+9
			case strings.HasPrefix(line[at:], "<!"):
				state.xmlMode, at = xmlHighlightDirective, at+2
			case strings.HasPrefix(line[at:], "<?"):
				state.xmlMode, at = xmlHighlightInstruction, at+2
			default:
				state.xmlMode, at = xmlHighlightTag, at+1
				if html {
					state.htmlRawTag = matchHTMLRawTag(line[at:])
				}
			}
		}

		switch state.xmlMode {
		case xmlHighlightText:
			end := at
			for end < len(line) {
				offset := strings.IndexByte(line[end:], '<')
				if offset < 0 {
					end = len(line)
					break
				}
				end += offset
				if !html || htmlTagStart(line[end+1:]) {
					break
				}
				end++
			}
			value := line[at:end]
			left := end - len(strings.TrimLeft(value, syntaxWhitespace))
			right := at + len(strings.TrimRight(value, syntaxWhitespace))
			h.markupValue(line, left, right)
			at = end
		case xmlHighlightComment, xmlHighlightCDATA:
			closing, color := "-->", h.comment
			if state.xmlMode == xmlHighlightCDATA {
				closing, color = "]]>", h.value
			}
			if offset := strings.Index(line[at:], closing); offset >= 0 {
				at += offset + len(closing)
				state.xmlMode = xmlHighlightText
			} else {
				at = len(line)
			}
			h.add(start, at, color)
		case xmlHighlightDirective:
			for at < len(line) && state.xmlMode == xmlHighlightDirective {
				character := line[at]
				at++
				if state.quote != 0 {
					if character == state.quote {
						state.quote = 0
					}
					continue
				}
				switch character {
				case '"', '\'':
					state.quote = character
				case '[':
					state.depth++
				case ']':
					state.depth = max(0, state.depth-1)
				case '>':
					if state.depth == 0 {
						state.xmlMode = xmlHighlightText
					}
				}
			}
			h.add(start, at, h.keyword)
		case xmlHighlightTag, xmlHighlightInstruction:
			if at == len(line) {
				return state
			}
			if state.quote != 0 {
				start := at
				if offset := strings.IndexByte(line[at:], state.quote); offset >= 0 {
					at += offset + 1
					state.quote, state.xmlValue = 0, false
				} else {
					at = len(line)
				}
				h.markupValue(line, start, at)
				continue
			}

			if html && state.xmlValue && !syntaxSpace(line[at]) && !strings.ContainsRune(">'\"", rune(line[at])) {
				start := at
				for at < len(line) && !syntaxSpace(line[at]) && line[at] != '>' {
					at++
				}
				h.markupValue(line, start, at)
				state.xmlValue = false
				continue
			}
			switch line[at] {
			case '=':
				at++
				state.xmlValue = true
			case '"', '\'':
				start := at
				quote := line[at]
				at++
				if offset := strings.IndexByte(line[at:], quote); offset >= 0 {
					at += offset + 1
					state.xmlValue = false
				} else {
					at, state.quote = len(line), quote
				}
				h.markupValue(line, start, at)
			case '<':
				state.xmlMode, state.htmlRawTag = xmlHighlightText, 0
			case '?':
				at++
				if state.xmlMode == xmlHighlightInstruction && at < len(line) && line[at] == '>' {
					at++
					state.xmlMode = xmlHighlightText
				}
			case '>':
				at++
				if state.xmlMode == xmlHighlightTag {
					state.xmlMode = xmlHighlightText
					if state.htmlRawTag != 0 {
						state.xmlMode = htmlHighlightRawText
					}
				}
			case '/', ' ', '\t', '\r', '\f':
				at++
			default:
				start := at
				for at < len(line) && !syntaxSpace(line[at]) && !strings.ContainsRune("/=<>?\"'", rune(line[at])) {
					at++
				}
				switch {
				case state.xmlName:
					h.add(start, at, h.symbol)
					state.xmlName = false
				case state.xmlValue:
					h.markupValue(line, start, at)
					state.xmlValue = false
				default:
					h.add(start, at, h.key)
				}
			}
		}
	}
	return state
}

func (h *syntaxLine) markupValue(line string, start, end int) {
	if h.value.Alpha == 0 || start >= end {
		return
	}
	value := line[start:end]
	if !strings.ContainsRune(value, '&') {
		h.addScalar(line, start, end)
		return
	}
	at := start
	for _, entity := range markupEntity.FindAllStringIndex(value, -1) {
		left, right := start+entity[0], start+entity[1]
		h.add(at, left, h.value)
		h.add(left, right, h.literal)
		at = right
	}
	h.add(at, end, h.value)
}

func htmlTagStart(rest string) bool {
	if rest == "" {
		return false
	}
	if rest[0] == '!' || rest[0] == '?' {
		return true
	}
	rest = strings.TrimPrefix(rest, "/")
	return rest != "" && (rest[0] >= 'a' && rest[0] <= 'z' || rest[0] >= 'A' && rest[0] <= 'Z')
}

func matchHTMLRawTag(rest string) uint8 {
	for i := 1; i < len(htmlRawTags); i++ {
		if htmlTagMatches(rest, htmlRawTags[i]) {
			return uint8(i)
		}
	}
	return 0
}

func htmlTagMatches(rest, tag string) bool {
	end := len(tag)
	return len(rest) >= end && strings.EqualFold(rest[:end], tag) &&
		(len(rest) == end || syntaxSpace(rest[end]) || rest[end] == '>' || rest[end] == '/')
}

func htmlRawTextEnd(line, tag string) int {
	for at := 0; at < len(line); {
		offset := strings.Index(line[at:], "</")
		if offset < 0 {
			break
		}
		at += offset
		if htmlTagMatches(line[at+2:], tag) {
			return at
		}
		at += 2
	}
	return len(line)
}
