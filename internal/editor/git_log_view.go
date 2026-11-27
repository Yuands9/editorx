package editor

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

func (m *editorApp) gitLogBrowser(c ui.C, s *gitReviewState) {
	ui.Column(c).FillWidth().Grow(1).MinHeight(0).Gap(8).Children(func() {
		ui.Row(c.Part("filters")).FillWidth().Gap(6).Items(ui.Center).Children(func() {
			choices := append([]string{"", "all"}, s.logReferences...)
			if ui.Select(c.Part("branch"), &s.logQuery.reference, choices, func(ref string) string {
				switch {
				case ref == "":
					return uiText("当前分支")
				case ref == "all":
					return uiText("所有分支和标签")
				case strings.HasPrefix(ref, "refs/heads/"):
					return uiText("本地分支") + " · " + strings.TrimPrefix(ref, "refs/heads/")
				case strings.HasPrefix(ref, "refs/remotes/"):
					return uiText("远程分支") + " · " + strings.TrimPrefix(ref, "refs/remotes/")
				default:
					return uiText("标签") + " · " + strings.TrimPrefix(ref, "refs/tags/")
				}
			}).Label(uiText("提交记录分支范围")).Width(210).Height(32).Disabled(s.log != nil).Changed() {
				c.After(func() { s.loadLog(false) })
			}
			search := ui.TextField(c.Part("search"), &s.logSearch, uiText("搜索提交记录"), uiText("提交说明或哈希")).Grow(1).MinWidth(90).Height(32)
			author := ui.TextField(c.Part("author"), &s.logAuthor, uiText("提交作者"), uiText("所有作者")).Width(130).Height(32)
			if ui.Button(c.Part("search-button"), uiText("搜索提交")).Icon(searchIcon).Height(32).Disabled(s.log != nil).Clicked() || (search.Submitted() || author.Submitted()) && s.log == nil {
				c.After(func() { s.loadLog(false) })
			}
		})
		ui.Row(c.Part("tools")).FillWidth().Gap(6).Items(ui.Center).Children(func() {
			if ui.Select(c.Part("date"), &s.logDate, []string{"", "7 days ago", "30 days ago", "90 days ago"}, func(value string) string {
				return uiText(map[string]string{"": "全部时间", "7 days ago": "最近 7 天", "30 days ago": "最近 30 天", "90 days ago": "最近 90 天"}[value])
			}).Label(uiText("提交时间范围")).Width(140).Height(28).Disabled(s.log != nil).Changed() {
				c.After(func() { s.logQuery.since = s.logDate; s.loadLog(false) })
			}
			if ui.Button(c, uiText("清除筛选")).Ghost().Height(28).Disabled(s.log != nil).Clicked() {
				c.After(func() {
					s.logSearch.SetText("")
					s.logAuthor.SetText("")
					s.logQuery, s.logDate = gitLogQuery{}, ""
					s.loadLog(false)
				})
			}
			ui.Slot(c).Grow(1)
			if s.log != nil {
				ui.Text(c, uiText("正在读取提交记录…")).FontSize(12).Muted()
			} else {
				ui.Text(c, fmt.Sprintf(uiText("已加载 %d 条提交"), len(s.commits))).FontSize(12).Muted()
			}
			if ui.Button(c, uiText("刷新提交记录")).Icon(refreshIcon).Ghost().Height(28).Disabled(s.log != nil).Clicked() {
				c.After(func() { s.loadLog(false) })
			}
		})
		if s.problem != "" {
			ui.Wrap(c, s.problem).FillWidth().Color(m.theme.Danger).FontSize(12)
		}
		if s.logSplit.Pos == 0 {
			s.logSplit.Pos = max(320, (c.Bounds().Width-6)*.65)
		}
		ui.Row(c.Part("content")).FillWidth().Grow(1).MinHeight(0).Children(func() {
			ui.Column(c.Part("commits-panel")).Width(s.logSplit.Pos).FillHeight().MinWidth(0).Gap(4).Children(func() {
				m.gitLogTable(c.Part("table"), s)
				if s.logHasMore && ui.Button(c, uiText("加载更多提交")).Ghost().FillWidth().Height(30).Disabled(s.log != nil).Clicked() {
					c.After(func() { s.loadLog(true) })
				}
			})
			ui.Splitter(c.Part("divider"), &s.logSplit).Mins(300, 260).SplitterThickness(8).Label(uiText("调整提交记录和详情宽度"))
			ui.Column(c.Part("details-panel")).Grow(1).MinWidth(0).FillHeight().Gap(6).PadLeft(6).Children(func() {
				m.gitLogDetails(c.Part("details"), s)
			})
		})
	})
}

