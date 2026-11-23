package editor

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/text/find"
	"github.com/vivaui/viva/ui"
)

type recentPath struct {
	Path      string
	Directory bool
	OpenedAt  time.Time
}
type paneSession struct {
	Content   string
	Selection text.Range
	Caret     int
	Scroll    gfx.Point
	Wrap      bool `json:",omitempty"` // 旧版会话中的折行状态
	Collapsed map[int]bool
}
type tabSession struct {
	Name, Path, Saved, EOL     string
	DisplayName                string `json:",omitempty"`
	LanguageOverride           string `json:",omitempty"`
	SavedMatchesSource         bool   `json:",omitempty"`
	BOM                        bool
	DiskHash                   [32]byte
	Picture                    bool
	Office                     *officeSession `json:",omitempty"`
	Pinned                     bool
	GroupID                    int `json:",omitempty"`
	Source, Comparison, Result paneSession
	SplitPos, SplitRatio       float32
	JSONAction                 string
	JSONIndent                 int   `json:",omitempty"` // 旧版会话中的 JSON 缩进
	JSONIndentOverride         int   `json:",omitempty"`
	WrapOverride               *bool `json:",omitempty"`
	JSONCompare                bool
	JSONWrapSet                bool    `json:",omitempty"`
	JSONResultKind             string  `json:",omitempty"`
	JSONResultAction           string  `json:",omitempty"`
	JSONDiffRatio              float32 `json:",omitempty"`
	MarkdownMode, HTMLMode     string
	MarkdownOffset             float32
	HTMLScroll                 gfx.Point
	OutlineCollapsed           map[int]bool
	SyncScroll                 bool
	ImageZoom                  float32
	ImagePan                   gfx.Point
	SearchQuery                string
	SearchOptions              find.Options
	SearchEscapes              bool `json:",omitempty"`
	SearchOpen, SearchWrap     bool
	SearchBounds               gfx.Rect
}
type projectSession struct {
	Path            string
	ExpandedFolders []string `json:",omitempty"`
}

type windowSession struct {
	Tabs                          []tabSession
	TabGroups                     []tabGroup `json:",omitempty"`
	Active                        int
	Light, ShowFiles, ShowOutline bool
	FilesWidth                    float32
	OutlineRatio                  float32          // 保存编辑区所占比例，兼容右侧大纲的旧版会话。
	AIEditorRatio                 float32          `json:",omitempty"`
	AIPromptRatio                 float32          `json:",omitempty"`
	AI                            *aiWindowSession `json:",omitempty"`
	Projects                      []projectSession `json:",omitempty"`
	Workspace                     string           `json:",omitempty"` // 旧版单文件夹会话
	ExpandedFolders               []string         `json:",omitempty"`
	Frame                         gfx.Rect
	Maximized                     bool
}

const editorSessionVersion = 1

type savedSession struct {
	Version int
	Windows []windowSession
	History []recentPath
}
type sessionStore struct {
	directory           string
	windows             []*editorApp
	history             []recentPath
	mu                  sync.Mutex
	lastHash            [32]byte
	hasLast             bool
	recovered           bool
	lastSnapshot        savedSession
	backgroundSaving    bool
	generation, written uint64
}

func editorDataDirectory() (string, error) {
	if configured := os.Getenv("VIVA_EDITOR_DATA_DIR"); configured != "" {
		return filepath.Abs(configured)
	}
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(executable), "data"), nil
}

func openSessionStore(directory string) (*sessionStore, error) {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	store := &sessionStore{directory: directory}
	path := filepath.Join(directory, "session.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, err
	}
	saved, err := decodeEditorSession(data)
	recovered := false
	if err != nil {
		backup, backupErr := os.ReadFile(path + ".bak")
		if backupErr == nil {
			saved, backupErr = decodeEditorSession(backup)
		}
		if backupErr != nil {
			return nil, fmt.Errorf("会话记录无法读取，原文件已保留：%s：%w", path, err)
		}
		data, recovered = backup, true
	}
	store.lastHash, store.hasLast, store.recovered = sha256.Sum256(data), true, recovered
	store.lastSnapshot = saved
	store.history = saved.History
	if len(store.history) > 30 {
		store.history = store.history[:30]
	}
	preferences, _ := readApplicationPreferences(directory)
	for _, state := range saved.Windows {
		m := newEditorApp()
		m.editorPrefs = preferences.Editor
		m.keyBindings = preferences.Shortcuts
		m.store, m.dataDirectory = store, directory
		m.restoreSession(state)
		if recovered {
			m.status = "主会话记录损坏，已从上一份备份恢复"
		}
		store.windows = append(store.windows, m)
	}
	return store, nil
}

