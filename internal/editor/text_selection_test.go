package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

func TestTextSelectionWithShiftAcrossScroll(t *testing.T) {
	for _, full := range []bool{false, true} {
		name, label := "普通文本", "代码编辑区"
		if full {
			name, label = "大文件完整编辑", "大文件内容"
		}
		t.Run(name, func(t *testing.T) {
			check := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			line := "alpha 中文 beta " + strings.Repeat("text ", 48)
			raw := strings.Repeat(line+"\n", 380) + "tail"
			if full {
				raw = strings.ReplaceAll(raw, "\n", "\r\n")
			}
			path := filepath.Join(t.TempDir(), "selection.txt")
			check(os.WriteFile(path, []byte(raw), 0600))
			m := newEditorApp()
			m.dataDirectory = t.TempDir()
			var tab *editorTab
			if full {
				page, err := readLargeTextPage(path, 0, -1, "", "")
				check(err)
				tab = m.addTab(document{path: path}, "selection.txt")
				tab.startLargeText(page)
				defer tab.largeText.close()
			} else {
				m.load(path)
				tab = m.active
			}
			s := openTestSession(t, m)
			check(s.Resize(1060, 670))
			if full {
				check(s.ClickLabel("button:打开完整编辑"))
				check(s.WaitFor(func(app.Snapshot) bool { return tab.hasLargeEdit() }, 4*time.Second))
			}
			pane := tab.source
			hit, err := s.Locate("textfield:" + label)
			check(err)
			at := gfx.Point{X: hit.Bounds.X + 124, Y: hit.Bounds.Y + 56}
			clickOffset := func(clicks int) int64 {
				t.Helper()
				offset, ok := pane.view.Code.OffsetAt(at)
				if !ok {
					t.Fatal("code editor did not lay out its content")
				}
				position := int64(offset)
				if full {
					position = tab.largeText.editing.viewport.absolute(offset)
				}
				check(s.Driver().PressCount(at.X, at.Y, gfx.PointerButtonPrimary, clicks))
				check(s.Driver().ReleaseCount(at.X, at.Y, gfx.PointerButtonPrimary, clicks))
				return position
			}
			shortcut := func(key rune) {
				t.Helper()
				handled, err := s.Driver().ShortcutChord(gfx.ModifierControl, key)
				check(err)
				if !handled {
					t.Fatalf("Ctrl+%c was not handled", key)
				}
			}
			checkCopy := func(start, end int64) {
				t.Helper()
				shortcut('c')
				want := raw[min(start, end):max(start, end)]
				got := s.Driver().Clipboard()
				if got != want {
					t.Fatalf("copied %d bytes, wanted bytes %d through %d", len(got), start, end)
				}
			}

			anchor := clickOffset(1)
			if full {
				bar := tab.largeText.editing.barBounds
				check(s.Click(bar.X+bar.Width/2, bar.Bottom()-3))
			} else {
				check(s.Scroll(gfx.ScrollEvent{X: at.X, Y: at.Y, DeltaY: 5000, Precise: true}))
			}
			check(s.KeyDown("shift"))
			end := clickOffset(2)
			check(s.KeyUp("shift"))
			if end-anchor <= largeEditWindowBytes/2 {
				t.Fatalf("scroll did not reach a distant row: %d to %d", anchor, end)
			}
			checkCopy(anchor, end)

			anchor = clickOffset(1)
			check(s.Keys("ctrl+shift+home"))
			checkCopy(anchor, 0)
			// The anchor of the reversed selection also survives the next Shift click.
			check(s.KeyDown("shift"))
			end = clickOffset(1)
			check(s.KeyUp("shift"))
			checkCopy(anchor, end)

			check(s.Keys("ctrl+home"))
			check(s.Keys("shift+right"))
			checkCopy(0, 1)
			check(s.Keys("ctrl+shift+right"))
			checkCopy(0, int64(len("alpha ")))
			check(s.Keys("shift+end"))
			checkCopy(0, int64(len(line)))
			check(s.Keys("shift+down"))
			check(s.Keys("shift+pagedown"))
			shortcut('c')
			if got := s.Driver().Clipboard(); len(got) <= 2*len(line) || !strings.HasPrefix(raw, got) {
				t.Fatal("Shift+PageDown did not extend the existing selection")
			}
			check(s.Keys("ctrl+shift+end"))
			checkCopy(0, int64(len(raw)))
			shortcut('x')
			if !tab.modified() {
				t.Fatal("cut did not modify the document")
			}
			shortcut('v')
			shortcut('a')
			checkCopy(0, int64(len(raw)))
			check(s.Keys("ctrl+home"))
			lineClick := clickOffset(3)
			lineStart := int64(strings.LastIndexByte(raw[:lineClick], '\n') + 1)
			lineEnd := lineClick + int64(strings.IndexByte(raw[lineClick:], '\n')) + 1
			checkCopy(lineStart, lineEnd)
		})
	}
}

func TestLargeTextBrowseKeyboardSelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preview.out")
	const value = "alpha beta\n中文 text\nlast"
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
	page, err := readLargeTextPage(path, 0, -1, "", "")
	if err != nil {
		t.Fatal(err)
	}
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	tab := m.addTab(document{path: path}, "preview.out")
	tab.startLargeText(page)
	s := openTestSession(t, m)
	if err := s.ClickLabel("大文件内容"); err != nil {
		t.Fatal(err)
	}
	if err := s.Keys("home"); err != nil {
		t.Fatal(err)
	}
	if err := s.Keys("ctrl+shift+right"); err != nil {
		t.Fatal(err)
	}
	if sel := tab.source.text.Selection(); sel.End <= sel.Start {
		t.Fatal("read-only large-file page did not extend the keyboard selection")
	}
	if handled, err := s.Driver().ShortcutChord(gfx.ModifierControl, 'a'); err != nil || !handled {
		t.Fatalf("Ctrl+A did not select the visible page: %v", err)
	}
	if handled, err := s.Driver().ShortcutChord(gfx.ModifierControl, 'c'); err != nil || !handled || s.Driver().Clipboard() != value {
		t.Fatalf("Ctrl+C did not copy the selected page: %v", err)
	}
}
