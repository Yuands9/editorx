package editor

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/window"
)

func TestLargeEditPreservesEncodingAndMixedLineEndings(t *testing.T) {
	for _, enc := range []textEncoding{utf8Encoding, utf16LEEncoding, utf16BEEncoding, gbkEncoding, gb18030Encoding, big5Encoding, shiftJISEncoding, windows1252Encoding, latin1Encoding} {
		t.Run(string(enc), func(t *testing.T) {
			char := "語"
			if enc == windows1252Encoding || enc == latin1Encoding {
				char = "é"
			}
			content := strings.Repeat("X", largeTextPageBytes-3) + strings.Repeat(char, 100) + "\r\nfirst\nlast\r\n"
			path := filepath.Join(t.TempDir(), "sample.out")
			doc := document{encoding: enc, bom: len(enc.bomPrefix()) > 0}
			raw, err := doc.fileBytes(content)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			store, err := newLargeEditStorage()
			if err != nil {
				t.Fatal(err)
			}
			defer store.close()
			ctx := context.Background()
			loaded, err := loadLargeEdit(ctx, store, path, enc, 0)
			if err != nil || loaded.doc.eol != "混合" || loaded.doc.encoding != enc || loaded.doc.bom != doc.bom {
				t.Fatalf("metadata or load: %v %+v", err, loaded.doc)
			}
			position := int64(strings.Index(content, "first"))
			next, err := loaded.revision.apply(ctx, []largeEditChange{{Start: position, End: position + 5, Text: char}})
			if err != nil {
				t.Fatal(err)
			}
			if at, err := next.lineStart(1); err != nil || at != position {
				t.Fatalf("line start %d, want %d, err %v", at, position, err)
			}
			if line, err := next.lineAt(position + int64(len(char)) + 1); err != nil || line != 2 {
				t.Fatalf("line %d, err %v", line, err)
			}
			if _, err := saveLargeEdit(ctx, loaded.doc, path, next); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			want, err := doc.fileBytes(strings.Replace(content, "first", char, 1))
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("saved bytes differ: %v", err)
			}
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if enc == gbkEncoding {
				bad, err := loaded.revision.apply(ctx, []largeEditChange{{Start: position, End: position, Text: "😀"}})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := saveLargeEdit(ctx, loaded.doc, path, bad); err == nil {
					t.Fatal("unencodable text was saved")
				}
				if bytesAfter, err := os.ReadFile(path); err != nil || !bytes.Equal(bytesAfter, raw) {
					t.Fatal("failed encoding replaced the original file")
				}
			}
			if err := os.WriteFile(path, append(raw, '!'), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := saveLargeEdit(ctx, loaded.doc, path, next); err == nil {
				t.Fatal("overwrote an external modification")
			}
		})
	}
}

func TestLargeEditSearchAcrossPiecesAndDenseReplace(t *testing.T) {
	store, err := newLargeEditStorage()
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	p, err := store.append([]byte(strings.Repeat("x", largeTextPageBytes-3) + "甲尾 end\n" + strings.Repeat("va ", 20000)))
	if err != nil {
		t.Fatal(err)
	}
	r := &largeEditRevision{store: store}
	r.addPiece(p)
	ctx := context.Background()
	position := int64(largeTextPageBytes - 3 + len("甲"))
	next, err := r.apply(ctx, []largeEditChange{{Start: position, End: position, Text: "中"}})
	if err != nil {
		t.Fatal(err)
	}
	query := "甲中尾"
	for _, direction := range []int{1, -1} {
		start := int64(0)
		if direction < 0 {
			start = next.size
		}
		match, err := findLargeEdit(ctx, next, query, start, direction)
		if match != int64(largeTextPageBytes-3) || err != nil {
			t.Fatalf("direction %d missed cross-piece match: %d, %v", direction, match, err)
		}
	}
	literal, err := (searchOptions{CaseSensitive: true}).compile("va")
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := newLargeEditQuery(literal)
	if err != nil {
		t.Fatal(err)
	}
	result, err := replaceAllLargeEdit(ctx, next, replacement, "茶")
	if err != nil || result.count != 20000 || len(result.revision.pieces) != 1 {
		t.Fatalf("dense replacement count %d: %v", result.count, err)
	}
	tail, err := result.revision.read(result.revision.size-4, result.revision.size)
	if tail != "茶 " || err != nil {
		t.Fatalf("last replacement missing: %q, %v", tail, err)
	}

	for _, revisions := range [][2]*largeEditRevision{{r, next}, {next, r}, {next, result.revision}, {result.revision, next}} {
		changes, err := inlineLargeEditChanges(revisions[1], largeEditSpans(revisions[0], revisions[1]))
		if err != nil {
			t.Fatal(err)
		}
		restored, err := revisions[0].apply(ctx, changes)
		if err != nil {
			t.Fatal(err)
		}
		actual, err := restored.read(0, restored.size)
		if err != nil {
			t.Fatal(err)
		}
		expected, err := revisions[1].read(0, revisions[1].size)
		if err != nil || actual != expected {
			t.Fatal("snapshot changes do not reconstruct the target revision")
		}
	}
}

