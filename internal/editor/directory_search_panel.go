package editor

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/vivaui/viva/ui"
)

func (m *editorApp) directoryResultsPanel(c ui.C, blocked bool) {
	s := m.directorySearch
	if s == nil || !s.panelOpen {
		return
	}
	busy := s.request != nil || s.planRequest != nil || s.replacing || s.replaceJob != nil
	ui.Column(c).FillWidth().Height(min(250, max(140, c.Bounds().Height/3))).Shrink(0).Pad(8).Gap(4).Background(m.theme.Card).Children(func() {
		ui.Row(c).FillWidth().Items(ui.Center).Gap(6).Children(func() {
			ui.Text(c, fmt.Sprintf(uiText("目录搜索 · %d 个匹配"), len(s.result.hits))).FontSize(12).Bold()
			ui.Text(c, s.root.Text()).FontSize(11).Muted().Grow(1).MinWidth(0).Ellipsis()
			if ui.Button(c, uiText("搜索选项")).Icon(searchIcon).Ghost().Height(26).Disabled(busy || blocked).Clicked() {
				c.After(func() { m.openDirectorySearch(s.root.Text()) })
			}
			if ui.Button(c, uiText("关闭目录搜索结果")).Icon(closeIcon).IconOnly().Ghost().Size(26, 26).Tooltip(uiText("关闭结果面板")).Clicked() {
				s.panelOpen, s.replacing = false, false
				if s.cancel != nil {
					s.cancel()
				}
			}
		})
		ui.Row(c).FillWidth().Items(ui.Center).Gap(6).Children(func() {
			ui.TextField(c.Part("replacement"), &s.replacement, uiText("目录替换内容"), uiText("替换为（可留空；正则支持 $1）")).Grow(1).MinWidth(80).Height(28).Disabled(busy || blocked)
			if ui.Button(c, uiText("预览替换")).Ghost().Height(28).Disabled(busy || blocked || len(s.result.hits) == 0).Clicked() {
				c.After(m.previewDirectoryReplacement)
			}
			if s.plan != nil && s.plan.template == s.replacement.Text() {
				count := 0
				for _, file := range s.plan.files {
					if file.selected {
						count++
					}
				}
				if ui.Button(c, fmt.Sprintf(uiText("替换所选 %d 个文件"), count)).Height(28).Disabled(busy || blocked || count == 0).Clicked() {
					c.After(m.startDirectoryReplacement)
				}
			}
			if busy && ui.Button(c, uiText("停止")).Ghost().Height(28).Clicked() {
				if s.cancel != nil {
					s.cancel()
				}
				s.replacing = false // The in-flight file finishes atomically; later files are not started.
			}
		})
		if s.plan != nil && s.plan.template == s.replacement.Text() {
			ui.List(c.Part("plan"), &s.plan.list, len(s.plan.files), func(row ui.ListRow) {
				file := &s.plan.files[row.Index]
				row.Element.Height(46).PadX(4).Children(func() {
					ui.Column(row.C).Fill().Children(func() {
						ui.Row(row.C).FillWidth().Items(ui.Center).Gap(8).Children(func() {
							ui.Checkbox(row.C, &file.selected, fmt.Sprintf(uiText("%s · %d 处"), filepath.Base(file.path), file.count)).Tooltip(file.path).Disabled(busy || blocked)
							ui.Text(row.C, filepath.Dir(file.path)).Grow(1).MinWidth(0).Ellipsis().FontSize(11).Muted()
						})
						ui.Text(row.C, file.preview).FillWidth().Ellipsis().FontSize(11).Muted().PadLeft(24)
					})
				})
			}).FillWidth().Grow(1).MinHeight(0)
		} else {
			s.list.Label = func(index int) string {
				hit := s.result.hits[index]
				return fmt.Sprintf("%s:%d:%d", filepath.Base(hit.path), hit.line, hit.column)
			}
			ui.List(c.Part("hits"), &s.list, len(s.result.hits), func(row ui.ListRow) {
				hit := s.result.hits[row.Index]
				row.Element.Height(40).PadX(6).Children(func() {
					ui.Column(row.C).Fill().Children(func() {
						ui.Text(row.C, fmt.Sprintf("%s:%d:%d", hit.path, hit.line, hit.column)).FillWidth().Ellipsis().FontSize(11).Muted()
						ui.Text(row.C, hit.excerpt).FillWidth().Ellipsis().FontSize(12)
					})
				})
				if row.Element.Clicked() && !blocked {
					c.After(func() { m.chooseDirectoryHit(row.Index) })
				}
			}).FillWidth().Grow(1).MinHeight(0)
			if index, ok := s.list.Submitted(); ok && !blocked {
				c.After(func() { m.chooseDirectoryHit(index) })
			}
		}
		message := fmt.Sprintf(uiText("已搜索 %d 个文件 · 跳过 %d 个文件"), s.result.files, s.result.skipped)
		if s.result.partial {
			message += " · 结果不完整，请缩小搜索范围后替换"
		}
		if s.request != nil {
			message = "正在搜索，可继续编辑文件…"
		}
		if s.planRequest != nil {
			message = "正在生成替换预览…"
		}
		if s.plan != nil {
			message = "已打开文件在编辑区替换，可撤销；未打开文件保存到磁盘并按设置记录历史。"
		}
		if s.replacing {
			message = fmt.Sprintf(uiText("正在替换 · 已完成 %d 个文件"), s.replacedFiles)
		}
		if s.problem != "" {
			message = s.problem
		}
		if len(s.replaceProblems) > 0 {
			message += " · " + strings.Join(s.replaceProblems, "；")
		}
		ui.Text(c, message).FillWidth().Ellipsis().FontSize(11).Muted()
	})
}
