package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	uimarkdown "github.com/vivaui/viva/ui/markdown"
)

type editorTab struct {
	id                int
	name              string
	displayName       string
	languageOverride  string
	doc               document
	picture           *imageDocument
	preview           *documentPreview
	office            *officeDocument
	html              htmlPreview
	htmlMode          string
	source            *editorPane
	result            *editorPane
	pinned            bool
	tabHit            gfx.ID
	tabBounds         gfx.Rect
	group             *tabGroup
	split             ui.SplitterState
	jsonAction        string
	jsonIndent        int
	jsonMenu          bool
	jsonCompare       bool
	comparison        *editorPane
	diffMenus         [2]bool
	diffSplit         ui.SplitterState
	diffList          ui.ListState
	diffLeft          string
	diffRight         string
	diffReady         bool
	diffError         string
	differences       []jsonDifference
	resultErrorOffset int
	resultSource      string
	resultError       string
	resultReady       bool
	resultKind        string
	markdown          uimarkdown.State
	markdownMode      string
	headings          []heading
	outlineSource     string
	outlineCollapsed  map[int]bool
	previewHeight     float32
	previewJump       int
	search            documentSearch
	scroll            scrollSync
	detectedSource    string
	detectedFormat    string
}

func (t *editorTab) title() string {
	if t.doc.path != "" {
		return filepath.Base(t.doc.path)
	}
	return t.name
}

func (t *editorTab) tabTitle() string {
	if t.displayName != "" {
		return t.displayName
	}
	return t.title()
}

func (t *editorTab) format() string {
	if t.preview != nil {
		return t.preview.kind
	}
	if t.office != nil {
		return strings.ToUpper(t.office.state.Kind)
	}
	if t.picture != nil {
		return "图片"
	}
	name := t.doc.path
	if name == "" {
		name = t.name
	}
	if t.doc.path == "" && filepath.Ext(name) == "" {
		if t.jsonCompare {
			return "JSON"
		}
		return t.detectFormat()
	}
	return documentFormat(name)
}

func documentFormat(name string) string {
	extension := strings.ToLower(filepath.Ext(name))
	switch extension {
	case ".json":
		return "JSON"
	case ".jsonl", ".ndjson":
		return "JSON Lines"
	case ".csv":
		return "CSV"
	case ".tsv":
		return "TSV"
	case ".log":
		return "日志"
	case ".md", ".markdown":
		return "Markdown"
	case ".html", ".htm":
		return "HTML"
	case ".xml", ".svg", ".xsd", ".xsl", ".xslt", ".wsdl", ".xhtml", ".plist", ".csproj":
		return "XML"
	case ".yaml", ".yml":
		return "YAML"
	case ".properties":
		return "Properties"
	case ".ini":
		return "INI"
	case ".toml":
		return "TOML"
	case ".env":
		return "ENV"
	}
	if syntax := codeSyntaxByExtension[extension]; syntax != nil {
		return syntax.name
	}
	if strings.HasPrefix(strings.ToLower(filepath.Base(name)), ".env.") {
		return "ENV"
	}
	return "纯文本"
}

func (t *editorTab) readOnlyPreview() bool { return t.picture != nil || t.preview != nil }

func (t *editorTab) modified() bool {
	if t.office != nil {
		return t.office.state.Dirty
	}
	return !t.readOnlyPreview() && t.source.text.String() != t.doc.saved
}

type editorApp struct {
	theme         gfx.Theme
	light         bool
	tabs          []*editorTab
	active        *editorTab
	nextID        int
	status        string
	errorText     string
	showError     bool
	confirm       bool
	confirmFocus  bool
	confirmTab    *editorTab
	pending       func()
	workspace     *folderTree
	showFiles     bool
	showOutline   bool
	filesSplit    ui.SplitterState
	outlineSplit  ui.SplitterState
	aiEditorSplit ui.SplitterState
	aiPromptSplit ui.SplitterState
	newWindow     func()
	tabReveal     bool
	tabDrag       ui.DragState
	tabScrollAt   time.Duration
	tabEndHit     gfx.ID
	tabGhost      *editorTab
	groupGhost    *tabGroup
	tabGrabX      float32
	nextGroupID   int
	rename        tabRenameState
	fileMenu      bool
	languageMenu  bool
	results       searchResults
	ai            aiAssistant
	tools         projectTools
	browser       *ui.EffectState[string, int]
	store         *sessionStore
	dataDirectory string
	showHistory   bool
	webHost       app.WebHost
	webReady      bool
	invalidator   app.Invalidator
	post          func(func())
	windowFrame   gfx.Rect
	maximized     bool
	windowClosed  bool
	closedSession *windowSession
}

