package editor

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

const paletteFileLimit = 20000
const paletteResultLimit = 100

type navigationItem struct {
	workflow                    string
	label, detail, path, search string
	tabID                       int
	action                      editorShortcut
	disabled                    bool
}

type paletteFiles struct {
	paths   []string
	partial bool
}

type navigationPalette struct {
	open, commands, focus bool
	query                 ui.Field
	items, matches        []navigationItem
	lastQuery             string
	dirty                 bool
	list                  ui.ListState
	scan                  *ui.EffectState[paletteFiles, int]
	partial               bool
}

func (m *editorApp) openNavigationPalette(commands bool) {
	state := &navigationPalette{open: true, commands: commands, focus: true, dirty: true}
	m.palette = state
	m.fileMenu = false
	if commands {
		for _, action := range workflowActions {
			label := m.menuText(action.label)
			state.items = append(state.items, navigationItem{label: label, search: strings.ToLower(label + " " + action.label + " " + action.id), workflow: action.id, disabled: !m.workflowEnabled(action.id)})
		}
		for action := shortcutNewFile; action < editorShortcutCount; action++ {
			if action == shortcutSearchNewline {
				continue
			}
			spec := editorShortcutSpecs[action]
			label := m.menuText(spec.label)
			state.items = append(state.items, navigationItem{label: label, detail: m.keyBindings.chord(action).Label(),
				search: strings.ToLower(label + " " + spec.label + " " + spec.id), action: action, disabled: !m.navigationCommandEnabled(action)})
		}
		return
	}
	for _, tab := range m.tabs {
		state.items = append(state.items, navigationItem{label: tab.tabTitle(), detail: "已打开 · " + tab.doc.path,
			path: tab.doc.path, tabID: tab.id, search: strings.ToLower(tab.tabTitle() + " " + tab.doc.path)})
	}
	if m.store != nil {
		for _, recent := range m.store.history {
			if !recent.Directory {
				state.addPath(recent.Path, "最近 · ")
			}
		}
	}
	var roots []string
	if m.workspace != nil {
		for _, root := range m.workspace.roots {
			roots = append(roots, root.path)
		}
	}
	if len(roots) > 0 {
		state.scan = ui.NewEffect[paletteFiles, int](func(ctx context.Context) (paletteFiles, error) {
			ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			return scanPaletteFiles(ctx, roots), nil
		})
	}
}

func (m *editorApp) navigationCommandEnabled(action editorShortcut) bool {
	tab := m.active
	switch action {
	case shortcutNewWindow:
		return m.newWindow != nil
	case shortcutReopenTab:
		return len(m.closedTabs) > 0
	case shortcutSaveAll:
		return slices.ContainsFunc(m.tabs, func(t *editorTab) bool { return t.modified() })
	case shortcutRecentFiles:
		return m.store != nil
	case shortcutDirectorySearch, shortcutDirectoryReplace, shortcutNewFile, shortcutOpenFile, shortcutOpenLink, shortcutOpenProject, shortcutQuickOpen, shortcutCommandPalette,
		shortcutFiles, shortcutGit, shortcutGitCommit, shortcutTerminal, shortcutOpenSettings:
		return true
	}
	if tab == nil {
		return false
	}
	switch action {
	case shortcutFind, shortcutFindNext, shortcutFindPrevious, shortcutGotoLine:
		return tab.canFind()
	case shortcutReplace:
		return tab.canReplace()
	case shortcutSave, shortcutSaveAs:
		return !tab.readOnlyPreview()
	case shortcutSelectNext, shortcutSelectAllMatches, shortcutSplitSelection, shortcutCaretAbove, shortcutCaretBelow:
		return editableTextSource(tab) && tab.largeText == nil
	case shortcutDuplicateLine, shortcutDuplicateLineAbove, shortcutDeleteLine:
		return m.editorSplit != nil || editableTextSource(tab) || tab.largeText != nil && tab.largeText.editing != nil
	case shortcutComplete:
		return m.editorSplit != nil || editableTextSource(tab)
	case shortcutRevealProjectFile:
		return tab.doc.path != "" && m.projectRoot(tab.doc.path) != ""
	case shortcutOutline:
		return tab.format() == "Markdown"
	}
	return true
}

