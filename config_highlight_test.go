package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

type expectedSyntaxRun struct {
	value, kind string
}

func checkSyntaxRuns(t *testing.T, line string, runs []components.CodeEditorRun, expected []expectedSyntaxRun, light bool) {
	t.Helper()
	if len(runs) != len(expected) {
		t.Fatalf("%q has %d runs, want %d: %+v", line, len(runs), len(expected), runs)
	}
	palette := syntaxColors(light)
	colors := map[string]gfx.Color{
		"key": palette.key, "value": palette.value, "number": palette.number,
		"literal": palette.literal, "comment": palette.comment, "keyword": palette.keyword,
		"symbol": palette.symbol, "punctuation": palette.punctuation, "alert": palette.alert,
	}
	previousEnd := 0
	for index, run := range runs {
		want := expected[index]
		if run.Start < previousEnd || run.End > len(line) || run.Start >= run.End {
			t.Fatalf("invalid highlighting span in %q: %+v", line, run)
		}
		value := line[run.Start:run.End]
		if !utf8.ValidString(value) || value != want.value || run.Color != colors[want.kind] || run.Italic != (want.kind == "comment") {
			t.Fatalf("highlighted %q with %+v in %q, want %q as %s", value, run.Color, line, want.value, want.kind)
		}
		previousEnd = run.End
	}
}

