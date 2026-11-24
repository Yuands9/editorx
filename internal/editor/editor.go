package editor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	uimarkdown "github.com/vivaui/viva/ui/markdown"
)

const applicationName = "EditorX"

type editorTab struct {
	id                int
	name              string
	displayName       string
	languageOverride  string
	doc               document
	linked            bool
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
	resultErrorPane   *editorPane
	indentOverride    bool
	wrapOverride      bool
	resultSource      string
	resultOutput      string
	resultDraft       bool
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
	if filepath.Ext(name) == "" && t.languageOverride == "JSON" {
		return "JSON"
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

func (t *editorTab) isTemporaryLink() bool {
	return t.linked && t.doc.path == "" && !t.modified() && !t.hasResultDraft() &&
		(!t.jsonCompare || t.comparison.text.CommittedString() == "")
}

func (t *editorTab) modified() bool {
	if t.office != nil {
		return t.office.state.Dirty
	}
	return !t.readOnlyPreview() && t.source.text.String() != t.doc.saved
}

type editorApp struct {
	linkedFile         linkedFileState
	theme              gfx.Theme
	light              bool
	tabs               []*editorTab
	active             *editorTab
	nextID             int
	status             string
	errorText          string
	showError          bool
	confirm            bool
	confirmFocus       bool
	dontAskToSave      bool
	confirmTab         *editorTab
	pending            func()
	workspace          *folderTree
	showFiles          bool
	showOutline        bool
	filesSplit         ui.SplitterState
	outlineSplit       ui.SplitterState
	aiEditorSplit      ui.SplitterState
	aiPromptSplit      ui.SplitterState
	newWindow          func()
	tabReveal          bool
	tabDrag            ui.DragState
	tabScrollAt        time.Duration
	tabEndHit          gfx.ID
	tabGhost           *editorTab
	groupGhost         *tabGroup
	tabGrabX           float32
	nextGroupID        int
	groupClick         tabGroupClick
	rename             tabRenameState
	fileMenu           bool
	newFileMenu        bool
	templateDialog     *templateDialogState
	templatePanel      *templateDialogState
	sidebarPrefs       sidebarPreferences
	translationEnabled bool
	translationMode    translationMode
	translation        *translationPanelState
	languageMenu       bool
	results            searchResults
	ai                 aiAssistant
	tools              projectTools
	browser            *ui.EffectState[string, int]
	desktop            *editorDesktop
	nativeWindow       *app.Window
	appSettings        settingsPageState
	backup             editorBackupState
	update             editorUpdateState
	editorPrefs        editorPreferences
	keyBindings        shortcutOverrides
	store              *sessionStore
	dataDirectory      string
	showHistory        bool
	webHost            app.WebHost
	webReady           bool
	invalidator        app.Invalidator
	post               func(func())
	windowFrame        gfx.Rect
	maximized          bool
	windowClosePending bool
	windowClosed       bool
	closedSession      *windowSession
}

func newEditorApp() *editorApp {
	return &editorApp{
		theme: editorTheme(), status: "就绪", showOutline: true, editorPrefs: defaultEditorPreferences(),
		filesSplit:    ui.SplitterState{Pos: 252},
		outlineSplit:  ui.SplitterState{Ratio: 0.20},
		aiEditorSplit: ui.SplitterState{Ratio: 0.66},
		aiPromptSplit: ui.SplitterState{Ratio: 0.76},
		tools:         projectTools{split: ui.SplitterState{Ratio: 0.70}},
	}
}

func (m *editorApp) addTab(doc document, name string) *editorTab {
	m.nextID++
	t := &editorTab{
		id: m.nextID, name: name, doc: doc, source: newEditorPane(doc.saved, false),
		result: newEditorPane("", false), split: ui.SplitterState{Ratio: 0.5},
		jsonAction: "格式化", comparison: newEditorPane("", false), diffSplit: ui.SplitterState{Ratio: 0.74},
		markdownMode: "分屏", htmlMode: "分屏", outlineCollapsed: map[int]bool{}, previewJump: -1,
	}
	t.source.light, t.result.light, t.comparison.light = m.light, m.light, m.light
	t.applyEditorPreferences(m.editorPrefs)
	t.result.view.Snapshot.Placeholder = "在此粘贴 JSON 或编辑结果"
	formatPaste := func(value string) string {
		if t.format() == "JSON" || t.doc.path == "" && filepath.Ext(t.name) == "" &&
			t.languageOverride == "" && strings.TrimSpace(t.source.text.CommittedString()) == "" {
			return formatJSONPaste(value, t.jsonIndent)
		}
		return value
	}
	t.result.input.TransformPaste = formatPaste
	t.comparison.input.TransformPaste = formatPaste
	t.initSearch(m)
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
	m.addTab(document{eol: "CRLF"}, m.untitledName(""))
	m.status = "新文档"
}

func (m *editorApp) untitledName(extension string) string {
	usedNames := make(map[string]bool, len(m.tabs))
	for _, tab := range m.tabs {
		for _, name := range []string{tab.title(), tab.tabTitle()} {
			if base, matches := strings.CutSuffix(name, extension); matches {
				usedNames[base] = true
			}
		}
	}
	return nextUntitledName(usedNames) + extension
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
	if m.active != t && m.translation != nil {
		m.translation.clearLookup()
		m.translation.message, m.translation.problem = "", ""
	}
	// 切换时取消尚未提交的组合，避免把拼音音节当成正文存入旧标签。
	if m.active != nil && m.active != t {
		m.active.source.text.CancelComposition()
		m.active.result.text.CancelComposition()
		m.active.comparison.text.CancelComposition()
		m.active.search.query.CancelComposition()
	}
	m.active = t
	if m.templatePanel != nil && m.templatePanel.project != m.activeTextProject(t) {
		m.loadTemplatePicker(&templateDialogState{open: true, project: m.activeTextProject(t)}, "", "")
	}
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
	if m.desktop != nil || m.dataDirectory != "" {
		preferences, err := m.applicationPreferences()
		if err == nil && preferences.SkipSaveConfirmation {
			m.saveBeforeClosing(t, action)
			return
		}
	}
	m.confirmTab, m.pending, m.confirm, m.confirmFocus, m.dontAskToSave = t, action, true, true, false
}

func (m *editorApp) saveBeforeClosing(tab *editorTab, proceed func()) {
	if tab.office != nil {
		m.saveOfficeTab(tab, false, proceed)
		return
	}
	if m.saveTab(tab, false) {
		proceed()
	}
}

func (m *editorApp) clearSaveConfirmation() {
	m.confirm, m.confirmFocus, m.dontAskToSave = false, false, false
	m.pending, m.confirmTab = nil, nil
}

func (m *editorApp) finishSaveConfirmation() {
	proceed, dontAsk := m.pending, m.dontAskToSave
	m.clearSaveConfirmation()
	if dontAsk {
		if err := m.rememberSkippingSaveConfirmation(); err != nil {
			m.fail(fmt.Errorf("无法保存询问设置：%w", err))
		}
	}
	if proceed != nil {
		proceed()
	}
}

func (m *editorApp) closeTab(t *editorTab) {
	m.closeTabs([]*editorTab{t}, false)
}

// tabs is a copy of the selected range, so removing a tab does not move the remaining targets.
func (m *editorApp) closeTabs(tabs []*editorTab, keepPinned bool) {
	for index, tab := range tabs {
		if (keepPinned && tab.isPinned()) || !slices.Contains(m.tabs, tab) {
			continue
		}
		waitingForOffice := tab.office != nil && tab.office.view != nil && tab.office.ready
		if !waitingForOffice && !tab.modified() {
			m.removeTab(tab)
			continue
		}
		proceed := func() {
			if !keepPinned || !tab.isPinned() {
				m.removeTab(tab)
			}
			m.closeTabs(tabs[index+1:], keepPinned)
		}
		if waitingForOffice {
			tab.office.flush(func(err error) {
				if (keepPinned && tab.isPinned()) || !slices.Contains(m.tabs, tab) {
					m.closeTabs(tabs[index+1:], keepPinned)
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
	m.releaseClosedTabText(t)
}

func (m *editorApp) requestClose(allow func()) {
	m.flushOfficeTabs(0, func(err error) {
		if err != nil {
			m.fail(err)
			return
		}
		m.finishClose(allow)
	})
}

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
	file, err := readEditorFile(path)
	if err != nil {
		m.fail(fmt.Errorf("打开 %s 失败\n%w", path, err))
		return
	}
	m.openLoadedFile(file)
}

type openedEditorFile struct {
	name    string
	doc     document
	picture *imageDocument
	office  *officeDocument
	preview *documentPreview
}

func readEditorFile(path string) (openedEditorFile, error) {
	var file openedEditorFile
	var err error
	switch {
	case previewFormat(path) != "":
		file.preview, err = readDocumentPreview(path)
		file.doc.path = path
	case officeKind(path) != "":
		file.doc, file.office, err = readOffice(path)
	case strings.EqualFold(filepath.Ext(path), ".xls") || strings.EqualFold(filepath.Ext(path), ".doc"):
		err = errLegacyOfficeFormat
	case isImageFile(path):
		file.picture, err = readImage(path)
		file.doc.path = path
	default:
		file.doc, err = readDocument(path)
	}
	return file, err
}

func (m *editorApp) openLoadedFile(file openedEditorFile) *editorTab {
	path, name := file.doc.path, file.name
	if path != "" {
		name = filepath.Base(path)
	}
	tab := m.addTab(file.doc, name)
	tab.linked = path == "" && file.name != ""
	tab.picture, tab.office, tab.preview = file.picture, file.office, file.preview
	if tab.office != nil && m.webReady {
		m.openOffice(tab)
	}
	if tab.preview != nil && m.webReady {
		m.openDocumentPreview(tab)
	}
	if tab.readOnlyPreview() || tab.office != nil {
		tab.source.readOnly, tab.source.view.Snapshot.ReadOnly = true, true
		tab.source.focus = false
	}
	if path != "" {
		m.rememberPath(path, false)
	}
	m.status = "已打开 " + tab.title()
	return tab
}

func (m *editorApp) saveTab(t *editorTab, saveAs bool) bool {
	if t.office != nil {
		m.saveOfficeTab(t, saveAs, nil)
		return false
	}
	if t.readOnlyPreview() {
		return false
	}
	if t.format() == "JSON" && !t.jsonCompare {
		if t.jsonComposing() {
			m.fail(errors.New("请先完成中文选词，再保存文件"))
			return false
		}
		t.updateResult()
	}
	if _, composing := t.source.text.Composition(); composing {
		m.fail(errors.New("请先完成中文选词，再保存文件"))
		return false
	}
	return m.saveTextTab(t, saveAs)
}

func (m *editorApp) saveTextTab(t *editorTab, saveAs bool) bool {
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
	window, err := ui.OpenWindow(application, m.editorWindowConfig(application), ui.PageConfig{View: m.page, Theme: &m.theme, Styles: &editorControlStyles})
	if err != nil {
		return err
	}
	m.nativeWindow = window
	if m.maximized {
		maximizeEditorWindow()
	}
	return nil
}

func (m *editorApp) editorWindowConfig(application *app.Application) app.Config {
	m.post = application.Post
	if m.dataDirectory == "" {
		m.dataDirectory, _ = editorDataDirectory()
	}
	m.newWindow = func() {
		next := newEditorApp()
		next.desktop = m.desktop
		next.setLightMode(m.light)
		next.editorPrefs = m.editorPrefs
		next.keyBindings = m.keyBindings
		next.applySidebarPreferences(m.sidebarPrefs)
		next.translationEnabled = m.translationEnabled
		next.store, next.dataDirectory = m.store, m.dataDirectory
		if err := openEditorWindow(application, next); err != nil {
			m.fail(err)
			return
		}
		if m.store != nil {
			m.store.attach(next)
		}
	}
	m.webHost.OnReady = func() {
		m.webReady = true
		for _, shell := range m.tools.terminals {
			m.mountShell(shell)
		}
		if len(m.tools.terminals) > 0 {
			m.invalidator.Invalidate()
		}
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
		Title: applicationName, Icon: themedApplicationIcon(m.light), Width: width, Height: height, MinWidth: 900, MinHeight: 560, Origin: origin, Web: &m.webHost,
		ClearColor:  app.Color{Red: float64(m.theme.Card.Red), Green: float64(m.theme.Card.Green), Blue: float64(m.theme.Card.Blue), Alpha: 1},
		Invalidator: &m.invalidator,
		OnShortcut:  m.windowShortcut,
		OnKey:       m.functionShortcut,
		OnFileDrop:  m.dropFiles,
		OnActivate: func() {
			if m.desktop != nil && !m.nativeWindow.Hidden() {
				m.desktop.lastActive = m
			}
		},
		OnCloseRequest: func(allow func()) {
			if m.desktop == nil {
				if m.appSettings.open {
					m.closeSettings()
				}
				m.requestClose(allow)
				return
			}
			m.desktop.close(m, allow)
		},
		OnFrameChange: func(frame gfx.Rect) { m.windowFrame = frame },
		OnMaximize:    func(value bool) { m.maximized = value },
		OnClosed:      m.releaseWindowState,
	}
}

// Run starts EditorX or forwards the requested paths to the running instance.
func Run(paths []string) {
	update, err := runEditor(paths)
	if err == nil && update.path != "" {
		err = update.restart()
	}
	if err != nil {
		app.ShowFatalError(applicationName, err.Error())
	}
}

func runEditor(paths []string) (installedEditorUpdate, error) {
	directory, err := editorDataDirectory()
	if err != nil {
		return installedEditorUpdate{}, err
	}
	release, err := claimEditorInstance(directory, paths)
	if err != nil || release == nil {
		return installedEditorUpdate{}, err
	}
	defer release()

	store, err := openSessionStore(directory)
	if err != nil {
		return installedEditorUpdate{}, err
	}
	os.Setenv("WEBVIEW2_USER_DATA_FOLDER", filepath.Join(directory, "WebView2"))
	if len(store.windows) == 0 {
		store.attach(newEditorApp())
	}
	desktop := &editorDesktop{store: store}
	desktop.update.initialize(directory, time.Now())
	defer desktop.update.close()
	desktop.preferences, err = readApplicationPreferences(directory)
	if err != nil {
		store.windows[0].status = "无法读取应用设置，已使用默认值：" + err.Error()
	}

	for _, window := range store.windows {
		window.applyEditorPreferences(desktop.preferences.Editor)
		window.keyBindings = desktop.preferences.Shortcuts
		window.applySidebarPreferences(desktop.preferences.Sidebar)
		window.translationEnabled = desktop.preferences.TranslationEnabled
	}
	store.windows[0].openLaunchPaths(paths)

	// 激活窗口与 Viva 的窗口使用同一 Windows 消息线程。
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var stopActivation, stopAppearance func()
	defer func() {
		if stopActivation != nil {
			stopActivation()
		}
		if stopAppearance != nil {
			stopAppearance()
		}
	}()
	stop := make(chan struct{})
	defer close(stop)
	err = app.RunApplication(func(application *app.Application) error {
		desktop.application = application
		stopActivation, err = listenForEditorActivation(directory, func(paths []string) bool {
			if desktop.exiting {
				return false
			}
			application.Post(func() { desktop.present(paths) })
			return true
		})
		if err != nil {
			return err
		}
		app.SetNativeAppearance(themeAppearance(desktop.preferences.Theme))
		stopAppearance = app.WatchSystemAppearance(desktop.refreshSystemTheme)
		for _, window := range store.windows {
			window.desktop = desktop
			window.applyThemePreference(desktop.preferences.Theme)
			if err := openEditorWindow(application, window); err != nil {
				return err
			}
		}
		if err := desktop.syncTray(); err != nil {
			store.windows[0].status = "无法显示系统托盘图标：" + err.Error()
		}
		if executable, err := os.Executable(); err == nil {
			go cleanupPreviousEditorExecutable(executable)
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
						desktop.checkEditorUpdates(time.Now())
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
	return desktop.updated, err
}

func (m *editorApp) interactionBlocked() bool {
	return m.windowClosed || m.windowClosePending || m.linkedFile.open || m.confirm || m.showError || m.showHistory || m.rename.open || m.update.open ||
		m.templateDialog != nil ||
		m.tools.git.branches.creating || m.tools.git.branches.switching ||
		m.ai.reviewOpen || m.ai.conversationEdit.open || m.ai.settings.discardPending || m.backup.selecting ||
		(m.desktop != nil && m.desktop.exiting)
}

func (m *editorApp) dropFiles(event gfx.FileDropEvent) gfx.EventResult {
	if m.interactionBlocked() {
		return gfx.EventResult{}
	}
	if event.Phase == gfx.FileDropOver {
		return gfx.EventResult{Handled: true}
	}
	if event.Phase != gfx.FileDropDrop || len(event.Paths) == 0 {
		return gfx.EventResult{}
	}

	paths := slices.Clone(event.Paths)
	open := func() {
		if m.interactionBlocked() {
			return
		}
		if m.appSettings.open {
			m.closeSettings()
		}
		m.openLaunchPaths(paths)
		m.invalidator.Invalidate()
	}
	if m.post != nil {
		m.post(open)
		return gfx.EventResult{Handled: true}
	}
	open()
	return gfx.EventResult{Handled: true, Repaint: true}
}

func (m *editorApp) openLaunchPaths(paths []string) {
	for _, path := range paths {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			m.addProjects(path)
		} else {
			m.load(path)
		}
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
	m.translation, m.templateDialog, m.templatePanel = nil, nil, nil
	if m.desktop == nil {
		m.update.close()
	}
	m.update, m.backup = editorUpdateState{}, editorBackupState{}
	m.linkedFile = linkedFileState{}
	m.tabDrag = ui.DragState{}
	m.tabGhost, m.groupGhost = nil, nil
	m.rename = tabRenameState{}
	m.clearSaveConfirmation()
	m.newWindow, m.browser = nil, nil
	m.webHost = app.WebHost{}
	m.webReady = false
	m.nativeWindow = nil
}
