package editor

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

func (h *syntaxLine) json(line string) {
	for at := 0; at < len(line); {
		start := at
		switch character := line[at]; {
		case character == '"':
			at, _ = assignmentQuoteEnd(line, at+1, character, 1)
			color := h.value
			if tail := strings.TrimLeft(line[at:], syntaxWhitespace); strings.HasPrefix(tail, ":") {
				color = h.key
			}
			h.add(start, at, color)
		case strings.ContainsRune("{}[]", rune(character)):
			at++
			h.add(start, at, h.punctuation)
		case character == '-' || character >= '0' && character <= '9':
			at++
			for at < len(line) && strings.ContainsRune("-+.eE0123456789", rune(line[at])) {
				at++
			}
			h.add(start, at, h.number)
		default:
			at = codeWordEnd(line, at, false)
			if at > start {
				switch line[start:at] {
				case "true", "false", "null":
					h.add(start, at, h.literal)
				}
				continue
			}
			_, size := utf8.DecodeRuneInString(line[at:])
			at += size
		}
	}
}

const syntaxURL = `https?://[^\s<>\p{Zs}\[\](){}"'，。、；！？：）…》【】]+`

var plainTokens = regexp.MustCompile(syntaxURL + `|\b(?:TRACE|DEBUG|INFO|NOTICE|WARN|WARNING|ERROR|FATAL|CRITICAL|true|false|null)\b|\b[A-Za-z_][A-Za-z0-9_.-]*=|\b[0-9]+(?:\.[0-9]+)?\b`)

func (h *syntaxLine) plain(line string, log bool) {
	line = strings.TrimRight(line, syntaxWhitespace)
	start := skipSyntaxSpace(line, 0)
	if start == len(line) {
		return
	}
	content := line[start:]
	if !log {
		if strings.HasPrefix(content, "# ") || strings.HasPrefix(content, "//") || strings.HasPrefix(content, "; ") {
			h.add(start, len(line), h.comment)
			return
		}
		if separator := strings.IndexByte(content, '='); separator > 0 &&
			markdownLabel.MatchString(strings.TrimRight(content[:separator], syntaxWhitespace)) &&
			!strings.HasPrefix(content[separator:], "==") {
			keyEnd := start + separator
			h.add(start, syntaxTrimmedEnd(line, start, keyEnd), h.key)
			valueStart := skipSyntaxSpace(line, keyEnd+1)
			comment := iniCommentStart(line, valueStart)
			h.addScalar(line, valueStart, comment)
			h.add(comment, len(line), h.comment)
			return
		}
		if h.markdownKeyValue(content, start) {
			return
		}
	}
	for _, bounds := range plainTokens.FindAllStringIndex(content, -1) {
		left, right := start+bounds[0], start+bounds[1]
		token := line[left:right]
		switch token {
		case "ERROR", "FATAL", "CRITICAL":
			h.add(left, right, h.alert)
		case "WARN", "WARNING":
			h.add(left, right, h.number)
		case "INFO", "NOTICE":
			h.add(left, right, h.symbol)
		case "DEBUG", "TRACE":
			h.add(left, right, h.comment)
		case "true", "false", "null":
			h.add(left, right, h.literal)
		default:
			switch {
			case strings.HasPrefix(token, "http://") || strings.HasPrefix(token, "https://"):
				h.add(left, left+len(strings.TrimRight(token, ".,!?;")), h.symbol)
			case strings.HasSuffix(token, "="):
				h.add(left, right-1, h.key)
			default:
				h.add(left, right, h.number)
			}
		}
	}
}

func (h *syntaxLine) delimited(line string, state configLexState, separator byte) configLexState {
	for at := 0; at < len(line); {
		start := at
		switch {
		case state.quote != 0 || line[at] == '"':
			if state.quote == 0 {
				state.quote = '"'
				at++
			}
			for at < len(line) {
				offset := strings.IndexByte(line[at:], '"')
				if offset < 0 {
					at = len(line)
					break
				}
				at += offset + 1
				if at == len(line) || line[at] != '"' {
					state.quote = 0
					break
				}
				at++
			}
			h.add(start, at, h.value)
		case line[at] == separator:
			at++
			h.add(start, at, h.punctuation)
		default:
			if offset := strings.IndexByte(line[at:], separator); offset >= 0 {
				at += offset
			} else {
				at = len(line)
			}
			h.addScalar(line, skipSyntaxSpace(line[:at], start), at)
		}
	}
	return state
}
