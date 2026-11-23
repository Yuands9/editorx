package editor

import (
	"sort"
	"strconv"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

type syntaxPalette struct {
	key, value, number, literal, comment gfx.Color
	keyword, symbol, punctuation, alert  gfx.Color
}

func syntaxColors(light bool) syntaxPalette {
	if light {
		return syntaxPalette{
			key: gfx.RGB(28, 86, 157), value: gfx.RGB(30, 109, 64), number: gfx.RGB(152, 73, 17),
			literal: gfx.RGB(111, 56, 156), comment: gfx.RGB(93, 103, 116), keyword: gfx.RGB(139, 48, 113),
			symbol: gfx.RGB(3, 105, 117), punctuation: gfx.RGB(93, 107, 128), alert: gfx.RGB(174, 45, 59),
		}
	}
	return syntaxPalette{
		key: gfx.RGB(149, 193, 250), value: gfx.RGB(164, 211, 151), number: gfx.RGB(237, 184, 121),
		literal: gfx.RGB(202, 163, 247), comment: gfx.RGB(147, 157, 171), keyword: gfx.RGB(232, 159, 200),
		symbol: gfx.RGB(116, 207, 206), punctuation: gfx.RGB(154, 166, 185), alert: gfx.RGB(251, 151, 155),
	}
}

type syntaxLine struct {
	syntaxPalette
	runs   []components.CodeEditorRun
	offset int
}

func (h *syntaxLine) add(start, end int, color gfx.Color) {
	if start < end && color.Alpha > 0 {
		h.runs = append(h.runs, components.CodeEditorRun{Start: h.offset + start, End: h.offset + end, Color: color, Italic: color == h.comment})
	}
}

func (h *syntaxLine) scalar(value string) gfx.Color {
	if h.value.Alpha == 0 {
		return h.value
	}
	value = strings.TrimSpace(value)
	switch value {
	case "true", "True", "TRUE", "false", "False", "FALSE", "null", "Null", "NULL", "~":
		return h.literal
	case ".inf", ".Inf", ".INF", "+.inf", "-.inf", ".nan", ".NaN", ".NAN":
		return h.number
	}
	if value != "" && strings.ContainsRune("+-.0123456789", rune(value[0])) {
		if _, err := strconv.ParseFloat(value, 64); err == nil {
			return h.number
		}
		if _, err := strconv.ParseInt(value, 0, 64); err == nil {
			return h.number
		}
	}
	return h.value
}

const syntaxWhitespace = " \t\r\f"

func syntaxSpace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\r' || value == '\f'
}

type configLineState struct {
	offset int
	state  configLexState
}

type configLexState struct {
	xmlMode             xmlHighlightMode
	htmlRawTag          uint8
	properties          propertyHighlightMode
	quote               byte
	xmlValue            bool
	xmlName             bool
	blockIndentKnown    bool
	codeRegion          codeRegion
	codeCSSValue        bool
	markdownMode        markdownHighlightMode
	markdownFence       byte
	markdownFenceSyntax uint8
	markdownFenceDepth  uint32
	markdownFenceLength uint32
	depth               int
	blockIndent         int
}

// Cache the lexical state at each reached line. Edits invalidate the suffix
// starting at the first changed line, so typing and scrolling don't re-scan the prefix.
type configHighlight struct {
	editor           *text.Editor
	language, source string
	revision         uint64
	composing        bool
	lines            []configLineState
	edits            []text.Edit
}

func (cache *configHighlight) prepare(editor *text.Editor, language string) {
	source := editor.String()
	_, composing := editor.Composition()
	if cache.editor != editor || cache.language != language {
		cache.editor, cache.language, cache.source = editor, language, source
		cache.revision, cache.composing = editor.TrackEdits(), composing
		cache.lines = append(cache.lines[:0], configLineState{})
		return
	}
	if cache.source == source {
		return
	}
	edits, revision, ok := editor.EditsSince(cache.revision, cache.edits)
	keep := 1
	if ok && len(edits) > 0 && !cache.composing && !composing {
		firstChanged := len(source)
		for _, edit := range edits {
			firstChanged = min(firstChanged, edit.Start)
		}
		keep = sort.Search(len(cache.lines), func(i int) bool { return cache.lines[i].offset > firstChanged })
	}
	cache.lines = cache.lines[:keep]
	cache.source, cache.edits, cache.revision, cache.composing = source, edits, revision, composing
}

func (p *editorPane) highlightConfigLine(language string, index int, line string, dst []components.CodeEditorRun) []components.CodeEditorRun {
	cache := &p.configHighlight
	for len(cache.lines) <= index {
		last := cache.lines[len(cache.lines)-1]
		previous, _, newline := strings.Cut(cache.source[last.offset:], "\n")
		if !newline {
			return dst
		}
		var stateOnly syntaxLine
		next := stateOnly.scan(language, previous, last.state)
		cache.lines = append(cache.lines, configLineState{offset: last.offset + len(previous) + 1, state: next})
	}
	entry := cache.lines[index]
	highlight := syntaxLine{syntaxPalette: syntaxColors(p.light), runs: dst}
	next := highlight.scan(language, line, entry.state)
	end := entry.offset + len(line)
	if index == len(cache.lines)-1 && end < len(cache.source) && cache.source[end] == '\n' {
		cache.lines = append(cache.lines, configLineState{offset: end + 1, state: next})
	}
	return highlight.runs
}

func (h *syntaxLine) scanSegment(language, line string, state configLexState, offset int) configLexState {
	h.offset += offset
	state = h.scan(language, line, state)
	h.offset -= offset
	return state
}

func (h *syntaxLine) scan(language, line string, state configLexState) configLexState {
	if syntax := codeSyntaxByName[language]; syntax != nil {
		return h.code(line, state, syntax)
	}
	switch language {
	case "JSON", "JSON Lines":
		if h.value.Alpha > 0 {
			h.json(line)
		}
	case "日志":
		if h.value.Alpha > 0 {
			h.plain(line, true)
		}
	case "CSV":
		return h.delimited(line, state, ',')
	case "TSV":
		return h.delimited(line, state, '\t')
	case "XML", "HTML":
		return h.markup(line, state, language == "HTML")
	case "YAML":
		return h.yaml(line, state)
	case "Markdown":
		return h.markdown(line, state)
	case "Properties":
		return h.propertyLine(line, state)
	case "INI":
		h.ini(line)
	case "TOML":
		return h.toml(line, state)
	case "ENV":
		return h.dotenv(line, state)
	}
	return state
}