func TestConfigHighlightThroughEditor(t *testing.T) {
	for _, sample := range []struct {
		name, format string
		lines        []string
		expected     map[int][]expectedSyntaxRun
	}{
		{
			name: "服务.xml", format: "XML",
			lines: []string{
				"<?xml version=\"1.0\"?>",
				"<配置 环境=\"本机\">",
				"  <服务",
				"      名称=\"演示\" port=\"8080\">正常 &amp; 就绪</服务>",
				"  <!-- 说明",
				"     <ignored/> -->",
				"  <计数>42</计数>",
				"  <说明><![CDATA[键: 值",
				"  <literal>不是标签</literal>]]></说明>",
				"</配置>",
			},
			expected: map[int][]expectedSyntaxRun{
				0: {{"xml", "symbol"}, {"version", "key"}, {"\"1.0\"", "value"}},
				1: {{"配置", "symbol"}, {"环境", "key"}, {"\"本机\"", "value"}},
				2: {{"服务", "symbol"}},
				3: {{"名称", "key"}, {"\"演示\"", "value"}, {"port", "key"}, {"\"8080\"", "value"}, {"正常 ", "value"}, {"&amp;", "literal"}, {" 就绪", "value"}, {"服务", "symbol"}},
				4: {{"<!-- 说明", "comment"}},
				5: {{"     <ignored/> -->", "comment"}},
				6: {{"计数", "symbol"}, {"42", "number"}, {"计数", "symbol"}},
				7: {{"说明", "symbol"}, {"<![CDATA[键: 值", "value"}},
				8: {{"  <literal>不是标签</literal>]]>", "value"}, {"说明", "symbol"}},
			},
		},
		{
			name: "页面.html", format: "HTML",
			lines: []string{
				"<!doctype html>",
				"<a id=\"链接\" href=https://example.test/a/b?q=one&n=1 target='_blank'>2 < 3</a>",
				"<!-- 说明 <div title='示例'>",
				"     第二行 -->",
				"<input data-help=\"第一行",
				"第二行 > 正文\" disabled value='你好'>",
				"<SCRIPT type=\"module\">",
				"if (a < b) console.log(\"</scripted><p>\");",
				"</sCrIPt><style>",
				".note::after { content: \"<section data-key='例子'>\"; }",
				"</style><div lang=zh-CN>文字</div>",
				"<textarea>这是 <b>字面文字</textarea>",
			},
			expected: map[int][]expectedSyntaxRun{
				0: {{"<!doctype html>", "keyword"}},
				1: {{"a", "symbol"}, {"id", "key"}, {"\"链接\"", "value"}, {"href", "key"}, {"https://example.test/a/b?q=one&n=1", "value"}, {"target", "key"}, {"'_blank'", "value"}, {"2 < 3", "value"}, {"a", "symbol"}},
				2: {{"<!-- 说明 <div title='示例'>", "comment"}},
				3: {{"     第二行 -->", "comment"}},
				4: {{"input", "symbol"}, {"data-help", "key"}, {"\"第一行", "value"}},
				5: {{"第二行 > 正文\"", "value"}, {"disabled", "key"}, {"value", "key"}, {"'你好'", "value"}},
				6: {{"SCRIPT", "symbol"}, {"type", "key"}, {"\"module\"", "value"}},
				7: {{"if", "keyword"}, {"(", "punctuation"}, {")", "punctuation"}, {"console", "symbol"},
					{"log", "symbol"}, {"(", "punctuation"}, {"\"</scripted><p>\"", "value"}, {")", "punctuation"}},
				8: {{"sCrIPt", "symbol"}, {"style", "symbol"}},
				9: {{"note", "symbol"}, {"after", "symbol"}, {"{", "punctuation"}, {"content", "key"},
					{"\"<section data-key='例子'>\"", "value"}, {"}", "punctuation"}},
				10: {{"style", "symbol"}, {"div", "symbol"}, {"lang", "key"}, {"zh-CN", "value"}, {"文字", "value"}, {"div", "symbol"}},
				11: {{"textarea", "symbol"}, {"这是 <b>字面文字", "value"}, {"textarea", "symbol"}},
			},
		},
		{
			name: "应用.YML", format: "YAML",
			lines: []string{
				"# 开发环境",
				"server:",
				"  url: https://host:8443/path#frag # 接口",
				"  enabled: true",
				"  jobs:",
				"    - \"任务:名\": '文字 # 保留'",
				"      startup: |-",
				"        echo \"字面\": # 仍是正文",
				"      retries: 3",
				"  options: {\"代理\":false, timeout: 2.5, items: [1, 'a#b', null]}",
				"  message: \"多行",
				"    字符串\" # 注",
			},
			expected: map[int][]expectedSyntaxRun{
				0: {{"# 开发环境", "comment"}},
				1: {{"server", "key"}},
				2: {{"url", "key"}, {"https://host:8443/path#frag", "value"}, {"# 接口", "comment"}},
				3: {{"enabled", "key"}, {"true", "literal"}},
				5: {{"\"任务:名\"", "key"}, {"'文字 # 保留'", "value"}},
				6: {{"startup", "key"}, {"|-", "value"}},
				7: {{"echo \"字面\": # 仍是正文", "value"}},
				8: {{"retries", "key"}, {"3", "number"}},
				9: {{"options", "key"}, {"\"代理\"", "key"}, {"false", "literal"}, {"timeout", "key"}, {"2.5", "number"},
					{"items", "key"}, {"1", "number"}, {"'a#b'", "value"}, {"null", "literal"}},
				10: {{"message", "key"}, {"\"多行", "value"}},
				11: {{"    字符串\"", "value"}, {"# 注", "comment"}},
			},
		},
		{
			name: "服务.yaml", format: "YAML", lines: []string{"port: 8080"},
			expected: map[int][]expectedSyntaxRun{0: {{"port", "key"}, {"8080", "number"}}},
		},
		{
			name: "应用.properties", format: "Properties",
			lines: []string{
				"# 本地配置",
				"! 独立注释\\",
				"区域\\ 名称\\:=华南",
				"server.port : 8080",
				"enabled true",
				"nodes=host-a,\\",
				"    alias=备用\\",
				"    # 仍是值",
				"continued\\",
				"  .key=3",
				"path=C:\\\\",
				"note=value#行内数据",
			},
			expected: map[int][]expectedSyntaxRun{
				0:  {{"# 本地配置", "comment"}},
				1:  {{"! 独立注释\\", "comment"}},
				2:  {{"区域\\ 名称\\:", "key"}, {"华南", "value"}},
				3:  {{"server.port", "key"}, {"8080", "number"}},
				4:  {{"enabled", "key"}, {"true", "literal"}},
				5:  {{"nodes", "key"}, {"host-a,\\", "value"}},
				6:  {{"alias=备用\\", "value"}},
				7:  {{"# 仍是值", "value"}},
				8:  {{"continued\\", "key"}},
				9:  {{".key", "key"}, {"3", "number"}},
				10: {{"path", "key"}, {"C:\\\\", "value"}},
				11: {{"note", "key"}, {"value#行内数据", "value"}},
			},
		},
		{
			name: "服务.ini", format: "INI",
			lines: []string{"; 本地配置", "[server] ; 连接", "  host = \"本地;网络#\"", "port : 8080 ; 端口", "enabled=true", "url=https://host/path#frag"},
			expected: map[int][]expectedSyntaxRun{
				0: {{"; 本地配置", "comment"}},
				1: {{"server", "symbol"}, {"; 连接", "comment"}},
				2: {{"host", "key"}, {"\"本地;网络#\"", "value"}},
				3: {{"port", "key"}, {"8080", "number"}, {"; 端口", "comment"}},
				4: {{"enabled", "key"}, {"true", "literal"}},
				5: {{"url", "key"}, {"https://host/path#frag", "value"}},
			},
		},
		{
			name: "服务.toml", format: "TOML",
			lines: []string{
				"[server.\"本地\"] # 说明",
				"port = 8080",
				"metadata = { \"zone=name\" = \"华南\", ready = true }",
				"path = 'D:\\code\\repo'",
				"message = \"\"\"部署",
				"port = 7777 # 属于正文",
				"完成\"\"\"",
				"[[jobs]]",
				"enabled = false",
				"tolerance = +inf",
			},
			expected: map[int][]expectedSyntaxRun{
				0: {{"server.\"本地\"", "symbol"}, {"# 说明", "comment"}},
				1: {{"port", "key"}, {"8080", "number"}},
				2: {{"metadata", "key"}, {"\"zone=name\"", "key"}, {"\"华南\"", "value"}, {"ready", "key"}, {"true", "literal"}},
				3: {{"path", "key"}, {"'D:\\code\\repo'", "value"}},
				4: {{"message", "key"}, {"\"\"\"部署", "value"}},
				5: {{"port = 7777 # 属于正文", "value"}},
				6: {{"完成\"\"\"", "value"}},
				7: {{"jobs", "symbol"}},
				8: {{"enabled", "key"}, {"false", "literal"}},
				9: {{"tolerance", "key"}, {"+inf", "number"}},
			},
		},
		{
			name: ".env.local", format: "ENV",
			lines: []string{
				"# 开发环境", "export PORT=8080 # 端口", "DEBUG=true", "NAME=\"研发#内网\" # 说明",
				"LINES=\"第一行", "# 属于值", "最后一行\"", "ORIGIN=https://example.test#备注",
			},
			expected: map[int][]expectedSyntaxRun{
				0: {{"# 开发环境", "comment"}},
				1: {{"export", "keyword"}, {"PORT", "key"}, {"8080", "number"}, {"# 端口", "comment"}},
				2: {{"DEBUG", "key"}, {"true", "literal"}},
				3: {{"NAME", "key"}, {"\"研发#内网\"", "value"}, {"# 说明", "comment"}},
				4: {{"LINES", "key"}, {"\"第一行", "value"}},
				5: {{"# 属于值", "value"}},
				6: {{"最后一行\"", "value"}},
				7: {{"ORIGIN", "key"}, {"https://example.test", "value"}, {"#备注", "comment"}},
			},
		},
		{
			name: ".env", format: "ENV", lines: []string{"APP_NAME=Viva Editor"},
			expected: map[int][]expectedSyntaxRun{0: {{"APP_NAME", "key"}, {"Viva Editor", "value"}}},
		},
		{
			name: "开发说明.md", format: "Markdown",
			lines: []string{
				"---",
				"title: 本地开发",
				"enabled: true",
				"---",
				"# 开发环境",
				"地址: https://example.test:8443",
				"- **端口**: 8080",
				"1. **工作目录:** 临时文件",
				"   - [x] `环境`：本机",
				"> 状态：true",
				"访问说明见 https://example.test/",
				"https://example.test:8443",
				"12:30 休息",
				"[文档](https://example.test)",
				"````text",
				"status: raw",
				"```",
				"状态：普通代码",
				"````",
				"    token: indented",
				"> ~~~",
				"> raw: value",
				"外部: 已恢复",
				"---",
				"负责人：测试",
				"引用 `APP_ENV` 和 **部署说明**",
				"> ```yaml",
				"> name: 本机",
				"> ```",
				"```java",
				"class App {}",
				"```",
				"```json",
				"{\"ready\":true}",
				"```",
			},
			expected: map[int][]expectedSyntaxRun{
				0: {{"---", "punctuation"}}, 1: {{"title", "key"}, {"本地开发", "value"}}, 2: {{"enabled", "key"}, {"true", "literal"}},
				3: {{"---", "punctuation"}}, 4: {{"#", "punctuation"}, {"开发环境", "symbol"}}, 5: {{"地址", "key"}, {"https://example.test:8443", "value"}},
				6: {{"端口", "key"}, {"8080", "number"}}, 7: {{"工作目录", "key"}, {"临时文件", "value"}},
				8: {{"环境", "key"}, {"本机", "value"}}, 9: {{"状态", "key"}, {"true", "literal"}},
				10: {{"https://example.test/", "symbol"}}, 11: {{"https://example.test:8443", "symbol"}}, 12: nil,
				13: {{"[文档]", "symbol"}, {"https://example.test", "value"}},
				14: {{"````", "punctuation"}, {"text", "keyword"}}, 15: nil, 16: nil, 17: nil,
				18: {{"````", "punctuation"}}, 19: {{"token: indented", "value"}},
				20: {{"~~~", "punctuation"}}, 21: nil, 22: {{"外部", "key"}, {"已恢复", "value"}},
				23: {{"---", "punctuation"}}, 24: {{"负责人", "key"}, {"测试", "value"}},
				25: {{"`APP_ENV`", "value"}, {"**部署说明**", "keyword"}},
				26: {{"```", "punctuation"}, {"yaml", "keyword"}}, 27: {{"name", "key"}, {"本机", "value"}}, 28: {{"```", "punctuation"}},
				29: {{"```", "punctuation"}, {"java", "keyword"}},
				30: {{"class", "keyword"}, {"App", "symbol"}, {"{", "punctuation"}, {"}", "punctuation"}},
				31: {{"```", "punctuation"}}, 32: {{"```", "punctuation"}, {"json", "keyword"}},
				33: {{"{", "punctuation"}, {"\"ready\"", "key"}, {"true", "literal"}, {"}", "punctuation"}},
				34: {{"```", "punctuation"}},
			},
		},
		{
			name: "配色.json", format: "JSON", lines: []string{"{\"字段\":\"内容\",\"port\":8080,\"enabled\":true}"},
			expected: map[int][]expectedSyntaxRun{0: {
				{"{", "punctuation"}, {"\"字段\"", "key"}, {"\"内容\"", "value"}, {"\"port\"", "key"}, {"8080", "number"},
				{"\"enabled\"", "key"}, {"true", "literal"}, {"}", "punctuation"},
			}},
		},
		{
			name: "服务.java", format: "Java",
			lines: []string{
				"package demo;",
				"@Deprecated",
				"public class Server {",
				"    /* 连接参数",
				"       多行注释 */ private static final int port = 8080;",
				"    String url = \"https://example.test?a=1\"; // 接口",
				"    boolean ready = true;",
				"    int size() { return port; }",
				"}",
			},
			expected: map[int][]expectedSyntaxRun{
				0: {{"package", "keyword"}}, 1: {{"@Deprecated", "symbol"}},
				2: {{"public", "keyword"}, {"class", "keyword"}, {"Server", "symbol"}, {"{", "punctuation"}},
				3: {{"/* 连接参数", "comment"}},
				4: {{"       多行注释 */", "comment"}, {"private", "keyword"}, {"static", "keyword"}, {"final", "keyword"}, {"int", "symbol"}, {"port", "key"}, {"8080", "number"}},
				5: {{"String", "symbol"}, {"url", "key"}, {"\"https://example.test?a=1\"", "value"}, {"// 接口", "comment"}},
				6: {{"boolean", "symbol"}, {"ready", "key"}, {"true", "literal"}},
				7: {{"int", "symbol"}, {"size", "symbol"}, {"(", "punctuation"}, {")", "punctuation"}, {"{", "punctuation"}, {"return", "keyword"}, {"}", "punctuation"}},
				8: {{"}", "punctuation"}},
			},
		},
		{
			name: "示例.GO", format: "Go",
			lines: []string{"package main", "count := 0x10", "note := `第一行", "// 这是字符串", "最后一行`", "fmt.Println(note)"},
			expected: map[int][]expectedSyntaxRun{
				0: {{"package", "keyword"}}, 1: {{"count", "key"}, {"0x10", "number"}},
				2: {{"note", "key"}, {"`第一行", "value"}}, 3: {{"// 这是字符串", "value"}}, 4: {{"最后一行`", "value"}},
				5: {{"Println", "symbol"}, {"(", "punctuation"}, {")", "punctuation"}},
			},
		},
		{
			name: "报表.sql", format: "SQL",
			lines: []string{"SELECT count(*) AS total FROM samples", "WHERE active = true; -- 有效记录"},
			expected: map[int][]expectedSyntaxRun{
				0: {{"SELECT", "keyword"}, {"count", "symbol"}, {"(", "punctuation"}, {")", "punctuation"}, {"AS", "keyword"}, {"FROM", "keyword"}},
				1: {{"WHERE", "keyword"}, {"active", "key"}, {"true", "literal"}, {"-- 有效记录", "comment"}},
			},
		},
		{
			name: "配置.ts", format: "TypeScript", lines: []string{"export const item: Record<string, number> = { limit: 15 };"},
			expected: map[int][]expectedSyntaxRun{0: {
				{"export", "keyword"}, {"const", "keyword"}, {"item", "key"}, {"Record", "symbol"}, {"string", "symbol"},
				{"number", "symbol"}, {"{", "punctuation"}, {"limit", "key"}, {"15", "number"}, {"}", "punctuation"},
			}},
		},
		{
			name: "处理.py", format: "Python",
			lines: []string{"def 启动(port: int = 8080):", "    message = \"\"\"第一行", "    # 数据", "    完成\"\"\""},
			expected: map[int][]expectedSyntaxRun{
				0: {{"def", "keyword"}, {"启动", "symbol"}, {"(", "punctuation"}, {"port", "key"}, {"int", "symbol"}, {"8080", "number"}, {")", "punctuation"}},
				1: {{"message", "key"}, {"\"\"\"第一行", "value"}}, 2: {{"    # 数据", "value"}}, 3: {{"    完成\"\"\"", "value"}},
			},
		},
		{
			name: "主题.css", format: "CSS",
			lines: []string{":root {", "  --theme-color: #3080e0;", "  margin: 12px;", "}"},
			expected: map[int][]expectedSyntaxRun{
				0: {{"root", "symbol"}, {"{", "punctuation"}}, 1: {{"--theme-color", "key"}, {"#3080e0", "number"}},
				2: {{"margin", "key"}, {"12px", "number"}}, 3: {{"}", "punctuation"}},
			},
		},
		{
			name: "启动.sh", format: "Shell", lines: []string{"export PORT=8080 # 监听端口"},
			expected: map[int][]expectedSyntaxRun{0: {
				{"export", "keyword"}, {"PORT", "key"}, {"8080", "number"}, {"# 监听端口", "comment"},
			}},
		},
		{
			name: "启动.ps1", format: "PowerShell", lines: []string{"$enabled = $true # 配置"},
			expected: map[int][]expectedSyntaxRun{0: {
				{"$enabled", "key"}, {"$true", "literal"}, {"# 配置", "comment"},
			}},
		},
		{
			name: "安装.cpp", format: "C/C++", lines: []string{"#define VERSION 42", "int count = 3;"},
			expected: map[int][]expectedSyntaxRun{
				0: {{"#define", "keyword"}, {"VERSION", "symbol"}, {"42", "number"}},
				1: {{"int", "symbol"}, {"count", "key"}, {"3", "number"}},
			},
		},
		{
			name: "设置.cs", format: "C#", lines: []string{"private string name = \"服务\";"},
			expected: map[int][]expectedSyntaxRun{0: {
				{"private", "keyword"}, {"string", "symbol"}, {"name", "key"}, {"\"服务\"", "value"},
			}},
		},
		{
			name: "设置.kt", format: "Kotlin", lines: []string{"val enabled: Boolean = true"},
			expected: map[int][]expectedSyntaxRun{0: {
				{"val", "keyword"}, {"enabled", "key"}, {"Boolean", "symbol"}, {"true", "literal"},
			}},
		},
		{
			name: "设置.rs", format: "Rust", lines: []string{"fn label<'a>(name: &'a str) {}"},
			expected: map[int][]expectedSyntaxRun{0: {
				{"fn", "keyword"}, {"'a", "symbol"}, {"(", "punctuation"}, {"name", "key"}, {"'a", "symbol"},
				{"str", "symbol"}, {")", "punctuation"}, {"{", "punctuation"}, {"}", "punctuation"},
			}},
		},
		{
			name: "无后缀", format: "纯文本",
			lines: []string{"server.port=8080", "状态：true", "参考 https://example.test/docs，重试 2 次", "标识 abc123 保持原色"},
			expected: map[int][]expectedSyntaxRun{
				0: {{"server.port", "key"}, {"8080", "number"}}, 1: {{"状态", "key"}, {"true", "literal"}},
				2: {{"https://example.test/docs", "symbol"}, {"2", "number"}}, 3: nil,
			},
		},
		{
			name: "服务.log", format: "日志",
			lines: []string{"[INFO] status=200 ready=true", "[WARN] duration=12", "[ERROR] 请求失败"},
			expected: map[int][]expectedSyntaxRun{
				0: {{"INFO", "symbol"}, {"status", "key"}, {"200", "number"}, {"ready", "key"}, {"true", "literal"}},
				1: {{"WARN", "number"}, {"duration", "key"}, {"12", "number"}}, 2: {{"ERROR", "alert"}},
			},
		},
		{
			name: "统计.csv", format: "CSV",
			lines: []string{"名称,数量,启用", "\"含,逗号\",42,true", "\"跨行", "备注\",3,false"},
			expected: map[int][]expectedSyntaxRun{
				0: {{"名称", "value"}, {",", "punctuation"}, {"数量", "value"}, {",", "punctuation"}, {"启用", "value"}},
				1: {{"\"含,逗号\"", "value"}, {",", "punctuation"}, {"42", "number"}, {",", "punctuation"}, {"true", "literal"}},
				2: {{"\"跨行", "value"}},
				3: {{"备注\"", "value"}, {",", "punctuation"}, {"3", "number"}, {",", "punctuation"}, {"false", "literal"}},
			},
		},
		{
			name: "数据.tsv", format: "TSV", lines: []string{"蓝色\t42\ttrue"},
			expected: map[int][]expectedSyntaxRun{0: {
				{"蓝色", "value"}, {"\t", "punctuation"}, {"42", "number"}, {"\t", "punctuation"}, {"true", "literal"},
			}},
		},
		{
			name: "记录.jsonl", format: "JSON Lines", lines: []string{"{\"ok\":true}", "[3, null]"},
			expected: map[int][]expectedSyntaxRun{
				0: {{"{", "punctuation"}, {"\"ok\"", "key"}, {"true", "literal"}, {"}", "punctuation"}},
				1: {{"[", "punctuation"}, {"3", "number"}, {"null", "literal"}, {"]", "punctuation"}},
			},
		},
	} {
		t.Run(sample.name, func(t *testing.T) {
			m := newEditorApp()
			path := filepath.Join(t.TempDir(), sample.name)
			source := strings.Join(sample.lines, "\r\n")
			if err := os.WriteFile(path, []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			m.load(path)
			if m.active == nil || m.active.format() != sample.format || m.active.doc.eol != "CRLF" && len(sample.lines) > 1 {
				t.Fatal("file detection or line-ending preservation failed")
			}
			if sample.format == "HTML" {
				m.active.htmlMode = "编辑"
			}
			s := openTestSession(t, m)
			if err := s.Resize(1100, 640); err != nil {
				t.Fatal(err)
			}
			for _, light := range []bool{false, true} {
				m.setLightMode(light)
				if err := s.Frame(); err != nil {
					t.Fatal(err)
				}
				view := &m.active.source.view.Snapshot
				if view.Language != sample.format {
					t.Fatalf("expected editor language %s, got %s", sample.format, view.Language)
				}
				for index, expected := range sample.expected {
					line := sample.lines[index]
					var runs []components.CodeEditorRun
					if view.HighlightLine != nil {
						runs = view.HighlightLine(view.Language, index, line, nil)
					} else {
						runs = view.Highlight(view.Language, line, nil)
					}
					checkSyntaxRuns(t, line, runs, expected, light)
				}
				if output := os.Getenv("VIVA_PREVIEW_DIR"); output != "" && len(sample.lines) > 1 {
					theme := "dark"
					if light {
						theme = "light"
					}
					name := "syntax-" + strings.ToLower(strings.ReplaceAll(sample.format, "/", "-")) + "-" + theme + ".png"
					if err := s.Screenshot(filepath.Join(output, name)); err != nil {
						t.Fatal(err)
					}
				}
			}
			if m.active.modified() {
				t.Fatal("syntax highlighting modified the document")
			}
		})
	}
}

func TestStatusLanguagePicker(t *testing.T) {
	directory := t.TempDir()
	noExtension := filepath.Join(directory, "settings")
	yamlPath := filepath.Join(directory, "config.YML")
	const ini = "port=8080"
	for path, value := range map[string]string{noExtension: ini, yamlPath: "port: 8080"} {
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	m := newEditorApp()
	m.load(noExtension)
	config := m.active
	m.load(yamlPath)
	yaml := m.active
	m.newDocument()
	draft := m.active
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	expectLanguage := func(want string) {
		t.Helper()
		if got := m.active.source.view.Snapshot.Language; got != want {
			t.Fatalf("source language = %q, want %q", got, want)
		}
	}
	check(s.Resize(1100, 640))
	trigger, err := s.Locate("button:纯文本")
	check(err)
	check(s.ClickLabel("button:纯文本"))
	option, err := s.Locate("menuitem:JSON")
	check(err)
	if option.Bounds.Y+option.Bounds.Height > trigger.Bounds.Y {
		t.Fatalf("menu overlaps or opens below the status bar: %+v", option.Bounds)
	}
	if preview := os.Getenv("VIVA_PREVIEW_DIR"); preview != "" {
		check(s.Screenshot(filepath.Join(preview, "syntax-language-menu.png")))
	}
	check(s.ClickLabel("menuitem:JSON"))
	expectLanguage("JSON")
	if draft.modified() || draft.doc.path != "" {
		t.Fatal("selecting JSON marked a new document as modified")
	}
	const jsonSource = `{"名称":"本机"}`
	check(s.Type(jsonSource))
	if draft.source.text.String() != jsonSource {
		t.Fatal("the source did not regain focus after selecting the language")
	}
	check(s.ClickLabel("button:JSON"))
	check(s.ClickLabel("menuitem:Markdown"))
	expectLanguage("Markdown")
	check(s.ClickLabel("button:Markdown"))
	check(s.ClickLabel("menuitem:YAML"))
	expectLanguage("YAML")
	check(s.ClickLabel("button:YAML"))
	check(s.ClickLabel("menuitem:代码与脚本"))
	check(s.ClickLabel("menuitem:Java"))
	expectLanguage("Java")

	m.activate(config)
	check(s.Frame())
	expectLanguage("纯文本")
	checkSyntaxRuns(t, ini, config.source.view.Snapshot.Highlight("纯文本", ini, nil),
		[]expectedSyntaxRun{{"port", "key"}, {"8080", "number"}}, false)
	check(s.ClickLabel("button:纯文本"))
	check(s.ClickLabel("menuitem:INI"))
	expectLanguage("INI")
	checkSyntaxRuns(t, ini, config.source.view.Snapshot.HighlightLine("INI", 0, ini, nil),
		[]expectedSyntaxRun{{"port", "key"}, {"8080", "number"}}, false)
	if config.doc.path != noExtension || config.modified() {
		t.Fatal("selecting INI changed the original file")
	}

	m.activate(yaml)
	check(s.Frame())
	expectLanguage("YAML")
	check(s.ClickLabel("button:YAML"))
	check(s.ClickLabel("menuitem:纯文本"))
	expectLanguage("纯文本")
	const plain = "port: 8080"
	checkSyntaxRuns(t, plain, yaml.source.view.Snapshot.Highlight("纯文本", plain, nil), nil, false)
	check(s.ClickLabel("button:纯文本"))
	check(s.Keys("home"))
	check(s.Keys("enter"))
	expectLanguage("YAML")
	if yaml.languageOverride != "" || yaml.modified() {
		t.Fatal("auto detection did not restore suffix-based mode")
	}

	restored := newEditorApp()
	restored.restoreSession(m.snapshotSession())
	for index, want := range []string{"INI", "YAML", "Java"} {
		if got := restored.tabs[index].syntaxLanguage(); got != want {
			t.Fatalf("restored tab %d language = %q, want %q", index, got, want)
		}
	}
}

func TestConfigHighlightTracksEditsBeforeScrolledLines(t *testing.T) {
	m := newEditorApp()
	const target = "    状态: true"
	source := "服务:\n  启动: |-\n" + strings.Repeat("    cmd: 测试\n", 90) + target
	p := m.addTab(document{saved: source, eol: "LF"}, "应用.yml").source
	s := openTestSession(t, m)
	check := func(want []expectedSyntaxRun) {
		t.Helper()
		if err := s.Frame(); err != nil {
			t.Fatal(err)
		}
		runs := p.view.Snapshot.HighlightLine("YAML", 92, target, nil)
		checkSyntaxRuns(t, target, runs, want, false)
	}
	check([]expectedSyntaxRun{{"状态: true", "value"}})
	start := p.text.RuneIndex(strings.Index(source, "|-"))
	p.text.ReplaceRange(text.Range{Start: start, End: start + 2}, "")
	check([]expectedSyntaxRun{{"状态", "key"}, {"true", "literal"}})
	if !p.text.Undo() {
		t.Fatal("could not undo the preceding edit")
	}
	check([]expectedSyntaxRun{{"状态: true", "value"}})
}
