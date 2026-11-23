package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
)

// 直接用 Viva 离屏渲染，不创建桌面窗口。
func TestRenderPreview(t *testing.T) {
	directory := os.Getenv("VIVA_PREVIEW_DIR")
	if directory == "" {
		t.Skip("preview is opt-in")
	}
	for _, sample := range []struct {
		name          string
		width, height int
		kind          string
	}{
		{"preview-light-json", 1440, 900, "JSON"},
		{"preview-light-md", 1100, 760, "Markdown"},
		{"preview-light-search", 900, 560, "JSONSearch"},
		{"preview-light-results", 1100, 760, "SearchResults"},
		{"preview-light-menu", 900, 560, "Menu"},
		{"preview-light-diff", 1440, 900, "JSONDiff"},
		{"preview-search-results", 1440, 900, "SearchResults"},
		{"preview-search-results-small", 900, 560, "SearchResults"},
		{"preview-search-json", 1440, 900, "JSONSearch"},
		{"preview-search-markdown", 1100, 760, "MarkdownSearch"},
		{"preview-search-small", 900, 560, "PlainSearch"},
		{"preview", 1440, 900, "JSON"},
		{"preview-compact", 1100, 760, "JSON"},
		{"preview-json-folded", 1100, 760, "JSONFolded"},
		{"preview-json-folded-array", 1100, 760, "JSONFoldArray"},
		{"preview-json-folded-root", 1100, 760, "JSONFoldRoot"},
		{"preview-json-diff", 1440, 900, "JSONDiff"},
		{"preview-json-diff-small", 900, 560, "JSONDiff"},
		{"preview-json-form", 1100, 760, "JSONForm"},
		{"preview-json-error", 1100, 760, "JSONError"},
		{"preview-json-hidden", 900, 560, "JSONHidden"},
		{"preview-json-minified", 1100, 760, "Compressed"},
		{"preview-markdown", 1440, 900, "Markdown"},
		{"preview-markdown-compact", 900, 560, "Markdown"},
		{"preview-empty", 1100, 720, ""},
		{"preview-focus", 900, 560, "Focus"},
		{"preview-menu", 1100, 720, "Menu"},
	} {
		t.Run(sample.name, func(t *testing.T) {
			m := newEditorApp()
			if strings.HasPrefix(sample.name, "preview-light") {
				m.setLightMode(true)
			}
			if sample.kind != "" {
				m.addProjects(examplesDirectory)
				m.load(filepath.Join(examplesDirectory, "配置.json"))
				jsonTab := m.active
				m.togglePin(jsonTab)
				m.load(filepath.Join(examplesDirectory, "使用指南.md"))
				if strings.HasPrefix(sample.kind, "JSON") || sample.kind == "Compressed" {
					m.activate(jsonTab)
					m.jsonAction(jsonTab, "键排序")
				}
				if m.showError {
					t.Fatal(m.errorText)
				}
			}
			if sample.kind == "Focus" {
				m.showFiles, m.showOutline = false, false
			}
			if sample.kind == "JSONDiff" {
				m.showFiles = false
				left, _, err := transformJSON(`{"name":"中文项目","server":{"port":8080,"enabled":true},"tags":["code","json"],"legacy":null}`, "格式化", 2)
				if err != nil {
					t.Fatal(err)
				}
				right, _, err := transformJSON(`{"name":"中文项目","server":{"port":9090,"enabled":"true"},"tags":["code","xml"],"timeout":30}`, "格式化", 2)
				if err != nil {
					t.Fatal(err)
				}
				m.active.source.replace(left)
				m.active.comparison.replace(right)
				m.active.jsonCompare = true
			}
			if sample.kind == "JSONForm" {
				m.jsonAction(m.active, "Form-Data")
			}
			if sample.kind == "JSONError" {
				m.active.source.replace("{\n  \"name\":\"中文配置\",\n  \"enabled\":truX\n}")
			}
			if sample.kind == "JSONHidden" {
				m.showFiles = false
			}
			if strings.HasPrefix(sample.kind, "JSONFold") {
				m.active.source.replace(`{"status":200,"msg":"操作成功","list":[{"name":"示例一","enabled":true},{"name":"示例二","enabled":false},{"name":"示例三","enabled":true}],"timeStamp":1786028229643}`)
				m.active.jsonIndent = 2
				m.jsonAction(m.active, "格式化")
				m.active.source.replace(m.active.result.text.String())
				m.active.updateResult()
				m.active.result.foldAll(true)
				if sample.kind != "JSONFoldRoot" {
					m.active.result.collapsed[0] = false
				}
				if sample.kind == "JSONFolded" {
					m.active.result.collapsed[m.active.result.folds[1].Header] = false
					m.active.result.collapsed[m.active.result.folds[4].Header] = false
				}
			}
			if strings.HasSuffix(sample.kind, "Search") {
				source := m.active.source.text.String()
				if sample.kind == "PlainSearch" {
					source = "# not-a-heading.txt\n\nserver = 中文配置\nport = 8080\n\nbackup = 中文配置\n"
				}
				m.newDocument()
				m.active.source.replace(source)
				if sample.kind == "PlainSearch" {
					m.active.name = "settings.txt"
				}
				m.showFiles, m.showOutline = false, false
			}
			if strings.HasPrefix(sample.name, "preview-light") {
				m.showFiles = false
			}
			page := ui.NewPage(ui.PageConfig{View: m.page, Theme: &m.theme})
			session, err := headless.Open(ui.Configure(page, app.Config{Title: applicationName, Width: sample.width, Height: sample.height}), nil, headless.Options{Scale: 1})
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			if strings.HasSuffix(sample.kind, "Search") {
				openSearchForTest(t, session, "中文")
			}
			if sample.kind == "SearchResults" {
				openSearchForTest(t, session, "JSON")
				if err := session.ClickLabel("查找全部已打开文档"); err != nil {
					t.Fatal(err)
				}
			}
			if sample.kind == "Compressed" {
				if err := session.ClickLabel("折叠"); err != nil {
					t.Fatal(err)
				}
				if err := session.ClickLabel("压缩"); err != nil {
					t.Fatal(err)
				}
			}
			if sample.kind == "Menu" {
				if err := session.ClickLabel("主菜单"); err != nil {
					t.Fatal(err)
				}
			}
			if sample.kind == "Menu" {
				if !m.fileMenu {
					t.Fatal("menu did not open")
				}
				if err := session.Advance(250 * time.Millisecond); err != nil {
					t.Fatal(err)
				}
				if !m.fileMenu {
					t.Fatal("menu closed unexpectedly")
				}
				if _, err := session.Locate("另存为…"); err != nil {
					t.Fatal(err)
				}
			}
			if err := session.Screenshot(filepath.Join(directory, sample.name+".png")); err != nil {
				t.Fatal(err)
			}
		})
	}
}
