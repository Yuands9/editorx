package editor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func TestCoreMultiCursorEditing(t *testing.T) {
	m := newEditorApp()
	m.keyBindings = shortcutPreset("Sublime Text")
	tab := m.addTab(document{saved: "中文 alpha 中文 alpha"}, "sample.txt")
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err := s.Driver().ShortcutChord(gfx.ModifierControl, 'd')
	check(err)
	_, err = s.Driver().ShortcutChord(gfx.ModifierControl, 'd')
	check(err)
	if tab.source.text.SelectionCount() != 2 {
		t.Fatalf("selections: %+v", tab.source.text.Selections())
	}
	check(s.Type("替换"))
	if tab.source.text.String() != "替换 alpha 替换 alpha" {
		t.Fatal(tab.source.text.String())
	}
	if len(tab.source.view.Snapshot.ExtraCarets) != 1 {
		t.Fatal("extra caret not painted")
	}
	_, err = s.Driver().ShortcutChord(gfx.ModifierControl, 'z')
	check(err)
	if tab.source.text.String() != "中文 alpha 中文 alpha" {
		t.Fatal("multi edit undo lost content")
	}
	_, err = s.Driver().ShortcutChord(gfx.ModifierControl, 'y')
	check(err)
	if tab.source.text.String() != "替换 alpha 替换 alpha" {
		t.Fatal("multi edit redo lost content")
	}
	p := tab.source
	p.text.SetSelections([]text.Range{{Start: 0, End: 2}, {Start: 9, End: 11}}, 1)
	if p.copySelections() != "替换\n替换" {
		t.Fatalf("copy: %q", p.copySelections())
	}
	p.text.Insert("")
	if p.text.String() != " alpha  alpha" {
		t.Fatal("multi cut did not remove both selections")
	}
	p.text.Undo()
	check(s.Driver().Key(gfx.KeyEscape, 0))
}

func TestCoreColumnAndVerticalCarets(t *testing.T) {
	p := newEditorPane("abcde\n12\nABCDE\n中文ab", false)
	ranges := columnSelections(p.text.String(), 1, 13, 4)
	p.text.SetSelections(ranges, 0)
	p.text.Insert("X")
	if p.text.String() != "aXe\n1X\nAXE\n中文ab" {
		t.Fatalf("column edit: %q %+v", p.text.String(), ranges)
	}
	p.text.Undo()
	p.text.MoveTo(3, false)
	p.addVerticalCaret(1)
	p.addVerticalCaret(1)
	if p.text.SelectionCount() != 3 {
		t.Fatal(p.text.Selections())
	}
	p.text.Insert("!")
	if p.text.String() != "abc!de\n12!\nAB!CDE\n中文ab" {
		t.Fatal(p.text.String())
	}
	// Tab and wide characters use visual cells, not bytes.
	ranges = columnSelections("\ta\n12345\n中文x", 1, 8, 4)
	if len(ranges) != 2 || ranges[0].Start != 1 || ranges[1].Start != 7 {
		t.Fatal(ranges)
	}
}

