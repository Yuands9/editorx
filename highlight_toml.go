package main

import "strings"

func (h *syntaxLine) toml(line string, state configLexState) configLexState {
	at := 0
	if state.quote != 0 {
		end, closed := assignmentQuoteEnd(line, 0, state.quote, 3)
		h.add(at, end, h.value)
		if !closed {
			return state
		}
		at, state.quote = end, 0
	}
	at = skipSyntaxSpace(line, at)
	firstKey := at < len(line) && state.depth == 0 && at == skipSyntaxSpace(line, 0)
	if firstKey && line[at] == '[' {
		start := at + 1
		if start < len(line) && line[start] == '[' {
			start++
		}
		start = skipSyntaxSpace(line, start)
		end := tomlBoundary(line, start, "]#")
		h.add(start, syntaxTrimmedEnd(line, start, end), h.symbol)
		if comment := strings.IndexByte(line[end:], '#'); comment >= 0 {
			h.add(end+comment, len(line), h.comment)
		}
		return state
	}

	for at < len(line) {
		if syntaxSpace(line[at]) {
			at++
			continue
		}
		switch line[at] {
		case '#':
			h.add(at, len(line), h.comment)
			return state
		case '[', '{':
			state.depth++
			at++
			continue
		case ']', '}':
			state.depth = max(0, state.depth-1)
			at++
			continue
		case ',', '=':
			at++
			continue
		}

		separator := tomlBoundary(line, at, "=,{}[]#")
		if separator < len(line) && line[separator] == '=' {
			h.add(at, syntaxTrimmedEnd(line, at, separator), h.key)
			at, firstKey = separator+1, false
			continue
		}
		if firstKey {
			h.add(at, syntaxTrimmedEnd(line, at, separator), h.key)
			if separator < len(line) && line[separator] == '#' {
				h.add(separator, len(line), h.comment)
			}
			return state
		}

		if line[at] == '"' || line[at] == '\'' {
			size := 1
			if at+2 < len(line) && line[at] == line[at+1] && line[at] == line[at+2] {
				size = 3
			}
			end, closed := assignmentQuoteEnd(line, at+size, line[at], size)
			h.add(at, end, h.value)
			if !closed && size == 3 {
				state.quote = line[at]
			}
			at = end
			continue
		}

		end := at + 1
		for end < len(line) && !strings.ContainsRune(",[]{}#", rune(line[end])) {
			end++
		}
		switch strings.TrimSpace(line[at:end]) {
		case "inf", "+inf", "-inf", "nan", "+nan", "-nan":
			h.add(at, syntaxTrimmedEnd(line, at, end), h.number)
		default:
			h.addScalar(line, at, end)
		}
		at = end
	}
	return state
}

func tomlBoundary(line string, at int, delimiters string) int {
	for at < len(line) {
		switch {
		case strings.ContainsRune(delimiters, rune(line[at])):
			return at
		case line[at] == '"' || line[at] == '\'':
			if at+2 < len(line) && line[at] == line[at+1] && line[at] == line[at+2] {
				return len(line)
			}
			at, _ = assignmentQuoteEnd(line, at+1, line[at], 1)
		default:
			at++
		}
	}
	return at
}
