package editor

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
)

type jsonMember struct {
	name  string
	start int
	value *jsonValue
}
type jsonValue struct {
	start, end int
	kind       string
	scalar     string
	members    []jsonMember
	items      []*jsonValue
}

// 使用 token 保存键顺序、重复键和数字原文，排序不改变数组顺序或大整数精度。
func parseJSON(source string) (*jsonValue, error) {
	decoder := json.NewDecoder(strings.NewReader(source))
	decoder.UseNumber()
	value, err := readJSONValue(decoder, source, 0)
	if err != nil {
		return nil, err
	}
	if _, err = decoder.Token(); err != io.EOF {
		if err == nil {
			err = errors.New("JSON 根值后还有其他内容")
		}
		return nil, err
	}
	return value, nil
}

func readJSONValue(decoder *json.Decoder, source string, depth int) (*jsonValue, error) {
	if depth > 512 {
		return nil, errors.New("JSON 嵌套超过 512 层")
	}
	start := jsonTokenStart(source, int(decoder.InputOffset()))
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	value := &jsonValue{start: start}
	switch token := token.(type) {
	case json.Delim:
		if token == '{' {
			value.kind = "object"
			for decoder.More() {
				keyStart := jsonTokenStart(source, int(decoder.InputOffset()))
				key, err := decoder.Token()
				if err != nil {
					return nil, err
				}
				name, ok := key.(string)
				if !ok {
					return nil, errors.New("JSON 对象键必须是字符串")
				}
				child, err := readJSONValue(decoder, source, depth+1)
				if err != nil {
					return nil, err
				}
				value.members = append(value.members, jsonMember{name: name, start: keyStart, value: child})
			}
		} else if token == '[' {
			value.kind = "array"
			for decoder.More() {
				child, err := readJSONValue(decoder, source, depth+1)
				if err != nil {
					return nil, err
				}
				value.items = append(value.items, child)
			}
		} else {
			return nil, errors.New("JSON 括号不匹配")
		}
		_, err = decoder.Token()
		if err != nil {
			return nil, err
		}
	case string:
		value.kind, value.scalar = "string", token
	case json.Number:
		value.kind, value.scalar = "number", token.String()
	case bool:
		value.kind, value.scalar = "boolean", fmt.Sprint(token)
	case nil:
		value.kind = "null"
	default:
		return nil, errors.New("无法识别的 JSON 值")
	}
	value.end = int(decoder.InputOffset())
	return value, nil
}

func jsonTokenStart(source string, offset int) int {
	for offset < len(source) && strings.ContainsRune(" \r\n\t:,", rune(source[offset])) {
		offset++
	}
	return offset
}

func (v *jsonValue) sortKeys() {
	sort.SliceStable(v.members, func(i, j int) bool { return v.members[i].name < v.members[j].name })
	for _, member := range v.members {
		member.value.sortKeys()
	}
	for _, item := range v.items {
		item.sortKeys()
	}
}

func (v *jsonValue) writeJSON(out *bytes.Buffer) {
	switch v.kind {
	case "object":
		out.WriteByte('{')
		for i, member := range v.members {
			if i > 0 {
				out.WriteByte(',')
			}
			name, _ := json.Marshal(member.name)
			out.Write(name)
			out.WriteByte(':')
			member.value.writeJSON(out)
		}
		out.WriteByte('}')
	case "array":
		out.WriteByte('[')
		for i, item := range v.items {
			if i > 0 {
				out.WriteByte(',')
			}
			item.writeJSON(out)
		}
		out.WriteByte(']')
	case "string":
		value, _ := json.Marshal(v.scalar)
		out.Write(value)
	case "null":
		out.WriteString("null")
	default:
		out.WriteString(v.scalar)
	}
}

