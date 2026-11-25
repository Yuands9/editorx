package editor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/vivaui/viva/text"
)

const (
	templateLibraryFile  = "templates.json"
	templateLibraryLimit = 4 << 20
	templateBodyLimit    = 128 << 10
	templateFieldLimit   = 20
)

type templateField struct {
	Name    string `json:"name"`
	Default string `json:"default,omitempty"`
}

type documentTemplate struct {
	Name     string          `json:"name"`
	Filename string          `json:"filename"`
	EOL      string          `json:"eol"`
	BOM      bool            `json:"bom,omitempty"`
	Body     string          `json:"body"`
	Fields   []templateField `json:"fields,omitempty"`
}

func builtInTemplates() []documentTemplate {
	return []documentTemplate{
		{Name: "发布说明", Filename: "release-notes.md", EOL: "LF",
			Body:   "# {{版本}} 发布说明\n\n## 更新内容\n- {{改动}}\n\n## 验证\n- [ ] \n",
			Fields: []templateField{{Name: "版本"}, {Name: "改动"}}},
		{Name: "会议纪要", Filename: "meeting-notes.md", EOL: "LF",
			Body: "# {{主题}} · 会议纪要\n\n## 结论\n- \n\n## 待办\n- [ ] \n", Fields: []templateField{{Name: "主题"}}},
		{Name: "接口响应示例", Filename: "response.json", EOL: "LF",
			Body:   "{\n    \"code\": 0,\n    \"message\": \"{{消息}}\",\n    \"data\": {}\n}\n",
			Fields: []templateField{{Name: "消息", Default: "success"}}},
		{Name: "每周工作汇报", Filename: "weekly-report.md", EOL: "LF",
			Body:   "# {{日期}} 工作周报\n\n## 本周完成\n- \n\n## 下周计划\n- [ ] \n\n## 风险与协助\n- 无\n",
			Fields: []templateField{{Name: "日期", Default: "本周"}}},
		{Name: "问题排查记录", Filename: "incident-notes.md", EOL: "LF",
			Body:   "# {{问题}}\n\n## 现象与影响\n\n## 复现步骤\n1. \n\n## 原因与处理\n\n## 验证结果\n- [ ] 已验证\n",
			Fields: []templateField{{Name: "问题", Default: "问题标题"}}},
	}
}

func validWindowsEntryName(name string) bool {
	return name != "" && len([]rune(name)) <= 255 && filepath.Base(name) == name && filepath.IsLocal(name) &&
		!strings.HasSuffix(name, ".") && !strings.HasSuffix(name, " ") &&
		!strings.ContainsAny(name, "\x3c\x3e:\"/\\|?*") && strings.IndexFunc(name, unicode.IsControl) < 0
}

func validateTextFilename(name string) error {
	if !validWindowsEntryName(name) {
		return errors.New("请输入有效的 Windows 文件名")
	}
	if hasNonTextExtension(name) {
		return errors.New("请选择可编辑的文本文件名")
	}
	return nil
}

func hasNonTextExtension(name string) bool {
	extension := filepath.Ext(name)
	return isImageFile(name) || previewFormat(name) != "" || officeKind(name) != "" ||
		strings.EqualFold(extension, ".doc") || strings.EqualFold(extension, ".xls")
}

func validateTemplate(template documentTemplate) error {
	if strings.TrimSpace(template.Name) == "" || len([]rune(template.Name)) > 80 {
		return errors.New("请输入不超过 80 个字符的模板名称")
	}
	if err := validateTextFilename(template.Filename); err != nil {
		return err
	}
	if template.EOL != "CRLF" && template.EOL != "LF" && template.EOL != "混合" {
		return errors.New("模板行尾格式无效")
	}
	if template.Body == "" || len(template.Body) > templateBodyLimit || strings.ContainsRune(template.Body, 0) {
		return errors.New("请输入不超过 128 KiB 的文本正文")
	}
	if len(template.Fields) > templateFieldLimit {
		return fmt.Errorf("每个模板最多支持 %d 个变量", templateFieldLimit)
	}
	seen := make(map[string]bool, len(template.Fields))
	for _, field := range template.Fields {
		if !placeholderName(field.Name) || seen[field.Name] || len([]rune(field.Name)) > 24 ||
			!strings.Contains(template.Body, "{{"+field.Name+"}}") || len(field.Default) > templateBodyLimit ||
			strings.ContainsRune(field.Default, 0) {
			return fmt.Errorf("模板变量无效：%s", field.Name)
		}
		seen[field.Name] = true
	}
	return nil
}

