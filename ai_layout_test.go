package main

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

func TestAIMessageBubblesAndSelection(t *testing.T) {
	m := newTestEditorWithDocument()
	m.ai.open = true
	const question = "查看 field_name 的用途"
	const answerPart = "288 个商家"
	turn := &aiTurn{
		prompt: question, provider: "deepseek", model: "deepseek-chat",
		reply: "结果有 **" + answerPart + "**。",
	}
	m.ai.turns = []*aiTurn{turn}
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	bounds := func(label string) gfx.Rect {
		t.Helper()
		hit, err := s.Locate(label)
		check(err)
		return hit.Bounds
	}
	drag := func(from, to gfx.Point) {
		t.Helper()
		check(s.Press(from.X, from.Y))
		check(s.Move(to.X, to.Y))
		check(s.Release(to.X, to.Y))
	}

	check(s.Resize(1180, 740))
	prompt := bounds("问题正文")
	answer := bounds("回复正文")
	composer := bounds("AI 指令")
	if prompt.Width >= composer.Width*.75 || answer.Width >= composer.Width*.75 || prompt.X <= answer.X {
		t.Fatalf("short bubbles did not fit their message and alignment: prompt=%+v reply=%+v input=%+v", prompt, answer, composer)
	}
	from := gfx.Point{X: prompt.X + 1, Y: prompt.Y + prompt.Height/2}
	to := gfx.Point{X: prompt.X + 2*turn.questionBody.font, Y: from.Y}
	drag(from, to)
	selected := turn.questionBody.selectedText()
	if strings.TrimSpace(selected) != "查看" {
		t.Fatalf("the mouse did not select the beginning of the question: %q", selected)
	}
	check(s.Keys("ctrl+c"))
	if got := s.Driver().Clipboard(); got != selected {
		t.Fatalf("copied question selection %q, want %q", got, selected)
	}

	view := &turn.answerBody.markdown.view
	rendered := view.SelectedText(components.MarkdownSelection{Focus: view.TextLen()})
	offset := strings.Index(rendered, answerPart)
	if offset < 0 {
		t.Fatalf("formatted answer did not render its text: %q", rendered)
	}
	left, ok := view.CaretAt(offset)
	right, endOK := view.CaretAt(offset + len(answerPart))
	if !ok || !endOK {
		t.Fatal("could not locate the selection in the formatted reply")
	}
	drag(gfx.Point{X: left.X, Y: left.Y + left.Height/2}, gfx.Point{X: right.X, Y: right.Y + right.Height/2})
	check(s.Keys("ctrl+c"))
	if got := s.Driver().Clipboard(); got != answerPart {
		t.Fatalf("copied Markdown selection %q, want %q", got, answerPart)
	}
	if target := os.Getenv("VIVA_PREVIEW_DIR"); target != "" {
		check(s.Screenshot(filepath.Join(target, "ai-message-selection-dark.png")))
	}
	s.Driver().SetClipboard("复制前")
	check(s.RightClickLabel("回复正文"))
	check(s.ClickLabel("menuitem:复制选中内容"))
	if got := s.Driver().Clipboard(); got != answerPart {
		t.Fatalf("copying through the context menu returned %q", got)
	}

	turn.prompt = strings.Repeat("请核对配置项的含义并给出推荐设置。", 5)
	turn.reply = strings.Repeat("当前环境配置和端口都可以在本地继续检查。", 5)
	check(s.Frame())
	longPrompt := bounds("问题正文")
	longAnswer := bounds("回复正文")
	if longPrompt.Width <= prompt.Width || longAnswer.Width <= answer.Width ||
		turn.questionBody.layoutWidth >= turn.questionBody.preferredWidth || len(turn.questionBody.plain.Rows) < 2 ||
		turn.answerBody.layoutWidth >= turn.answerBody.preferredWidth || turn.answerBody.markdown.view.Height() <= answer.Height {
		t.Fatalf("long messages did not expand and wrap: question=%+v reply=%+v", longPrompt, longAnswer)
	}
	if target := os.Getenv("VIVA_PREVIEW_DIR"); target != "" {
		m.setLightMode(true)
		check(s.Frame())
		check(s.Screenshot(filepath.Join(target, "ai-message-wrap-light.png")))
	}
}