func (v *jsonValue) writeXML(encoder *xml.Encoder, name string, key *string) error {
	// XML 1.0 无法表达部分 JSON 控制字符；明确失败，避免编码器替换成其他字符。
	values := []string{v.scalar}
	if key != nil {
		values = append(values, *key)
	}
	for _, value := range values {
		for _, r := range value {
			if r != 9 && r != 10 && r != 13 && (r < 32 || r == 0xfffe || r == 0xffff) {
				return fmt.Errorf("XML 1.0 无法表示字符 U+%04X", r)
			}
		}
	}
	start := xml.StartElement{Name: xml.Name{Local: name}, Attr: []xml.Attr{{Name: xml.Name{Local: "type"}, Value: v.kind}}}
	if key != nil {
		start.Attr = append(start.Attr, xml.Attr{Name: xml.Name{Local: "name"}, Value: *key})
	}
	if err := encoder.EncodeToken(start); err != nil {
		return err
	}
	for _, member := range v.members {
		if err := member.value.writeXML(encoder, "member", &member.name); err != nil {
			return err
		}
	}
	for _, item := range v.items {
		if err := item.writeXML(encoder, "item", nil); err != nil {
			return err
		}
	}
	if v.kind != "object" && v.kind != "array" && v.kind != "null" {
		if err := encoder.EncodeToken(xml.CharData(v.scalar)); err != nil {
			return err
		}
	}
	return encoder.EncodeToken(start.End())
}

func transformJSON(source, action string, indent int) (string, string, error) {
	var out bytes.Buffer
	if action == "格式化" || action == "压缩" || action == "转义" || action == "Unicode 编码" {
		var err error
		if action != "格式化" {
			err = json.Compact(&out, []byte(source))
		} else {
			err = json.Indent(&out, []byte(source), "", strings.Repeat(" ", indent))
		}
		if err != nil && action == "转义" {
			repaired := repairEscapedJSONFields(source)
			if repaired != source {
				out.Reset()
				err = json.Compact(&out, []byte(repaired))
			}
		}
		if err != nil {
			return "", "JSON", err
		}
		if action == "转义" {
			value := out.String()
			out.Reset()
			encoder := json.NewEncoder(&out)
			encoder.SetEscapeHTML(false)
			if err := encoder.Encode(value); err != nil {
				return "", "JSON", err
			}
			return strings.TrimSuffix(out.String(), "\n"), "JSON", nil
		}
		if action == "Unicode 编码" {
			var encoded strings.Builder
			for _, r := range out.String() {
				if r <= 127 {
					encoded.WriteRune(r)
				} else if r <= 0xffff {
					fmt.Fprintf(&encoded, "\\u%04x", r)
				} else {
					hi, lo := utf16.EncodeRune(r)
					fmt.Fprintf(&encoded, "\\u%04x\\u%04x", hi, lo)
				}
			}
			return encoded.String(), "JSON", nil
		}
		return out.String(), "JSON", nil
	}
	if action == "去转义" {
		value, err := unescapeJSON(source, indent)
		return value, "JSON", err
	}
	value, err := parseJSON(source)
	if err != nil {
		return "", "", err
	}
	if action == "转 XML" {
		out.WriteString(xml.Header)
		encoder := xml.NewEncoder(&out)
		encoder.Indent("", strings.Repeat(" ", indent))
		if err := value.writeXML(encoder, "root", nil); err != nil {
			return "", "", err
		}
		if err := encoder.Flush(); err != nil {
			return "", "", err
		}
		return out.String(), "XML", nil
	}
	if action == "Form-Data" {
		if value.kind != "object" {
			return "", "", errors.New("Form-Data 需要 JSON 对象作为根值")
		}
		for _, member := range value.members {
			if strings.ContainsAny(member.name, ":\r\n") {
				return "", "", errors.New("Form-Data 批量文本的字段名不能包含冒号或换行")
			}
			var field bytes.Buffer
			if member.value.kind == "string" {
				if strings.ContainsAny(member.value.scalar, "\r\n") {
					return "", "", fmt.Errorf("字段 %q 含换行，不能直接转换为 Form-Data 批量文本", member.name)
				}
				field.WriteString(member.value.scalar)
			} else {
				member.value.writeJSON(&field)
			}
			fmt.Fprintf(&out, "%s:%s\n", member.name, field.String())
		}
		return out.String(), "Form-Data", nil
	}
	if action != "键排序" && action != "Unicode 解码" {
		return "", "", errors.New("未知 JSON 操作")
	}
	if action == "键排序" {
		value.sortKeys()
	}
	var compact bytes.Buffer
	value.writeJSON(&compact)
	if err := json.Indent(&out, compact.Bytes(), "", strings.Repeat(" ", indent)); err != nil {
		return "", "", err
	}
	return out.String(), "JSON", nil
}

