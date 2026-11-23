package editor

import "github.com/vivaui/viva/ui"

func (m *editorApp) shortcutSettings(c ui.C) {
	for _, group := range []struct {
		title string
		keys  [][2]string
	}{
		{"文件与标签", [][2]string{
			{"新建文件", "Ctrl+N"}, {"新建窗口", "Ctrl+Shift+N"},
			{"打开文件", "Ctrl+O"}, {"添加项目", "Ctrl+Shift+O"},
			{"保存", "Ctrl+S"}, {"另存为", "Ctrl+Shift+S"},
			{"关闭当前标签", "Ctrl+W"}, {"下一个标签", "Ctrl+Tab"}, {"上一个标签", "Ctrl+Shift+Tab"},
		}},
		{"查找与编辑", [][2]string{
			{"查找", "Ctrl+F"}, {"替换", "Ctrl+R"},
			{"下一个匹配", "F3"}, {"上一个匹配", "Shift+F3"},
			{"查找框内输入换行", "Ctrl+Enter"}, {"自动折行", "Alt+Z"},
		}},
		{"工作区", [][2]string{
			{"文件栏", "Ctrl+B"}, {"Markdown 大纲", "Ctrl+Alt+B"},
			{"Git", "Ctrl+Shift+G"}, {"终端", "Ctrl+Alt+T"}, {"设置", "Ctrl+,"},
		}},
	} {
		ui.Text(c, group.title).Bold().FontSize(14).Height(22)
		ui.Column(c.Key(group.title)).FillWidth().Radius(11).Border(1, m.theme.Border).Clip().Children(func() {
			for index, key := range group.keys {
				if index > 0 {
					ui.Slot(c).FillWidth().Height(1).Background(m.theme.Border)
				}
				ui.Row(c.Key(key[1])).FillWidth().Height(34).PadX(12).Gap(10).Items(ui.Center).Children(func() {
					ui.Text(c, key[0]).Grow(1).MinWidth(0).Ellipsis()
					ui.Text(c, key[1]).FontSize(12).Color(m.theme.SecondaryForeground).
						Background(m.theme.Secondary).Radius(6).PadX(8).PadY(3)
				})
			}
		})
	}
}
