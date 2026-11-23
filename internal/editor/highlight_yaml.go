package editor

import "strings"

func (h *syntaxLine) yaml(line string, state configLexState) configLexState {
	at := len(line) - len(strings.TrimLeft(line, syntaxWhitespace))
	indent := at
	if state.blockIndent > 0 {
		if at == len(line) {
			return state
		}
		if indent >= state.blockIndent {
			if !state.blockIndentKnown {
				state.blockIndent, state.blockIndentKnown = indent, true
			}
			h.add(at, len(line), h.value)
			return state
		}
		state.blockIndent, state.blockIndentKnown = 0, false
	}
	if state.quote != 0 {
		end, closed := yamlQuoteEnd(line, 0, state.quote)
		h.add(0, end, h.value)
		if !closed {
			return state
		}
		state.quote, at = 0, end
	}

	parentIndent := indent
	leading, explicitKey := at == indent && state.depth == 0, false
	for at < len(line) {
		if syntaxSpace(line[at]) {
			at++
			continue
		}
		if line[at] == '#' {
			h.add(at, len(line), h.comment)
			break
		}
		if leading {
			if line[at] == '%' {
				h.add(at, len(line), h.keyword)
				break
			}
			if (strings.HasPrefix(line[at:], "---") || strings.HasPrefix(line[at:], "...")) && yamlSpaceAfter(line, at+3) {
				h.add(at, at+3, h.punctuation)
				at += 3
				continue
			}
			if (line[at] == '-' || line[at] == '?') && yamlSpaceAfter(line, at+1) {
				parentIndent, explicitKey = at, line[at] == '?'
				at++
				continue
			}
		}
		leading = false
		switch line[at] {
		case '[', '{':
			state.depth++
			at++
			continue
		case ']', '}':
			state.depth = max(0, state.depth-1)
			at++
			continue
		case ',':
			if state.depth > 0 {
				at++
				continue
			}
		}
		flow := state.depth > 0
		if yamlKeySeparator(line, at, flow) {
			at++
			continue
		}

		start := at
		quoted := line[start] == '"' || line[start] == '\''
		property := strings.ContainsRune("&*!", rune(line[start]))
		var end, separator int
		if quoted {
			var closed bool
			end, closed = yamlQuoteEnd(line, start+1, line[start])
			if !closed {
				state.quote = line[start]
				h.add(start, end, h.value)
				return state
			}
			separator = end
			for separator < len(line) && syntaxSpace(line[separator]) {
				separator++
			}
		} else {
			separator = yamlPlainEnd(line, start, flow, property)
			end = start + len(strings.TrimRight(line[start:separator], syntaxWhitespace))
		}

		hasSeparator := yamlKeySeparator(line, separator, flow) ||
			quoted && flow && separator < len(line) && line[separator] == ':'
		switch {
		case hasSeparator || explicitKey:
			h.add(start, end, h.key)
			parentIndent = start
		case quoted:
			h.add(start, end, h.value)
		case property:
			h.add(start, end, h.symbol)
		default:
			scalar := line[start:end]
			if !flow && (line[start] == '|' || line[start] == '>') {
				if extraIndent, ok := yamlBlockHeader(scalar); ok {
					state.blockIndent = parentIndent + max(1, extraIndent)
					state.blockIndentKnown = extraIndent > 0
				}
			}
			h.add(start, end, h.scalar(scalar))
		}
		explicitKey = false
		at = end
		if hasSeparator {
			at = separator + 1
		}
	}
	return state
}

func yamlSpaceAfter(line string, at int) bool {
	return at == len(line) || syntaxSpace(line[at])
}

func yamlKeySeparator(line string, at int, flow bool) bool {
	return at < len(line) && line[at] == ':' && (yamlSpaceAfter(line, at+1) ||
		flow && strings.ContainsRune(",[]{}", rune(line[at+1])))
}

func yamlPlainEnd(line string, start int, flow, property bool) int {
	at := start + 1
	for at < len(line) {
		character := line[at]
		if yamlKeySeparator(line, at, flow) || character == '#' && syntaxSpace(line[at-1]) ||
			flow && strings.ContainsRune(",[]{}", rune(character)) || property && syntaxSpace(character) {
			break
		}
		at++
	}
	return at
}

func yamlQuoteEnd(line string, start int, quote byte) (int, bool) {
	for at := start; at < len(line); at++ {
		if quote == '"' && line[at] == '\\' {
			at++
			continue
		}
		if line[at] == quote {
			if quote == '\'' && at+1 < len(line) && line[at+1] == '\'' {
				at++
				continue
			}
			return at + 1, true
		}
	}
	return len(line), false
}

func yamlBlockHeader(value string) (indent int, ok bool) {
	chomp := false
	for at := 1; at < len(value); at++ {
		switch character := value[at]; {
		case (character == '+' || character == '-') && !chomp:
			chomp = true
		case character >= '1' && character <= '9' && indent == 0:
			indent = int(character - '0')
		default:
			return 0, false
		}
	}
	return indent, true
}