func TestLargeEditViewportKeyboardIMEAndDraft(t *testing.T) {
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(t.TempDir(), "sample.out")
	line := strings.Repeat("a文", 80) + "\r\n"
	raw := strings.Repeat(line, 1800) + "END"
	check(os.WriteFile(path, []byte(raw), 0600))
	page, err := readLargeTextPage(path, 0, -1, "", "")
	check(err)
	m := newEditorApp()
	tab := m.addTab(document{path: path}, "sample")
	tab.startLargeText(page)
	defer tab.largeText.close()
	session := openTestSession(t, m)
	check(session.Resize(1060, 670))
	check(session.ClickLabel("button:打开完整编辑"))
	check(session.WaitFor(func(app.Snapshot) bool { return tab.largeText.editing != nil }, 3*time.Second))
	ed := tab.largeText.editing
	if ed.revision.breaks != 1800 || len(tab.source.text.String()) > largeEditWindowBytes {
		t.Fatal("editor did not bound its visible text")
	}
	startWindow := ed.viewport.start
	for i := 0; i < 12; i++ {
		check(session.Scroll(gfx.ScrollEvent{X: ed.bounds.X + 120, Y: ed.bounds.Y + 120, DeltaY: 330, Precise: true}))
	}
	if ed.viewport.start <= startWindow || ed.top == 0 {
		t.Fatalf("wheel did not cross the viewport boundary: top=%d, windowStart=%d", ed.top, ed.viewport.start)
	}
	if ed.barBounds.Width > 0 {
		check(session.Driver().Click(ed.barBounds.X+6, ed.barBounds.Bottom()-3))
		line, err := ed.revision.lineAt(ed.top)
		check(err)
		if line < 1600 {
			t.Fatalf("scrollbar did not navigate the whole file: line %d", line)
		}
	} else {
		t.Fatal("global document scrollbar is missing")
	}
	check(m.revealTextLine(tab, 1751))
	check(session.Frame())
	start := ed.position.caret
	original := ed.revision
	check(session.Driver().Compose("zhong"))
	if tab.modified() || ed.revision != original || !strings.Contains(tab.source.text.String(), "zhong") {
		t.Fatal("IME preview did not stay in the viewport")
	}
	check(session.Type("中"))
	if !tab.modified() || ed.position.caret != start+int64(len("中")) {
		t.Fatal("IME commit did not update the document")
	}
	shortcut := func(key rune) {
		t.Helper()
		handled, err := session.Driver().ShortcutChord(gfx.ModifierControl, key)
		check(err)
		if !handled {
			t.Fatalf("ctrl+%c not handled", key)
		}
	}
	shortcut('z')
	if tab.modified() || ed.revision != original {
		t.Fatal("undo did not restore the pre-IME revision")
	}
	shortcut('y')
	if !tab.modified() {
		t.Fatal("redo lost the committed IME edit")
	}
	check(session.Driver().Key(gfx.KeyHome, gfx.ModifierControl))
	if ed.position.caret != 0 {
		t.Fatal("Ctrl+Home did not move to the document start")
	}
	check(session.Driver().Key(gfx.KeyEnd, gfx.ModifierControl|gfx.ModifierShift))
	if ed.position.anchor != 0 || ed.position.caret != ed.revision.size {
		t.Fatalf("Ctrl+Shift+End did not select the whole document: %+v", ed.position)
	}
	shortcut('c')
	if len(session.Driver().Clipboard()) != int(ed.revision.size) {
		t.Fatal("copy did not include offscreen text")
	}
	tab.moveLargeEditCaret(start, false)
	check(session.Frame())
	tab.handleLargeEditText(window.TextEvent{Kind: window.TextSetMarked, CompositionPhase: text.CompositionUpdate, Text: "ni", MarkedSelection: window.TextRange{Start: 2, End: 2}})
	if _, ok := tab.source.text.Composition(); !ok {
		t.Fatal("explicit composition did not start")
	}
	rev := ed.revision
	tab.handleLargeEditText(window.TextEvent{Kind: window.TextCancelComposition, CompositionPhase: text.CompositionCancel})
	if _, ok := tab.source.text.Composition(); ok || ed.revision != rev {
		t.Fatal("cancelled composition changed document or left marked text")
	}
	tab.search.field.SetText("中")
	m.findAll(tab, false)
	check(session.WaitFor(func(app.Snapshot) bool { return m.results.total == 1 }, 3*time.Second))
	result := m.results.groups[0].stream.hits[0]
	if result.line != 1751 || int64(result.span.Start) != start {
		t.Fatal("find all did not see an unsaved edit at its absolute line")
	}
	tab.search.replaceField.SetText("新词")
	tab.replaceInLargeEdit(true)
	check(session.WaitFor(func(app.Snapshot) bool { return tab.largeText.replacing == nil }, 3*time.Second))
	found, err := findLargeEdit(context.Background(), ed.revision, "新词", 0, 1)
	check(err)
	if found != start {
		t.Fatal("whole-document replacement did not use the unsaved revision")
	}
	snapshot := m.snapshotSession()
	data, err := json.Marshal(snapshot)
	check(err)
	if len(data) > 12<<10 || snapshot.Tabs[0].Source.Content != "" {
		t.Fatal("session includes viewport or original contents")
	}
	restored := newEditorApp()
	restored.restoreSession(snapshot)
	defer restored.active.largeText.close()
	again := openTestSession(t, restored)
	check(again.WaitFor(func(app.Snapshot) bool { return restored.active.largeText.editing != nil }, 3*time.Second))
	if !restored.active.modified() {
		t.Fatal("unsaved edit was not restored")
	}
	reopened := restored.active.largeText.editing.revision
	found, err = findLargeEdit(context.Background(), reopened, "新词", 0, 1)
	check(err)
	if found != start {
		t.Fatal("restored content lost the replacement")
	}
	m.saveTab(tab, false)
	check(session.WaitFor(func(app.Snapshot) bool { return tab.largeText.saving == nil }, 3*time.Second))
	if m.showError || tab.modified() || len(tab.snapshotLargeEdit(m.dataDirectory).Changes) != 0 {
		t.Fatalf("save did not mark the revision clean, error=%s", m.errorText)
	}
	tab.undoLargeEdit(false)
	draft := tab.snapshotLargeEdit(m.dataDirectory)
	check(os.WriteFile(path, []byte("different original\n"), 0600))
	conflict := newEditorApp()
	conflict.restoreSession(m.snapshotSession())
	defer conflict.active.largeText.close()
	conflictSession := openTestSession(t, conflict)
	check(conflictSession.WaitFor(func(app.Snapshot) bool {
		return strings.Contains(conflict.active.largeText.problem, "原文件已变化")
	}, 3*time.Second))
	if conflict.active.largeText.editing != nil || len(draft.Changes) == 0 || len(conflict.active.snapshotLargeEdit(conflict.dataDirectory).Changes) != len(draft.Changes) {
		t.Fatal("conflicting recovery lost data or applied stale byte coordinates")
	}
}

