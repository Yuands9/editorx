package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestSearchReplaceCurrentFileWithMultilineInput(t *testing.T) {
	m := newTestEditorWithDocument()
	tab := m.active
	original := "甲 old\nline\n乙 old\nline"
	tab.source.replace(original)
	session := openTestSession(t, m)
	openSearchForTest(t, session, "old")
	if err := session.Driver().Key(gfx.KeyEnter, gfx.ModifierControl); err != nil {
		t.Fatal(err)
	}
	if err := session.Type("line"); err != nil {
		t.Fatal(err)
	}
	if tab.search.query.String() != "old\nline" || len(tab.search.matches) != 2 {
		t.Fatalf("multiline search didn't match both instances: %q, %v", tab.search.query.String(), tab.search.matches)
	}
	for _, chord := range []struct {
		key       rune
		replacing bool
	}{{'r', true}, {'f', false}} {
		if handled, err := session.Driver().ShortcutChord(gfx.ModifierControl, chord.key); err != nil || !handled || tab.search.replacing != chord.replacing || tab.search.query.String() != "old\nline" {
			t.Fatalf("Ctrl+%c didn't switch modes and preserve the multiline query: %t, %v", chord.key, handled, err)
		}
	}
	for _, label := range []string{"button:替换", "替换为"} {
		if err := session.ClickLabel(label); err != nil {
			t.Fatal(err)
		}
	}
	if err := session.Type("$1\n新行"); err != nil {
		t.Fatal(err)
	}
	if err := session.ClickLabel("button:替换当前匹配"); err != nil {
		t.Fatal(err)
	}
	first := "甲 $1\n新行\n乙 old\nline"
	if tab.source.text.String() != first {
		t.Fatalf("expected one literal multiline replacement, got %q", tab.source.text.String())
	}
	if err := session.ClickLabel("button:替换当前文件全部"); err != nil {
		t.Fatal(err)
	}
	if tab.source.text.String() != "甲 $1\n新行\n乙 $1\n新行" {
		t.Fatalf("expected the remaining match to be replaced, got %q", tab.source.text.String())
	}
	if !tab.source.text.Undo() || tab.source.text.String() != first {
		t.Fatal("replace all didn't undo in one step")
	}
	if !tab.source.text.Undo() || tab.source.text.String() != original {
		t.Fatal("single replacement didn't undo")
	}
}

func TestSearchReplaceAllOpenWithEscapesAndMixedEndings(t *testing.T) {
	m := newEditorApp()
	directory := t.TempDir()
	m.dataDirectory = directory
	paths := []string{filepath.Join(directory, "甲.txt"), filepath.Join(directory, "乙.txt")}
	contents := []string{"头\r\nold\r\nline\r\n尾\n", "头\nold\nline\n尾\n"}
	for index, path := range paths {
		if err := os.WriteFile(path, []byte(contents[index]), 0600); err != nil {
			t.Fatal(err)
		}
		m.load(path)
	}
	readOnly := m.addTab(document{saved: "old\nline", eol: "LF"}, "只读")
	readOnly.source.readOnly = true
	m.activate(m.tabs[0])
	session := openTestSession(t, m)
	if handled, err := session.Driver().ShortcutChord(gfx.ModifierControl, 'r'); err != nil || !handled || !m.active.search.replacing {
		t.Fatalf("Ctrl+R didn't open replace: %t, %v", handled, err)
	}
	if err := session.Type("old\\r\\nline"); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"转义字符", "替换为"} {
		if err := session.ClickLabel(label); err != nil {
			t.Fatal(err)
		}
	}
	if err := session.Type("新值\\n下一行"); err != nil {
		t.Fatal(err)
	}
	if err := session.ClickLabel("button:替换全部已打开文档"); err != nil {
		t.Fatal(err)
	}
	for index, path := range paths {
		pane := m.tabs[index].source
		if !strings.Contains(pane.text.String(), "新值\n下一行") || !m.tabs[index].modified() {
			t.Fatalf("tab %d didn't receive the replacement: %q", index, pane.text.String())
		}
		if data, err := os.ReadFile(path); err != nil || string(data) != contents[index] {
			t.Fatalf("replacement touched the file on disk: %v", err)
		}
		if !pane.text.Undo() || pane.text.String() != contents[index] {
			t.Fatalf("undo did not restore the original line endings of file %d: %q", index, pane.text.String())
		}
	}
	if readOnly.source.text.String() != "old\nline" {
		t.Fatal("replace all modified a read-only tab")
	}
	restored := newEditorApp()
	restored.restoreSession(m.snapshotSession())
	if restored.tabs[0].search.options.mode != searchEscapes {
		t.Fatal("escape search mode was not restored")
	}
}

func TestSearchRegexReplaceAllBeyondHighlightLimit(t *testing.T) {
	m := newTestEditorWithDocument()
	tab := m.active
	s := &tab.search
	original := strings.Repeat("甲旧\n值\n", 10005)
	tab.source.replace(original)
	tab.openReplace()
	s.options.mode = searchRegexp
	s.field.SetText("(旧)\\n(值)")
	s.replaceField.SetText("$2\\n$1")
	s.refresh()
	if !s.truncated || len(s.matches) != 10000 {
		t.Fatal("fixture did not reach the highlight limit")
	}
	m.replaceAll(tab, false)
	if got := tab.source.text.String(); got != strings.Repeat("甲值\n旧\n", 10005) {
		t.Fatalf("replace all stopped at the highlight limit or lost the groups, length: %d", len(got))
	}
	if !tab.source.text.Undo() || tab.source.text.String() != original {
		t.Fatal("regex batch did not undo as one unit")
	}
}
