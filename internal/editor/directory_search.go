package editor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/text/find"
	"github.com/vivaui/viva/ui"
)

const directorySearchLimit = 5000
const directorySearchFileLimit = 64 << 20

type directorySearchHit struct {
	path, excerpt string
	line, column  int
	span          text.Span
}
type directorySearchResult struct {
	query          *find.Query
	hits           []directorySearchHit
	files, skipped int
	partial        bool
}
type directorySearchState struct {
	panelOpen                                                bool
	replaceMode                                              bool
	replacement                                              ui.Field
	plan                                                     *directoryReplacementPlan
	planRequest                                              *ui.EffectState[*directoryReplacementPlan, int]
	replaceJob                                               *directoryReplacementJob
	replacing                                                bool
	replaceIndex, replacedFiles                              int
	replaceProblems                                          []string
	open, focus, caseSensitive, wholeWord, regularExpression bool
	root, query, include, exclude                            ui.Field
	list                                                     ui.ListState
	result                                                   directorySearchResult
	problem                                                  string
	request                                                  *ui.EffectState[directorySearchResult, int]
	cancel                                                   context.CancelFunc
}

func (m *editorApp) openDirectorySearch(root string) {
	if m.directorySearch == nil {
		m.directorySearch = &directorySearchState{}
		m.directorySearch.include.SetText("*")
		m.directorySearch.exclude.SetText(".git;.hg;.svn;node_modules;.dev")
	}
	s := m.directorySearch
	s.open, s.focus, s.replaceMode = true, true, false
	m.tools.active = ""
	if root == "" {
		root = m.activeTextProject(m.active)
	}
	if root == "" && m.active != nil && m.active.doc.path != "" {
		root = filepath.Dir(m.active.doc.path)
	}
	if root != "" {
		s.root.SetText(root)
	}
	if m.active != nil && !m.active.readOnlyPreview() {
		p := m.active.source
		selection := p.text.Selection()
		value := p.text.String()[p.text.ByteOffset(selection.Start):p.text.ByteOffset(selection.End)]
		if len(value) > 0 && len(value) < 1024 && !strings.Contains(value, "\n") {
			s.query.SetText(value)
		}
	}
}

func directoryPatterns(value string) ([]string, error) {
	var patterns []string
	for _, pattern := range strings.Split(value, ";") {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if _, err := filepath.Match(pattern, "test"); err != nil {
			return nil, fmt.Errorf("文件过滤格式无效：%s", pattern)
		}
		patterns = append(patterns, strings.ToLower(pattern))
	}
	return patterns, nil
}

func matchesDirectoryPattern(name string, patterns []string) bool {
	for _, pattern := range patterns {
		if match, _ := filepath.Match(pattern, strings.ToLower(name)); match {
			return true
		}
	}
	return false
}

func searchDirectory(ctx context.Context, root string, query *find.Query, include, exclude []string) (directorySearchResult, error) {
	return searchDirectoryWithBuffers(ctx, root, query, include, exclude, nil)
}

func searchDirectoryWithBuffers(ctx context.Context, root string, query *find.Query, include, exclude []string, buffers map[string]string) (directorySearchResult, error) {
	result := directorySearchResult{query: query}
	info, err := os.Stat(root)
	if err != nil {
		return result, err
	}
	if !info.IsDir() {
		return result, errors.New("请选择一个目录")
	}
	visited := 0
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if ctx.Err() != nil || len(result.hits) >= directorySearchLimit || visited >= 100000 {
			result.partial = true
			return fs.SkipAll
		}
		visited++
		if walkErr != nil {
			result.skipped++
			return nil
		}
		if path != root && matchesDirectoryPattern(entry.Name(), exclude) {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.IsDir() || !entry.Type().IsRegular() || len(include) > 0 && !matchesDirectoryPattern(entry.Name(), include) {
			return nil
		}
		info, err := entry.Info()
		if err != nil || info.Size() > directorySearchFileLimit {
			result.skipped++
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			result.skipped++
			return nil
		}
		data, err := io.ReadAll(io.LimitReader(largeEditContextReader{ctx: ctx, Reader: file}, directorySearchFileLimit+1))
		file.Close()
		if err != nil || len(data) > directorySearchFileLimit {
			result.skipped++
			return nil
		}
		doc, err := decodeDocument(path, data)
		if err != nil {
			result.skipped++
			return nil
		}
		result.files++
		if buffer, ok := buffers[strings.ToLower(path)]; ok {
			doc.saved = buffer
		}
		spans, truncated := query.Matches(nil, doc.saved, directorySearchLimit-len(result.hits))
		result.partial = result.partial || truncated
		line, start := 1, 0
		for _, span := range spans {
			for next := strings.IndexByte(doc.saved[start:span.Start], '\n'); next >= 0; next = strings.IndexByte(doc.saved[start:span.Start], '\n') {
				start += next + 1
				line++
			}
			column := utf8.RuneCountInString(doc.saved[start:span.Start]) + 1
			item := newSearchResultLine(line, column, 0, int64(span.Start), doc.saved[start:span.Start], doc.saved[span.Start:], false)
			result.hits = append(result.hits, directorySearchHit{path: path, excerpt: item.excerpt, line: line, column: column, span: span})
		}
		return nil
	})
	return result, err
}

