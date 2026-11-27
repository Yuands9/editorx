package editor

import (
	"sort"
	"strings"

	"github.com/vivaui/viva/components"
)

func foldableLanguage(language string) bool {
	return language == "JSON" || language == "Markdown" || language == "YAML" || language == "XML" || language == "HTML" || codeSyntaxByName[language] != nil
}

// Folding is lexical: strings/comments cannot open brace blocks. Python/YAML
// use indentation and Markdown uses headings/fences; no project index is kept.
func editorFolds(value, language string) []components.CodeEditorFoldRange {
	if len(value) > localHistoryFileSizeLimit {
		return nil
	}
	if language == "JSON" {
		return jsonFolds(value)
	}
	if !foldableLanguage(language) {
		return nil
	}
	lines := strings.Split(value, "\n")
	var folds []components.CodeEditorFoldRange
	add := func(first, last int) {
		if last > first {
			folds = append(folds, components.CodeEditorFoldRange{Header: first, LastLine: last, Display: strings.TrimRight(lines[first], "\r") + " …"})
		}
	}
	if language == "Python" || language == "YAML" {
		type indentBlock struct{ line, indent int }
		var stack []indentBlock
		previous, previousIndent := -1, 0
		for i, line := range lines {
			content := strings.TrimSpace(line)
			if content == "" || strings.HasPrefix(content, "#") {
				continue
			}
			indent := 0
			for _, r := range line {
				if r == ' ' {
					indent++
				} else if r == '\t' {
					indent += 4 - indent%4
				} else {
					break
				}
			}
			for len(stack) > 0 && indent <= stack[len(stack)-1].indent {
				top := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				add(top.line, previous)
			}
			if previous >= 0 && indent > previousIndent {
				stack = append(stack, indentBlock{previous, previousIndent})
			}
			previous, previousIndent = i, indent
		}
		for _, top := range stack {
			add(top.line, previous)
		}
	} else if language == "Markdown" {
		type section struct{ line, level int }
		var stack []section
		fence, fenceLine := "", 0
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
				mark := trimmed[:3]
				if fence == "" {
					fence, fenceLine = mark, i
				} else if fence == mark {
					add(fenceLine, i)
					fence = ""
				}
				continue
			}
			if fence != "" {
				continue
			}
			level := len(trimmed) - len(strings.TrimLeft(trimmed, "#"))
			if level < 1 || level > 6 || len(trimmed) <= level || trimmed[level] != ' ' {
				continue
			}
			for len(stack) > 0 && stack[len(stack)-1].level >= level {
				top := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				add(top.line, i-1)
			}
			stack = append(stack, section{i, level})
		}
		for _, top := range stack {
			add(top.line, len(lines)-1)
		}
	} else {
		type delimiter struct {
			line int
			mark byte
			name string
		}
		var stack []delimiter
		line, quote, blockComment, htmlComment := 0, byte(0), false, false
		lineComment := "//"
		if syntax := codeSyntaxByName[language]; syntax != nil {
			lineComment = syntax.lineComment
		}
		markup := language == "HTML" || language == "XML"
		for i := 0; i < len(value); i++ {
			ch := value[i]
			if ch == '\n' {
				line++
			}
			if htmlComment {
				if strings.HasPrefix(value[i:], "-->") {
					htmlComment = false
					i += 2
				}
				continue
			}
			if blockComment {
				if strings.HasPrefix(value[i:], "*/") {
					blockComment = false
					i++
				}
				continue
			}
			if quote != 0 {
				if ch == '\\' && quote != '`' {
					if i+1 < len(value) && value[i+1] == '\n' {
						line++
					}
					i++
				} else if ch == quote {
					quote = 0
				}
				continue
			}
			if markup {
				if strings.HasPrefix(value[i:], "<!--") {
					htmlComment = true
					i += 3
					continue
				}
				if ch != '<' {
					continue
				}
				end := strings.IndexByte(value[i:], '>')
				if end < 0 {
					break
				}
				end += i
				tag := strings.TrimSpace(value[i+1 : end])
				fields := strings.Fields(strings.TrimPrefix(tag, "/"))
				if len(fields) == 0 {
					continue
				}
				name := strings.TrimSuffix(fields[0], "/")
				if language == "HTML" {
					name = strings.ToLower(name)
				}
				if strings.HasPrefix(tag, "/") {
					if len(stack) > 0 && stack[len(stack)-1].name == name {
						top := stack[len(stack)-1]
						stack = stack[:len(stack)-1]
						add(top.line, line+strings.Count(value[i:end], "\n"))
					}
				} else if !strings.HasPrefix(tag, "!") && !strings.HasPrefix(tag, "?") && !strings.HasSuffix(tag, "/") && !(language == "HTML" && strings.Contains(" area base br col embed hr img input link meta param source track wbr ", " "+name+" ")) {
					stack = append(stack, delimiter{line: line, name: name})
				}
				line += strings.Count(value[i:end], "\n")
				i = end
				continue
			}
			if lineComment != "" && strings.HasPrefix(value[i:], lineComment) {
				end := strings.IndexByte(value[i:], '\n')
				if end < 0 {
					break
				}
				i += end - 1
				continue
			}
			if strings.HasPrefix(value[i:], "/*") {
				blockComment = true
				i++
				continue
			}
			if ch == '\'' || ch == '"' || ch == '`' {
				quote = ch
				continue
			}
			switch ch {
			case '{', '[', '(':
				stack = append(stack, delimiter{line: line, mark: ch})
			case '}', ']', ')':
				if len(stack) == 0 {
					continue
				}
				top := stack[len(stack)-1]
				if top.mark == '{' && ch == '}' || top.mark == '[' && ch == ']' || top.mark == '(' && ch == ')' {
					stack = stack[:len(stack)-1]
					add(top.line, line)
				}
			}
		}
	}
	sort.Slice(folds, func(i, j int) bool {
		if folds[i].Header == folds[j].Header {
			return folds[i].LastLine > folds[j].LastLine
		}
		return folds[i].Header < folds[j].Header
	})
	unique := folds[:0]
	for _, fold := range folds {
		if len(unique) == 0 || unique[len(unique)-1].Header != fold.Header {
			unique = append(unique, fold)
		}
	}
	return unique
}