func (m *editorApp) copyResult(t *editorTab) {
	t.updateResult()
	if t.resultError != "" {
		m.fail(errors.New(t.resultError))
		return
	}
	if err := app.SetClipboardText(t.result.text.CommittedString()); err != nil {
		m.fail(err)
		return
	}
	m.status = "已复制完整结果"
}

func (t *editorTab) jsonResultApplyLabel() string {
	if t.resultError != "" || t.resultKind == "" {
		return ""
	}
	if t.resultKind != "JSON" {
		return "在新标签打开结果"
	}
	if !t.resultDraft && t.jsonAction == "压缩" || t.result.text.CommittedString() == t.source.text.CommittedString() {
		return ""
	}
	return "应用到原文"
}

func (m *editorApp) applyResult(t *editorTab) {
	t.updateResult()
	if t.resultError != "" {
		m.fail(errors.New(t.resultError))
		return
	}
	if t.jsonComposing() {
		m.fail(errors.New("请先完成中文选词"))
		return
	}
	if t.resultKind != "JSON" {
		n := m.addTab(document{eol: "CRLF"}, "转换结果"+jsonResultExtension(t.resultKind))
		n.source.replace(t.result.text.CommittedString())
	} else {
		value := t.result.text.CommittedString()
		if value != t.source.text.CommittedString() {
			t.source.replace(value)
		}
		// 写回后恢复同步，避免对刚写入的 JSON 再转换一次。
		if !t.jsonLiveResult() || t.resultDraft {
			t.jsonAction, t.resultDraft, t.resultReady = "格式化", false, false
			t.resultAction, t.resultOutput = "", value
			t.updateResult()
			t.result.text.ClearHistory()
		}
		t.source.focus = true
	}
	m.status = "结果已放入编辑区，Ctrl+S 保存"
}

func (m *editorApp) exportResult(t *editorTab) {
	t.updateResult()
	if t.resultError != "" {
		m.fail(errors.New(t.resultError))
		return
	}
	n := m.addTab(document{eol: "CRLF"}, "处理结果"+jsonResultExtension(t.resultKind))
	n.source.replace(t.result.text.CommittedString())
	m.saveTextTab(n, true)
}

func jsonResultExtension(kind string) string {
	if kind == "XML" {
		return ".xml"
	}
	if kind == "Form-Data" {
		return ".txt"
	}
	return ".json"
}

func jsonErrorMessage(source string, err error) (string, int) {
	offset := -1
	var syntax *json.SyntaxError
	// Token 解码器的错误偏移可能只覆盖当前片段，用完整原文重新校验位置。
	var checked json.RawMessage
	if validationError := json.Unmarshal([]byte(source), &checked); validationError != nil {
		if errors.As(validationError, &syntax) {
			offset = min(len(source), max(0, int(syntax.Offset)-1))
			if strings.HasPrefix(syntax.Error(), "unexpected end") {
				offset = len(source)
			}
		} else if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			offset = len(source)
		}
	}
	if offset < 0 {
		return "处理失败：" + err.Error(), -1
	}
	line := strings.Count(source[:offset], "\n") + 1
	column := utf8.RuneCountInString(source[strings.LastIndex(source[:offset], "\n")+1:offset]) + 1
	return fmt.Sprintf("第 %d 行，第 %d 列：%s", line, column, err.Error()), offset
}
