package editor

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/text/find"
)

func TestLargeEditSearchMatchesNormalizedText(t *testing.T) {
	prefix := "头\r\nCat\r\ncAT猫\r\ncats CAT\r\nOPEN"
	source := prefix + strings.Repeat("x", largeTextPageBytes-len(prefix)-3) + "\r\nſDK sdk SDK sdk2\r\n终\r\nCLOSE\r\n结尾"
	store, err := newLargeEditStorage()
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	piece, err := store.append([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	revision := &largeEditRevision{store: store}
	revision.addPiece(piece)
	viewport := newLargeEditWindow(0, 0, source)

	for _, test := range []struct {
		pattern string
		options searchOptions
	}{
		{"cat", searchOptions{WholeWord: true}},
		{"sdk", searchOptions{WholeWord: true}},
		{"CAT", searchOptions{CaseSensitive: true, WholeWord: true}},
		{"\\r\\nCat\\n", searchOptions{mode: searchEscapes, CaseSensitive: true}},
		{"\\A头|^cat$|\\z", searchOptions{mode: searchRegexp}},
		{"(?s)OPEN(.*?)CLOSE", searchOptions{mode: searchRegexp, CaseSensitive: true}},
		{"\\bcat\\b", searchOptions{mode: searchRegexp}},
	} {
		t.Run(test.pattern, func(t *testing.T) {
			query, err := test.options.compile(test.pattern)
			if err != nil {
				t.Fatal(err)
			}
			stream, err := newLargeEditQuery(query)
			if err != nil {
				t.Fatal(err)
			}
			stream.normalizeCRLF = true
			expected, _ := query.Matches(nil, viewport.value, find.DefaultLimit)
			for i, span := range expected {
				expected[i] = text.Span{Start: int(viewport.absolute(span.Start)), End: int(viewport.absolute(span.End))}
			}
			var actual []text.Span
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err = stream.walk(ctx, revision, 0, func(span text.Span, _ []int) bool {
				actual = append(actual, span)
				return len(actual) <= len(expected)
			})
			if err != nil || !slices.Equal(actual, expected) {
				t.Fatalf("matches %v, want %v: %v", actual, expected, err)
			}
		})
	}
}

func TestLargeEditFloatingFindAndReplace(t *testing.T) {
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	filler := strings.Repeat("irrelevant "+strings.Repeat("日志", 30)+"\r\n", 1500)
	source := "Cat\r\nCAT long tail\r\n" + filler + "cat\r\n"
	path := filepath.Join(t.TempDir(), "large.out")
	check(os.WriteFile(path, []byte(source), 0600))
	page, err := readLargeTextPage(path, 0, -1, "", "")
	check(err)
	m := newEditorApp()
	ordinary := m.addTab(document{saved: "CAT\ncat\n", eol: "LF"}, "ordinary.txt")
	tab := m.openLoadedFile(openedEditorFile{doc: document{path: path}, largeText: &page})
	defer tab.largeText.close()
	s := openTestSession(t, m)
	wait := func(done func() bool) {
		t.Helper()
		check(s.WaitFor(func(app.Snapshot) bool { return done() }, 5*time.Second))
	}

	check(s.ClickLabel("button:打开完整编辑"))
	wait(tab.hasLargeEdit)
	edit := tab.largeText.editing
	openSearchForTest(t, s, "^(cat)$")
	if !tab.search.open || tab.largeText.searchOpen {
		t.Fatal("Ctrl+F must open the shared dialog")
	}
	check(s.ClickLabel("正则表达式"))
	check(s.ClickLabel("button:计数"))
	wait(func() bool { return tab.search.message == "当前范围共有 2 个匹配" })
	check(s.ClickLabel("button:下一个匹配"))
	wait(func() bool { return edit.searchSelected && edit.searchMatch == (text.Span{Start: 0, End: 3}) })

	check(s.ClickLabel("搜索内容"))
	check(s.Driver().Key(gfx.KeyEnter, 0))
	last := len(source) - len("cat\r\n")
	wait(func() bool { return edit.searchMatch == (text.Span{Start: last, End: last + 3}) })
	if edit.viewport.start == 0 {
		t.Fatal("the match should be outside the initial viewport")
	}
	check(s.Driver().Key(gfx.KeyEnter, gfx.ModifierShift))
	wait(func() bool { return edit.searchMatch == (text.Span{Start: 0, End: 3}) })
	check(s.ClickLabel("循环查找"))
	check(s.ClickLabel("button:上一个匹配"))
	wait(func() bool { return tab.search.message == "此方向没有更多匹配" })
	check(s.ClickLabel("循环查找"))
	check(s.ClickLabel("button:上一个匹配"))
	wait(func() bool { return edit.searchMatch.Start == last })

	check(s.ClickLabel("button:查找全部已打开文档"))
	wait(func() bool { return m.results.open && m.results.total == 4 && !tab.search.open })
	if len(m.results.groups) != 2 || m.results.groups[0].tab != ordinary || m.results.groups[1].tab != tab {
		t.Fatal("all-document search should include the ordinary and complete-edit tabs")
	}

	if handled, err := s.Driver().ShortcutChord(gfx.ModifierControl, 'r'); err != nil || !handled || !tab.search.open || !tab.search.replacing {
		t.Fatalf("Ctrl+R failed to open shared Replace: %t %v", handled, err)
	}
	tab.search.field.SetText("^(cat)$")
	check(s.Frame())
	check(s.ClickLabel("替换为"))
	check(s.Type("$1-new"))
	check(s.ClickLabel("button:替换当前匹配"))
	wait(func() bool { return tab.largeText.replacing == nil && edit.revision.size != int64(len(source)) })
	suffix, err := edit.revision.read(int64(last), edit.revision.size)
	check(err)
	if suffix != "cat-new\r\n" || ordinary.source.text.String() != "CAT\ncat\n" {
		t.Fatalf("replace current selected the wrong document or span: %q", suffix)
	}
	check(s.ClickLabel("button:替换全部已打开文档"))
	wait(func() bool { return m.searchBatch == nil && tab.search.message == "已替换 3 处 · 2 个文件" })
	prefix, err := edit.revision.read(0, int64(len("Cat-new\r\n")))
	check(err)
	if prefix != "Cat-new\r\n" || ordinary.source.text.String() != "CAT-new\ncat-new\n" || len(tab.source.text.String()) > largeEditWindowBytes {
		t.Fatal("multi-document regex replacement did not preserve captures, EOL or the bounded viewport")
	}
	check(s.Driver().Key(gfx.KeyEscape, 0))
	pane, err := s.Locate("textfield:大文件内容")
	check(err)
	if tab.search.open || s.Driver().Snapshot().Focused != pane.ID {
		t.Fatalf("Escape did not return focus to the document: open=%t, focus=%v, pane=%v, focused=%t",
			tab.search.open, s.Driver().Snapshot().Focused, pane.ID, tab.source.focused)
	}
	if !ordinary.source.text.Undo() || ordinary.source.text.String() != "CAT\ncat\n" {
		t.Fatal("ordinary file replacement did not undo")
	}
	tab.undoLargeEdit(false)
	if value, err := edit.revision.read(0, 5); value != "Cat\r\n" || err != nil {
		t.Fatal("large file replacement did not undo")
	}
	tab.undoLargeEdit(false)
	if tab.modified() {
		t.Fatal("undo did not restore the initial large-file revision")
	}
	data, err := os.ReadFile(path)
	check(err)
	if string(data) != source {
		t.Fatal("search and replace must not change the saved file")
	}
}
