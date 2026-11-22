package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/vivaui/viva/text"
)

func (m *editorApp) mergeJSONDifference(tab *editorTab, difference jsonDifference, intoLeft bool) {
	for _, pane := range []*editorPane{tab.source, tab.comparison} {
		if _, composing := pane.text.Composition(); composing {
			m.status = "请先完成中文选词，再合并差异"
			return
		}
	}
	if !tab.jsonCompare || !tab.diffReady || tab.diffError != "" ||
		tab.source.text.String() != tab.diffLeft || tab.comparison.text.String() != tab.diffRight {
		m.status = "对比内容已更新，请重新选择差异"
		return
	}
	destination, side := tab.comparison, "右侧"
	if intoLeft {
		destination, side = tab.source, "左侧"
	}
	span, value, err := jsonMergeEdit(tab.diffLeft, tab.diffRight, difference, intoLeft, tab.jsonIndent)
	if err != nil {
		m.fail(err)
		return
	}
	editor := destination.text
	editor.ReplaceRange(text.Range{Start: editor.RuneIndex(span.Start), End: editor.RuneIndex(span.End)}, value)
	tab.source.focus, tab.comparison.focus = intoLeft, !intoLeft
	m.status = "已合并到" + side + " · Ctrl+Z 撤销"
}

// 只替换目标值或一段容器成员，保留差异之外的换行、键顺序及数字原文。
func jsonMergeEdit(left, right string, difference jsonDifference, intoLeft bool, indent int) (text.Span, string, error) {
	target, source := right, left
	to, from := difference.right, difference.left
	if intoLeft {
		target, source = left, right
		to, from = difference.left, difference.right
	}
	hasTarget, hasSource := to.End > to.Start, from.End > from.Start
	if hasTarget && hasSource {
		prefix := jsonLineIndent(target, to.Start)
		if prefix == "" && !strings.ContainsRune(target[to.Start:to.End], '\n') {
			indent = 0
		}
		value, err := formatJSONMergeValue(source[from.Start:from.End], prefix, indent)
		return to, value, err
	}
	root, err := parseJSON(target)
	if err != nil {
		return text.Span{}, "", err
	}
	parent, key := jsonMergeParent(root, difference.path)
	if parent == nil || (parent.kind != "object" && parent.kind != "array") {
		return text.Span{}, "", errors.New("无法定位差异的父级，请重新比较")
	}
	index, count := parent.memberIndex(key), len(parent.members)
	if parent.kind == "array" {
		index, err = strconv.Atoi(key)
		count = len(parent.items)
		if err != nil || index < 0 || index > count {
			return text.Span{}, "", errors.New("请先合并前面缺失的数组项")
		}
	}
	if !hasSource {
		if index < 0 || index >= count {
			return text.Span{}, "", errors.New("无法定位要删除的值，请重新比较")
		}
		if count == 1 {
			return text.Span{Start: parent.start + 1, End: parent.end - 1}, "", nil
		}
		entry := parent.entrySpan(index)
		if index+1 < count {
			entry.End = parent.entrySpan(index + 1).Start
		} else {
			entry.Start = parent.entrySpan(index - 1).End
		}
		return entry, "", nil
	}

	offset, separator := parent.start+1, ""
	if count > 0 {
		offset, separator = parent.entrySpan(count-1).End, ","
	}
	prefix, colon := "", ":"
	if strings.ContainsRune(target[parent.start:parent.end], '\n') {
		prefix = jsonLineIndent(target, parent.start) + strings.Repeat(" ", indent)
		if count > 0 {
			last := parent.entrySpan(count - 1).Start
			if strings.LastIndexByte(target[parent.start:last], '\n') >= 0 {
				prefix = jsonLineIndent(target, last)
			}
		}
		separator += "\n" + prefix
		colon = ": "
	} else {
		indent = 0
	}
	if parent.kind == "object" {
		encoded, _ := json.Marshal(key)
		separator += string(encoded) + colon
	}
	value, err := formatJSONMergeValue(source[from.Start:from.End], prefix, indent)
	return text.Span{Start: offset, End: offset}, separator + value, err
}

var jsonPointerUnescape = strings.NewReplacer("~1", "/", "~0", "~")

func jsonMergeParent(value *jsonValue, path string) (*jsonValue, string) {
	path, ok := strings.CutPrefix(path, "/")
	if !ok {
		return nil, ""
	}
	segments := strings.Split(path, "/")
	for _, segment := range segments[:len(segments)-1] {
		key := jsonPointerUnescape.Replace(segment)
		if value.kind == "object" {
			index := value.memberIndex(key)
			if index < 0 {
				return nil, ""
			}
			value = value.members[index].value
			continue
		}
		index, err := strconv.Atoi(key)
		if err != nil || index < 0 || index >= len(value.items) {
			return nil, ""
		}
		value = value.items[index]
	}
	return value, jsonPointerUnescape.Replace(segments[len(segments)-1])
}

func (v *jsonValue) memberIndex(name string) int {
	return slices.IndexFunc(v.members, func(member jsonMember) bool { return member.name == name })
}

func (v *jsonValue) entrySpan(index int) text.Span {
	if v.kind == "object" {
		entry := v.members[index]
		return text.Span{Start: entry.start, End: entry.value.end}
	}
	item := v.items[index]
	return text.Span{Start: item.start, End: item.end}
}

func jsonLineIndent(source string, at int) string {
	start := strings.LastIndexByte(source[:at], '\n') + 1
	end := start
	for end < at && (source[end] == ' ' || source[end] == '\t') {
		end++
	}
	return source[start:end]
}

func formatJSONMergeValue(value, prefix string, indent int) (string, error) {
	var out bytes.Buffer
	var err error
	if indent > 0 {
		err = json.Indent(&out, []byte(value), prefix, strings.Repeat(" ", indent))
	} else {
		err = json.Compact(&out, []byte(value))
	}
	return out.String(), err
}
