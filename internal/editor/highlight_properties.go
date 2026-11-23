package editor

import "strings"

type propertyHighlightMode uint8

const (
	propertyStart propertyHighlightMode = iota
	propertyKeyContinued
	propertySeparatorContinued
	propertyValueContinued
)

func (h *syntaxLine) propertyLine(line string, state configLexState) configLexState {
	line = strings.TrimSuffix(line, "\r")
	start := len(line) - len(strings.TrimLeft(line, syntaxWhitespace))
	mode := state.properties
	state.properties = propertyStart
	if start == len(line) {
		return state
	}
	if mode == propertyStart && (line[start] == '#' || line[start] == '!') {
		h.add(start, len(line), h.comment)
		return state
	}

	end := len(line)
	for end > start && line[end-1] == '\\' {
		end--
	}
	continued := (len(line)-end)%2 != 0
	if mode <= propertyKeyContinued {
		at := start
		for at < len(line) && !syntaxSpace(line[at]) && line[at] != ':' && line[at] != '=' {
			if line[at] == '\\' {
				at = min(at+2, len(line))
			} else {
				at++
			}
		}
		h.add(start, at, h.key)
		if at == len(line) {
			if continued {
				state.properties = propertyKeyContinued
			}
			return state
		}
		start = len(line) - len(strings.TrimLeft(line[at:], syntaxWhitespace))
	}
	hasSeparator := false
	if mode != propertyValueContinued && start < len(line) && (line[start] == ':' || line[start] == '=') {
		start = len(line) - len(strings.TrimLeft(line[start+1:], syntaxWhitespace))
		hasSeparator = true
	}
	h.add(start, len(line), h.scalar(line[start:]))
	if continued {
		state.properties = propertyValueContinued
		if mode != propertyValueContinued && !hasSeparator && start == len(line)-1 {
			state.properties = propertySeparatorContinued
		}
	}
	return state
}