func (m *editorApp) gitLogTable(c ui.C, s *gitReviewState) {
	wide := min(s.logSplit.Pos, (c.Bounds().Width-40)*.65) >= 620
	ui.Row(c.Part("headings")).FillWidth().Height(28).PadX(10).Gap(8).Items(ui.Center).Background(m.theme.Card).Children(func() {
		ui.Text(c, uiText("提交说明")).Bold().FontSize(12).Grow(1)
		if wide {
			ui.Text(c, uiText("作者")).Width(100).Muted().FontSize(12)
			ui.Text(c, uiText("提交时间")).Width(122).Muted().FontSize(12)
			ui.Text(c, uiText("哈希")).Width(70).Muted().FontSize(12)
		}
	})
	if len(s.commits) == 0 && s.log == nil {
		ui.Text(c, uiText("没有符合条件的提交记录")).Muted().Pad(16)
	}
	s.logList.Label = func(i int) string { return s.commits[i].subject }
	ui.List(c.Part("commits"), &s.logList, len(s.commits), func(row ui.ListRow) {
		record := s.commits[row.Index]
		height := float32(60)
		if wide {
			height = 42
		}
		row.Element.Height(height).Children(func() {
			item := ui.Column(row.C).Fill().PadX(10).PadY(5).Gap(3).Radius(6)
			item.HitWith(ui.ID(row.C.Part("record")), uiText("查看提交 ")+record.hash, gfx.HitOptions{Role: gfx.RoleButton}).
				Tooltip(record.subject + "\n" + record.author + " · " + gitLogTime(record.at) + "\n" + record.hash).
				Hover(ui.Look{}.Background(m.theme.Hover))
			if record.hash == s.revision {
				item.Background(m.theme.Selection)
			}
			item.Children(func() {
				ui.Row(row.C).FillWidth().Items(ui.Center).Gap(8).Children(func() {
					ui.Icon(row.C, gitCommitIcon).Size(14, 14).Color(m.theme.Ring).Shrink(0)
					ui.Text(row.C, record.subject).FontSize(13).Grow(1).MinWidth(0).Ellipsis()
					if wide {
						ui.Text(row.C, record.author).FontSize(12).Width(100).Ellipsis()
						ui.Text(row.C, gitLogTime(record.at)).FontSize(11).Width(122).Muted().Ellipsis()
						ui.Text(row.C, record.hash[:min(8, len(record.hash))]).FontSize(11).Width(70).Muted()
					}
				})
				if !wide {
					ui.Row(row.C).FillWidth().Items(ui.Center).Gap(8).PadLeft(22).Children(func() {
						ui.Text(row.C, record.author).FontSize(11).Grow(1).MinWidth(0).Ellipsis().Muted()
						ui.Text(row.C, gitLogTime(record.at)).FontSize(11).Muted().Shrink(0)
						ui.Text(row.C, record.hash[:min(8, len(record.hash))]).FontSize(11).Muted().Shrink(0)
					})
				}
			})
			if item.Clicked() {
				row.C.After(func() { s.logList.Select(row.Index); s.selectCommit(record) })
			}
		})
	}).FillWidth().Grow(1).MinHeight(0).Label(uiText("提交列表"))
	if index := s.logList.Selected(); s.logList.Changed() && index >= 0 && index < len(s.commits) && s.commits[index].hash != s.revision {
		c.After(func() { s.selectCommit(s.commits[index]) })
	}
}