func scanPaletteFiles(ctx context.Context, roots []string) paletteFiles {
	result := paletteFiles{}
	visited := 0
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if ctx.Err() != nil || visited >= paletteFileLimit {
				result.partial = true
				return fs.SkipAll
			}
			visited++
			if err != nil {
				result.partial = true
				return nil
			}
			if entry.IsDir() {
				if path != root {
					switch entry.Name() {
					case ".git", ".hg", ".svn", "node_modules", ".dev":
						return fs.SkipDir
					}
				}
				return nil
			}
			if entry.Type().IsRegular() {
				result.paths = append(result.paths, path)
			}
			return nil
		})
		if err != nil {
			result.partial = true
		}
		if ctx.Err() != nil || visited >= paletteFileLimit {
			break
		}
	}
	return result
}

func (state *navigationPalette) addPath(path, prefix string) {
	for _, item := range state.items {
		if strings.EqualFold(item.path, path) {
			return
		}
	}
	state.items = append(state.items, navigationItem{label: filepath.Base(path), detail: prefix + path, path: path,
		search: strings.ToLower(filepath.Base(path) + " " + path)})
}

// Subsequence matching supports abbreviated filenames; contiguous basename matches win.
func paletteMatch(item navigationItem, query string) int {
	if query == "" {
		return 0
	}
	label := strings.ToLower(item.label)
	if label == query {
		return 0
	}
	if strings.HasPrefix(label, query) {
		return 1
	}
	if strings.Contains(label, query) {
		return 2
	}
	if strings.Contains(item.search, query) {
		return 3
	}
	remaining := item.search
	for _, character := range query {
		index := strings.IndexRune(remaining, character)
		if index < 0 {
			return -1
		}
		remaining = remaining[index+len(string(character)):]
	}
	return 4
}

func (state *navigationPalette) filter() {
	query := strings.ToLower(strings.TrimSpace(state.query.Text()))
	if !state.dirty && query == state.lastQuery {
		return
	}
	state.lastQuery, state.dirty = query, false
	state.matches = nil
	// Five stable buckets avoid sorting the entire project for each keystroke.
	buckets := [5][]navigationItem{}
	for _, item := range state.items {
		rank := paletteMatch(item, query)
		if rank >= 0 && len(buckets[rank]) < paletteResultLimit {
			buckets[rank] = append(buckets[rank], item)
		}
	}
	for _, bucket := range buckets {
		count := min(len(bucket), paletteResultLimit-len(state.matches))
		state.matches = append(state.matches, bucket[:count]...)
	}
	state.list.Select(0)
	state.list.ScrollIntoView(0)
}

func (m *editorApp) chooseNavigationItem(index int) {
	state := m.palette
	if state == nil || index < 0 || index >= len(state.matches) {
		return
	}
	item := state.matches[index]
	if item.disabled {
		return
	}
	m.palette = nil
	if state.commands {
		if item.workflow != "" {
			m.runWorkflow(item.workflow)
			return
		}
		m.runEditorShortcut(item.action)
		return
	}
	for _, tab := range m.tabs {
		if tab.id == item.tabID {
			m.activate(tab)
			return
		}
	}
	m.load(item.path)
}

