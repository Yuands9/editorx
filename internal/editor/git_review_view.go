package editor

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"github.com/vivaui/viva/ui/codeeditor"
)

type gitReviewState struct {
	open, history, onlyChanges, reveal bool
	root, revision, parent, problem    string
	change                             gitChange
	staged                             bool
	selected                           int
	data                               *gitReviewData
	read                               *ui.EffectState[gitReviewData, int]
	apply                              *ui.EffectState[bool, int]
	log                                *ui.EffectState[[]gitCommitRecord, int]
	files                              *ui.EffectState[gitCommitFiles, int]
	commits                            []gitCommitRecord
	changes                            []gitChange
	logList, fileList                  ui.ListState
	diff                               codeeditor.Diff
}

func (m *editorApp) openGitReview(change gitChange, staged bool) {
	s := &gitReviewState{open: true, root: m.tools.git.snapshot.root, change: change, staged: staged}
	m.tools.git.review = s
	s.reload()
}

func (s *gitReviewState) reload() {
	root, change, staged, revision, parent := s.root, s.change, s.staged, s.revision, s.parent
	s.data, s.problem, s.selected = nil, "", 0
	s.diff = codeeditor.Diff{}
	s.read = ui.NewEffect[gitReviewData, int](func(ctx context.Context) (gitReviewData, error) {
		return readGitReview(ctx, root, change, staged, revision, parent)
	})
}

func (m *editorApp) openGitLog() {
	s := &gitReviewState{open: true, history: true, root: m.tools.git.snapshot.root}
	s.log = ui.NewEffect[[]gitCommitRecord, int](func(ctx context.Context) ([]gitCommitRecord, error) {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		return readGitLog(ctx, s.root)
	})
	m.tools.git.review = s
}

func (s *gitReviewState) selectCommit(record gitCommitRecord) {
	s.revision, s.problem, s.data, s.read = record.hash, "", nil, nil
	s.changes = nil
	s.fileList = ui.ListState{}
	root := s.root
	s.files = ui.NewEffect[gitCommitFiles, int](func(ctx context.Context) (gitCommitFiles, error) {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		return readGitCommitFiles(ctx, root, record.hash)
	})
}

func (m *editorApp) gitReviewDialog(c ui.C) {
	s := m.tools.git.review
	if s == nil {
		return
	}
	if s.log != nil {
		ui.Effect(c.Part("log"), s.log, 0)
		if records, err, done := s.log.Take(); done {
			s.log = nil
			if err != nil {
				s.problem = err.Error()
			} else {
				s.commits = records
				if len(records) > 0 {
					s.selectCommit(records[0])
					m.invalidator.Invalidate()
				}
			}
		}
	}
	if s.files != nil {
		ui.Effect(c.Part("files"), s.files, 0)
		if files, err, done := s.files.Take(); done {
			s.files = nil
			if err != nil {
				s.problem = err.Error()
			} else {
				s.changes, s.parent = files.changes, files.parent
				if len(s.changes) > 0 {
					s.change = s.changes[0]
					s.reload()
					m.invalidator.Invalidate()
				}
			}
		}
	}
	if s.read != nil {
		ui.Effect(c.Part("read"), s.read, 0)
		if data, err, done := s.read.Take(); done {
			s.read = nil
			if err != nil {
				s.problem = err.Error()
			} else {
				s.data, s.reveal = &data, true
			}
		}
	}
	if s.apply != nil {
		ui.Effect(c.Part("apply"), s.apply, 0)
		if _, err, done := s.apply.Take(); done {
			s.apply = nil
			if err != nil {
				s.problem = err.Error()
			} else {
				s.reload()
				m.startGit(gitRefresh, "", nil)
				m.invalidator.Invalidate()
			}
		}
	}
	busy := s.apply != nil || m.tools.git.request != nil
	title := "Git 差异"
	if s.history {
		title = "Git 提交历史 · 最近 100 次"
	}
	ui.ModalWith(c, &s.open, title, ui.ModalSpec{Width: min(1180, c.Bounds().Width-32), Height: min(780, c.Bounds().Height-40), NoClose: s.apply != nil}, func() {
		ui.Row(c).FillWidth().Grow(1).MinHeight(0).Gap(8).Children(func() {
			if s.history {
				m.gitLogSidebar(c.Part("history"), s)
			}
			ui.Column(c.Part("body")).Grow(1).MinWidth(0).FillHeight().Gap(6).Children(func() {
				if s.problem != "" {
					ui.Wrap(c, s.problem).FillWidth().Color(m.theme.Danger).FontSize(12)
				}
				if s.read != nil || s.log != nil || s.files != nil {
					ui.Text(c, "正在读取 Git…").Muted()
				}
				if s.data != nil {
					m.gitDiffBody(c.Part("diff-body"), s, busy)
				}
			})
		})
	}, func() {
		if !s.history && ui.Button(c, "刷新差异").Icon(refreshIcon).Ghost().Disabled(busy || s.read != nil).Clicked() {
			c.After(s.reload)
		}
		if ui.Button(c, "关闭").Ghost().Disabled(s.apply != nil).Clicked() {
			c.After(func() { s.open = false })
		}
	})
	if !s.open {
		c.After(func() { m.tools.git.review = nil })
	}
}

