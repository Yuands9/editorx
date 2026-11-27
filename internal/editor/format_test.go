package editor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestDBXDebugLogFormat(t *testing.T) {
	const source = "DBX debug log\r\nExported: 2026-11-27T23:49:41+08:00\r\nPlatform: Win32\r\n\r\n" +
		"[2026-11-27T19:37:39.486+08:00] [DEBUG] [DBX][api:success] request to https://example.test **operation**\r\n"
	m := newTestEditorWithDocument()
	m.active.source.replace(source)
	if got := m.active.format(); got != "日志" {
		t.Fatalf("pasted DBX log detected as %s", got)
	}
	for _, test := range []struct{ name, source, want string }{
		{"debug.log", source, ""},
		{"debug.txt", source, "日志"},
		{"debug.md", source, ""},
		{"example.txt", "# 日志示例\n\n\x60\x60\x60text\n" + source + "\x60\x60\x60", "Markdown"},
	} {
		if got := suggestOpenedFormat(test.name, test.source); got != test.want {
			t.Fatalf("%s: got %q, want %q", test.name, got, test.want)
		}
	}
}

func TestSuggestOpenedFormat(t *testing.T) {
	for _, test := range []struct {
		name, source, want string
	}{
		{"object.txt", "  {\n\"内容\": [1, 2]\n}", "JSON"},
		{"array", `[{"a": 1}]`, "JSON"},
		{"wrong.md", `{"a": 1}`, "JSON"},
		{"native.JSON", `["**bold**"]`, ""},
		{"partial.txt", `{"a": 1`, ""},
		{"scalar.txt", "123", ""},
		{"readme", "# 标题\n\n正文", "Markdown"},
		{"wrong.json", "## 标题\n\n正文", "Markdown"},
		{"notes.txt", "```go\nprintln(1)\n```", "Markdown"},
		{"tasks.txt", "- 第一项\n- 第二项", "Markdown"},
		{"inline.txt", "内容含有 **加粗文本**", "Markdown"},
		{"link.txt", "[主页](https://example.test)", "Markdown"},
		{"native.Markdown", "# 标题", ""},
		{"config.yaml", "# 配置\nvalue: true", ""},
		{"script", "#!/bin/sh\n# 注释\necho hello", ""},
		{"plain.txt", "普通正文 https://example.test", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := suggestOpenedFormat(test.name, test.source); got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestOpenTextFormatPrompt(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "数据.txt")
	markdownPath := filepath.Join(dir, "无扩展名")
	droppedPath := filepath.Join(dir, "保留.txt")
	const jsonSource = `{"name":"样例"}`
	const markdownSource = "# Markdown 标题\n\n正文"
	for path, source := range map[string]string{jsonPath: jsonSource, markdownPath: markdownSource, droppedPath: markdownSource} {
		if err := os.WriteFile(path, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}

	m := newEditorApp()
	session := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(session.Driver().QueueOpenFiles([]string{jsonPath, markdownPath}, nil))
	check(session.ClickLabel("button:打开文件"))
	assertChoice := func(tab *editorTab, path, source, format string) {
		t.Helper()
		if tab == nil || tab.doc.path != path || tab.source.text.CommittedString() != source || tab.format() != format || tab.modified() {
			t.Fatalf("opening %q did not preserve the document in %s", path, format)
		}
		data, err := os.ReadFile(path)
		if err != nil || string(data) != source {
			t.Fatalf("opening %q changed the file: %v", path, err)
		}
	}

	if m.active == nil || m.active.format() != "纯文本" || !session.HasText("检测到文件格式") {
		t.Fatal("opening Markdown with no extension did not offer a format choice")
	}
	check(session.ClickLabel("button:以 Markdown 展示"))
	assertChoice(m.active, markdownPath, markdownSource, "Markdown")
	if m.active.markdownMode != "分屏" || !session.HasText("Markdown 标题") {
		t.Fatal("Markdown format did not open in split view")
	}

	m.activate(m.tabAtPath(jsonPath))
	check(session.Frame())
	check(session.ClickLabel("button:以 JSON 展示"))
	assertChoice(m.active, jsonPath, jsonSource, "JSON")
	_, err := session.Locate("textfield:JSON 处理结果")
	check(err)

	dropped, err := session.Drop(gfx.FileDropEvent{Phase: gfx.FileDropDrop, Paths: []string{droppedPath}})
	check(err)
	if !dropped {
		t.Fatal("file drop was rejected")
	}
	check(session.Frame())
	check(session.ClickLabel("button:保留原格式"))
	assertChoice(m.active, droppedPath, markdownSource, "纯文本")
	m.load(droppedPath)
	check(session.Frame())
	if len(m.tabs) != 3 || session.HasText("检测到文件格式") {
		t.Fatal("reopening an existing tab repeated the format prompt")
	}

	m.closeTab(m.active)
	m.load(droppedPath)
	check(session.Frame())
	check(session.Driver().Key(gfx.KeyEscape, 0))
	check(session.Frame())
	assertChoice(m.active, droppedPath, markdownSource, "纯文本")
	if session.HasText("检测到文件格式") {
		t.Fatal("Escape did not dismiss the format prompt")
	}
}
