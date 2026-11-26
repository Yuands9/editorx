package editor

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/vivaui/viva/components"
)

// Folding needs bracket positions, not a second full JSON value tree.
func jsonFolds(source string) []components.CodeEditorFoldRange {
	if !json.Valid([]byte(source)) {
		return nil
	}
	type bracket struct {
		offset, line, lineStart, count int
		array, next                    bool
	}
	type fold struct {
		offset int
		value  components.CodeEditorFoldRange
	}
	var stack []bracket
	var found []fold
	line, lineStart := 0, 0
	quoted, escape := false, false
	for index := 0; index < len(source); index++ {
		character := source[index]
		if quoted {
			if escape {
				escape = false
			} else if character == '\\' {
				escape = true
			} else if character == '"' {
				quoted = false
			}
			continue
		}
		if character == '\n' {
			line++
			lineStart = index + 1
			continue
		}
		if character == ' ' || character == '\r' || character == '\t' {
			continue
		}
		if len(stack) > 0 {
			parent := &stack[len(stack)-1]
			if parent.array && parent.next && character != ']' {
				parent.count++
				parent.next = false
			}
		}
		switch character {
		case '"':
			quoted = true
		case '{', '[':
			stack = append(stack, bracket{offset: index, line: line, lineStart: lineStart, array: character == '[', next: true})
		case ',':
			if len(stack) > 0 {
				stack[len(stack)-1].next = true
			}
		case '}', ']':
			node := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if node.line == line {
				continue
			}
			summary := "Object{...}"
			if node.array {
				summary = fmt.Sprintf("Array[%d]", node.count)
			}
			end := len(source)
			if newline := strings.IndexByte(source[index+1:], '\n'); newline >= 0 {
				end = index + 1 + newline
			}
			prefix := source[node.lineStart:node.offset]
			found = append(found, fold{node.offset, components.CodeEditorFoldRange{Header: node.line, LastLine: line,
				Display: prefix + summary + strings.TrimSuffix(source[index+1:end], "\r"), SummaryStart: len(prefix)}})
		}
	}
	sort.Slice(found, func(i, j int) bool { return found[i].offset < found[j].offset })
	result := make([]components.CodeEditorFoldRange, 0, len(found))
	for _, item := range found {
		if len(result) == 0 || result[len(result)-1].Header != item.value.Header {
			result = append(result, item.value)
		}
	}
	return result
}
