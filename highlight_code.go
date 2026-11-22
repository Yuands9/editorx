package main

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/vivaui/viva/gfx"
)

type codeRegion uint8

const (
	codeOutsideRegion codeRegion = iota
	codeSlashComment
	codeHashComment
	codeSingleQuoted
	codeDoubleQuoted
	codeBacktickQuoted
	codeTripleSingleQuoted
	codeTripleDoubleQuoted
)

var codeRegionEnd = [...]string{
	codeSlashComment: "*/", codeHashComment: "#>",
	codeSingleQuoted: "'", codeDoubleQuoted: "\"", codeBacktickQuoted: "`",
	codeTripleSingleQuoted: "'''", codeTripleDoubleQuoted: "\"\"\"",
}

var codeNumber = regexp.MustCompile(`(?i)^(?:0x[0-9a-f_]+(?:\.[0-9a-f_]*)?(?:p[+-]?[0-9_]+)?|0b[01_]+|0o[0-7_]+|(?:[0-9][0-9_]*(?:\.[0-9_]*)?|\.[0-9][0-9_]*)(?:e[+-]?[0-9][0-9_]*)?)(?:[dfijlun]\b)?`)

func (h *syntaxLine) code(line string, state configLexState, syntax *codeSyntax) configLexState {
	at := 0
	if state.codeRegion != codeOutsideRegion {
		at, state.codeRegion = h.codeRegion(line, 0, 0, state.codeRegion, syntax)
	}
	for at < len(line) {
		start := at
		rest := line[at:]
		character := rest[0]
		if syntaxSpace(character) {
			at++
			continue
		}
		if syntax.lineComment != "" && strings.HasPrefix(rest, syntax.lineComment) &&
			(syntax.options&codeShellQuotes == 0 || at == 0 || syntaxSpace(line[at-1]) || strings.ContainsRune(";|&()", rune(line[at-1]))) {
			h.add(at, len(line), h.comment)
			break
		}
		if character == '\'' && syntax.name == "Rust" {
			end := codeWordEnd(line, at+1, false)
			if end > at+1 && (end == len(line) || line[end] != '\'') {
				h.add(at, end, h.symbol)
				at = end
				continue
			}
		}
		region, opening := syntax.openRegion(rest)
		if region != codeOutsideRegion {
			at, state.codeRegion = h.codeRegion(line, at, at+opening, region, syntax)
			continue
		}
		css := syntax.options&codeCSSProperties != 0
		switch character {
		case ':':
			if css && strings.HasPrefix(rest, "::") {
				at += 2
				continue
			}
			before := syntaxTrimmedEnd(line, 0, at)
			state.codeCSSValue = css && before > 0 &&
				!strings.ContainsRune("{;}", rune(line[before-1]))
			at++
			continue
		case ';', '{', '}':
			state.codeCSSValue = false
		}
		if h.value.Alpha == 0 {
			at++
			continue
		}
		if strings.ContainsRune("[]{}()", rune(character)) {
			h.add(at, at+1, h.punctuation)
			at++
			continue
		}
		if character >= '0' && character <= '9' || character == '.' && len(rest) > 1 && rest[1] >= '0' && rest[1] <= '9' {
			match := codeNumber.FindString(rest)
			at += len(match)
			if css {
				at = codeWordEnd(line, at, true)
				if at < len(line) && line[at] == '%' {
					at++
				}
			}
			h.add(start, at, h.number)
			continue
		}
		if character == '@' || character == '#' && (css || syntax.options&codePreprocessor != 0) {
			at = codeWordEnd(line, at+1, css)
			color := h.symbol
			if character == '#' && !css {
				color = h.keyword
			} else if character == '#' && state.codeCSSValue && cssHexColor(line[start+1:at]) {
				color = h.number
			}
			h.add(start, at, color)
			continue
		}
		at = codeWordEnd(line, at, css)
		if at > start {
			word := line[start:at]
			tail := line[skipSyntaxSpace(line, at):]
			h.add(start, at, h.codeWordColor(word, tail, state.codeCSSValue, syntax))
			continue
		}
		_, size := utf8.DecodeRuneInString(line[at:])
		at += size
	}
	return state
}