func TestAIResizableEditorAndPromptKeepTheirState(t *testing.T) {
	store, err := openSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := newEditorApp()
	store.attach(m)
	const source = "APP_NAME=Viva Editor\nAPP_ENV=development\nHTTP_PORT=8080\n"
	tab := m.addTab(document{saved: source, eol: "LF"}, "应用配置.env")
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	bounds := func(label string) gfx.Rect {
		t.Helper()
		hit, err := s.Locate(label)
		check(err)
		return hit.Bounds
	}
	drag := func(label string, dx, dy float32) {
		t.Helper()
		rect := bounds(label)
		x, y := rect.X+rect.Width/2, rect.Y+rect.Height/2
		check(s.Move(x, y))
		check(s.Press(x, y))
		check(s.Move(x+dx/2, y+dy/2))
		check(s.Move(x+dx, y+dy))
		check(s.Release(x+dx, y+dy))
		check(s.Frame())
	}

	check(s.Resize(1230, 760))
	check(s.ClickLabel("AI 助手"))
	check(s.ClickLabel("AI 指令"))
	check(s.Compose("qingfenxi"))
	check(s.Type("请分析这份配置。"))
	turn := &aiTurn{
		prompt: "怎么看这份配置？", provider: "deepseek", model: "deepseek-chat",
		reply: strings.Repeat("根据环境切换配置。请检查端口之后再保存。", 4),
	}
	m.ai.turns = append(m.ai.turns, turn)
	check(s.Frame())
	beforePrompt := bounds("AI 指令")
	beforeEditor := bounds("代码编辑区")
	beforeMarkdown := turn.answerBody.layoutWidth

	drag("调整编辑区与 AI 助手宽度", -100, 0)
	widePrompt := bounds("AI 指令")
	if widePrompt.Width < beforePrompt.Width+75 || bounds("代码编辑区").Width > beforeEditor.Width-75 || turn.answerBody.layoutWidth < beforeMarkdown+75 {
		t.Fatalf("sideways drag did not resize the editor, input and reply: input %g -> %g, markdown %g -> %g", beforePrompt.Width, widePrompt.Width, beforeMarkdown, turn.answerBody.layoutWidth)
	}
	drag("调整 AI 输入框高度", 0, -90)
	if got := bounds("AI 指令").Height; got < widePrompt.Height+65 {
		t.Fatalf("vertical drag did not enlarge the input field: %g -> %g", widePrompt.Height, got)
	}
	drag("调整 AI 输入框高度", 0, 53)
	if got := bounds("AI 指令").Height; got > widePrompt.Height+54 {
		t.Fatalf("vertical drag back did not shrink the input field: %g", got)
	}
	if m.active != tab || tab.source.text.String() != source || m.ai.prompt.Text() != "请分析这份配置。" {
		t.Fatal("resizing changed the selected tab, source or draft")
	}

	widthRatio, heightRatio := m.aiEditorSplit.Ratio, m.aiPromptSplit.Ratio
	check(store.save())
	restored, err := openSessionStore(store.directory)
	check(err)
	window := restored.windows[0]
	if window.aiEditorSplit.Ratio != widthRatio || window.aiPromptSplit.Ratio != heightRatio {
		t.Fatal("AI splitter ratios did not survive a session restore")
	}
	if target := os.Getenv("VIVA_PREVIEW_DIR"); target != "" {
		m.setLightMode(true)
		check(s.Frame())
		check(s.Screenshot(filepath.Join(target, "ai-resized-light.png")))
	}
	m.showFiles = true
	check(s.Resize(900, 560))
	check(s.Frame())
	drag("调整编辑区与 AI 助手宽度", 600, 0)
	drag("调整 AI 输入框高度", 0, 540)
	send := bounds("发送给模型")
	prompt := bounds("AI 指令")
	model := bounds("选择模型")
	if model.X+model.Width > send.X || !s.HasText("选择模型") {
		t.Fatalf("model button overlaps send or is too narrow: model=%+v send=%+v", model, send)
	}
	if target := os.Getenv("VIVA_PREVIEW_DIR"); target != "" {
		check(s.Screenshot(filepath.Join(target, "ai-resized-small.png")))
	}
	if send.X+send.Width > 900 || send.Y+send.Height > 532 || prompt.Height < 46 ||
		bounds("代码编辑区").Width < 205 || m.ai.prompt.Text() != "请分析这份配置。" {
		t.Fatalf("minimum viewport clipped controls, editor or draft: send=%+v prompt=%+v", send, prompt)
	}
}

