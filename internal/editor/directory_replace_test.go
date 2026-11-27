package editor

import (
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
)

func TestDirectoryReplacementPreservesDraftEncodingAndConflict(t *testing.T) {
	root := t.TempDir()
	openPath, closedPath, conflictPath := filepath.Join(root, "open.txt"), filepath.Join(root, "utf16.txt"), filepath.Join(root, "conflict.txt")
	for _, path := range []string{openPath, conflictPath} {
		if err := os.WriteFile(path, []byte("one target\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	doc := document{encoding: utf16LEEncoding, bom: true, eol: "CRLF"}
	if err := doc.create(closedPath, "中文 target\n"); err != nil {
		t.Fatal(err)
	}
	m := newEditorApp()
	m.load(openPath)
	tab := m.active
	tab.source.text.Insert("draft ")
	session := openTestSession(t, m)
	m.openDirectorySearch(root)
	s := m.directorySearch
	s.query.SetText("target")
	s.replacement.SetText("new")
	m.startDirectorySearch()
	if err := session.Frame(); err != nil {
		t.Fatal(err)
	}
	wait := func(ready func() bool) {
		t.Helper()
		if err := session.WaitFor(func(app.Snapshot) bool { return ready() }, 5*time.Second); err != nil {
			t.Fatal(err)
		}
	}
	wait(func() bool { return s.request == nil })
	if !s.panelOpen || s.open || len(s.result.hits) != 3 {
		t.Fatal("directory results did not stay in a non-modal panel")
	}
	m.chooseDirectoryHit(0)
	if !s.panelOpen {
		t.Fatal("navigation closed search results")
	}
	// Return the non-open files to disk-only targets for the transaction test.
	if m.active != tab {
		m.removeTab(m.active)
		m.activate(tab)
	}
	m.previewDirectoryReplacement()
	wait(func() bool { return s.planRequest == nil })
	if s.plan == nil || len(s.plan.files) != 3 {
		t.Fatal("replacement preview missing", s.problem)
	}
	if directory := os.Getenv("VIVA_PREVIEW_DIR"); directory != "" {
		if err := session.Resize(1100, 700); err != nil {
			t.Fatal(err)
		}
		if err := session.Screenshot(filepath.Join(directory, "directory-replacement.png")); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(conflictPath, []byte("external edit"), 0600); err != nil {
		t.Fatal(err)
	}
	m.startDirectoryReplacement()
	wait(func() bool { return !s.replacing && s.replaceJob == nil })
	if s.replacedFiles != 2 || len(s.replaceProblems) != 1 {
		t.Fatalf("applied=%d errors=%v", s.replacedFiles, s.replaceProblems)
	}
	if tab.source.text.String() != "draft one new\n" || !tab.modified() {
		t.Fatal("open draft was not updated")
	}
	tab.source.text.Undo()
	if tab.source.text.String() != "draft one target\n" {
		t.Fatal("replacement did not undo as one action")
	}
	unchanged, _ := os.ReadFile(openPath)
	if string(unchanged) != "one target\n" {
		t.Fatal("replacement saved the open draft")
	}
	result, err := readDocument(closedPath)
	if err != nil || result.encoding != utf16LEEncoding || !result.bom || result.eol != "CRLF" || result.saved != "中文 new\n" {
		t.Fatal("encoding or EOL changed", err)
	}
	conflict, _ := os.ReadFile(conflictPath)
	if string(conflict) != "external edit" {
		t.Fatal("overwrote external change")
	}
}

func TestDirectorySearchUsesOpenBuffersAndRegexPreview(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.txt")
	if err := os.WriteFile(path, []byte("disk"), 0600); err != nil {
		t.Fatal(err)
	}
	query, err := (searchOptions{mode: searchRegexp}).compile(`value=(\d+)`)
	if err != nil {
		t.Fatal(err)
	}
	buffers := map[string]string{strings.ToLower(path): "value=12\nvalue=34"}
	result, err := searchDirectoryWithBuffers(context.Background(), root, query, nil, nil, buffers)
	if err != nil || len(result.hits) != 2 {
		t.Fatal("unsaved matches missing", err)
	}
	plan, err := planDirectoryReplacement(context.Background(), []string{path}, query, "number=$1", buffers)
	if err != nil || len(plan.files) != 1 || plan.files[0].count != 2 || !strings.Contains(plan.files[0].preview, "number=12") {
		t.Fatal("regex replacement preview failed", err)
	}
}

func TestDirectoryReplacementKeepsUndoBudget(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "large.txt")
	before := strings.Repeat("x", historyMergeUndoRunes+1)
	m := newEditorApp()
	tab := m.addTab(document{path: path, saved: before}, "large.txt")
	session := openTestSession(t, m)
	m.openDirectorySearch(root)
	s := m.directorySearch
	query, err := (searchOptions{mode: searchRegexp}).compile(`(?s).+`)
	if err != nil {
		t.Fatal(err)
	}
	s.plan = &directoryReplacementPlan{query: query, files: []directoryReplacementFile{{path: path, digest: sha256.Sum256([]byte(before)), count: 1, selected: true}}}
	m.startDirectoryReplacement()
	if err := session.WaitFor(func(app.Snapshot) bool { return !s.replacing && s.replaceJob == nil }, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if tab.source.text.String() != before || s.replacedFiles != 0 || len(s.replaceProblems) != 1 || !strings.Contains(s.replaceProblems[0], "可撤销上限") {
		t.Fatal("oversized replacement modified the draft or reported success")
	}
}
