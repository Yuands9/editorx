package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

func TestEditingDepthFoldsAndCompletion(t *testing.T) {
	for _, sample := range []struct {
		language, value string
		count           int
	}{
		{"Go", "func main() {\n  s := \"} ignored\" // {\n  if true {\n    println(s)\n  }\n}\n", 2},
		{"JavaScript", "function demo() {\n /* } */\n const text = `}`;\n return text;\n}", 1},
		{"Python", "def f():\n    if yes:\n        run()\n    done()\nother()", 2},
		{"YAML", "root:\n  a: 1\n  b: 2\nother: 3", 1},
		{"Markdown", "# Main\ntext\n## Sub\n```go\n# not heading\n```\n", 3},
		{"XML", "<root>\n<!-- <fake> -->\n<child>\nhi\n</child>\n</root>", 2},
	} {
		folds := editorFolds(sample.value, sample.language)
		if len(folds) != sample.count {
			t.Fatalf("%s: %+v", sample.language, folds)
		}
		for _, fold := range folds {
			if fold.Header >= fold.LastLine {
				t.Fatal(fold)
			}
		}
	}
	m := newEditorApp()
	tab := m.addTab(document{saved: "function longerName() {}\nlonger"}, "sample.js")
	s := openTestSession(t, m)
	tab.source.text.MoveTo(tab.source.text.RuneCount(), false)
	s.Frame()
	if err := s.ClickLabel("代码编辑区"); err != nil {
		t.Fatal(err)
	}
	tab.source.text.MoveTo(tab.source.text.RuneCount(), false)
	if handled, err := s.Driver().ShortcutChord(ui.Alt, '/'); err != nil || !handled {
		t.Fatalf("Alt+/ completion: handled=%v error=%v", handled, err)
	}
	s.Frame()
	if tab.source.completion == nil || len(tab.source.completion.items) != 1 || tab.source.completion.items[0] != "longerName" {
		t.Fatalf("completion %+v", tab.source.completion)
	}
	if directory := os.Getenv("VIVA_PREVIEW_DIR"); directory != "" {
		s.Screenshot(filepath.Join(directory, "word-completion.png"))
	}
	if err := s.Driver().Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(tab.source.text.String(), "longerName") {
		t.Fatal(tab.source.text.String())
	}
	if !tab.source.text.Undo() || !strings.HasSuffix(tab.source.text.String(), "longer") {
		t.Fatal("completion undo failed")
	}
	if !m.runEditorShortcut(shortcutFoldAll) {
		t.Fatal("fold action")
	}
	s.Frame()
}

func TestAutomaticCompletionSettingsAndTyping(t *testing.T) {
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	const fixture = "longerName\n"
	tab := m.addTab(document{saved: fixture}, "completion.txt")
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	reset := func(value string) {
		t.Helper()
		tab.source.replace(value)
		tab.source.text.MoveTo(tab.source.text.RuneCount(), false)
		check(s.Frame())
	}
	check(s.ClickLabel("代码编辑区"))
	reset(fixture)
	check(s.Type("lon"))
	check(s.Driver().Advance(automaticCompletionDelay))
	if tab.source.completion != nil || m.editorPrefs.AutomaticCompletion {
		t.Fatal("automatic completion was enabled in a fresh profile")
	}
	check(s.ClickLabel("button:设置"))
	check(s.ClickLabel("button:编辑器"))
	check(s.ClickLabel("switch:输入时自动补全"))
	saved, err := readApplicationPreferences(m.dataDirectory)
	check(err)
	if !saved.Editor.AutomaticCompletion || !tab.source.automaticCompletion {
		t.Fatal("automatic completion setting did not apply and persist")
	}
	check(s.ClickLabel("button:关闭设置"))
	check(s.ClickLabel("代码编辑区"))
	reset(fixture)
	check(s.Type("lo"))
	check(s.Driver().Advance(automaticCompletionDelay / 2))
	check(s.Type("n"))
	check(s.Driver().Advance(automaticCompletionDelay/2 + time.Millisecond))
	if tab.source.completion != nil {
		t.Fatal("candidate scan ran before the most recent input pause")
	}
	check(s.Driver().Advance(automaticCompletionDelay))
	if popup := tab.source.completion; popup == nil || !popup.automatic || len(popup.items) != 1 || popup.items[0] != "longerName" {
		t.Fatalf("missing automatic candidate: %+v", popup)
	}
	if preview := os.Getenv("VIVA_PREVIEW_DIR"); preview != "" {
		check(s.Screenshot(filepath.Join(preview, "automatic-completion.png")))
	}
	check(s.Driver().Key(gfx.KeyEnter, 0))
	if tab.source.text.String() != fixture+"lon\n" {
		t.Fatal("an unselected suggestion intercepted Enter")
	}

	reset(fixture + "ler")
	tab.source.text.MoveTo(tab.source.text.RuneCount()-3, false)
	check(s.Frame())
	check(s.Type("long"))
	check(s.Driver().Advance(automaticCompletionDelay))
	check(s.Driver().Key(gfx.KeyTab, 0))
	if got := tab.source.text.String(); got != fixture+"longerName" {
		t.Fatalf("Tab did not replace the full word: %q", got)
	}
	if !tab.source.text.Undo() || tab.source.text.String() != fixture+"longler" {
		t.Fatal("completion did not undo in one step")
	}

	reset("编辑器\n")
	check(s.Driver().Compose("编辑"))
	check(s.Driver().Advance(automaticCompletionDelay * 2))
	if tab.source.completion != nil {
		t.Fatal("EditorX obscured the IME composition")
	}
	check(s.Type("编辑"))
	check(s.Driver().Advance(automaticCompletionDelay))
	check(s.ClickLabel("button:编辑器"))
	if tab.source.text.String() != "编辑器\n编辑器" {
		t.Fatal("committed Chinese text did not allow mouse completion")
	}
}

