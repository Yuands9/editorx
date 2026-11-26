package editor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
)

func openHistoryTestSession(t *testing.T, m *editorApp) *headless.Session {
	t.Helper()
	state := m.historyCompare
	if state == nil {
		t.Fatal("the editor did not open history comparison")
	}
	session, err := headless.Open(ui.Configure(ui.NewPage(state.pageConfig()), state.windowConfig()), nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Close)
	return session
}

func TestHistoryDiffPreservesLineBoundaries(t *testing.T) {
	texts := []string{"", "甲", "甲\n", "甲\n尾", "甲\r\n尾", "\n甲\n尾\n", "🙂\n甲\n尾\n", "尾\n尾\n"}
	for _, previous := range texts {
		for _, current := range texts {
			diff := compareHistoryLines(previous, current)
			result := current
			for index := len(diff.blocks) - 1; index >= 0; index-- {
				start, end, value := diff.replacement(diff.blocks[index], len(current))
				if start < 0 || start > end || end > len(result) {
					t.Fatalf("bad range %d:%d for %q versus %q", start, end, previous, current)
				}
				result = result[:start] + value + result[end:]
			}
			if result != previous {
				t.Fatalf("merging %q into %q produced %q", previous, current, result)
			}
			for _, block := range diff.blocks {
				visible := block.visibleRange(true)
				if !slices.Equal(diff.changes.Rows[visible.Start:visible.End], diff.full.Rows[block.rows.Start:block.rows.End]) {
					t.Fatal("compact view changed the block's original line numbers")
				}
			}
			for _, mark := range diff.changes.Inline {
				if mark.Row >= len(diff.changes.Rows) || diff.changes.Rows[mark.Row].Kind != components.CodeDiffChanged {
					t.Fatalf("misaligned inline change: %+v", mark)
				}
			}
		}
	}
}

func TestHistoryDiffRetainsFourContextLines(t *testing.T) {
	current := make([]string, 46)
	for i := range current {
		current[i] = fmt.Sprintf("Line %02d", i+1)
	}
	history := slices.Clone(current)
	for _, i := range []int{6, 10, 30} {
		history[i] = "History " + current[i]
	}
	diff := compareHistoryLines(strings.Join(history, "\n"), strings.Join(current, "\n"))
	if len(diff.blocks) != 3 {
		t.Fatalf("expected three independent merge blocks, got %d", len(diff.blocks))
	}
	wantRows := []int{-1}
	for i := 2; i < 15; i++ {
		wantRows = append(wantRows, i)
	}
	wantRows = append(wantRows, -1)
	for i := 26; i < 35; i++ {
		wantRows = append(wantRows, i)
	}
	wantRows = append(wantRows, -1)
	if len(diff.changes.Rows) != len(wantRows) {
		t.Fatalf("compact context has %d rows, want %d", len(diff.changes.Rows), len(wantRows))
	}
	for index, row := range diff.changes.Rows {
		if row.LeftLine != wantRows[index] || row.RightLine != wantRows[index] {
			t.Fatalf("incorrect context at display row %d: %+v", index, row)
		}
	}
	if len(diff.separators) != 3 || diff.separators[0] != "省略 2 行" ||
		diff.separators[14] != "省略 11 行" || diff.separators[24] != "省略 11 行" {
		t.Fatalf("incorrect omission markers: %v", diff.separators)
	}
}