func newEditorApp() *editorApp {
	return &editorApp{
		theme: editorTheme(), status: "就绪", showOutline: true,
		filesSplit:    ui.SplitterState{Pos: 252},
		outlineSplit:  ui.SplitterState{Ratio: 0.80},
		aiEditorSplit: ui.SplitterState{Ratio: 0.66},
		aiPromptSplit: ui.SplitterState{Ratio: 0.76},
		tools:         projectTools{split: ui.SplitterState{Ratio: 0.70}},
	}
}

func (m *editorApp) addTab(doc document, name string) *editorTab {
	m.nextID++
	t := &editorTab{
		id: m.nextID, name: name, doc: doc, source: newEditorPane(doc.saved, false),
		result: newEditorPane("", true), split: ui.SplitterState{Ratio: 0.5},
		jsonAction: "格式化", jsonIndent: 4, comparison: newEditorPane("", false), diffSplit: ui.SplitterState{Ratio: 0.74},
		markdownMode: "分屏", htmlMode: "分屏", outlineCollapsed: map[int]bool{}, previewJump: -1,
	}
	t.source.light, t.result.light, t.comparison.light = m.light, m.light, m.light
	t.initSearch()
	t.scroll.enabled = true
	t.markdown.Style = uimarkdown.Style{Size: 16, LineHeight: 27}
	t.markdown.Scroll = func(event gfx.ScrollEvent) gfx.EventResult {
		t.markdown.Offset = max(0, min(t.markdown.Offset+event.StepY(48), max(0, t.markdown.Height()-t.previewHeight)))
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	m.tabs = append(m.tabs, t)
	m.activate(t)
	return t
}

func (m *editorApp) newDocument() {
	usedNames := make(map[string]bool, len(m.tabs))
	for _, tab := range m.tabs {
		usedNames[tab.title()] = true
		usedNames[tab.tabTitle()] = true
	}
	m.addTab(document{eol: "CRLF"}, nextUntitledName(usedNames))
	m.status = "新文档"
}

func nextUntitledName(usedNames map[string]bool) string {
	for number := 1; ; number++ {
		name := fmt.Sprintf("未命名-%d", number)
		if !usedNames[name] {
			return name
		}
	}
}

func (m *editorApp) activate(t *editorTab) {
	m.languageMenu = false
	// 切换时取消尚未提交的组合，避免把拼音音节当成正文存入旧标签。
	if m.active != nil && m.active != t {
		m.active.source.text.CancelComposition()
		m.active.comparison.text.CancelComposition()
		m.active.search.query.CancelComposition()
	}
	m.active = t
	m.tabReveal = t != nil
	if t == nil {
		return
	}
	if t.group != nil {
		t.group.Collapsed = false
	}
	if t.office != nil && m.webReady {
		m.openOffice(t)
	}
	if t.preview != nil && t.preview.issue == "" && m.webReady {
		m.openDocumentPreview(t)
	}
	t.source.focus = t.office == nil && !t.readOnlyPreview()
	m.queueHTMLPreview(t)
}

func (m *editorApp) focusActiveSource() {
	if t := m.active; t != nil {
		t.source.focus = t.markdownMode != "预览" && !t.readOnlyPreview()
	}
}

func (m *editorApp) fail(err error) { m.errorText, m.showError = err.Error(), true }

func (m *editorApp) requestTab(t *editorTab, action func()) {
	if !t.modified() {
		action()
		return
	}
	m.activate(t)
	m.confirmTab, m.pending, m.confirm, m.confirmFocus = t, action, true, true
}

func (m *editorApp) closeTab(t *editorTab) {
	if t.isPinned() {
		m.status = t.pinnedCloseHint()
		return
	}
	m.closeTabs([]*editorTab{t})
}

// tabs is a copy of the selected range, so removing a tab does not move the remaining targets.
func (m *editorApp) closeTabs(tabs []*editorTab) {
	for index, tab := range tabs {
		if tab.isPinned() || !slices.Contains(m.tabs, tab) {
			continue
		}
		waitingForOffice := tab.office != nil && tab.office.view != nil && tab.office.ready
		if !waitingForOffice && !tab.modified() {
			m.removeTab(tab)
			continue
		}
		proceed := func() {
			m.removeTab(tab)
			m.closeTabs(tabs[index+1:])
		}
		if waitingForOffice {
			tab.office.flush(func(err error) {
				if tab.isPinned() || !slices.Contains(m.tabs, tab) {
					m.closeTabs(tabs[index+1:])
					return
				}
				if err != nil {
					m.fail(err)
					return
				}
				m.requestTab(tab, proceed)
			})
		} else {
			m.requestTab(tab, proceed)
		}
		return
	}
}

func (m *editorApp) removeTab(t *editorTab) {
	if t.isPinned() {
		return
	}
	index := slices.Index(m.tabs, t)
	if index < 0 {
		return
	}
	t.html.close()
	t.office.close()
	t.preview.close()
	m.tabs = slices.Delete(m.tabs, index, index+1)
	m.results.removeTab(t)
	m.ai.attachments = slices.DeleteFunc(m.ai.attachments, func(item aiAttachment) bool { return item.tabID == t.id })
	if m.active == t {
		next := m.visibleTabNear(index)
		if next == nil && len(m.tabs) > 0 {
			next = m.tabs[min(index, len(m.tabs)-1)]
		}
		m.activate(next)
	}
	if len(m.tabs) == 0 {
		m.results = searchResults{}
		m.status = "就绪"
	}
}

func (m *editorApp) requestClose(allow func()) { m.flushOfficeTabs(0, func() { m.finishClose(allow) }) }

func (m *editorApp) finishClose(allow func()) {
	if m.store != nil {
		if err := m.store.save(); err != nil {
			m.fail(fmt.Errorf("无法保存本次会话，窗口保持打开：%w", err))
			return
		}
		state := m.snapshotSession()
		m.closedSession = &state
		for _, t := range m.tabs {
			t.html.close()
			t.office.close()
			t.preview.close()
		}
		allow()
		return
	}
	for _, t := range m.tabs {
		if t.modified() {
			m.requestTab(t, func() {
				// 已明确选择放弃或保存这一份，继续询问其他有改动的标签。
				index := slices.Index(m.tabs, t)
				if index >= 0 {
					t.html.close()
					t.office.close()
					t.preview.close()
					m.results.removeTab(t)
					m.ai.attachments = slices.DeleteFunc(m.ai.attachments, func(item aiAttachment) bool { return item.tabID == t.id })
					m.tabs = slices.Delete(m.tabs, index, index+1)
				}
				m.activate(m.visibleTabNear(0))
				m.requestClose(allow)
			})
			return
		}
	}
	allow()
}

func (m *editorApp) open() {
	paths, err := app.OpenFiles(app.OpenFileOptions{
		Title: "打开文件", AllowMultiple: true,
		Filters: []app.FileFilter{{Name: "所有文件", Pattern: "*.*"}, {Name: "代码与配置", Pattern: "*.xml;*.json;*.md;*.yaml;*.yml;*.properties;*.toml;*.ini;*.env;.env.*;*.go;*.java;*.txt;*.html;*.htm;*.css;*.js;*.svg"}, {Name: "Office 文档", Pattern: "*.xlsx;*.docx"}, {Name: "PDF 与思维导图", Pattern: "*.pdf;*.xmind"}, {Name: "图片", Pattern: "*.png;*.jpg;*.jpeg;*.gif;*.bmp;*.webp;*.tif;*.tiff"}},
	})
	m.focusActiveSource()
	if errors.Is(err, app.ErrCanceled) {
		return
	}
	if err != nil {
		m.fail(err)
		return
	}
	for _, path := range paths {
		m.load(path)
	}
}

func (m *editorApp) tabAtPath(path string) *editorTab {
	for _, tab := range m.tabs {
		if tab.doc.path != "" && strings.EqualFold(tab.doc.path, path) {
			return tab
		}
	}
	return nil
}

func (m *editorApp) load(path string) {
	path, err := filepath.Abs(path)
	if err != nil {
		m.fail(err)
		return
	}
	if tab := m.tabAtPath(path); tab != nil {
		m.activate(tab)
		m.rememberPath(path, false)
		return
	}
	var doc document
	var picture *imageDocument
	var office *officeDocument
	var preview *documentPreview
	if previewFormat(path) != "" {
		preview, err = readDocumentPreview(path)
		doc.path = path
	} else if officeKind(path) != "" {
		doc, office, err = readOffice(path)
	} else if strings.EqualFold(filepath.Ext(path), ".xls") || strings.EqualFold(filepath.Ext(path), ".doc") {
		err = errors.New("请先用 Office 将旧格式另存为 .xlsx 或 .docx 后打开")
	} else if isImageFile(path) {
		picture, err = readImage(path)
		doc.path = path
	} else {
		doc, err = readDocument(path)
	}
	if err != nil {
		m.fail(fmt.Errorf("打开 %s 失败\n%w", path, err))
		return
	}

	tab := m.addTab(doc, filepath.Base(path))
	tab.picture, tab.office, tab.preview = picture, office, preview
	if office != nil && m.webReady {
		m.openOffice(tab)
	}
	if preview != nil && m.webReady {
		m.openDocumentPreview(tab)
	}
	if tab.readOnlyPreview() || office != nil {
		tab.source.readOnly, tab.source.view.Snapshot.ReadOnly = true, true
		tab.source.focus = false
	}
	m.rememberPath(path, false)
	m.status = "已打开 " + filepath.Base(path)
}

func (m *editorApp) saveTab(t *editorTab, saveAs bool) bool {
	if t.office != nil {
		m.saveOfficeTab(t, saveAs, nil)
		return false
	}
	if t.readOnlyPreview() {
		return false
	}
	if _, composing := t.source.text.Composition(); composing {
		m.fail(errors.New("请先完成中文选词，再保存文件"))
		return false
	}
	path := t.doc.path
	if saveAs || path == "" {
		var err error
		suggested := t.title()
		if t.format() == "HTML" && filepath.Ext(suggested) == "" {
			suggested += ".html"
		}
		path, err = app.SaveFile(app.SaveFileOptions{
			Title: "保存 " + t.title(), SuggestedName: suggested,
			Filters: []app.FileFilter{{Name: "所有文件", Pattern: "*.*"}},
		})
		if errors.Is(err, app.ErrCanceled) {
			return false
		}
		if err != nil {
			m.fail(err)
			return false
		}
	}
	path, err := filepath.Abs(path)
	if err != nil {
		m.fail(err)
		return false
	}
	for _, other := range m.tabs {
		if other != t && strings.EqualFold(other.doc.path, path) {
			m.fail(errors.New("该文件已在另一个标签中打开，请切换到该标签或使用其他文件名"))
			return false
		}
	}
	if err := t.doc.save(path, t.source.text.String()); err != nil {
		m.fail(fmt.Errorf("保存 %s 失败\n%w", path, err))
		return false
	}
	m.rememberPath(path, false)
	if t.html.view != nil {
		m.refreshHTML(t)
	}
	m.status, t.source.focus = "已保存 "+filepath.Base(path), true
	return true
}

func (m *editorApp) openFolder() {
	paths, err := app.OpenDirectories(app.OpenDirectoryOptions{Title: "选择项目文件夹"})
	m.focusActiveSource()
	if errors.Is(err, app.ErrCanceled) {
		return
	}
	if err != nil {
		m.fail(err)
		return
	}
	m.addProjects(paths...)
}

func openEditorWindow(application *app.Application, m *editorApp) error {
	_, err := ui.OpenWindow(application, m.editorWindowConfig(application), ui.PageConfig{View: m.page, Theme: &m.theme})
	if err == nil && m.maximized {
		maximizeEditorWindow()
	}
	return err
}

func (m *editorApp) editorWindowConfig(application *app.Application) app.Config {
	m.post = application.Post
	if m.dataDirectory == "" {
		m.dataDirectory, _ = editorDataDirectory()
	}
	m.newWindow = func() {
		next := newEditorApp()
		if m.store != nil {
			m.store.attach(next)
		}
		if err := openEditorWindow(application, next); err != nil {
			m.fail(err)
		}
	}
	m.webHost.OnReady = func() {
		m.webReady = true
		t := m.active
		if t == nil {
			return
		}
		if t.office != nil {
			m.openOffice(t)
		}
		if t.preview != nil && t.preview.issue == "" {
			m.openDocumentPreview(t)
		}
		m.queueHTMLPreview(t)
	}
	width, height := 1440, 900
	var origin *gfx.Point
	if m.windowFrame.Width >= 900 && m.windowFrame.Height >= 560 {
		width, height = int(m.windowFrame.Width), int(m.windowFrame.Height)
		origin = &gfx.Point{X: m.windowFrame.X, Y: m.windowFrame.Y}
	}
	return app.Config{
		Title: "Viva Editor", Icon: applicationIcon, Width: width, Height: height, MinWidth: 900, MinHeight: 560, Origin: origin, Web: &m.webHost,
		Invalidator:    &m.invalidator,
		OnShortcut:     m.aiPromptShortcut,
		OnCloseRequest: m.requestClose,
		OnFrameChange:  func(frame gfx.Rect) { m.windowFrame = frame },
		OnMaximize:     func(value bool) { m.maximized = value },
		OnClosed:       m.releaseWindowState,
	}
}

func main() {
	directory, err := editorDataDirectory()
	if err != nil {
		app.ShowFatalError("Viva Editor", err.Error())
		return
	}
	release, err := lockSessionDirectory(directory)
	if err != nil {
		app.ShowFatalError("Viva Editor", err.Error())
		return
	}
	defer release()
	store, err := openSessionStore(directory)
	if err != nil {
		app.ShowFatalError("Viva Editor", err.Error())
		return
	}
	os.Setenv("WEBVIEW2_USER_DATA_FOLDER", filepath.Join(directory, "WebView2"))
	if len(store.windows) == 0 {
		store.attach(newEditorApp())
	}
	m := store.windows[0]
	for _, path := range os.Args[1:] {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			m.addProjects(path)
		} else {
			m.load(path)
		}
	}
	stop := make(chan struct{})
	defer close(stop)
	err = app.RunApplication(func(application *app.Application) error {
		for _, window := range store.windows {
			if err := openEditorWindow(application, window); err != nil {
				return err
			}
		}
		go func() {
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-stop:
					return
				case <-ticker.C:
					application.Post(func() {
						if store.backgroundSaving {
							return
						}
						store.backgroundSaving = true
						saved, sequence := store.capture()
						go func() {
							err := store.write(saved, sequence)
							application.Post(func() {
								store.backgroundSaving = false
								if err != nil {
									for _, window := range store.windows {
										if !window.windowClosed {
											window.status = "会话自动保存失败：" + err.Error()
										}
									}
								}
							})
						}()
					})
				}
			}
		}()
		return nil
	})
	if err != nil {
		app.ShowFatalError("Viva Editor", err.Error())
	}
}

// 保留多窗口恢复所需的轻量快照，窗口关闭后释放撤销栈、图片像素和布局缓存。
func (m *editorApp) releaseWindowState() {
	if m.windowClosed {
		return
	}
	state := m.snapshotSession()
	m.closedSession = &state
	m.windowClosed = true
	for len(m.tools.terminals) > 0 {
		m.closeLocalShell(m.tools.terminals[len(m.tools.terminals)-1])
	}
	for _, tab := range m.tabs {
		tab.html.close()
		tab.office.close()
		tab.preview.close()
	}
	m.tabs, m.active, m.workspace = nil, nil, nil
	m.results = searchResults{}
	m.ai = aiAssistant{}
	m.tabDrag = ui.DragState{}
	m.tabGhost, m.groupGhost = nil, nil
	m.rename = tabRenameState{}
	m.confirmTab, m.pending, m.newWindow, m.browser = nil, nil, nil, nil
	m.webHost = app.WebHost{}
	m.webReady = false
}