func TestLargeEditStreamsDraftAndBackup(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "draft.out")
	const original = "original\r\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	largeStore, err := newLargeEditStorage()
	if err != nil {
		t.Fatal(err)
	}
	defer largeStore.close()
	loaded, err := loadLargeEdit(ctx, largeStore, path, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	m := newEditorApp()
	store, err := openSessionStore(filepath.Join(dir, "session"))
	if err != nil {
		t.Fatal(err)
	}
	store.attach(m)
	tab := m.addTab(loaded.doc, "draft.out")
	tab.startLargeText(largeTextPage{})
	if err := tab.enterLargeEdit(loaded, loaded.revision, largeEditPosition{}, 0); err != nil {
		t.Fatal(err)
	}
	content := "xy" + strings.Repeat("拉😀\r\n", 130000)
	tab.replaceLargeEdit(0, 0, content, false)

	if err := store.save(); err != nil {
		t.Fatal(err)
	}
	sessionPath := filepath.Join(store.directory, "session.json")
	info, err := os.Stat(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	draft := tab.snapshotLargeEdit(store.directory)
	if info.Size() >= 16<<10 || draft.Patch == "" || len(draft.Changes) != 0 {
		t.Fatal("large draft contents were not streamed to a separate file")
	}
	saved, err := openSessionStore(store.directory)
	if err != nil {
		t.Fatal(err)
	}
	staged := saved.windows[0].active.largeText.editSession
	if staged == nil || staged.patchPath != draft.patchPath {
		t.Fatal("session did not bind the streamed draft")
	}

	backup := filepath.Join(dir, "draft.zip")
	if err := writeEditorBackup(ctx, backup, savedSession{Version: editorSessionVersion, Windows: []windowSession{m.snapshotSession()}}); err != nil {
		t.Fatal(err)
	}
	imported, err := readEditorBackupDrafts(backup, filepath.Join(dir, "restored"))
	if err != nil {
		t.Fatal(err)
	}
	restored := imported.Windows[0].Tabs[0].LargeEdit
	if restored.patchPath == draft.patchPath || restored.Patch == draft.Patch {
		t.Fatal("import did not make an independent draft")
	}
	verify := func(draft *largeEditSession) {
		t.Helper()
		opening, err := restoreLargeEditOpening(ctx, loaded, draft)
		if err != nil {
			t.Fatal(err)
		}
		r := opening.loaded.revision
		if r.size != int64(len(content)+len(original)) || r.breaks != 130001 {
			t.Fatalf("restored file has wrong size or line count")
		}
		at, err := r.lineStart(129999)
		if err != nil {
			t.Fatal(err)
		}
		tail, err := r.read(at, r.size)
		if err != nil || tail != "拉😀\r\n"+original {
			t.Fatal("streamed patch lost the last line")
		}
	}
	verify(staged)
	verify(restored)
	if err := os.WriteFile(restored.patchPath, []byte("invalid data"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := restoreLargeEditOpening(ctx, loaded, restored); err == nil {
		t.Fatal("accepted a corrupted streamed draft")
	}
}

func TestLargeEditTabAndLineCommands(t *testing.T) {
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(t.TempDir(), "commands.out")
	raw := "a\r\nb\nc"
	check(os.WriteFile(path, []byte(raw), 0600))
	page, err := readLargeTextPage(path, 0, -1, "", "")
	check(err)
	m := newEditorApp()
	tab := m.addTab(document{path: path}, "commands")
	tab.startLargeText(page)
	defer tab.largeText.close()
	s := openTestSession(t, m)
	check(s.ClickLabel("button:打开完整编辑"))
	check(s.WaitFor(func(app.Snapshot) bool { return tab.largeText.editing != nil }, 3*time.Second))
	read := func() string {
		t.Helper()
		revision := tab.largeText.editing.revision
		value, err := revision.read(0, revision.size)
		check(err)
		return value
	}
	check(s.Driver().Key(gfx.KeyTab, 0))
	if !tab.modified() || !strings.HasSuffix(read(), raw) {
		t.Fatal("Tab changed the visible copy instead of the full buffer")
	}
	tab.undoLargeEdit(false)
	check(m.revealTextLine(tab, 3))
	check(s.Frame())
	check(s.Driver().Key(gfx.KeyUp, gfx.ModifierAlt))
	if read() != "a\r\nc\nb" {
		t.Fatal("moving the last line changed the original mixed line endings")
	}
	_, err = s.Driver().ShortcutChord(gfx.ModifierControl, 'd')
	check(err)
	if read() != "a\r\nc\nc\nb" {
		t.Fatal("duplicate line failed")
	}
	_, err = s.Driver().ShortcutChord(gfx.ModifierControl, 'l')
	check(err)
	if read() != "a\r\nc\nb" {
		t.Fatal("delete line failed")
	}
}