func (syntax *codeSyntax) openRegion(rest string) (codeRegion, int) {
	switch rest[0] {
	case '/':
		if syntax.options&codeBlockComments != 0 && strings.HasPrefix(rest, "/*") {
			return codeSlashComment, 2
		}
	case '<':
		if syntax.options&codePowerShellQuotes != 0 && strings.HasPrefix(rest, "<#") {
			return codeHashComment, 2
		}
	case '"':
		if syntax.options&(codeTripleQuotes|codeTripleDoubleQuotes) != 0 && strings.HasPrefix(rest, "\"\"\"") {
			return codeTripleDoubleQuoted, 3
		}
		return codeDoubleQuoted, 1
	case '\'':
		if syntax.options&codeTripleQuotes != 0 && strings.HasPrefix(rest, "'''") {
			return codeTripleSingleQuoted, 3
		}
		return codeSingleQuoted, 1
	case '`':
		if syntax.options&(codeRawBackticks|codeTemplateBackticks) != 0 {
			return codeBacktickQuoted, 1
		}
	}
	return codeOutsideRegion, 0
}

func (h *syntaxLine) codeRegion(line string, start, at int, region codeRegion, syntax *codeSyntax) (int, codeRegion) {
	closing := codeRegionEnd[region]
	if region == codeSlashComment || region == codeHashComment {
		if offset := strings.Index(line[at:], closing); offset >= 0 {
			end := at + offset + len(closing)
			h.add(start, end, h.comment)
			return end, codeOutsideRegion
		}
		h.add(start, len(line), h.comment)
		return len(line), region
	}

	escape := byte('\\')
	doubled := false
	color := h.value
	switch {
	case syntax.options&codeSQLQuotes != 0:
		escape, doubled = 0, true
		if region != codeSingleQuoted {
			color = h.symbol
		}
	case syntax.options&codePowerShellQuotes != 0:
		escape, doubled = '`', true
		if region == codeSingleQuoted {
			escape = 0
		}
	case region == codeBacktickQuoted && syntax.options&codeTemplateBackticks == 0,
		region == codeSingleQuoted && syntax.options&codeShellQuotes != 0:
		escape = 0
	}
	end := at
	for end < len(line) {
		switch {
		case escape != 0 && line[end] == escape:
			end = min(end+2, len(line))
		case strings.HasPrefix(line[end:], closing):
			end += len(closing)
			if doubled && end < len(line) && line[end] == closing[0] {
				end++
				continue
			}
			if syntax.options&codeSQLQuotes == 0 && strings.HasPrefix(line[skipSyntaxSpace(line, end):], ":") &&
				(syntax.name == "JavaScript" || syntax.name == "TypeScript" || syntax.name == "Python") {
				color = h.key
			}
			h.add(start, end, color)
			return end, codeOutsideRegion
		default:
			end++
		}
	}
	h.add(start, len(line), color)
	if region >= codeBacktickQuoted || syntax.options&(codeSQLQuotes|codeShellQuotes|codePowerShellQuotes) != 0 {
		return len(line), region
	}
	return len(line), codeOutsideRegion
}

func (h *syntaxLine) codeWordColor(word, tail string, cssValue bool, syntax *codeSyntax) gfx.Color {
	lookup := word
	if syntax.options&codeFoldWords != 0 {
		lookup = strings.ToLower(word)
	}
	switch syntax.words[lookup] {
	case codeKeyword:
		return h.keyword
	case codeType:
		return h.symbol
	case codeLiteral:
		return h.literal
	}
	if cssValue {
		return h.value
	}
	if strings.HasPrefix(tail, ":") && !strings.HasPrefix(tail, "::") ||
		strings.HasPrefix(tail, "=") && !strings.HasPrefix(tail, "==") && !strings.HasPrefix(tail, "=>") {
		return h.key
	}
	first, _ := utf8.DecodeRuneInString(word)
	if strings.HasPrefix(tail, "(") || syntax.options&codeCSSProperties != 0 ||
		syntax.options&codeFoldWords == 0 && unicode.IsUpper(first) {
		return h.symbol
	}
	if first == '$' && (syntax.options&(codeShellQuotes|codePowerShellQuotes) != 0) {
		return h.key
	}
	return gfx.Color{}
}

func codeWordEnd(line string, at int, allowDash bool) int {
	for at < len(line) {
		r, size := utf8.DecodeRuneInString(line[at:])
		if r != '_' && r != '$' && !(allowDash && r == '-') && !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsMark(r) {
			break
		}
		at += size
	}
	return at
}

func cssHexColor(value string) bool {
	switch len(value) {
	case 3, 4, 6, 8:
	default:
		return false
	}
	for i := range len(value) {
		c := value[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}
