package editor

import "unicode/utf8"

// Map a preview edit through whitespace-only formatting. Token bytes, including
// spaces and escapes inside strings, must match up to the edit. Callers only
// use this for a current, whitespace-only preview, never for sorted keys.
func jsonPreviewEdit(source, preview, edited string) (string, bool) {
	start := 0
	for start < min(len(preview), len(edited)) && preview[start] == edited[start] {
		start++
	}
	for start > 0 && start < len(preview) && !utf8.RuneStart(preview[start]) {
		start--
	}
	end, replacementEnd := len(preview), len(edited)
	for end > start && replacementEnd > start && preview[end-1] == edited[replacementEnd-1] {
		end--
		replacementEnd--
	}
	for end < len(preview) && !utf8.RuneStart(preview[end]) {
		end++
		replacementEnd++
	}
	a, b, mappedStart, mappedEnd := 0, 0, -1, -1
	quoted, escaped := false, false
	space := func(v byte) bool { return v == ' ' || v == '\t' || v == '\r' || v == '\n' }
	for {
		if b == start && mappedStart < 0 {
			mappedStart = a
		}
		if b == end && mappedEnd < 0 {
			mappedEnd = a
		}
		if mappedStart >= 0 && mappedEnd >= mappedStart {
			return source[:mappedStart] + edited[start:replacementEnd] + source[mappedEnd:], true
		}
		if !quoted && a < len(source) && space(source[a]) {
			a++
			continue
		}
		if !quoted && b < len(preview) && space(preview[b]) {
			b++
			continue
		}
		if a == len(source) || b == len(preview) {
			break
		}
		if source[a] != preview[b] {
			return "", false
		}
		ch := preview[b]
		if escaped {
			escaped = false
		} else if quoted && ch == '\\' {
			escaped = true
		} else if ch == '"' {
			quoted = !quoted
		}
		a++
		b++
	}
	return "", false
}
