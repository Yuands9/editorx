package editor

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

type gitConflictBlock struct {
	start, end   int // Byte offsets in the unchanged buffer snapshot.
	ours, theirs string
}

func gitMetadataPath(root, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(root, path)
}

func gitConflictBlocks(value string) []gitConflictBlock {
	var blocks []gitConflictBlock
	start, ours, base, theirs, offset := -1, 0, -1, -1, 0
	for _, line := range strings.SplitAfter(value, "\n") {
		switch {
		case strings.HasPrefix(line, "<<<<<<< "):
			start, ours, base, theirs = offset, offset+len(line), -1, -1
		case start >= 0 && theirs < 0 && strings.HasPrefix(line, "||||||| "):
			base = offset
		case start >= 0 && strings.TrimRight(line, "\r\n") == "=======":
			if base < 0 {
				base = offset
			}
			theirs = offset + len(line)
		case start >= 0 && theirs >= 0 && strings.HasPrefix(line, ">>>>>>> "):
			blocks = append(blocks, gitConflictBlock{start: start, end: offset + len(line), ours: value[ours:base], theirs: value[theirs:offset]})
			start = -1
		}
		offset += len(line)
	}
	return blocks
}

func unresolvedGitMarkers(value string) bool {
	for _, line := range strings.Split(value, "\n") {
		if strings.HasPrefix(line, "<<<<<<< ") || strings.HasPrefix(line, ">>>>>>> ") || strings.HasPrefix(line, "||||||| ") || strings.TrimSuffix(line, "\r") == "=======" {
			return true
		}
	}
	return false
}

func (m *editorApp) openGitConflict(change gitChange) {
	path := filepath.Join(m.tools.git.snapshot.root, change.path)
	m.loadTextFile(path, "", func(tab *editorTab) {
		// Conflict separators resemble Markdown headings; preserve the file's
		// editing mode while resolving instead of showing a format suggestion.
		tab.formatPromptOpen, tab.suggestedFormat = false, ""
		if !historyEditableTab(tab) {
			m.tools.git.issue = uiText("请在普通文本编辑模式处理冲突")
			return
		}
		m.tools.git.conflict = tab
		m.status = uiText("处理冲突：可采用任一侧或手动编辑，Ctrl+Z 可撤销")
	})
}

func (m *editorApp) gitConflictToolbar(c ui.C, busy bool) {
	git := &m.tools.git
	tab := git.conflict
	if tab == nil || m.tabAtPath(tab.doc.path) != tab {
		git.conflict = nil
		return
	}
	if m.active != tab {
		return
	}
	value := tab.source.text.String()
	blocks := gitConflictBlocks(value)
	current := 0
	caret := tab.source.text.ByteOffset(tab.source.text.Caret())
	for i, block := range blocks {
		if caret >= block.start {
			current = i
		}
	}
	busy = busy || tab.savingText != nil || tab.source.text.CompositionActive()
	ui.Row(c).FillWidth().Gap(4).Items(ui.Center).Children(func() {
		ui.Text(c, fmt.Sprintf(uiText("冲突处理 · 剩余 %d 块"), len(blocks))).FontSize(12).Muted()
		for _, choice := range []string{"采用当前分支", "采用传入分支", "保留双方"} {
			if ui.Button(c.Key(choice), choice).Ghost().Height(27).Disabled(busy || len(blocks) == 0).Clicked() {
				c.After(func() {
					if tab.source.text.String() != value {
						return
					}
					block := blocks[current]
					replacement := block.ours
					if choice == "采用传入分支" {
						replacement = block.theirs
					} else if choice == "保留双方" {
						replacement += block.theirs
					}
					start, end, replacement := trimHistoryReplacement(value, block.start, block.end, replacement)
					if utf8.RuneCountInString(value[start:end])+utf8.RuneCountInString(replacement) > historyMergeUndoRunes {
						git.issue = uiText("这块冲突超过可撤销上限，请分段编辑")
						return
					}
					tab.source.text.ReplaceRange(text.Range{Start: tab.source.text.RuneIndex(start), End: tab.source.text.RuneIndex(end)}, replacement)
					tab.source.text.MoveTo(utf8.RuneCountInString(value[:block.start]), false)
					m.focusActiveSource()
				})
			}
		}
		if ui.Button(c, uiText("下一个冲突")).Icon(historyDownIcon).IconOnly().Ghost().Size(27, 27).Disabled(len(blocks) == 0 || busy).Clicked() {
			c.After(func() {
				tab.source.text.MoveTo(utf8.RuneCountInString(value[:blocks[(current+1)%len(blocks)].start]), false)
				m.focusActiveSource()
			})
		}
		ui.Slot(c).Grow(1)
		if ui.Button(c, uiText("保存并标记已解决")).Height(27).Disabled(busy || unresolvedGitMarkers(value)).Clicked() {
			c.After(func() { m.saveGitResolution(tab) })
		}
	})
}

func (m *editorApp) saveGitResolution(tab *editorTab) {
	git := &m.tools.git
	if tab == nil || tab.source.text.CompositionActive() || unresolvedGitMarkers(tab.source.text.String()) {
		return
	}
	root, path := git.snapshot.root, tab.doc.path
	if !gitPathWithin(root, path) {
		return
	}
	m.saveTextTabWithCallback(tab, false, func(ok bool) {
		if !ok || tab.modified() || unresolvedGitMarkers(tab.source.text.String()) || git.snapshot.root != root {
			return
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			git.issue = err.Error()
			return
		}
		m.startGit(gitStage, "", []string{relative})
		git.conflict = nil
	})
}
