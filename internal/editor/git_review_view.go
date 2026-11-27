package editor

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/vivaui/viva/app"
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
	wholeWorking                       bool
	selected                           int
	data                               *gitReviewData
	read                               *ui.EffectState[gitReviewData, int]
	apply                              *ui.EffectState[bool, int]
	log                                *ui.EffectState[gitLogPage, int]
	files                              *ui.EffectState[gitCommitFiles, int]
	commits                            []gitCommitRecord
	changes                            []gitChange
	logList, fileList                  ui.ListState
	diff                               codeeditor.Diff
	logQuery                           gitLogQuery
	logSearch, logAuthor               ui.Field
	logReferences                      []string
	logHasMore, logDiff                bool
	logDate                            string
	logSplit                           ui.SplitterState
	logScope                           string
	logDirectory                       bool
	window                             *app.Window
	invalidator                        app.Invalidator
}

func (m *editorApp) openGitReview(change gitChange, staged bool) {
	s := &gitReviewState{open: true, root: m.tools.git.snapshot.root, change: change, staged: staged}
	m.tools.git.review = s
	s.reload()
}

func (m *editorApp) openGitWorkingReview(change gitChange) {
	s := &gitReviewState{open: true, root: m.tools.git.snapshot.root, change: change, wholeWorking: true}
	m.tools.git.review = s
	s.reload()
}

func (s *gitReviewState) reload() {
	root, change, staged, revision, parent := s.root, s.change, s.staged, s.revision, s.parent
	wholeWorking := s.wholeWorking
	s.data, s.problem, s.selected = nil, "", 0
	s.diff = codeeditor.Diff{}
	s.read = ui.NewEffect[gitReviewData, int](func(ctx context.Context) (gitReviewData, error) {
		if wholeWorking {
			return readGitWorkingReview(ctx, root, change)
		}
		return readGitReview(ctx, root, change, staged, revision, parent)
	})
}

func (m *editorApp) openGitLog() {
	m.openGitLogAt(m.tools.git.snapshot.root, "", true)
}

func (s *gitReviewState) loadLog(more bool) {
	s.problem = ""
	if more {
		s.logQuery.offset += gitLogPageSize
	} else {
		s.logQuery.offset = 0
		s.logQuery.text, s.logQuery.author = strings.TrimSpace(s.logSearch.Text()), strings.TrimSpace(s.logAuthor.Text())
		s.commits, s.changes, s.data, s.read, s.files = nil, nil, nil, nil, nil
		s.logList, s.fileList = ui.ListState{}, ui.ListState{}
		s.revision, s.problem, s.change, s.logDiff = "", "", gitChange{}, false
	}
	query, root := s.logQuery, s.root
	query.scope = s.logScope
	s.log = ui.NewEffect[gitLogPage, int](func(ctx context.Context) (gitLogPage, error) {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		return readGitLogPage(ctx, root, query)
	})
}

func (s *gitReviewState) selectCommit(record gitCommitRecord) {
	s.revision, s.problem, s.data, s.read = record.hash, "", nil, nil
	s.changes, s.change, s.logDiff = nil, gitChange{}, false
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
	m.updateGitReview(c, s, m.invalidator.Invalidate)
	m.gitReviewModal(c, s)
}