func loadTemplates(path string) ([]documentTemplate, error) {
	data, err := readTextAsset(path, templateLibraryLimit)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var templates []documentTemplate
	if err := json.Unmarshal(data, &templates); err != nil {
		return nil, err
	}
	for _, template := range templates {
		if err := validateTemplate(template); err != nil {
			return nil, fmt.Errorf("%s：%w", template.Name, err)
		}
	}
	return templates, nil
}

func writeTemplate(path string, template documentTemplate, original *documentTemplate) error {
	if err := validateTemplate(template); err != nil {
		return err
	}
	templates, err := loadTemplates(path)
	if err != nil {
		return err
	}
	editIndex := -1
	for i, item := range templates {
		if original != nil && item.Name == original.Name {
			if item.Filename != original.Filename || item.EOL != original.EOL || item.BOM != original.BOM ||
				item.Body != original.Body || !slices.Equal(item.Fields, original.Fields) {
				return errors.New("模板已有其他修改，请重新打开后编辑")
			}
			editIndex = i
			continue
		}
		if strings.EqualFold(item.Name, template.Name) {
			return errors.New("这个范围下已有同名模板，请换一个名称")
		}
	}
	if original != nil && editIndex < 0 {
		return errors.New("原模板已被移除或更名，请重新打开模板库")
	}
	if editIndex >= 0 {
		templates[editIndex] = template
	} else {
		templates = append(templates, template)
	}
	data, err := json.MarshalIndent(templates, "", "  ")
	if err != nil {
		return err
	}
	return writeTextAsset(path, append(data, '\n'), templateLibraryLimit)
}

func renderTemplate(template documentTemplate, filename string, values []string, fragment bool) (string, error) {
	if len(values) != len(template.Fields) {
		return "", errors.New("模板变量数量不匹配")
	}
	jsonDocument := documentFormat(filename) == "JSON"
	pairs := make([]string, 0, 2*len(values))
	expandedSize := len(template.Body)
	var problem error
	for i, field := range template.Fields {
		value := values[i]
		if strings.TrimSpace(value) == "" && problem == nil {
			problem = fmt.Errorf("请填写变量：%s", field.Name)
		}
		if len(value) > templateBodyLimit || strings.ContainsRune(value, 0) {
			return "", fmt.Errorf("变量过长或含有空字符：%s", field.Name)
		}
		if jsonDocument {
			encoded, _ := json.Marshal(value)
			value = string(encoded[1 : len(encoded)-1])
		}
		marker := "{{" + field.Name + "}}"
		if value == "" && !jsonDocument {
			value = marker
		}
		expandedSize += strings.Count(template.Body, marker) * (len(value) - len(marker))
		pairs = append(pairs, marker, value)
	}
	if expandedSize > templateBodyLimit {
		return "", errors.New("生成的正文超过 128 KiB")
	}
	value := strings.NewReplacer(pairs...).Replace(template.Body)
	if problem == nil && jsonDocument && !fragment && !json.Valid([]byte(value)) {
		problem = errors.New("生成内容不是有效的 JSON，请检查模板正文和变量位置")
	}
	return value, problem
}

func (m *editorApp) createTextDraft(filename, body string, doc document) *editorTab {
	tab := m.addTab(doc, filename)
	if body != "" {
		tab.source.text.ReplaceRange(text.Range{}, body)
		tab.source.text.MoveTo(0, false)
	}
	tab.applyDefaultWrap(m.editorPrefs)
	m.queueHTMLPreview(tab)
	m.status = "新文档"
	return tab
}

func (m *editorApp) createTextFile(directory, filename, body string, doc document) error {
	if !validWindowsEntryName(filename) {
		return errors.New("请输入有效的 Windows 文件名")
	}
	path := filepath.Join(directory, filename)
	if m.tabAtPath(path) != nil {
		return errors.New("同一路径已在标签中打开，请换一个名称")
	}
	if err := doc.create(path, body); err != nil {
		if errors.Is(err, os.ErrExist) {
			return errors.New("目录中已有同名文件，请换一个名称")
		}
		return fmt.Errorf("无法新建 %s：%w", filename, err)
	}
	if root := m.projectRoot(directory); root != "" {
		m.refreshProject(m.workspace.project(root), path)
	}
	if hasNonTextExtension(filename) {
		m.status = "已创建文件 " + filename
		return nil
	}
	m.load(path)
	return nil
}