func decodeEditorSession(data []byte) (savedSession, error) {
	var saved savedSession
	if err := json.Unmarshal(data, &saved); err != nil {
		return savedSession{}, err
	}
	if err := saved.validateVersion(); err != nil {
		return savedSession{}, err
	}
	return saved, nil
}

func (saved savedSession) validateVersion() error {
	if saved.Version != editorSessionVersion {
		return fmt.Errorf("不支持的会话版本：%d", saved.Version)
	}
	return nil
}

func (store *sessionStore) attach(m *editorApp) {
	m.store, m.dataDirectory = store, store.directory
	store.windows = append(store.windows, m)
}

func snapshotPane(p *editorPane) paneSession {
	scroll := gfx.Point{X: p.view.Code.ScrollX(), Y: p.view.Code.ScrollY()}
	if p.restoreScroll != nil {
		scroll = *p.restoreScroll
	}
	return paneSession{Content: p.text.CommittedString(), Selection: p.text.Selection(), Caret: p.text.Caret(),
		Scroll: scroll, Collapsed: maps.Clone(p.collapsed)}
}

func (m *editorApp) snapshotSession() windowSession {
	if m.closedSession != nil {
		return *m.closedSession
	}
	return m.snapshotWorkspace(m.tabs, true)
}

func (m *editorApp) snapshotWorkspace(tabs []*editorTab, includeAI bool) windowSession {
	state := windowSession{Active: slices.Index(tabs, m.active), Light: m.light, ShowFiles: m.showFiles, ShowOutline: m.showOutline,
		FilesWidth: m.filesSplit.Pos, OutlineRatio: 1 - m.outlineSplit.Ratio,
		AIEditorRatio: m.aiEditorSplit.Ratio, AIPromptRatio: m.aiPromptSplit.Ratio, Frame: m.windowFrame, Maximized: m.maximized}
	if includeAI {
		state.AI = m.snapshotAI()
	}
	if m.workspace != nil {
		for _, root := range m.workspace.roots {
			state.Projects = append(state.Projects, projectSession{Path: root.path, ExpandedFolders: expandedProjectFolders(root)})
		}
	}
	var previousGroup *tabGroup
	for _, tab := range tabs {
		if tab.format() == "JSON" && tab.resultReady {
			tab.updateResult()
		}
		item := tabSession{Name: tab.name, DisplayName: tab.displayName, LanguageOverride: tab.languageOverride, Path: tab.doc.path, Saved: tab.doc.saved, EOL: tab.doc.eol, BOM: tab.doc.bom, DiskHash: tab.doc.diskHash,
			Picture: tab.picture != nil, Pinned: tab.pinned, Source: snapshotPane(tab.source), Comparison: snapshotPane(tab.comparison), Result: snapshotPane(tab.result),
			SplitPos: tab.split.Pos, SplitRatio: tab.split.Ratio, JSONAction: tab.jsonAction, JSONCompare: tab.jsonCompare, JSONDiffRatio: tab.diffSplit.Ratio,
			MarkdownMode: tab.markdownMode, HTMLMode: tab.htmlMode, MarkdownOffset: tab.markdown.Offset, HTMLScroll: tab.html.scroll, OutlineCollapsed: maps.Clone(tab.outlineCollapsed), SyncScroll: tab.scroll.enabled,
			SearchQuery: tab.search.query.CommittedString(), SearchOptions: tab.search.options.findOptions(), SearchEscapes: tab.search.options.mode == searchEscapes, SearchOpen: tab.search.open, SearchWrap: tab.search.wrap, SearchBounds: tab.search.bounds}
		if tab.indentOverride {
			item.JSONIndentOverride = tab.jsonIndent
		}
		if tab.wrapOverride {
			wrap := tab.source.view.Snapshot.Wrap
			item.WrapOverride = &wrap
		}
		if tab.group != nil {
			item.GroupID = tab.group.ID
			if tab.group != previousGroup {
				state.TabGroups = append(state.TabGroups, tabGroup{ID: tab.group.ID, Name: tab.group.Name, Color: tab.group.Color, Collapsed: tab.group.Collapsed, Pinned: tab.group.Pinned})
			}
		}
		previousGroup = tab.group
		if item.Saved != "" && item.Saved == item.Source.Content {
			item.Saved, item.SavedMatchesSource = "", true
		}
		if tab.hasResultDraft() {
			item.JSONResultKind, item.JSONResultAction = tab.resultKind, tab.resultAction
			if item.JSONResultKind == "" {
				item.JSONResultKind = "JSON"
			}
		} else if tab.format() == "JSON" {
			item.Result.Content = ""
		}
		if tab.office != nil {
			item.Office = tab.office.snapshotSession()
		}
		if tab.picture != nil {
			item.ImageZoom, item.ImagePan = tab.picture.zoom, tab.picture.pan
		}
		state.Tabs = append(state.Tabs, item)
	}
	return state
}