func (m *editorApp) updateGitReview(c ui.C, s *gitReviewState, invalidate func()) {
	if s.log != nil {
		ui.Effect(c.Part("log"), s.log, 0)
		if page, err, done := s.log.Take(); done {
			s.log = nil
			if err != nil {
				s.problem = err.Error()
				if s.logQuery.offset > 0 {
					s.logQuery.offset -= gitLogPageSize
				}
			} else {
				s.logHasMore = page.hasMore
				if page.root != "" {
					s.root = page.root
				}
				if s.logQuery.offset == 0 {
					s.logReferences = page.references
				}
				for _, record := range page.commits {
					if !slices.ContainsFunc(s.commits, func(existing gitCommitRecord) bool { return existing.hash == record.hash }) {
						s.commits = append(s.commits, record)
					}
				}
				if s.revision == "" && len(s.commits) > 0 {
					s.selectCommit(s.commits[0])
					invalidate()
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
				if s.logScope != "" {
					s.changes = slices.DeleteFunc(s.changes, func(change gitChange) bool {
						return !gitChangeInScope(s.root, s.logScope, s.logDirectory, change)
					})
				}
				if len(s.changes) > 0 {
					s.change = s.changes[0]
					invalidate()
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
}

func (m *editorApp) gitReviewModal(c ui.C, s *gitReviewState) {
	busy := s.apply != nil || m.tools.git.request != nil
	closeReview := func() {
		if m.tools.git.review == s {
			m.tools.git.review = nil
			m.invalidator.Invalidate()
		}
	}
	title := uiText("Git 差异")
	if s.history {
		title = uiText("Git 提交记录") + " · " + filepath.Base(s.root)
	}
	ui.ModalWith(c, &s.open, title, ui.ModalSpec{Width: min(1560, c.Bounds().Width-24), Height: min(940, c.Bounds().Height-24), NoClose: s.apply != nil}, func() {
		m.gitReviewBody(c, s, busy)
	}, func() {
		if !s.history && ui.Button(c.Part("refresh-diff"), uiText("刷新差异")).Icon(refreshIcon).Ghost().Disabled(busy || s.read != nil).Clicked() {
			c.After(s.reload)
		}
		if ui.Button(c.Part("close-git-review"), uiText("关闭")).Ghost().Disabled(s.apply != nil).Clicked() {
			c.After(closeReview)
		}
	})
	if !s.open {
		c.After(closeReview)
	}
}

func (m *editorApp) gitReviewBody(c ui.C, s *gitReviewState, busy bool) {
	if s.history && !s.logDiff {
		m.gitLogBrowser(c.Part("history"), s)
		return
	}
	if s.history && ui.Button(c.Part("return-to-log"), uiText("返回提交记录")).Icon(chevronLeftIcon).Ghost().Height(30).Clicked() {
		c.After(func() { s.logDiff, s.data, s.read = false, nil, nil })
	}
	ui.Row(c).FillWidth().Grow(1).MinHeight(0).Gap(8).Children(func() {
		ui.Column(c.Part("body")).Grow(1).MinWidth(0).FillHeight().Gap(6).Children(func() {
			if s.problem != "" {
				ui.Wrap(c, s.problem).FillWidth().Color(m.theme.Danger).FontSize(12)
			}
			if s.read != nil || s.log != nil || s.files != nil {
				ui.Text(c, uiText("正在读取 Git…")).Muted()
			}
			if s.data != nil {
				m.gitDiffBody(c.Part("diff-body"), s, busy)
			}
		})
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
			if ui.Button(c.Key(step.label), uiText(step.label)).Icon(step.icon).IconOnly().Ghost().Size(28, 28).Tooltip(uiText(step.label)).Disabled(count == 0).Clicked() {
				c.After(func() { s.selected = (s.selected + count + step.delta) % count; s.reveal = true })
			}
		}
		ui.Text(c, fmt.Sprintf("%d / %d", min(s.selected+1, count), count)).Muted().FontSize(12)
		ui.Slot(c).Grow(1)
		label, icon := "只看区别", collapseIcon
		if s.onlyChanges {
			label, icon = "展示全文件", expandIcon
		}
		if ui.Button(c.Part("context"), uiText(label)).Icon(icon).IconOnly().Ghost().Size(28, 28).Tooltip(uiText(label)).Clicked() {
			c.After(func() { s.onlyChanges = !s.onlyChanges; s.reveal = true })
		}
		if len(data.hunks) > 0 {
			action := "暂存此块"
			if data.staged {
				action = "取消暂存此块"
			}
			if ui.Button(c.Part("hunk"), uiText(action)).Height(28).Disabled(busy).Clicked() {
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
	if data.revision != "" {
		ui.Row(c.Part("versions")).FillWidth().Gap(12).Items(ui.Center).Children(func() {
			ui.Text(c, uiText("提交前")+" · "+s.parent[:min(8, len(s.parent))]).Grow(1).MinWidth(0).Ellipsis().FontSize(12).Muted()
			ui.Text(c, uiText("此提交")+" · "+data.revision[:min(8, len(data.revision))]).Grow(1).MinWidth(0).Ellipsis().FontSize(12).Muted()
		})
	}
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
	codeeditor.DiffView(c.Part("diff"), uiText("Git 文件差异"), &s.diff).FillWidth().Grow(1).MinHeight(0).DrawOver(func(d *ui.DrawContext) {
		if s.reveal && count > 0 {
			s.reveal = false
			s.diff.Code.RevealRow(max(0, rows.Start-4))
			d.Canvas.RequestFrame()
		}
	})
}