func TestHistoryCompareFromEditorAndProject(t *testing.T) {
	project := t.TempDir()
	path := filepath.Join(project, "说明.txt")
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	store := m.documentHistory()
	var contextLines strings.Builder
	for i := range 70 {
		fmt.Fprintf(&contextLines, "第 %02d 行保留原样\n", i+1)
	}
	current := "编辑示例\n当前第一段\n" + contextLines.String() + "当前第二段\n结束\n"
	previous := strings.ReplaceAll(current, "当前", "历史")
	currentPartial := strings.Replace(current, "当前第二段", "历史第二段", 1)
	oldest := time.Now().Add(-2 * time.Hour)
	recordHistoryFixture(t, store, path, "更早的版本\n", oldest)
	recordHistoryFixture(t, store, path, previous, oldest.Add(time.Hour))
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(os.WriteFile(path, []byte(current), 0600))
	m.addProjects(project)
	main := openTestSession(t, m)
	var session *headless.Session
	waitForDiff := func(predicate func(*historyCompareState) bool) {
		t.Helper()
		check(session.WaitFor(func(app.Snapshot) bool {
			state := m.historyCompare
			return state != nil && state.comparison != nil && predicate(state)
		}, 3*time.Second))
	}
	check(main.RightClickLabel("说明.txt"))
	if directory := os.Getenv("VIVA_PREVIEW_DIR"); directory != "" {
		check(main.Screenshot(filepath.Join(directory, "project-file-menu.png")))
	}
	check(main.ClickLabel("menuitem:比对历史…"))
	session = openHistoryTestSession(t, m)
	waitForDiff(func(state *historyCompareState) bool { return len(state.comparison.diff.blocks) == 2 })
	state := m.historyCompare
	if m.active != nil || state.source != path || state.comparison.fromEditor {
		t.Fatal("project history did not compare the closed file")
	}
	if _, shown := main.AccessByRole(gfx.RoleButton, "下一个区别"); shown {
		t.Fatal("history comparison also painted in the main window")
	}
	if right, shown := session.AccessByRole(gfx.RoleTextField, "当前版本编辑区"); !shown || right.ReadOnly {
		t.Fatal("the current side did not expose an editable text field")
	}
	check(session.ClickLabel("button:上一个区别"))
	if state.block != 1 {
		t.Fatal("previous from the first difference did not wrap to the last")
	}
	check(session.ClickLabel("button:下一个区别"))
	if state.block != 0 {
		t.Fatal("next from the last difference did not wrap to the first")
	}
	check(session.ClickLabel("button:下一个区别"))
	check(session.Frame())
	if state.block != 1 || (state.diff.Code.Geometry.OffsetY <= 0) {
		t.Fatal("next difference did not scroll to the second block")
	}
	check(session.ClickLabel("button:上一个区别"))
	if state.block != 0 {
		t.Fatal("previous difference did not select the first block")
	}
	check(session.ClickLabel("button:只看区别"))
	if !state.onlyChanges || len(state.diff.Snapshot.Result.Rows) != 14 || !session.HasText("省略 62 行") {
		t.Fatal("differences-only mode did not show context and the omitted line count")
	}
	if directory := os.Getenv("VIVA_PREVIEW_DIR"); directory != "" {
		check(session.Screenshot(filepath.Join(directory, "history-comparison.png")))
		check(session.Resize(900, 560))
		check(session.Screenshot(filepath.Join(directory, "history-comparison-small.png")))
		check(session.Resize(1440, 900))
	}
	check(session.ClickLabel("button:展示全文件"))
	if state.onlyChanges || len(state.diff.Snapshot.Result.Rows) <= 14 || len(state.diff.Snapshot.RowSeparators) != 0 {
		t.Fatal("full-file mode did not restore unchanged lines")
	}
	check(session.ClickLabel("button:只看区别"))
	check(session.WaitFor(func(app.Snapshot) bool {
		first, hasFirst := session.AccessByRole(gfx.RoleButton, "合并第 1 处历史改动")
		second, hasSecond := session.AccessByRole(gfx.RoleButton, "合并第 2 处历史改动")
		return state.block == 0 && hasFirst && hasSecond && first.Bounds.X == second.Bounds.X && second.Bounds.Y > first.Bounds.Y
	}, time.Second))
	check(session.ClickLabel("button:合并第 2 处历史改动"))
	waitForDiff(func(state *historyCompareState) bool { return len(state.comparison.diff.blocks) == 1 })
	tab := m.active
	if tab == nil || tab.source.text.CommittedString() != currentPartial || tab.doc.saved != current {
		t.Fatal("merging did not change just the second block in the editor buffer")
	}
	if disk, err := os.ReadFile(path); err != nil || string(disk) != current {
		t.Fatalf("merge changed the saved file: %v", err)
	}
	check(session.ClickLabel("button:合并此区别"))
	waitForDiff(func(state *historyCompareState) bool { return len(state.comparison.diff.blocks) == 0 })
	if handled, err := session.Driver().ShortcutChord(gfx.ModifierControl, 'z'); err != nil || !handled {
		t.Fatalf("the last merge could not be undone after the differences disappeared: %v", err)
	}
	waitForDiff(func(state *historyCompareState) bool { return len(state.comparison.diff.blocks) == 1 })
	if tab.source.text.CommittedString() != currentPartial {
		t.Fatal("undoing the last merge also changed the earlier merge")
	}
	if handled, err := session.Driver().ShortcutChord(gfx.ModifierControl, 'z'); err != nil || !handled {
		t.Fatalf("history merge undo was not delivered in the comparison window: %v", err)
	}
	waitForDiff(func(state *historyCompareState) bool { return len(state.comparison.diff.blocks) == 2 })
	if tab.source.text.CommittedString() != current {
		t.Fatal("the merge was not one undo operation")
	}
	check(session.Driver().Key(gfx.KeyEscape, 0))
	check(session.Frame())
	if m.historyCompare != nil {
		t.Fatal("Escape did not close the comparison")
	}
	session.Close()

	working := "未保存的开头\n" + current
	tab.source.replace(working)
	check(main.Frame())
	check(main.RightClickLabel("textfield:代码编辑区"))
	if directory := os.Getenv("VIVA_PREVIEW_DIR"); directory != "" {
		check(main.Screenshot(filepath.Join(directory, "editor-history-menu.png")))
	}
	check(main.ClickLabel("menuitem:比对历史…"))
	session = openHistoryTestSession(t, m)
	waitForDiff(func(state *historyCompareState) bool { return state.comparison.current.saved == working })
	state = m.historyCompare
	if !state.comparison.fromEditor {
		t.Fatal("history ignored the current editing buffer")
	}
	check(session.ClickLabel("button:下一个区别"))
	check(session.ClickLabel("button:下一个区别"))
	tab.source.replace(working + "更新的末尾\n")
	check(session.ClickLabel("button:合并此区别"))
	waitForDiff(func(state *historyCompareState) bool {
		return strings.HasSuffix(state.comparison.current.saved, "更新的末尾\n")
	})
	if tab.source.text.CommittedString() != working+"更新的末尾\n" || !strings.Contains(state.notice, "重新比对") {
		t.Fatal("merging with stale data did not recompare the current buffer")
	}
	check(session.ClickLabel("button:合并此区别"))
	mergedWorking := "未保存的开头\n" + currentPartial + "更新的末尾\n"
	if tab.source.text.CommittedString() != mergedWorking || tab.doc.saved != current {
		t.Fatal("merging a validated block did not preserve the other unsaved edits")
	}
	check(session.ClickLabel("button:" + oldest.Local().Format(time.DateTime)))
	waitForDiff(func(state *historyCompareState) bool { return state.comparison.old.saved == "更早的版本\n" })
	check(session.ClickLabel("button:恢复整个版本"))
	if tab.source.text.CommittedString() != "更早的版本\n" || tab.doc.saved != current {
		t.Fatal("restoring the selected version did not update only the editing buffer")
	}
}