func (m *editorApp) gitLogSidebar(c ui.C, s *gitReviewState) {
	ui.Column(c).Width(230).FillHeight().Gap(6).Children(func() {
		ui.List(c.Part("commits"), &s.logList, len(s.commits), func(row ui.ListRow) {
			record := s.commits[row.Index]
			row.Element.Height(55).Children(func() {
				button := ui.Button(row.C, record.subject).Ghost().FillWidth().Height(29).Tooltip(record.hash)
				if s.revision == record.hash {
					button.Secondary()
				}
				if button.Clicked() {
					c.After(func() { s.selectCommit(record) })
				}
				ui.Text(row.C, record.author+" · "+record.at[:min(10, len(record.at))]).FontSize(11).Muted().Ellipsis()
			})
		}).FillWidth().Grow(1).MinHeight(0).Label("提交列表")
		ui.Text(c, "提交中的文件").Bold().FontSize(12)
		ui.List(c.Part("files"), &s.fileList, len(s.changes), func(row ui.ListRow) {
			change := s.changes[row.Index]
			row.Element.Height(30).Children(func() {
				b := ui.Button(row.C, change.status+"  "+change.path).Ghost().FillWidth().Height(28).Tooltip(change.path)
				if s.change.path == change.path {
					b.Secondary()
				}
				if b.Clicked() {
					c.After(func() { s.change = change; s.reload() })
				}
			})
		}).FillWidth().Grow(1).MinHeight(0).Label("提交文件列表")
	})
}

func (m *editorApp) gitDiffBody(c ui.C, s *gitReviewState, busy bool) {
	data := s.data
	ui.Text(c, gitReviewTitle(*data)).FontSize(12).Ellipsis().FillWidth()
	count := len(data.comparison.diff.blocks)
	if len(data.hunks) > 0 {
		count = len(data.hunks)
	}
	ui.Row(c.Part("toolbar")).FillWidth().Items(ui.Center).Gap(4).Children(func() {
		for _, step := range []struct {
			label string
			icon  gfx.Icon
			delta int
		}{{"上一个区别", historyUpIcon, -1}, {"下一个区别", historyDownIcon, 1}} {
			if ui.Button(c.Key(step.label), step.label).Icon(step.icon).IconOnly().Ghost().Size(28, 28).Tooltip(step.label).Disabled(count == 0).Clicked() {
				c.After(func() { s.selected = (s.selected + count + step.delta) % count; s.reveal = true })
			}
		}
		ui.Text(c, fmt.Sprintf("%d / %d", min(s.selected+1, count), count)).Muted().FontSize(12)
		ui.Slot(c).Grow(1)
		label, icon := "只看区别", collapseIcon
		if s.onlyChanges {
			label, icon = "展示全文件", expandIcon
		}
		if ui.Button(c.Part("context"), label).Icon(icon).IconOnly().Ghost().Size(28, 28).Tooltip(label).Clicked() {
			c.After(func() { s.onlyChanges = !s.onlyChanges; s.reveal = true })
		}
		if len(data.hunks) > 0 {
			action := "暂存此块"
			if data.staged {
				action = "取消暂存此块"
			}
			if ui.Button(c.Part("hunk"), action).Height(28).Disabled(busy).Clicked() {
				c.After(func() {
					selected, snapshot := s.selected, *data
					s.problem = ""
					s.apply = ui.NewEffect[bool, int](func(ctx context.Context) (bool, error) {
						err := applyGitHunk(ctx, snapshot, selected)
						return err == nil, err
					})
				})
			}
		}
	})
	result := data.comparison.diff.full
	var separators map[int]string
	if s.onlyChanges {
		result, separators = data.comparison.diff.changes, data.comparison.diff.separators
	}
	var rows components.CodeDiffHunk
	if count > 0 {
		if len(data.hunks) > 0 {
			rows = data.hunks[s.selected].rows(result)
		} else {
			rows = data.comparison.diff.blocks[s.selected].visibleRange(s.onlyChanges)
		}
	}
	s.diff.Snapshot = components.CodeDiffSnapshot{Result: result, Value: data.comparison.current.saved, ReadOnly: true,
		CenterGutters: true, Language: documentFormat(filepath.Base(data.path)), Highlight: m.highlightHistoryLine,
		RowSeparators: separators, ActiveRows: rows, FontSize: float32(m.editorPrefs.FontSize), TabWidth: m.editorPrefs.TabWidth,
		MonoFace: gfx.MonoFamily(m.editorPrefs.FontFamily), VerticalScrollbarID: gfx.Item(1), HorizontalScrollbarID: gfx.Item(2)}
	codeeditor.DiffView(c.Part("diff"), "Git 文件差异", &s.diff).FillWidth().Grow(1).MinHeight(0).DrawOver(func(d *ui.DrawContext) {
		if s.reveal && count > 0 {
			s.reveal = false
			s.diff.Code.RevealRow(max(0, rows.Start-4))
			d.Canvas.RequestFrame()
		}
	})
}