func TestCoreDirectorySearch(t *testing.T) {
	root := t.TempDir()
	for name, value := range map[string]string{"a.txt": "alpha\n中文 Alpha alphabet\n", "sub/b.txt": "alpha", ".git/ignored.txt": "alpha", "skip.log": "alpha", "binary.txt": "a\x00b"} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	options := searchOptions{WholeWord: true}
	query, err := options.compile("alpha")
	if err != nil {
		t.Fatal(err)
	}
	result, err := searchDirectory(context.Background(), root, query, []string{"*.txt"}, []string{".git"})
	if err != nil || len(result.hits) != 3 || result.files != 2 || result.skipped != 1 || result.partial {
		t.Fatalf("%+v %v", result, err)
	}
	if result.hits[1].line != 2 || result.hits[1].column != 4 {
		t.Fatal(result.hits)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err = searchDirectory(ctx, root, query, nil, nil)
	if err != nil || !result.partial || len(result.hits) != 0 {
		t.Fatal("cancel not respected")
	}
	if _, err := directoryPatterns("[broken"); err == nil {
		t.Fatal("invalid filter accepted")
	}
	options.mode, options.CaseSensitive = searchRegexp, true
	query, err = options.compile("(?m)^a[a-z]+$")
	if err != nil {
		t.Fatal(err)
	}
	result, err = searchDirectory(context.Background(), root, query, []string{"*.txt"}, []string{".git"})
	if err != nil || len(result.hits) != 2 {
		t.Fatal(result, err)
	}
}

func TestCoreDirectorySearchUI(t *testing.T) {
	m := newEditorApp()
	root := t.TempDir()
	path := filepath.Join(root, "中文.txt")
	if err := os.WriteFile(path, []byte("first\nneedle 中文\nlast"), 0600); err != nil {
		t.Fatal(err)
	}
	m.addProjects(root)
	s := openTestSession(t, m)
	_, err := s.Driver().ShortcutChord(gfx.ModifierControl|gfx.ModifierShift, 'f')
	if err != nil {
		t.Fatal(err)
	}
	if m.directorySearch == nil || !m.directorySearch.open {
		t.Fatal("directory shortcut missing")
	}
	if err = s.Type("needle"); err != nil {
		t.Fatal(err)
	}
	if err = s.ClickLabel("button:搜索目录"); err != nil {
		t.Fatal(err)
	}
	if err = s.WaitFor(func(app.Snapshot) bool { return m.directorySearch.request == nil }, 4*time.Second); err != nil {
		t.Fatal(err)
	}
	if len(m.directorySearch.result.hits) != 1 {
		t.Fatal(m.directorySearch.problem, m.directorySearch.result)
	}
	if dir := os.Getenv("VIVA_PREVIEW_DIR"); dir != "" {
		if err = s.Screenshot(filepath.Join(dir, "directory-search.png")); err != nil {
			t.Fatal(err)
		}
	}
	m.setLightMode(true)
	if err = s.Resize(900, 560); err != nil {
		t.Fatal(err)
	}
	if dir := os.Getenv("VIVA_PREVIEW_DIR"); dir != "" {
		if err = s.Screenshot(filepath.Join(dir, "directory-search-light-small.png")); err != nil {
			t.Fatal(err)
		}
	}
	m.directorySearch.query.SetText("尚未开始的新搜索")
	m.chooseDirectoryHit(0)
	if m.active == nil || m.active.doc.path != path || m.active.source.text.Selection() != (text.Range{Start: 6, End: 12}) {
		t.Fatal("result did not open/select")
	}
}

func TestCoreEncodingAndEOLConversion(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "convert.txt")
	original := []byte("中文\r\nsecond\r\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	m := newEditorApp()
	m.load(path)
	tab := m.active
	m.convertTextEncoding(tab, utf16LEEncoding, true)
	m.convertTextEOL(tab, "CR")
	if !tab.modified() {
		t.Fatal("format-only edit not dirty")
	}
	state := m.snapshotSession()
	if !state.Tabs[0].FormatDirty {
		t.Fatal("format flag lost in draft")
	}
	if !m.saveTextTab(tab, false) {
		t.Fatal(m.errorText)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want, err := (document{encoding: utf16LEEncoding, bom: true, eol: "CR"}).fileBytes("中文\nsecond\n")
	if err != nil || !bytes.Equal(data, want) || tab.modified() {
		t.Fatalf("conversion %x wanted %x %v", data, want, err)
	}
	m.convertTextEncoding(tab, latin1Encoding, false)
	if m.saveTextTab(tab, false) {
		t.Fatal("unrepresentable character conversion succeeded")
	}
	data, _ = os.ReadFile(path)
	if !bytes.Equal(data, want) {
		t.Fatal("failed conversion overwrote original")
	}
	for _, raw := range []string{"a\rb\r", "a\r\nb\nc\r", "a\r\nb\r"} {
		doc, err := decodeDocument("sample", []byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		if raw == "a\rb\r" && (doc.eol != "CR" || doc.saved != "a\nb\n") {
			t.Fatal(doc)
		}
		if strings.Contains(raw, "\r\n") && doc.eol != "混合" {
			t.Fatal(doc)
		}
	}
}

func TestCoreBackgroundIOAndJSON(t *testing.T) {
	m := newEditorApp()
	root := t.TempDir()
	path := filepath.Join(root, "large.json")
	value := "{\"text\":\"" + strings.Repeat("x", backgroundTextBytes) + "\"}"
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
	s := openTestSession(t, m)
	m.load(path)
	if m.active != nil || len(m.textOpens) != 1 {
		t.Fatal("large file decoded synchronously")
	}
	if err := s.WaitFor(func(app.Snapshot) bool { return m.active != nil && m.active.resultReady && m.active.jsonPreview == nil }, 8*time.Second); err != nil {
		t.Fatal(err)
	}
	tab := m.active
	if !strings.Contains(tab.result.text.String(), "\n") || tab.modified() {
		t.Fatal("preview changed original")
	}
	tab.source.text.MoveTo(tab.source.text.RuneCount(), false)
	tab.source.text.Insert("\n")
	if m.saveTextTab(tab, false) || tab.savingText == nil {
		t.Fatal("large save did not run in background")
	}
	tab.source.text.Insert(" ")
	if err := s.WaitFor(func(app.Snapshot) bool { return tab.savingText == nil }, 8*time.Second); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != value+"\n" || !tab.modified() {
		t.Fatal("snapshot save lost concurrent changes", err)
	}
	// A stale preview may never overwrite the text typed while it was running.
	if err := s.WaitFor(func(app.Snapshot) bool { return tab.resultSource == tab.source.text.String() && tab.jsonPreview == nil }, 8*time.Second); err != nil {
		t.Fatal(err)
	}
	if tab.source.text.String() != value+"\n " {
		t.Fatal("preview overwrote current source")
	}
	if err := os.WriteFile(path, []byte("external"), 0600); err != nil {
		t.Fatal(err)
	}
	m.saveTextTab(tab, false)
	if err := s.WaitFor(func(app.Snapshot) bool { return tab.savingText == nil }, 8*time.Second); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if string(data) != "external" || !m.showError {
		t.Fatal("background save bypassed conflict protection")
	}
}

func BenchmarkCoreJSONFolds4MiB(b *testing.B) {
	source := "[\n" + strings.Repeat("  {\"name\": \"sample\", \"n\": 1234},\n", 120000) + "  null\n]"
	b.ReportAllocs()
	b.SetBytes(int64(len(source)))
	for b.Loop() {
		jsonFolds(source)
	}
}
