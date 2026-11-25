package editor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

func TestLargeTextPageBoundariesAndSearch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.out")
	const needle = "跨页标记"
	prefix := "\ufefffirst\r\n" + strings.Repeat("a", largeTextPageBytes-len("\ufefffirst\r\n")-2)
	raw := prefix + needle + strings.Repeat("😀", 37000) + "\r\n最后一行 " + needle
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	var rendered strings.Builder
	for index := int64(0); index < largeTextPageCount(int64(len(raw))); index++ {
		page, err := readLargeTextPage(path, index, -1, "", "")
		if err != nil || !utf8.ValidString(page.value) || len(page.value) > largeTextPageBytes+4096 {
			t.Fatalf("invalid page %d, size %d: %v", index, len(page.value), err)
		}
		rendered.WriteString(page.value)
	}
	expected := strings.ReplaceAll(strings.TrimPrefix(raw, "\ufeff"), "\r\n", "\n")
	if rendered.String() != expected {
		t.Fatal("paging dropped or duplicated text")
	}
	ctx := context.Background()
	first, err := findLargeText(ctx, path, needle, 0, 1, "")
	if err != nil || first != int64(len(prefix)) {
		t.Fatalf("forward search missed a cross-page match: %d, %v", first, err)
	}
	last, err := findLargeText(ctx, path, needle, int64(len(raw)), -1, "")
	if err != nil || last != int64(strings.LastIndex(raw, needle)) {
		t.Fatalf("reverse search missed the last match: %d, %v", last, err)
	}
	before, err := findLargeText(ctx, path, needle, last, -1, "")
	if err != nil || before != first {
		t.Fatalf("reverse search skipped an earlier match: %d, %v", before, err)
	}
	shown, err := readLargeTextPage(path, 0, first, needle, "")
	if err != nil {
		t.Fatal(err)
	}
	if string([]rune(shown.value)[shown.selectionStart:shown.selectionEnd]) != needle {
		t.Fatal("search did not select the entire match across the page boundary")
	}
}

func TestLargeTextBrowsingAndSession(t *testing.T) {
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "sample.out")
	file, err := os.Create(path)
	must(err)
	line := strings.Repeat("x", 60) + "\r\n"
	block := []byte(strings.Repeat(line, 4096))
	for written := 0; written < largeTextThreshold; written += len(block) {
		_, err = file.Write(block)
		must(err)
	}
	const marker = "末尾定位-测试"
	_, err = file.WriteString(marker + "\n")
	must(err)
	must(file.Close())
	store, err := openSessionStore(filepath.Join(directory, "data"))
	must(err)
	m := newEditorApp()
	store.attach(m)
	m.load(path)
	tab := m.active
	if tab == nil || tab.largeText == nil || tab.modified() || len(tab.doc.saved) != 0 ||
		len(tab.source.text.String()) > largeTextPageBytes+4096 {
		t.Fatal("opening a large text file did not use a bounded read-only page")
	}
	session := openTestSession(t, m)
	must(session.Resize(1060, 670))
	must(session.ClickLabel("button:下一页"))
	if tab.largeText.index != 1 {
		t.Fatal("next page did not move")
	}
	must(session.Frame())
	var bounds gfx.Rect
	for _, hit := range session.Snapshot().Hits {
		if hit.Label == "大文件内容" && hit.Bounds.Width > bounds.Width {
			bounds = hit.Bounds
		}
	}
	must(session.Scroll(gfx.ScrollEvent{X: bounds.X + bounds.Width/2, Y: bounds.Y + 30, DeltaY: -70, Precise: true}))
	if tab.largeText.index != 0 {
		t.Fatal("scrolling up at the page boundary did not show the previous page")
	}
	must(session.Driver().Press(bounds.X+25, bounds.Y+30, gfx.PointerButtonPrimary))
	must(session.Driver().Move(bounds.X+100, bounds.Y+30))
	must(session.Driver().Release(bounds.X+100, bounds.Y+30, gfx.PointerButtonPrimary))
	if selection := tab.source.text.Selection(); selection.Start == selection.End {
		t.Fatal("mouse drag could not select text")
	}
	must(session.ClickLabel("button:查找"))
	must(session.Type(marker))
	must(session.Keys("enter"))
	must(session.WaitFor(func(app.Snapshot) bool { return tab.largeText.search == nil }, 3*time.Second))
	from := tab.source.text.Selection()
	source := tab.source.text.String()
	if selected := source[tab.source.text.ByteOffset(from.Start):tab.source.text.ByteOffset(from.End)]; selected != marker {
		t.Fatalf("full-file search selected %q", selected)
	}
	must(session.ClickLabel("button:关闭查找"))
	must(session.Frame())
	handled, err := session.Driver().ShortcutChord(gfx.ModifierControl, 'c')
	must(err)
	if !handled || session.Driver().Clipboard() != marker {
		t.Fatalf("could not copy the selection: handled=%t focus=%t bytes=%d", handled, tab.source.focused, len(session.Driver().Clipboard()))
	}
	page := tab.largeText.index
	must(store.save())
	stat, err := os.Stat(filepath.Join(store.directory, "session.json"))
	must(err)
	if stat.Size() > 16<<10 {
		t.Fatal("session included file content")
	}
	restored, err := openSessionStore(store.directory)
	must(err)
	again := restored.windows[0].active
	if again == nil || again.largeText == nil || again.largeText.index != page || !strings.Contains(again.source.text.String(), marker) {
		t.Fatal("restoring the session lost the saved page")
	}
	if !m.runEditorShortcut(shortcutCloseTab) || m.active != nil {
		t.Fatal("large read-only tab did not close")
	}
	original := "切换到完整编辑\r\n"
	must(os.WriteFile(path, []byte(original), 0600))
	// The existing tab refreshes from the file on disk, even after truncation.
	again.moveLargeText(-1, true)
	if again.largeText.index != 0 || again.source.text.String() != strings.ReplaceAll(original, "\r\n", "\n") {
		t.Fatal("jumping to the end did not refresh the changed file")
	}
	editSession := openTestSession(t, restored.windows[0])
	must(editSession.Resize(1060, 670))
	must(editSession.ClickLabel("button:打开完整编辑"))
	if err := editSession.WaitFor(func(app.Snapshot) bool { return again.largeText.editing != nil }, 3*time.Second); err != nil {
		t.Fatalf("full editor did not open: %v, problem=%q, loading=%t", err, again.largeText.problem, again.largeText.edit != nil)
	}
	if again.readOnlyPreview() || again.source.readOnly || again.doc.eol != "CRLF" {
		t.Fatal("full editor was not restored")
	}
	_, err = editSession.Driver().ShortcutChord(gfx.ModifierControl, 'a')
	must(err)
	must(editSession.Type("可以编辑\n"))
	_, err = editSession.Driver().ShortcutChord(gfx.ModifierControl, 's')
	must(err)
	must(editSession.WaitFor(func(app.Snapshot) bool { return !again.modified() || restored.windows[0].showError }, 3*time.Second))
	if restored.windows[0].showError {
		t.Fatal(restored.windows[0].errorText)
	}
	changed, err := os.ReadFile(path)
	must(err)
	if string(changed) != "可以编辑\r\n" {
		t.Fatal("saving through the full editor changed the file encoding or line ending")
	}
}
