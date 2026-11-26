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

func TestLargeSelectionGoToTranslation(t *testing.T) {
	for _, complete := range []bool{false, true} {
		name := "browse"
		if complete {
			name = "complete edit"
		}
		t.Run(name, func(t *testing.T) {
			check := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			directory := t.TempDir()
			path := filepath.Join(directory, "text.out")
			raw := strings.Repeat("Hello.\r\n", 900)
			check(os.WriteFile(path, []byte(raw), 0600))
			page, err := readLargeTextPage(path, 0, -1, "", "")
			check(err)
			m := newEditorApp()
			m.dataDirectory = directory
			tab := m.addTab(document{path: path}, "text.out")
			tab.startLargeText(page)
			defer tab.largeText.close()
			s := openTestSession(t, m)
			if complete {
				check(s.ClickLabel("button:打开完整编辑"))
				check(s.WaitFor(func(app.Snapshot) bool { return tab.hasLargeEdit() }, 3*time.Second))
			}
			check(s.ClickLabel("textfield:大文件内容"))
			check(s.Keys("ctrl+home"))
			check(s.RightClickLabel("textfield:大文件内容"))
			selectAll := "全选当前页"
			if complete {
				selectAll = "全选"
			} else if _, present := s.AccessByRole(gfx.RoleMenuItem, "粘贴"); present {
				t.Fatal("read-only page offered to paste")
			}
			if _, present := s.AccessByRole(gfx.RoleMenuItem, "查找…"); !present {
				t.Fatal("large-file context menu has no find command")
			}
			check(s.ClickLabel("menuitem:" + selectAll))
			check(s.RightClickLabel("textfield:大文件内容"))
			check(s.ClickLabel("menuitem:复制"))
			if got := strings.ReplaceAll(s.Driver().Clipboard(), "\r\n", "\n"); got != strings.ReplaceAll(raw, "\r\n", "\n") {
				t.Fatalf("context menu copied %d bytes of %d", len(got), len(raw))
			}
			check(s.RightClickLabel("textfield:大文件内容"))
			check(s.ClickLabel("menuitem:去翻译"))
			state := m.translation
			if state == nil || strings.ReplaceAll(state.query.Text(), "\r\n", "\n") != strings.ReplaceAll(raw, "\r\n", "\n") ||
				state.localLookup || state.request != nil || m.translationEnabled {
				t.Fatal("right-click did not bring the complete selection into translation")
			}
			state.output.SetText("你好。")
			state.hasOutput = true
			check(s.Frame())
			_, replaceErr := s.Locate("button:替换选区")
			if !complete {
				if replaceErr == nil {
					t.Fatal("read-only page offered to replace the selection")
				}
				return
			}
			check(replaceErr)
			check(s.ClickLabel("button:替换选区"))
			edit := tab.largeText.editing
			got, err := edit.revision.read(0, edit.revision.size)
			check(err)
			if got != "你好。" {
				t.Fatalf("replacement produced %q", got)
			}
			if handled, err := s.Driver().ShortcutChord(gfx.ModifierControl, 'z'); err != nil || !handled {
				t.Fatalf("undo replacement: %v", err)
			}
			got, err = edit.revision.read(0, edit.revision.size)
			check(err)
			if got != raw {
				t.Fatal("undo did not restore the complete edit selection")
			}
		})
	}
}
