package editor

import (
	"bytes"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

type jsonDifference struct {
	path, kind, before, after string
	left, right               text.Span
	arrayGap                  bool
}

func compareJSON(left, right string) ([]jsonDifference, error) {
	a, err := parseJSON(left)
	if err != nil {
		message, _ := jsonErrorMessage(left, err)
		return nil, fmt.Errorf("左侧 JSON：%s", message)
	}
	b, err := parseJSON(right)
	if err != nil {
		message, _ := jsonErrorMessage(right, err)
		return nil, fmt.Errorf("右侧 JSON：%s", message)
	}
	// 重复键没有唯一的字段路径，明确报错，避免把某个值静默覆盖后比较。
	if err := uniqueJSONKeys(a, ""); err != nil {
		return nil, fmt.Errorf("左侧 JSON：%w", err)
	}
	if err := uniqueJSONKeys(b, ""); err != nil {
		return nil, fmt.Errorf("右侧 JSON：%w", err)
	}
	var differences []jsonDifference
	compareJSONValues(a, b, "", &differences)
	return differences, nil
}

func uniqueJSONKeys(value *jsonValue, path string) error {
	seen := map[string]bool{}
	for _, member := range value.members {
		if seen[member.name] {
			return fmt.Errorf("路径 %s 存在重复键 %q，无法唯一对比", jsonDisplayPath(path), member.name)
		}
		seen[member.name] = true
		if err := uniqueJSONKeys(member.value, jsonChildPath(path, member.name)); err != nil {
			return err
		}
	}
	for i, item := range value.items {
		if err := uniqueJSONKeys(item, jsonChildPath(path, strconv.Itoa(i))); err != nil {
			return err
		}
	}
	return nil
}

func compareJSONValues(left, right *jsonValue, path string, differences *[]jsonDifference) {
	kind := ""
	if left == nil {
		kind = "新增"
	} else if right == nil {
		kind = "删除"
	} else if left.kind != right.kind {
		kind = "类型变化"
	} else if left.kind == "object" {
		leftValues, rightValues := map[string]*jsonValue{}, map[string]*jsonValue{}
		for _, member := range left.members {
			leftValues[member.name] = member.value
		}
		for _, member := range right.members {
			rightValues[member.name] = member.value
		}
		for _, member := range left.members {
			compareJSONValues(member.value, rightValues[member.name], jsonChildPath(path, member.name), differences)
		}
		for _, member := range right.members {
			if _, exists := leftValues[member.name]; !exists {
				compareJSONValues(nil, member.value, jsonChildPath(path, member.name), differences)
			}
		}
		return
	} else if left.kind == "array" {
		for i := 0; i < max(len(left.items), len(right.items)); i++ {
			var a, b *jsonValue
			if i < len(left.items) {
				a = left.items[i]
			}
			if i < len(right.items) {
				b = right.items[i]
			}
			compareJSONValues(a, b, jsonChildPath(path, strconv.Itoa(i)), differences)
			if (a == nil || b == nil) && i > min(len(left.items), len(right.items)) {
				// 数组按位置比较，补入第 i 项前，目标数组必须已经包含前 i 项。
				(*differences)[len(*differences)-1].arrayGap = true
			}
		}
		return
	} else if left.kind == "number" {
		if normalizedJSONNumber(left.scalar) == normalizedJSONNumber(right.scalar) {
			return
		}
		kind = "值变化"
	} else if left.scalar == right.scalar {
		return
	} else {
		kind = "值变化"
	}
	difference := jsonDifference{path: path, kind: kind, before: jsonValueText(left), after: jsonValueText(right)}
	if left != nil {
		difference.left = text.Span{Start: left.start, End: left.end}
	}
	if right != nil {
		difference.right = text.Span{Start: right.start, End: right.end}
	}
	*differences = append(*differences, difference)
}

// 规范化有效 JSON 数字的系数和指数，不转 float，也不展开巨大指数。
func normalizedJSONNumber(value string) string {
	coefficient, exponentText, _ := strings.Cut(strings.ToLower(value), "e")
	var exponent big.Int
	if exponentText != "" {
		exponent.SetString(exponentText, 10)
	}
	negative := strings.HasPrefix(coefficient, "-")
	coefficient = strings.TrimPrefix(coefficient, "-")
	fractional := 0
	if dot := strings.IndexByte(coefficient, '.'); dot >= 0 {
		fractional = len(coefficient) - dot - 1
		coefficient = coefficient[:dot] + coefficient[dot+1:]
	}
	coefficient = strings.TrimLeft(coefficient, "0")
	if coefficient == "" {
		return "0"
	}
	trimmed := strings.TrimRight(coefficient, "0")
	exponent.Add(&exponent, big.NewInt(int64(len(coefficient)-len(trimmed)-fractional)))
	if negative {
		trimmed = "-" + trimmed
	}
	return trimmed + "e" + exponent.String()
}

func jsonChildPath(path, key string) string {
	return path + "/" + strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
}

func jsonDisplayPath(path string) string {
	if path == "" {
		return "（根值）"
	}
	return path
}

func jsonValueText(value *jsonValue) string {
	if value == nil {
		return "（不存在）"
	}
	var out bytes.Buffer
	value.writeJSON(&out)
	return out.String()
}

func (t *editorTab) updateDiff() {
	left, right := t.source.text.String(), t.comparison.text.String()
	if t.diffReady && left == t.diffLeft && right == t.diffRight {
		return
	}
	t.diffReady, t.diffLeft, t.diffRight = true, left, right
	t.diffError, t.differences = "", nil
	t.source.view.Snapshot.MarkLayers, t.comparison.view.Snapshot.MarkLayers = nil, nil
	if strings.TrimSpace(right) == "" {
		return
	}
	var err error
	t.differences, err = compareJSON(left, right)
	if err != nil {
		t.diffError = err.Error()
		return
	}
	var leftSpans, rightSpans []text.Span
	for _, difference := range t.differences {
		if difference.left.End > difference.left.Start {
			leftSpans = append(leftSpans, difference.left)
		}
		if difference.right.End > difference.right.Start {
			rightSpans = append(rightSpans, difference.right)
		}
	}
	sort.Slice(leftSpans, func(i, j int) bool { return leftSpans[i].Start < leftSpans[j].Start })
	sort.Slice(rightSpans, func(i, j int) bool { return rightSpans[i].Start < rightSpans[j].Start })
	t.source.view.Snapshot.MarkLayers = []components.CodeEditorMarkLayer{{Spans: leftSpans, Color: gfx.RGBA(185, 80, 90, 0.25)}}
	t.comparison.view.Snapshot.MarkLayers = []components.CodeEditorMarkLayer{{Spans: rightSpans, Color: gfx.RGBA(80, 165, 110, 0.25)}}
}

func (m *editorApp) copyDiff(t *editorTab) {
	t.updateDiff()
	if t.diffError != "" || strings.TrimSpace(t.diffRight) == "" {
		return
	}
	var report strings.Builder
	fmt.Fprintf(&report, "JSON 对比：%d 处差异（左侧 → 右侧）\n", len(t.differences))
	for _, difference := range t.differences {
		fmt.Fprintf(&report, "\n%s %s\n左：%s\n右：%s\n", difference.kind, jsonDisplayPath(difference.path), difference.before, difference.after)
	}
	if err := app.SetClipboardText(report.String()); err != nil {
		m.fail(err)
		return
	}
	m.status = "已复制完整差异报告"
}

func (t *editorTab) locateDifference(difference jsonDifference) {
	pane, span := t.comparison, difference.right
	if span.End == span.Start {
		pane, span = t.source, difference.left
	}
	pane.foldAll(false)
	pane.text.SetSelection(text.Range{Start: pane.text.RuneIndex(span.Start), End: pane.text.RuneIndex(span.End)})
	t.source.focus, t.comparison.focus = pane == t.source, pane == t.comparison
}