func (m *editorApp) navigationPaletteDialog(c ui.C) {
	state := m.palette
	if state == nil {
		return
	}
	if state.scan != nil {
		ui.Effect(c.Part("project-scan"), state.scan, 0)
		if result, _, done := state.scan.Take(); done {
			// Deduplicate in linear time; scanning overlapping roots is allowed.
			seen := make(map[string]bool, len(state.items)+len(result.paths))
			for _, item := range state.items {
				seen[strings.ToLower(item.path)] = true
			}
			for _, path := range result.paths {
				key := strings.ToLower(path)
				if !seen[key] {
					seen[key] = true
					state.items = append(state.items, navigationItem{label: filepath.Base(path), detail: path, path: path,
						search: strings.ToLower(filepath.Base(path) + " " + path)})
				}
			}
			state.scan, state.partial, state.dirty = nil, result.partial, true
		}
	}
	state.filter()
	title, placeholder := uiText("快速打开文件"), uiText("输入文件名或路径片段")
	if state.commands {
		title, placeholder = uiText("命令面板"), uiText("搜索命令，例如：保存、替换、settings")
	}
	dismissed := ui.ModalWith(c, &state.open, m.menuText(title), ui.ModalSpec{Width: min(640, c.Bounds().Width-32)}, func() {
		input := ui.TextField(c.Part("query"), &state.query, m.menuText(title+"搜索"), m.menuText(placeholder)).FillWidth().Height(34).OnKey(func(event gfx.KeyEvent) gfx.EventResult {
			if event.Release || event.Modifiers != 0 {
				return gfx.EventResult{}
			}
			if _, composing := state.query.GetEditor().Composition(); composing {
				return gfx.EventResult{}
			}
			switch event.Key {
			case gfx.KeyUp, gfx.KeyDown:
				if len(state.matches) > 0 {
					direction := 1
					if event.Key == gfx.KeyUp {
						direction = -1
					}
					index := (max(0, state.list.Selected()) + direction + len(state.matches)) % len(state.matches)
					state.list.Select(index)
					state.list.ScrollIntoView(index)
				}
				return gfx.EventResult{Handled: true, Repaint: true}
			}
			return gfx.EventResult{}
		})
		if state.focus {
			input.AutoFocus()
			state.focus = false
		}
		if input.Submitted() {
			c.After(func() { m.chooseNavigationItem(state.list.Selected()) })
		}
		state.list.Label = func(index int) string { return state.matches[index].label }
		ui.List(c.Part("results"), &state.list, len(state.matches), func(row ui.ListRow) {
			item := state.matches[row.Index]
			box := row.Element.Height(44).PadX(8).Gap(8).Items(ui.Center).Radius(6)
			if row.Selected() {
				box.Background(m.theme.Selection)
			}
			box.Children(func() {
				icon := fileIcon
				if state.commands {
					icon = blankMenuIcon
				}
				ui.Icon(row.C, icon).Size(16, 16).Color(m.theme.MutedForeground)
				ui.Column(row.C).Grow(1).MinWidth(0).Children(func() {
					label := ui.Text(row.C, item.label).FontSize(13).FillWidth().Ellipsis()
					if item.disabled {
						label.Muted()
					}
					ui.Text(row.C, item.detail).FontSize(11).Muted().FillWidth().Ellipsis()
				})
				if item.disabled {
					ui.Text(row.C, m.menuText("当前不可用")).FontSize(11).Muted()
				}
			})
			if box.Clicked() {
				c.After(func() { m.chooseNavigationItem(row.Index) })
			}
		}).FillWidth().Height(min(352, max(44, float32(len(state.matches)*44)), max(88, c.Bounds().Height-200)))
		if index, submitted := state.list.Submitted(); submitted {
			c.After(func() { m.chooseNavigationItem(index) })
		}
		hint := fmt.Sprintf(uiText("%d 项 · ↑↓ 选择 · Enter 打开 · Esc 返回"), len(state.matches))
		if len(state.matches) == 0 {
			hint = uiText("没有匹配项")
		}
		if state.scan != nil {
			hint += uiText(" · 正在扫描项目…")
		}
		if state.partial {
			hint += uiText(" · 部分项目未列出，可用打开文件选择")
		}
		ui.Wrap(c, hint).FillWidth().Muted().FontSize(11)
	}, nil)
	if dismissed || !state.open {
		m.palette = nil
		m.focusActiveSource()
	}
}
