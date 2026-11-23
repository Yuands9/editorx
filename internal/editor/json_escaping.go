package editor

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

func formatJSONPaste(value string, indent int) string {
	source := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "\ufeff"))
	if !isJSONContainer(source) {
		return value
	}
	formatted, _, err := transformJSON(source, "格式化", indent)
	if err != nil {
		repaired := repairEscapedJSONFields(source)
		if repaired == source {
			return value
		}
		formatted, _, err = transformJSON(repaired, "格式化", indent)
	}
	if err != nil {
		return value
	}
	return formatted
}

func isJSONContainer(source string) bool {
	return source != "" && (source[0] == '{' || source[0] == '[')
}

func unescapeJSON(source string, indent int) (string, error) {
	source = strings.TrimSpace(source)
	value, err := parseJSON(source)
	if err != nil {
		repaired := repairEscapedJSONFields(source)
		if repaired != source {
			if parsed, repairErr := parseJSON(repaired); repairErr == nil {
				source, value, err = repaired, parsed, nil
			}
		}
	}
	if err != nil {
		// Some logs and clipboard tools omit the enclosing string quotes.
		var decoded string
		if decodeErr := json.Unmarshal([]byte("\""+source+"\""), &decoded); decodeErr != nil {
			return "", fmt.Errorf("需要 JSON 对象、数组，或包含 JSON 的转义文本：%w", err)
		}
		return formatUnescapedJSON(decoded, indent)
	}
	switch value.kind {
	case "string":
		// Preserve the inverse of 转义, including string fields in the original.
		return formatUnescapedJSON(value.scalar, indent)
	case "object", "array":
		if expandJSONStringFields(value, 0) {
			var out bytes.Buffer
			value.writeJSON(&out)
			source = out.String()
		}
		formatted, _, err := transformJSON(source, "格式化", indent)
		return formatted, err
	default:
		return "", errors.New("需要 JSON 对象、数组，或包含 JSON 的转义文本")
	}
}

func formatUnescapedJSON(source string, indent int) (string, error) {
	value, _, err := transformJSON(source, "格式化", indent)
	if err == nil {
		return value, nil
	}
	if repaired := repairEscapedJSONFields(source); repaired != source {
		if value, _, err := transformJSON(repaired, "格式化", indent); err == nil {
			return value, nil
		}
	}
	return "", fmt.Errorf("去转义后不是有效 JSON：%w", err)
}

func expandJSONStringFields(value *jsonValue, layers int) bool {
	if value.kind == "string" {
		content := strings.TrimSpace(value.scalar)
		if layers >= 16 || !isJSONContainer(content) {
			return false
		}
		nested, err := parseJSON(content)
		if err != nil {
			return false
		}
		*value = *nested
		expandJSONStringFields(value, layers+1)
		return true
	}

	changed := false
	for _, member := range value.members {
		if expandJSONStringFields(member.value, layers) {
			changed = true
		}
	}
	for _, item := range value.items {
		if expandJSONStringFields(item, layers) {
			changed = true
		}
	}
	return changed
}

// Some log exports add one backslash before the quotes in an embedded JSON
// object. Only repair strings that start with this recognizable container form.
func repairEscapedJSONFields(source string) string {
	if !strings.Contains(source, `\\"`) {
		return source
	}
	var repaired strings.Builder
	last := 0
	for offset := 0; offset < len(source); {
		quote := strings.IndexByte(source[offset:], '"')
		if quote < 0 {
			break
		}
		opening := offset + quote
		offset = opening + 1
		content := strings.TrimLeft(source[offset:], " \t\r\n")
		firstValue := strings.TrimLeft(content, " \t\r\n{[")
		extraQuoteLayer := isJSONContainer(content) && strings.HasPrefix(firstValue, `\\"`)
		var field strings.Builder
		segment := opening

		for offset < len(source) {
			if source[offset] == '"' {
				offset++
				break
			}
			if source[offset] != '\\' {
				offset++
				continue
			}
			slashes := offset
			for offset < len(source) && source[offset] == '\\' {
				offset++
			}
			if offset == len(source) || source[offset] != '"' {
				continue
			}
			if (offset-slashes)%2 == 0 {
				if !extraQuoteLayer {
					offset++
					break
				}
				field.WriteString(source[segment : offset-1])
				segment = offset
			}
			offset++
		}
		if segment != opening {
			field.WriteString(source[segment:offset])
			var decoded string
			if json.Unmarshal([]byte(field.String()), &decoded) == nil && json.Valid([]byte(decoded)) {
				repaired.WriteString(source[last:opening])
				repaired.WriteString(field.String())
				last = offset
			}
		}
	}
	if last == 0 {
		return source
	}
	repaired.WriteString(source[last:])
	return repaired.String()
}