func gitLogTime(value string) string {
	if at, err := time.Parse(time.RFC3339, value); err == nil {
		return at.Local().Format("2006-01-02 15:04")
	}
	return value
}

func (m *editorApp) gitLogDetails(c ui.C, s *gitReviewState) {
	var record *gitCommitRecord
	for i := range s.commits {
		if s.commits[i].hash == s.revision {
			record = &s.commits[i]
			break
		}
	}
	if record == nil {
		ui.Text(c, uiText("选择提交查看详情和变更文件")).Muted().FontSize(12)
		return
	}
	ui.Row(c).FillWidth().Items(ui.Center).Children(func() {
		ui.Text(c, uiText("提交详情")).Bold().Grow(1)
		if ui.Button(c, uiText("复制提交哈希")).Icon(copyIcon).IconOnly().Ghost().Size(28, 28).Tooltip(uiText("复制提交哈希")).Clicked() {
			_ = app.SetClipboardText(record.hash)
		}
	})
	ui.Text(c, record.hash).FillWidth().Ellipsis().FontSize(11).Muted()
	ui.Text(c, record.author+" · "+gitLogTime(record.at)).FillWidth().Ellipsis().FontSize(12).Muted()
	if record.refs != "" {
		ui.Wrap(c, record.refs).FillWidth().FontSize(12).Color(m.theme.Ring)
	}
	if len(strings.Fields(record.parents)) > 1 {
		ui.Text(c, uiText("合并提交 · 与第一父提交比较")).FontSize(11).Muted()
	}
	messageHeight := float32(110)
	if c.Bounds().Height < 650 {
		messageHeight = 60
	}
	ui.Scroll(c.Part("message")).FillWidth().Height(messageHeight).Children(func() {
		ui.Wrap(c, record.body).FillWidth().FontSize(13)
	})
	ui.Slot(c).FillWidth().Height(1).Background(m.theme.Border)
	ui.Text(c, fmt.Sprintf(uiText("变更文件 · %d"), len(s.changes))).Bold().FontSize(13)
	if s.files != nil {
		ui.Text(c, uiText("正在读取文件列表…")).Muted().FontSize(12)
	}
	s.fileList.Label = func(i int) string { return s.changes[i].path }
	ui.List(c.Part("files"), &s.fileList, len(s.changes), func(row ui.ListRow) {
		change := s.changes[row.Index]
		row.Element.Height(52).Children(func() {
			item := ui.Row(row.C).Fill().PadX(6).Gap(7).Items(ui.Center).Radius(6)
			item.HitWith(ui.ID(row.C.Part("file")), uiText("查看提交文件 ")+change.path, gfx.HitOptions{Role: gfx.RoleButton}).
				Tooltip(filepath.Join(s.root, change.path)).Hover(ui.Look{}.Background(m.theme.Hover))
			item.Children(func() {
				icon, color := m.fileAppearance(change.path)
				ui.Icon(row.C, icon).Size(16, 16).Color(color).Shrink(0)
				ui.Column(row.C).Grow(1).MinWidth(0).Gap(3).Children(func() {
					ui.Text(row.C, path.Base(change.path)).FontSize(13).FillWidth().Ellipsis()
					directory := path.Dir(change.path)
					if directory == "." {
						directory = uiText("仓库根目录") + " · " + filepath.Base(s.root)
					}
					ui.Text(row.C, directory).FontSize(11).Muted().FillWidth().Ellipsis()
				})
				ui.Text(row.C, gitChangeLabel(change)).FontSize(11).Muted().Shrink(0)
			})
			if item.Clicked() {
				row.C.After(func() { s.change, s.logDiff = change, true; s.reload() })
			}
		})
	}).FillWidth().Grow(1).MinHeight(0).Label(uiText("提交文件列表"))
	if index, submitted := s.fileList.Submitted(); submitted && index >= 0 && index < len(s.changes) {
		c.After(func() { s.change, s.logDiff = s.changes[index], true; s.reload() })
	}
	ui.Text(c, uiText("点击文件查看该次提交的差异")).FontSize(11).Muted()
}
