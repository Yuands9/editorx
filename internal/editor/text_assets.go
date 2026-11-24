package editor

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/vivaui/viva/text"
)

func (m *editorApp) projectRoot(path string) string {
	if m.workspace == nil {
		return ""
	}
	root := ""
	for _, project := range m.workspace.roots {
		if _, inside := aiRelativeTo(project.path, path); inside && len(project.path) > len(root) {
			root = project.path
		}
	}
	return root
}

func (m *editorApp) activeTextProject(tab *editorTab) string {
	if tab != nil && tab.doc.path != "" {
		return m.projectRoot(tab.doc.path)
	}
	if m.workspace != nil && len(m.workspace.roots) == 1 {
		return m.workspace.roots[0].path
	}
	return ""
}

func (m *editorApp) textAssetPath(project, name string) (string, error) {
	if project != "" {
		return filepath.Join(project, ".editorx", name), nil
	}
	if m.dataDirectory == "" {
		directory, err := editorDataDirectory()
		if err != nil {
			return "", err
		}
		m.dataDirectory = directory
	}
	return filepath.Join(m.dataDirectory, name), nil
}

func readTextAsset(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("文件超过 %d MiB：%s", limit>>20, filepath.Base(path))
	}
	return data, nil
}

func writeTextAsset(path string, data []byte, limit int64) error {
	if int64(len(data)) > limit {
		return fmt.Errorf("库文件超过 %d MiB", limit>>20)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return writeFile(path, data)
}

func placeholderName(name string) bool {
	return name != "" && strings.IndexFunc(name, func(value rune) bool {
		return !unicode.IsLetter(value) && !unicode.IsDigit(value) && !strings.ContainsRune("_.-", value)
	}) < 0
}

type textEditTarget struct {
	tab      *editorTab
	span     text.Range
	selected string
}

func captureTextTarget(tab *editorTab, requireSelection bool, limit int) (textEditTarget, error) {
	if !editableTextSource(tab) {
		return textEditTarget{}, errors.New("请先打开可编辑的文本源文件")
	}
	editor := tab.source.text
	if _, composing := editor.Composition(); composing {
		return textEditTarget{}, errors.New("请先完成中文选词")
	}
	if editor.SelectionCount() > 1 {
		return textEditTarget{}, errors.New("请使用单个选区")
	}
	span := editor.Selection()
	if requireSelection && span.Start == span.End {
		return textEditTarget{}, errors.New("请先选中一段文本")
	}
	start, end := editor.ByteOffset(span.Start), editor.ByteOffset(span.End)
	if end-start > limit {
		return textEditTarget{}, fmt.Errorf("选区超过 %d KiB，请缩小选区", limit>>10)
	}
	return textEditTarget{tab: tab, span: span, selected: strings.Clone(editor.CommittedString()[start:end])}, nil
}

func editableTextSource(tab *editorTab) bool {
	return aiTextTab(tab) && !tab.jsonCompare && !tab.source.readOnly &&
		(tab.format() != "Markdown" || tab.markdownMode != "预览") &&
		(tab.format() != "HTML" || tab.htmlMode != "预览")
}

func (target textEditTarget) current(tab *editorTab) (value string, start, end int, err error) {
	if target.tab == nil || target.tab != tab || !editableTextSource(tab) {
		return "", 0, 0, errors.New("请回到原来的文本标签并重新选择")
	}
	editor := tab.source.text
	if _, composing := editor.Composition(); composing {
		return "", 0, 0, errors.New("请先完成中文选词")
	}
	if editor.SelectionCount() > 1 || editor.Selection() != target.span {
		return "", 0, 0, errors.New("选区已变化，请重新选择")
	}
	value = editor.CommittedString()
	start, end = editor.ByteOffset(target.span.Start), editor.ByteOffset(target.span.End)
	if value[start:end] != target.selected {
		return "", 0, 0, errors.New("原文已变化，请重新选择")
	}
	return value, start, end, nil
}