func restorePane(p *editorPane, state paneSession) {
	p.setResult(state.Content)
	selection := state.Selection
	if state.Caret == selection.Start {
		selection.Start, selection.End = selection.End, selection.Start
	}
	p.text.SetSelection(selection)
	p.updateFolds()
	if state.Collapsed != nil {
		p.collapsed = maps.Clone(state.Collapsed)
	}
	scroll := state.Scroll
	p.restoreScroll = &scroll
}

func (m *editorApp) restoreSession(state windowSession) {
	m.tabs, m.active = nil, nil
	m.setLightMode(state.Light)
	m.showFiles, m.showOutline = state.ShowFiles, state.ShowOutline
	m.filesSplit.Pos = state.FilesWidth
	if m.filesSplit.Pos < 210 {
		m.filesSplit.Pos = 252
	}
	if state.OutlineRatio > 0 && state.OutlineRatio < 1 {
		m.outlineSplit.Ratio = 1 - state.OutlineRatio
	}
	if state.AIEditorRatio > 0 && state.AIEditorRatio < 1 {
		m.aiEditorSplit.Ratio = state.AIEditorRatio
	}
	if state.AIPromptRatio > 0 && state.AIPromptRatio < 1 {
		m.aiPromptSplit.Ratio = state.AIPromptRatio
	}
	m.windowFrame, m.maximized = state.Frame, state.Maximized
	issues := 0
	groups := make(map[int]*tabGroup, len(state.TabGroups))
	for _, group := range state.TabGroups {
		groups[group.ID] = &group
		m.nextGroupID = max(m.nextGroupID, group.ID)
	}
	for _, item := range state.Tabs {
		if item.SavedMatchesSource {
			item.Saved = item.Source.Content
		}
		doc := document{path: item.Path, saved: item.Saved, eol: item.EOL, bom: item.BOM, diskHash: item.DiskHash}
		previewKind := previewFormat(item.Path)
		if item.Path != "" && previewKind == "" && !item.Picture && item.Office == nil && item.Source.Content == item.Saved {
			if current, err := readDocument(item.Path); err == nil {
				doc = current
				item.Source.Content = current.saved
			} else {
				issues++
			}
		}
		tab := m.addTab(doc, item.Name)
		tab.displayName = item.DisplayName
		if supportedSyntaxLanguage(item.LanguageOverride) {
			tab.languageOverride = item.LanguageOverride
		}
		if item.Office != nil {
			var err error
			if !item.Office.Dirty {
				var current document
				current, tab.office, err = readOffice(item.Path)
				if err == nil {
					tab.doc = current
				}
			}
			if tab.office == nil {
				tab.office, err = restoreOffice(*item.Office, tab.title())
			}
			if err != nil {
				issues++
				tab.office = &officeDocument{state: *item.Office, errorText: err.Error()}
			}
			tab.source.readOnly, tab.source.view.Snapshot.ReadOnly = true, true
		}
		tab.pinned = item.Pinned
		if item.GroupID != 0 {
			tab.group = groups[item.GroupID]
		}
		restorePane(tab.source, item.Source)
		tab.split = ui.SplitterState{Pos: item.SplitPos, Ratio: item.SplitRatio}
		if item.JSONAction != "" {
			tab.jsonAction = item.JSONAction
		}
		switch {
		case item.JSONIndentOverride == 2 || item.JSONIndentOverride == 4:
			tab.indentOverride = true
			tab.setJSONIndent(item.JSONIndentOverride)
		case item.JSONIndent == 2:
			tab.indentOverride = true
			tab.setJSONIndent(item.JSONIndent)
		}
		tab.jsonCompare = item.JSONCompare
		if item.JSONDiffRatio > 0 && item.JSONDiffRatio < 1 {
			tab.diffSplit.Ratio = item.JSONDiffRatio
		}
		tab.markdownMode, tab.htmlMode = item.MarkdownMode, item.HTMLMode
		if tab.markdownMode == "" {
			tab.markdownMode = "分屏"
		}
		if tab.htmlMode == "" {
			tab.htmlMode = "分屏"
		}
		tab.markdown.Offset = item.MarkdownOffset
		tab.html.scroll = item.HTMLScroll
		if item.OutlineCollapsed != nil {
			tab.outlineCollapsed = maps.Clone(item.OutlineCollapsed)
		}
		tab.scroll.enabled = item.SyncScroll
		restorePane(tab.comparison, item.Comparison)
		if item.JSONResultKind != "" {
			tab.resultDraft, tab.resultKind, tab.resultAction = true, item.JSONResultKind, item.JSONResultAction
			tab.result.view.Snapshot.Language = tab.resultKind
		} else if tab.format() == "JSON" {
			tab.updateResult()
			item.Result.Content = tab.result.text.String()
		}
		restorePane(tab.result, item.Result)
		tab.resultOutput = tab.result.text.CommittedString()
		isJSON := tab.format() == "JSON"
		tab.applyDefaultWrap(m.editorPrefs)
		if item.WrapOverride != nil {
			tab.wrapOverride = true
			tab.setWrap(*item.WrapOverride)
		} else if isJSON && item.JSONWrapSet && !item.Source.Wrap || !isJSON && item.Source.Wrap {
			// 旧版只记录实际显示状态，和原默认值不同的折行状态属于手动选择。
			tab.wrapOverride = true
			tab.setWrap(item.Source.Wrap)
		}
		if previewKind != "" {
			preview, err := readDocumentPreview(item.Path)
			if err != nil {
				issues++
				preview.issue = "原文件暂时无法读取：" + err.Error()
			}
			tab.preview = preview
			tab.source.readOnly, tab.source.view.Snapshot.ReadOnly = true, true
		}
		if item.Picture {
			picture, err := readImage(item.Path)
			if err != nil {
				issues++
				picture = &imageDocument{errorText: "原图片暂时无法读取：" + err.Error()}
			}
			tab.picture = picture
			picture.zoom, picture.pan = item.ImageZoom, item.ImagePan
			tab.source.readOnly, tab.source.view.Snapshot.ReadOnly = true, true
		}
		tab.search.query.Insert(item.SearchQuery)
		options := searchOptions{CaseSensitive: item.SearchOptions.CaseSensitive, WholeWord: item.SearchOptions.WholeWord}
		if item.SearchOptions.Regexp {
			options.mode = searchRegexp
		} else if item.SearchEscapes {
			options.mode = searchEscapes
		}
		tab.search.options, tab.search.wrap, tab.search.bounds = options, item.SearchWrap, item.SearchBounds
		tab.search.open = item.SearchOpen && !tab.readOnlyPreview() && item.Office == nil
		tab.search.refresh()
	}
	if state.Active >= 0 && len(m.tabs) > 0 {
		m.activate(m.tabs[min(state.Active, len(m.tabs)-1)])
	} else {
		m.activate(m.visibleTabNear(0))
	}
	if m.active != nil && m.active.search.open {
		m.active.search.focus = true
		m.active.source.focus = false
	}
	issues += m.restoreProjects(state)
	m.restoreAI(state.AI)
	m.status = "已恢复上次会话"
	if issues > 0 {
		m.status = fmt.Sprintf("已恢复会话；%d 个文件或目录暂时无法读取，文本副本已保留", issues)
	}
}

