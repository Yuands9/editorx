package editor

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/components"
)

const historyContextLines = 4

type historyDiffBlock struct {
	rows                   components.CodeDiffHunk
	changesRow             int
	historyFrom, historyTo int
	currentFrom, currentTo int
}

type historyDiff struct {
	full, changes components.CodeDiffResult
	blocks        []historyDiffBlock
	separators    map[int]string
}

func compareHistoryLines(previous, current string) historyDiff {
	full := components.DiffCodeLines(previous, current)
	diff := historyDiff{
		full:    full,
		changes: components.CodeDiffResult{Left: full.Left, Right: full.Right, Stats: full.Stats},
	}
	row, left, right := 0, 0, 0
	for _, hunk := range full.Hunks(nil) {
		for ; row < hunk.Start; row++ {
			// The rows preceding a hunk are unchanged and contain both sides.
			left++
			right++
		}
		block := historyDiffBlock{rows: hunk, historyFrom: left, currentFrom: right}
		for ; row < hunk.End; row++ {
			entry := full.Rows[row]
			if entry.LeftLine >= 0 {
				left++
			}
			if entry.RightLine >= 0 {
				right++
			}
		}
		block.historyTo, block.currentTo = left, right
		diff.blocks = append(diff.blocks, block)
	}
	diff.showChangesWithContext()
	return diff
}

func (diff *historyDiff) showChangesWithContext() {
	if len(diff.blocks) == 0 {
		return
	}
	shownUntil, inline := 0, 0
	for index := range diff.blocks {
		block := &diff.blocks[index]
		start := max(0, block.rows.Start-historyContextLines)
		if start > shownUntil {
			diff.appendSeparator(start - shownUntil)
			shownUntil = start
		}
		rowOffset := len(diff.changes.Rows) - shownUntil
		block.changesRow = block.rows.Start + rowOffset
		end := min(len(diff.full.Rows), block.rows.End+historyContextLines)
		diff.changes.Rows = append(diff.changes.Rows, diff.full.Rows[shownUntil:end]...)
		for ; inline < len(diff.full.Inline) && diff.full.Inline[inline].Row < end; inline++ {
			mark := diff.full.Inline[inline]
			mark.Row += rowOffset
			diff.changes.Inline = append(diff.changes.Inline, mark)
		}
		shownUntil = end
	}
	if remaining := len(diff.full.Rows) - shownUntil; remaining > 0 {
		diff.appendSeparator(remaining)
	}
}

func (diff *historyDiff) appendSeparator(count int) {
	if diff.separators == nil {
		diff.separators = make(map[int]string)
	}
	diff.separators[len(diff.changes.Rows)] = fmt.Sprintf(uiText("省略 %d 行"), count)
	diff.changes.Rows = append(diff.changes.Rows, components.CodeDiffRow{LeftLine: -1, RightLine: -1})
}

func historyLineOffset(lines []string, index int) int {
	offset := 0
	for _, line := range lines[:index] {
		offset += len(line) + 1
	}
	return offset
}

// A middle block owns the newline on its right. At end of file the separator
// belongs to its left, so inserting or removing the last line preserves EOF.
func (diff historyDiff) replacement(block historyDiffBlock, currentSize int) (start, end int, value string) {
	history := diff.full.Left[block.historyFrom:block.historyTo]
	current := diff.full.Right
	value = strings.Join(history, "\n")
	if block.currentTo < len(current) {
		start = historyLineOffset(current, block.currentFrom)
		end = historyLineOffset(current, block.currentTo)
		if len(history) != 0 {
			value += "\n"
		}
		return start, end, value
	}
	if block.currentFrom > 0 {
		previous := block.currentFrom - 1
		start = historyLineOffset(current, previous) + len(current[previous])
		if len(history) != 0 {
			value = "\n" + value
		}
	}
	return start, currentSize, value
}

// Keep unchanged text out of the undo record, including within a changed line.
func trimHistoryReplacement(current string, start, end int, replacement string) (int, int, string) {
	removed := current[start:end]
	shared := min(len(removed), len(replacement))
	prefix := 0
	for prefix < shared && removed[prefix] == replacement[prefix] {
		prefix++
	}
	for prefix < len(removed) && !utf8.RuneStart(removed[prefix]) {
		prefix--
	}
	suffix := 0
	for suffix < shared-prefix && removed[len(removed)-1-suffix] == replacement[len(replacement)-1-suffix] {
		suffix++
	}
	for suffix > 0 && !utf8.RuneStart(removed[len(removed)-suffix]) {
		suffix--
	}
	return start + prefix, end - suffix, replacement[prefix : len(replacement)-suffix]
}

func (block historyDiffBlock) visibleRange(onlyChanges bool) components.CodeDiffHunk {
	if onlyChanges {
		return components.CodeDiffHunk{Start: block.changesRow, End: block.changesRow + block.rows.End - block.rows.Start}
	}
	return block.rows
}