func TestHistoryComparisonEditsCurrentBuffer(t *testing.T) {
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	path := filepath.Join(t.TempDir(), "editable.txt")
	const current = "hello\ncurrent version\n"
	const updated = "中文 hello\ncurrent version\n"
	recordHistoryFixture(t, m.documentHistory(), path, "hello\nprevious version\n", time.Now())
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(os.WriteFile(path, []byte(current), 0600))
	main := openTestSession(t, m)
	m.openHistoryComparison(path, localHistoryVersion{})
	session := openHistoryTestSession(t, m)
	state := m.historyCompare
	check(session.WaitFor(func(app.Snapshot) bool { return state.editor != nil && state.comparison.current.saved == current }, 3*time.Second))
	viewport := state.diff.Code.Geometry.Viewport
	check(session.Driver().Click(viewport.X+32, viewport.Y+14))
	_ = session.Type("left")
	if m.active != nil || state.editor.String() != current {
		t.Fatal("typing on the historical side changed the current document")
	}
	check(session.ClickLabel("textfield:当前版本编辑区"))
	check(session.Driver().Key(gfx.KeyHome, gfx.ModifierControl))
	if m.active != nil {
		t.Fatal("selecting current text unexpectedly opened a tab")
	}
	check(session.Compose("zhongwen"))
	check(session.Type("中文 "))
	check(session.Frame())
	if m.active == nil || state.editor != m.active.source.text || m.active.source.text.CommittedString() != updated || state.comparison.current.saved != updated {
		t.Fatalf("right-side editing did not update the shared buffer: %q", state.editor.String())
	}
	check(main.Frame())
	if !main.HasText("中文 hello") || m.active.doc.saved != current {
		t.Fatal("the main editor did not show the unsaved change")
	}
	if handled, err := session.Driver().ShortcutChord(gfx.ModifierControl, 'z'); err != nil || !handled {
		t.Fatalf("right-side undo was not delivered: %v", err)
	}
	if m.active.source.text.CommittedString() != current {
		t.Fatal("right-side IME edit was not undoable as one operation")
	}
	if handled, err := session.Driver().ShortcutChord(gfx.ModifierControl, 'y'); err != nil || !handled {
		t.Fatalf("right-side redo was not delivered: %v", err)
	}
	if handled, err := session.Driver().ShortcutChord(gfx.ModifierControl, 's'); err != nil || !handled {
		t.Fatalf("right-side save was not delivered: %v", err)
	}
	if disk, err := os.ReadFile(path); err != nil || string(disk) != updated {
		t.Fatalf("saving from history comparison did not save the current version: %q, %v", disk, err)
	}
}