func (m *editorApp) restoreProjects(state windowSession) int {
	projects := state.Projects
	if len(projects) == 0 && state.Workspace != "" {
		expanded := state.ExpandedFolders
		if len(expanded) == 0 {
			expanded = []string{state.Workspace}
		}
		projects = []projectSession{{Path: state.Workspace, ExpandedFolders: expanded}}
	}
	m.workspace = nil
	if len(projects) == 0 {
		return 0
	}
	tree := newFolderTree()
	issues := 0
	for _, project := range projects {
		path, err := filepath.Abs(project.Path)
		if err != nil || project.Path == "" {
			issues++
			continue
		}
		if tree.project(path) != nil {
			continue
		}
		root := tree.newProject(path)
		tree.roots = append(tree.roots, root)
		if err := tree.restoreExpanded(root, project.ExpandedFolders); err != nil {
			issues++
		}
	}
	if len(tree.roots) > 0 {
		tree.flatten()
		m.workspace = tree
	}
	return issues
}

func (store *sessionStore) capture() (savedSession, uint64) {
	store.generation++
	saved := savedSession{Version: editorSessionVersion, History: append([]recentPath(nil), store.history...)}
	for _, m := range store.windows {
		saved.Windows = append(saved.Windows, m.snapshotSession())
	}
	return saved, store.generation
}

