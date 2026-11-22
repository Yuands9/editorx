package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func TestCompactSessionFallbackAndClosedWindow(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "配置.json")
	source := `{"中文":[1,2,3]}`
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := openSessionStore(filepath.Join(directory, "data"))
	if err != nil {
		t.Fatal(err)
	}
	m := newEditorApp()
	store.attach(m)
	m.load(path)
	m.active.updateResult()
	result := m.active.result.text.String()
	scroll := gfx.Point{Y: 37}
	m.active.result.restoreScroll = &scroll
	m.newDocument()
	m.active.source.replace("保留草稿 中文😀")
	m.active.source.replace("草稿新版本")
	m.active.search.query.Insert("草稿")
	m.findAll(m.active, true)
	second := newTestEditorWithDocument()
	store.attach(second)
	m.requestClose(m.releaseWindowState)
	if !m.windowClosed || m.closedSession == nil || m.tabs != nil || m.active != nil || m.results.origin != nil || len(m.results.groups) != 0 {
		t.Fatal("closed window retained editor buffers or search results")
	}
	snapshot := m.snapshotSession()
	if !snapshot.Tabs[0].SavedMatchesSource || snapshot.Tabs[0].Saved != "" || snapshot.Tabs[0].Result.Content != "" {
		t.Fatal("clean source or generated JSON still duplicated in snapshot")
	}
	if snapshot.Tabs[1].Source.Content != "草稿新版本" {
		t.Fatal("closed snapshot lost draft")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	second.active.source.replace("另一个窗口继续编辑")
	if err := store.save(); err != nil {
		t.Fatal(err)
	}
	restored, err := openSessionStore(store.directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(restored.windows) != 2 || restored.windows[0].tabs[0].source.text.String() != source || restored.windows[0].tabs[0].modified() {
		t.Fatal("missing clean file fallback lost content or became modified")
	}
	if restored.windows[0].tabs[0].result.text.String() != result || restored.windows[0].tabs[0].result.restoreScroll.Y != 37 {
		t.Fatal("JSON result or view state not rebuilt")
	}
	if restored.windows[0].active.source.text.String() != "草稿新版本" || restored.windows[1].active.source.text.String() != "另一个窗口继续编辑" {
		t.Fatal("multiwindow drafts not restored")
	}
}

func TestSessionRecoveredBackupRepairsPrimary(t *testing.T) {
	store, err := openSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := newTestEditorWithDocument()
	store.attach(m)
	m.active.source.replace("原先草稿")
	if err := store.save(); err != nil {
		t.Fatal(err)
	}
	m.active.source.replace("后来草稿")
	if err := store.save(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.directory, "session.json")
	if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	restored, err := openSessionStore(store.directory)
	if err != nil {
		t.Fatal(err)
	}
	if restored.windows[0].active.source.text.String() != "原先草稿" {
		t.Fatal("backup not recovered")
	}
	if err := restored.save(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !json.Valid(data) {
		t.Fatal("unchanged recovered session did not repair primary", err)
	}
	backup, err := os.ReadFile(path + ".bak")
	if err != nil || !json.Valid(backup) {
		t.Fatal("recovery replaced backup with corrupt data", err)
	}
}

func TestClosedSearchResultsReleaseAndRefill(t *testing.T) {
	m := newEditorApp()
	first := m.addTab(document{saved: strings.Repeat("hit\n", 10000), eol: "LF"}, "first.txt")
	second := m.addTab(document{saved: "hit 中文\nhit", eol: "LF"}, "second.txt")
	first.search.query.Insert("hit")
	m.findAll(first, true)
	m.results.open = false
	m.closeTab(first)
	if m.results.origin != nil || m.results.pane != nil {
		t.Fatal("hidden results retain closed origin")
	}
	for _, group := range m.results.groups {
		if group.tab == first {
			t.Fatal("hidden results retain closed group")
		}
	}
	m.results.refresh(m)
	if m.results.total != 2 || m.results.truncated {
		t.Fatal("remaining results did not refill after capped group closed")
	}
	m.closeTab(second)
	if len(m.results.rows) != 0 {
		t.Fatal("closed results retain snippets")
	}
}

func TestSearchSnippetsKeepUnicodeColumns(t *testing.T) {
	prefix := strings.Repeat("中文😀", 30)
	suffix := strings.Repeat("后缀😀", 45)
	source := prefix + "hit" + suffix + "hit\r\n中文hit"
	first, second, third := len(prefix), len(prefix)+3+len(suffix), len(prefix)+3+len(suffix)+5+6
	results := searchResults{groups: []searchResultGroup{{source: source, matches: []text.Span{{Start: first, End: first + 3}, {Start: second, End: second + 3}, {Start: third, End: third + 3}}}}}
	results.buildRows()
	if len(results.rows) != 4 {
		t.Fatal("missing results")
	}
	a, b, c := results.rows[1], results.rows[2], results.rows[3]
	if a.column != 91 || b.column != 229 || c.column != 3 || c.line != 2 {
		t.Fatalf("incorrect Unicode columns: %+v %+v %+v", a, b, c)
	}
	if a.before != "…"+strings.Repeat("中文😀", 8) || len([]rune(a.after)) != 101 || !strings.HasSuffix(a.after, "…") || b.after != "" {
		t.Fatalf("incorrect bounded snippets: %+v %+v", a, b)
	}
}

func TestSessionExternalDamageKeepsBackupAndRepairsCurrent(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "modified", true: "missing"}[missing], func(t *testing.T) {
			store, err := openSessionStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			m := newTestEditorWithDocument()
			store.attach(m)
			m.active.source.replace("已备份草稿")
			if err := store.save(); err != nil {
				t.Fatal(err)
			}
			m.active.source.replace("稍后草稿")
			if err := store.save(); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(store.directory, "session.json")
			backup, err := os.ReadFile(path + ".bak")
			if err != nil {
				t.Fatal(err)
			}
			if missing {
				err = os.Remove(path)
			} else {
				err = os.WriteFile(path, []byte("broken"), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			m.active.source.replace("最后草稿必须保留")
			m.requestClose(m.releaseWindowState)
			if !m.windowClosed {
				t.Fatal("damaged primary prevented normal close", m.errorText)
			}
			preserved, err := os.ReadFile(path + ".bak")
			if err != nil || string(preserved) != string(backup) {
				t.Fatal("external damage replaced valid backup", err)
			}
			again, err := openSessionStore(store.directory)
			if err != nil || again.windows[0].active.source.text.String() != "最后草稿必须保留" {
				t.Fatal("latest draft not saved", err)
			}
		})
	}
}