func TestHistoryComparisonLargerText(t *testing.T) {
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	path := filepath.Join(t.TempDir(), "large-history.log")
	line := strings.Repeat("2026-11-26 INFO example entry. ", 4) + "\n"
	body := strings.Repeat(line, (8<<20)/len(line)+1)
	previous, current := "状态：旧版\n"+body, "状态：日版\n"+body
	recordHistoryFixture(t, m.documentHistory(), path, previous, time.Now())
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(os.WriteFile(path, []byte(current), 0600))
	m.openHistoryComparison(path, localHistoryVersion{})
	session := openHistoryTestSession(t, m)
	state := m.historyCompare
	check(session.WaitFor(func(app.Snapshot) bool { return state.comparison != nil }, 8*time.Second))
	if comparison := state.comparison; comparison.oldProblem != "" || comparison.diskProblem != "" || len(comparison.diff.blocks) != 1 {
		t.Fatalf("could not compare the large versions: previous=%q, current=%q, blocks=%d", comparison.oldProblem, comparison.diskProblem, len(comparison.diff.blocks))
	}
	check(session.ClickLabel("button:恢复整个版本"))
	if m.active == nil || m.active.source.text.CommittedString() != previous {
		t.Fatal("restoring a large version did not apply the changed text")
	}
	if handled, err := session.Driver().ShortcutChord(gfx.ModifierControl, 'z'); err != nil || !handled {
		t.Fatalf("undo was not delivered: %v", err)
	}
	if m.active.source.text.CommittedString() != current {
		t.Fatal("the large version restore did not keep its undo record")
	}
}

func TestHistoryComparisonBoundsAndUndo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dense.txt")
	if _, err := decodeHistoryComparison(path, []byte(strings.Repeat("\n", historyCompareLineLimit))); !errors.Is(err, errHistoryCompareLines) {
		t.Fatalf("expected a line limit for dense text, got %v", err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(file.Truncate(localHistoryFileSizeLimit+1), file.Close()); err != nil {
		t.Fatal(err)
	}
	if _, err := readHistoryCurrentFile(context.Background(), path); !errors.Is(err, errHistoryCompareBytes) {
		t.Fatalf("expected a byte limit for the current file, got %v", err)
	}

	m := newEditorApp()
	current := document{path: path, saved: strings.Repeat("a", historyMergeUndoRunes/2+1), eol: "LF"}
	tab := m.addTab(current, filepath.Base(path))
	comparison := &localHistoryComparison{
		old:     document{path: path, saved: strings.Repeat("b", historyMergeUndoRunes/2+1), eol: "LF"},
		current: current,
	}
	comparison.compareLines()
	state := &historyCompareState{owner: m, tab: tab, editor: tab.source.text, comparison: comparison, source: path}
	m.historyCompare = state
	m.mergeHistoryChange(state, false)
	if tab.source.text.CommittedString() != current.saved || !strings.Contains(state.problem, "无法保留撤销记录") {
		t.Fatal("a merge beyond the undo budget changed the document or gave no explanation")
	}
}

func TestEditorStorageCountsAndClearsCache(t *testing.T) {
	directory := t.TempDir()
	write := func(name string, size int) string {
		t.Helper()
		path := filepath.Join(directory, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(strings.Repeat("x", size)), 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	settings := write("session.json", 120)
	version := write(localHistoryDirectory+"/test/snapshot.gz", 53)
	legacyDownload := write("downloads/link-1234/document.txt", 15)
	unknownPreview := write(editorPreviewCacheDirectory+"/notes.html", 12)
	preview := write(editorPreviewCacheDirectory+"/page-1234.html", 23)
	rewrite := write(editorPreviewCacheDirectory+"/.viva-preview-4567", 7)
	cli := write(editorCLICacheDirectory+"/reply-1234/nested/reply.txt", 34)
	m := &editorApp{dataDirectory: directory}
	activePreview, err := m.writePreviewFile("active preview")
	if err != nil {
		t.Fatal(err)
	}
	defer removeEditorPreviewCache(activePreview)
	job, err := (aiSettings{cliDirectory: directory}).newCLIJob()
	if err != nil {
		t.Fatal(err)
	}
	defer removeCLIJob(job)
	activeCLI := filepath.Join(job, "reply.txt")
	if err := os.WriteFile(activeCLI, []byte("active CLI"), 0600); err != nil {
		t.Fatal(err)
	}
	retained := int64(120 + 15 + 12 + len("active preview") + len("active CLI"))
	const available = 23 + 7 + 34
	scan, err := scanEditorStorage(context.Background(), directory, false)
	if err != nil || scan.usage != (editorStorageUsage{data: retained + available, history: 53, cache: available}) {
		t.Fatalf("unexpected storage totals: %+v, %v", scan, err)
	}
	scan, err = scanEditorStorage(context.Background(), directory, true)
	if err != nil || scan.cleared != available || scan.usage != (editorStorageUsage{data: retained, history: 53}) {
		t.Fatalf("unexpected storage after cleanup: %+v, %v", scan, err)
	}
	for _, path := range []string{settings, version, legacyDownload, unknownPreview, activePreview, activeCLI} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{preview, rewrite, cli} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("cache file remained at %s: %v", path, err)
		}
	}
}