func TestWorkspacePanelsShareCornersAndPromptHasNoOutline(t *testing.T) {
	m := newEditorApp()
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	root := t.TempDir()
	config := filepath.Join(root, "应用配置")
	web := filepath.Join(root, "页面示例")
	check(os.Mkdir(config, 0700))
	check(os.Mkdir(web, 0700))
	path := filepath.Join(config, "server.toml")
	check(os.WriteFile(path, []byte("[server]\nport = 8080\nhost = \"localhost\"\n"), 0600))
	m.addProjects(config, web)
	m.load(path)

	s := openTestSession(t, m)
	check(s.Resize(1240, 700))
	check(s.ClickLabel("AI 助手"))
	for _, light := range []bool{false, true} {
		m.setLightMode(light)
		check(s.Frame())
		check(s.ClickLabel("AI 指令"))
		input, err := s.Locate("AI 指令")
		check(err)
		frame := s.Snapshot()
		if frame.Focused != input.ID {
			t.Fatal("style change prevented the AI prompt from taking keyboard focus")
		}
		var panels []gfx.Rect
		promptFill := false
		for _, shape := range frame.Primitives {
			bounds := shape.Bounds
			// Paint coordinates are snapped to device pixels while hit regions use layout coordinates.
			isInput := math.Abs(float64(bounds.X-input.Bounds.X)) <= 1 && math.Abs(float64(bounds.Y-input.Bounds.Y)) <= 1 &&
				math.Abs(float64(bounds.Width-input.Bounds.Width)) <= 1 && math.Abs(float64(bounds.Height-input.Bounds.Height)) <= 1
			if isInput && shape.Color == m.theme.Primary {
				t.Fatalf("the focused prompt painted its old outline in light=%t", light)
			}
			if shape.Color != m.theme.Background {
				continue
			}
			if shape.Radius == m.theme.Metrics.RadiusSurface && shape.Bounds.Height > 400 {
				panels = append(panels, shape.Bounds)
			}
			if isInput && shape.Radius == m.theme.Metrics.RadiusControl {
				promptFill = true
			}
		}
		if !promptFill || len(panels) != 3 {
			t.Fatalf("missing rounded panels or input fill in light=%t: %+v", light, panels)
		}
		for i, panel := range panels {
			if panel.Y != workspaceInset || panel.Height != panels[0].Height ||
				(i > 0 && panels[i-1].X+panels[i-1].Width >= panel.X) {
				t.Fatalf("workspace panels have uneven insets or overlap: %+v", panels)
			}
		}
		if preview := os.Getenv("VIVA_PREVIEW_DIR"); preview != "" {
			name := "workspace-panels-dark.png"
			if light {
				name = "workspace-panels-light.png"
			}
			check(s.Screenshot(filepath.Join(preview, name)))
		}
	}
}