func (m *editorApp) startDirectorySearch() {
	s := m.directorySearch
	if s == nil || s.request != nil || s.planRequest != nil || s.replacing || s.replaceJob != nil {
		return
	}
	options := searchOptions{CaseSensitive: s.caseSensitive, WholeWord: s.wholeWord}
	if s.regularExpression {
		options.mode = searchRegexp
	}
	query, err := options.compile(s.query.Text())
	if err != nil {
		s.problem = err.Error()
		return
	}
	if s.query.Text() == "" {
		s.problem = "请输入查找内容"
		return
	}
	include, err := directoryPatterns(s.include.Text())
	if err != nil {
		s.problem = err.Error()
		return
	}
	exclude, err := directoryPatterns(s.exclude.Text())
	if err != nil {
		s.problem = err.Error()
		return
	}
	root := strings.TrimSpace(s.root.Text())
	buffers := map[string]string{}
	for _, tab := range m.tabs {
		if historyEditableTab(tab) && tab.doc.path != "" {
			if _, inside := aiRelativeTo(root, tab.doc.path); inside {
				if tab.format() == "JSON" {
					tab.updateResult()
				}
				buffers[strings.ToLower(tab.doc.path)] = tab.source.text.CommittedString()
			}
		}
	}
	s.problem = ""
	s.panelOpen, s.open = true, false
	s.plan = nil
	s.result = directorySearchResult{}
	s.list.Select(0)
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.request = ui.NewEffect[directorySearchResult, int](func(effectContext context.Context) (directorySearchResult, error) {
		stop := context.AfterFunc(effectContext, cancel)
		defer stop()
		defer cancel()
		return searchDirectoryWithBuffers(ctx, root, query, include, exclude, buffers)
	})
}

func (m *editorApp) chooseDirectoryHit(index int) {
	s := m.directorySearch
	if index < 0 || index >= len(s.result.hits) {
		return
	}
	hit := s.result.hits[index]
	query := s.result.query
	if query == nil {
		return
	}
	s.open = false
	m.loadTextFile(hit.path, "", func(tab *editorTab) {
		if tab == nil || tab.largeText != nil || tab.readOnlyPreview() {
			return
		}
		// Re-find against current content rather than applying stale disk offsets to a dirty editor.
		spans, _ := query.Matches(nil, tab.source.text.String(), directorySearchLimit)
		closest, distance := -1, int(^uint(0)>>1)
		for index, span := range spans {
			delta := span.Start - hit.span.Start
			if delta < 0 {
				delta = -delta
			}
			if delta < distance {
				closest, distance = index, delta
			}
		}
		if closest >= 0 {
			span := spans[closest]
			tab.source.text.SetSelection(text.Range{Start: tab.source.text.RuneIndex(span.Start), End: tab.source.text.RuneIndex(span.End)})
			tab.source.focus = true
		} else {
			m.status = "文件内容已变化，原匹配已不存在"
		}
	})
}