func BenchmarkAutomaticCompletionCandidates(b *testing.B) {
	value := strings.Repeat("const Value = anotherValue\n", 600000) + "ano"
	caret := len(value)
	b.ReportAllocs()
	for b.Loop() {
		items := completionCandidates(value, "JavaScript", caret-3, caret, caret, automaticCompletionRadius)
		if len(items) != 1 || items[0] != "anotherValue" {
			b.Fatalf("unexpected candidates: %v", items)
		}
	}
}

func TestEditingDepthSplitAndNavigation(t *testing.T) {
	m := newEditorApp()
	left := m.addTab(document{saved: strings.Repeat("left line\n", 30)}, "left.txt")
	right := m.addTab(document{saved: "right text"}, "right.txt")
	s := openTestSession(t, m)
	m.activate(left)
	s.Frame()
	m.openEditorSplit(left)
	s.Frame()
	if m.editorSplit == nil || m.editorSplit.left != left || m.editorSplit.right != right {
		t.Fatal("split choices")
	}
	if err := s.ClickLabel("右侧编辑区"); err != nil {
		t.Fatal(err)
	}
	if err := s.Type("changed"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(right.source.text.String(), "changed") || strings.Contains(left.source.text.String(), "changed") || m.active != right {
		t.Fatal("split focus edited wrong file")
	}
	m.editorSplit.right = left
	m.editorSplit.mirror = nil
	s.Frame()
	if err := s.ClickLabel("右侧编辑区"); err != nil {
		t.Fatal(err)
	}
	if err := s.Type("mirror"); err != nil {
		t.Fatal(err)
	}
	if m.editorSplit.mirror.text != left.source.text || !strings.Contains(left.source.text.String(), "mirror") {
		t.Fatal("same document split not shared")
	}
	if !m.editorSplit.mirror.text.Undo() || strings.Contains(left.source.text.String(), "mirror") {
		t.Fatal("shared undo failed")
	}
	s.Frame()
	if directory := os.Getenv("VIVA_PREVIEW_DIR"); directory != "" {
		s.Screenshot(filepath.Join(directory, "ordinary-split.png"))
	}
	m.editorSplit = nil
	m.navigation = editorNavigation{}
	m.activate(left)
	left.source.text.MoveTo(0, false)
	m.observeNavigation()
	left.source.text.MoveTo(left.source.text.RuneCount()-1, false)
	m.observeNavigation()
	m.activate(right)
	m.observeNavigation()
	m.navigateLocation(-1)
	if m.active != left || left.source.text.Caret() != left.source.text.RuneCount()-1 {
		t.Fatal("back to prior document")
	}
	m.navigateLocation(-1)
	if left.source.text.Caret() != 0 {
		t.Fatal("back to prior location")
	}
	m.navigateLocation(1)
	if left.source.text.Caret() != left.source.text.RuneCount()-1 {
		t.Fatal("forward location")
	}
	m.removeTab(left)
	if m.editorSplit != nil {
		t.Fatal("split retained closed tab")
	}
}

func TestEditingDepthKeyboardColumn(t *testing.T) {
	m := newEditorApp()
	tab := m.addTab(document{saved: "abcde\n12345\nABCDE"}, "column.txt")
	s := openTestSession(t, m)
	tab.source.text.MoveTo(1, false)
	s.Frame()
	for _, key := range []gfx.Key{gfx.KeyRight, gfx.KeyRight, gfx.KeyDown, gfx.KeyDown} {
		if err := s.Driver().Key(key, ui.Alt|ui.Shift); err != nil {
			t.Fatal(err)
		}
	}
	if tab.source.text.SelectionCount() != 3 {
		t.Fatalf("selections %+v", tab.source.text.Selections())
	}
	if err := s.Type("x"); err != nil {
		t.Fatal(err)
	}
	if tab.source.text.String() != "axde\n1x45\nAxDE" {
		t.Fatalf("column content %q", tab.source.text.String())
	}
	if !tab.source.text.Undo() || tab.source.text.String() != "abcde\n12345\nABCDE" {
		t.Fatal("column undo")
	}
}