func TestAIProviderLogosAndGroupedModelSearch(t *testing.T) {
	for _, key := range aiPresetIDs {
		if key == "custom" || key == "kimi" || key == aiQoderCLI {
			continue
		}
		stem := aiProviderLogos[key]
		files := aiLogoBitmaps()
		if files[stem+".png"].Width == 0 && (files[stem+"-dark.png"].Width == 0 || files[stem+"-light.png"].Width == 0) {
			t.Errorf("provider logo pack is missing %s", key)
		}
	}
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	m.addTab(document{saved: "APP_ENV=development\nPORT=8080\n", eol: "LF"}, "本地配置.env")
	profiles := aiProfiles{DefaultID: 1}
	specs := []struct{ key, name, model string }{
		{"deepseek", "DeepSeek", "deepseek-chat"},
		{"openai", "OpenAI", "gpt-4.1-mini"},
		{"anthropic", "Claude", "claude-sonnet"},
		{"qwen", "通义千问", "qwen-plus"},
		{"gemini", "Gemini", "gemini-flash"},
		{"minimax", "MiniMax", "MiniMax-M2"},
		{"ollama", "本地 Ollama", "llama3.2"},
	}
	for i, spec := range specs {
		profiles.Profiles = append(profiles.Profiles, aiSettings{
			ID: i + 1, Name: spec.name, Model: spec.model, Provider: spec.key,
			BaseURL: "https://example.test/v1", Protocol: aiChat,
		})
	}
	if err := profiles.save(m.dataDirectory); err != nil {
		t.Fatal(err)
	}
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(s.Resize(1180, 720))
	check(s.ClickLabel("AI 助手"))
	check(s.ClickLabel("AI 指令"))
	check(s.Type("请解释这两个配置项的作用"))
	model, err := s.Locate("切换模型配置")
	check(err)
	mode, err := s.Locate("选择 AI 模式")
	check(err)
	send, err := s.Locate("发送给模型")
	check(err)
	if model.Bounds.X-mode.Bounds.X-mode.Bounds.Width < 16 ||
		send.Bounds.X-model.Bounds.X-model.Bounds.Width > 8 ||
		model.Bounds.X+model.Bounds.Width > send.Bounds.X {
		t.Fatalf("model picker is not adjacent to send with room on the left: %+v", model.Bounds)
	}
	if target := os.Getenv("VIVA_PREVIEW_DIR"); target != "" {
		check(s.Screenshot(filepath.Join(target, "ai-composer-dark.png")))
	}
	check(s.ClickLabel("切换模型配置"))
	search, err := s.Locate("搜索模型")
	check(err)
	if search.Bounds.Y+search.Bounds.Height > model.Bounds.Y {
		t.Fatal("model picker did not open above the composer")
	}
	check(s.Move(search.Bounds.X+10, search.Bounds.Y+10))
	if s.Snapshot().Focused == search.ID {
		t.Fatal("opening the model picker focused its search field")
	}
	if target := os.Getenv("VIVA_PREVIEW_DIR"); target != "" {
		check(s.Screenshot(filepath.Join(target, "ai-model-picker-dark.png")))
	}
	check(s.ClickLabel("思考水平"))
	firstLevel, err := s.Locate("radio:服务默认")
	check(err)
	high, err := s.Locate("radio:高")
	check(err)
	if !firstLevel.Checked || high.Bounds.Y <= firstLevel.Bounds.Y ||
		high.Bounds.Y+high.Bounds.Height >= model.Bounds.Y {
		t.Fatal("reasoning levels did not fit inside the model picker")
	}
	check(s.ClickLabel("radio:高"))
	if m.ai.effort != "high" || m.ai.modelPicker.reasoning {
		t.Fatal("choosing a reasoning level did not return to model selection")
	}
	check(s.ClickLabel("DeepSeek 模型"))
	if _, err := s.Locate("选择配置 DeepSeek"); err == nil {
		t.Fatal("collapsing a provider left its models visible")
	}
	check(s.ClickLabel("搜索模型"))
	if s.Snapshot().Focused != search.ID {
		t.Fatal("the search field did not take focus on click")
	}
	check(s.Type("GEMINI"))
	_, err = s.Locate("选择配置 Gemini")
	check(err)
	if _, err := s.Locate("选择配置 DeepSeek"); err == nil {
		t.Fatal("search did not filter the other provider group")
	}
	check(s.ClickLabel("选择配置 Gemini"))
	if m.ai.profileID != 5 || m.ai.profileMenu {
		t.Fatal("selecting a search result did not close the picker and switch models")
	}
	check(s.ClickLabel("选择 AI 模式"))
	check(s.ClickLabel("menuitem:新建草稿"))
	if m.ai.mode != aiDraft || m.ai.prompt.Text() != "请解释这两个配置项的作用" {
		t.Fatal("mode switch lost the draft")
	}
	check(s.ClickLabel("切换模型配置"))
	check(s.Keys("escape"))
	if m.ai.profileMenu {
		t.Fatal("model picker did not dismiss with Escape")
	}
}