func (store *sessionStore) write(saved savedSession, generation uint64) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if generation < store.written {
		return nil
	}
	// 文本快照是不可变字符串；未变化时直接比较引用与布局，避免每五秒重新编码整份文档。
	if store.hasLast && !store.recovered && reflect.DeepEqual(saved, store.lastSnapshot) {
		store.written = generation
		return nil
	}
	data, err := json.Marshal(saved)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(data)
	if store.hasLast && !store.recovered && digest == store.lastHash {
		store.lastSnapshot, store.written = saved, generation
		return nil
	}
	path := filepath.Join(store.directory, "session.json")
	if store.hasLast && !store.recovered {
		if err = backupSessionFile(path, store.lastHash); err != nil {
			return err
		}
	}
	if err = writeFile(path, data); err != nil {
		return err
	}
	store.lastHash, store.hasLast, store.recovered = digest, true, false
	store.lastSnapshot, store.written = saved, generation
	return nil
}

// 流式备份已验证的上一份会话，常驻内存中不再保留整份 JSON 副本。
func backupSessionFile(path string, expected [32]byte) error {
	input, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.CreateTemp(filepath.Dir(path), ".viva-backup-*")
	if err != nil {
		return err
	}
	temp := output.Name()
	defer os.Remove(temp)
	digest := sha256.New()
	if _, err = io.Copy(io.MultiWriter(output, digest), input); err != nil {
		output.Close()
		return err
	}
	var actual [32]byte
	copy(actual[:], digest.Sum(nil))
	if actual != expected {
		output.Close()
		// 保留上一份有效备份，用当前内存快照修复被外部修改的主记录。
		return nil
	}
	if err = output.Sync(); err != nil {
		output.Close()
		return err
	}
	if err = output.Close(); err != nil {
		return err
	}
	return os.Rename(temp, path+".bak")
}

func (store *sessionStore) save() error {
	saved, sequence := store.capture()
	return store.write(saved, sequence)
}

func (m *editorApp) rememberPath(path string, directory bool) {
	if m.store == nil {
		return
	}
	path = filepath.Clean(path)
	m.store.history = slices.DeleteFunc(m.store.history, func(item recentPath) bool { return strings.EqualFold(item.Path, path) })
	m.store.history = append([]recentPath{{Path: path, Directory: directory, OpenedAt: time.Now()}}, m.store.history...)
	if len(m.store.history) > 30 {
		m.store.history = m.store.history[:30]
	}
}
