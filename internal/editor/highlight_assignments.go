package editor

import "strings"

func skipSyntaxSpace(line string, at int) int {
	for at < len(line) && syntaxSpace(line[at]) {
		at++
	}
	return at
}

func syntaxTrimmedEnd(line string, start, end int) int {
	for end > start && syntaxSpace(line[end-1]) {
		end--
	}
	return end
}

func (h *syntaxLine) addScalar(line string, start, end int) {
	end = syntaxTrimmedEnd(line, start, end)
	h.add(start, end, h.scalar(line[start:end]))
}

func assignmentQuoteEnd(line string, at int, quote byte, size int) (int, bool) {
	for at < len(line) {
		switch {
		case quote == '"' && line[at] == '\\':
			at += 2
		case line[at] != quote:
			at++
		case size == 1:
			return at + 1, true
		case at+2 < len(line) && line[at+1] == quote && line[at+2] == quote:
			end := at + 3
			for end < len(line) && end < at+5 && line[end] == quote {
				end++
			}
			return end, true
		default:
			at++
		}
	}
	return len(line), false
}

func iniCommentStart(line string, at int) int {
	start := at
	for at < len(line) {
		switch {
		case line[at] == '"' || line[at] == '\'':
			at, _ = assignmentQuoteEnd(line, at+1, line[at], 1)
		case (line[at] == ';' || line[at] == '#') && (at == start || syntaxSpace(line[at-1])):
			return at
		default:
			at++
		}
	}
	return len(line)
}

func (h *syntaxLine) ini(line string) {
	start := skipSyntaxSpace(line, 0)
	if start == len(line) {
		return
	}
	if line[start] == ';' || line[start] == '#' {
		h.add(start, len(line), h.comment)
		return
	}
	if line[start] == '[' {
		close := strings.IndexByte(line[start+1:], ']')
		if close < 0 {
			h.add(start+1, len(line), h.symbol)
			return
		}
		end := start + 1 + close
		h.add(start+1, end, h.symbol)
		comment := iniCommentStart(line, end+1)
		h.add(comment, len(line), h.comment)
		return
	}
	comment := iniCommentStart(line, start)
	separator := strings.IndexAny(line[start:comment], "=:")
	if separator < 0 {
		h.add(start, syntaxTrimmedEnd(line, start, comment), h.key)
	} else {
		keyEnd := start + separator
		h.add(start, syntaxTrimmedEnd(line, start, keyEnd), h.key)
		valueStart := min(skipSyntaxSpace(line, keyEnd+1), comment)
		h.addScalar(line, valueStart, comment)
	}
	h.add(comment, len(line), h.comment)
}

func (h *syntaxLine) dotenv(line string, state configLexState) configLexState {
	at := 0
	if state.quote == 0 {
		at = skipSyntaxSpace(line, at)
		if strings.HasPrefix(line[at:], "export") && at+6 < len(line) && syntaxSpace(line[at+6]) {
			h.add(at, at+6, h.keyword)
			at = skipSyntaxSpace(line, at+6)
		}
		separator := strings.IndexAny(line[at:], "=#")
		if separator < 0 {
			h.add(at, syntaxTrimmedEnd(line, at, len(line)), h.key)
			return state
		}
		separator += at
		h.add(at, syntaxTrimmedEnd(line, at, separator), h.key)
		if line[separator] == '#' {
			h.add(separator, len(line), h.comment)
			return state
		}
		at = skipSyntaxSpace(line, separator+1)
	}

	valueStart := at
	if state.quote == 0 && at < len(line) && (line[at] == '"' || line[at] == '\'') {
		state.quote = line[at]
		at++
	}
	if state.quote != 0 {
		end, closed := assignmentQuoteEnd(line, at, state.quote, 1)
		h.add(valueStart, end, h.value)
		if !closed {
			return state
		}
		state.quote, valueStart = 0, end
	}
	comment := len(line)
	if offset := strings.IndexByte(line[valueStart:], '#'); offset >= 0 {
		comment = valueStart + offset
	}
	h.addScalar(line, skipSyntaxSpace(line[:comment], valueStart), comment)
	h.add(comment, len(line), h.comment)
	return state
}