func (m *editorApp) directorySearchDialog(c ui.C) {
	s := m.directorySearch
	if s == nil {
		return
	}
	m.updateDirectoryReplacement(c.Part("replacement"))
	if s.request != nil {
		ui.Effect(c.Part("search"), s.request, 0)
		if result, err, done := s.request.Take(); done {
			s.result, s.request, s.cancel = result, nil, nil
			if err != nil {
				s.problem = err.Error()
			}
		}
	}
	if !s.open {
		return
	}
	busy := s.request != nil || s.planRequest != nil || s.replacing || s.replaceJob != nil
	title := "在目录中查找"
	if s.replaceMode {
		title = "在目录中替换"
	}
	dismissed := ui.ModalWith(c, &s.open, title, ui.ModalSpec{Width: min(850, c.Bounds().Width-32)}, func() {
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			ui.Row(c).FillWidth().Gap(6).Children(func() {
				ui.TextField(c.Part("root"), &s.root, "搜索目录", "目录路径").Grow(1).Height(32).Disabled(busy)
				if ui.Button(c, "选择目录…").Ghost().Disabled(busy).Clicked() {
					c.After(func() {
						paths, err := app.OpenDirectories(app.OpenDirectoryOptions{Title: "搜索目录"})
						if err == nil && len(paths) > 0 {
							s.root.SetText(paths[0])
						}
					})
				}
			})
			field := ui.TextField(c.Part("query"), &s.query, "目录查找内容", "查找内容").FillWidth().Height(32).Disabled(busy)
			ui.TextField(c.Part("replacement"), &s.replacement, "目录替换内容", "替换为（可留空；正则支持 $1）").FillWidth().Height(32).Disabled(busy)
			if s.focus {
				field.AutoFocus()
				s.focus = false
			}
			if field.Submitted() {
				c.After(m.startDirectorySearch)
			}
			ui.Row(c).FillWidth().Gap(8).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
					ui.Text(c, "包含文件").FontSize(11).Muted()
					ui.TextField(c.Part("include"), &s.include, "包含文件", "如 *.go;*.md").FillWidth().Height(32).Disabled(busy)
				})
				ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
					ui.Text(c, "排除文件或目录").FontSize(11).Muted()
					ui.TextField(c.Part("exclude"), &s.exclude, "排除文件或目录", "用分号分隔名称或通配符").FillWidth().Height(32).Disabled(busy)
				})
			})
			ui.Row(c).FillWidth().Gap(12).Items(ui.Center).Children(func() {
				ui.Checkbox(c, &s.caseSensitive, "区分大小写").Disabled(busy)
				ui.Checkbox(c, &s.wholeWord, "整词").Disabled(busy)
				ui.Checkbox(c, &s.regularExpression, "正则").Disabled(busy)
				ui.Slot(c).Grow(1)
				if busy {
					if ui.Button(c, "停止搜索").Clicked() && s.cancel != nil {
						s.cancel()
					}
				} else if ui.Button(c, "搜索目录").Clicked() {
					c.After(m.startDirectorySearch)
				}
			})
		})
		s.list.Label = func(index int) string {
			hit := s.result.hits[index]
			return fmt.Sprintf("%s:%d:%d %s", filepath.Base(hit.path), hit.line, hit.column, hit.excerpt)
		}
		ui.List(c.Part("results"), &s.list, len(s.result.hits), func(row ui.ListRow) {
			hit := s.result.hits[row.Index]
			box := row.Element.Height(44).PadX(8)
			if row.Selected() {
				box.Background(m.theme.Selection)
			}
			box.Children(func() {
				ui.Column(row.C).FillWidth().Children(func() {
					ui.Text(row.C, fmt.Sprintf("%s:%d:%d", hit.path, hit.line, hit.column)).FontSize(11).Muted().FillWidth().Ellipsis()
					ui.Text(row.C, hit.excerpt).FontSize(13).FillWidth().Ellipsis()
				})
			})
			if box.Clicked() {
				c.After(func() { m.chooseDirectoryHit(row.Index) })
			}
		}).FillWidth().Height(min(340, max(88, c.Bounds().Height-400)))
		if index, submitted := s.list.Submitted(); submitted {
			c.After(func() { m.chooseDirectoryHit(index) })
		}
		message := fmt.Sprintf("%d 个匹配 · 已搜索 %d 个文件 · 跳过 %d 个不可读取、二进制或超过 64 MiB 的文件", len(s.result.hits), s.result.files, s.result.skipped)
		if busy {
			message = "正在搜索，可停止；已打开文件使用当前编辑内容。"
		}
		if s.result.partial {
			message += " · 结果未全部列出，请缩小范围或继续搜索"
		}
		if s.problem != "" {
			message = s.problem
		}
		ui.Wrap(c, message).FillWidth().Muted().FontSize(11)
	}, nil)
	if dismissed {
		s.open = false
		if s.cancel != nil {
			s.cancel()
		}
		m.focusActiveSource()
	}
}
