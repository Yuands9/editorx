package editor

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
)

var installedEditorFonts = sync.OnceValue(func() []string {
	return append([]string{""}, app.MonospaceFamilies()...)
})

var editorFontSizes = func() []int {
	sizes := make([]int, 0, maxEditorFontSize-minEditorFontSize+1)
	for size := minEditorFontSize; size <= maxEditorFontSize; size++ {
		sizes = append(sizes, size)
	}
	return sizes
}()

func (s *settingsPageState) loadEditorFonts() {
	s.fontRequest = ui.NewEffect[[]string, int](func(context.Context) ([]string, error) {
		return installedEditorFonts(), nil
	})
}

func (m *editorApp) editorSettings(c ui.C) {
	s := &m.appSettings
	draft := &s.draft.Editor
	before := *draft
	if s.fontRequest != nil {
		ui.Effect(c.Part("font-list"), s.fontRequest, 0)
		if fonts, _, ready := s.fontRequest.Take(); ready {
			s.fonts, s.fontRequest = fonts, nil
		}
	}
	fonts := s.fonts
	if len(fonts) == 0 {
		fonts = []string{""}
	}
	if !slices.Contains(fonts, draft.FontFamily) {
		fonts = append(slices.Clone(fonts), draft.FontFamily)
	}
	previewBelow := c.Bounds().Height < 660
	if !previewBelow {
		m.editorSettingsPreview(c.Part("preview"), *draft)
	}
	m.applicationSetting(c.Part("font"), "编辑区字体", "使用系统已安装的等宽字体。", func() {
		ui.Select(c, &draft.FontFamily, fonts, func(font string) string {
			if font == "" {
				return "系统默认"
			}
			return font
		}).Label("编辑区字体").Width(224).Height(34).ListMaxHeight(288).Disabled(s.fontRequest != nil)
	})
	m.applicationSetting(c.Part("size"), "字号", "适用于代码、文本和 JSON 编辑区。", func() {
		ui.Select(c, &draft.FontSize, editorFontSizes, func(size int) string { return fmt.Sprintf("%d px", size) }).
			Label("编辑区字号").Width(112).Height(34).ListMaxHeight(286)
	})
	m.applicationSetting(c.Part("tab"), "Tab 缩进", "设置 Tab 的列宽和按键插入的字符。", func() {
		ui.Row(c.Part("tab-controls")).Items(ui.Center).Gap(8).Children(func() {
			ui.Select(c.Part("tab-width"), &draft.TabWidth, editorTabWidths, func(width int) string { return fmt.Sprintf("%d 列", width) }).
				Label("Tab 宽度").Width(82).Height(34)
			ui.Select(c.Part("tab-character"), &draft.HardTabs, []bool{false, true}, func(hardTabs bool) string {
				if hardTabs {
					return "Tab 字符"
				}
				return "空格"
			}).Label("Tab 键输入").Width(112).Height(34)
		})
	})
	m.applicationSetting(c.Part("wrap"), "默认自动折行", "按文件类型时，JSON 默认折行。", func() {
		ui.Select(c, &draft.Wrap, []string{wrapByFileType, wrapEnabled, wrapDisabled}, func(mode string) string {
			switch mode {
			case wrapEnabled:
				return "开启"
			case wrapDisabled:
				return "关闭"
			default:
				return "按文件类型"
			}
		}).Label("默认自动折行").Width(144).Height(34)
	})
	m.applicationSetting(c.Part("json-indent"), "JSON 默认缩进", "用于双栏格式化、粘贴和转换结果。", func() {
		ui.Select(c, &draft.JSONIndent, []int{2, 4}, func(indent int) string { return fmt.Sprintf("%d 个空格", indent) }).
			Label("JSON 默认缩进").Width(144).Height(34)
	})
	if previewBelow {
		m.editorSettingsPreview(c.Part("preview"), *draft)
	}
	if *draft != before {
		c.After(m.saveApplicationSettings)
	}
}

func (m *editorApp) editorSettingsPreview(c ui.C, preferences editorPreferences) {
	settings := &m.appSettings
	indent := strings.Repeat(" ", preferences.JSONIndent)
	sample := "{\n" + indent + "\"name\": \"EditorX\",\n" + indent + "\"preview\": true\n}"
	pane := settings.editorPreview
	if pane == nil {
		pane = newEditorPane(sample, true)
		settings.editorPreview = pane
	} else if pane.text.String() != sample {
		pane.replace(sample)
	}
	pane.light = m.light
	pane.applyEditorPreferences(preferences)
	pane.view.Snapshot.Language = "JSON"
	pane.view.Snapshot.Wrap = preferences.Wrap != wrapDisabled
	ui.Text(c, "实时预览").Muted().FontSize(12)
	pane.declare(c, "编辑器设置预览", false).FillWidth().Height(max(110, float32(preferences.FontSize)*6)).Radius(10).Clip()
}
