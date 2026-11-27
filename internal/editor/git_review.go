package editor

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/vivaui/viva/components"
)

type gitPatchHunk struct {
	patch                                string
	oldLine, oldCount, newLine, newCount int
}

type gitReviewData struct {
	root, path, revision string
	staged               bool
	wholeWorking         bool
	change               gitChange
	comparison           localHistoryComparison
	patch                string
	hunks                []gitPatchHunk
}

type gitCommitRecord struct{ hash, author, at, subject, refs, body, parents string }
type gitCommitFiles struct {
	parent  string
	changes []gitChange
}

func (change gitChange) conflicted() bool {
	return strings.Contains(change.status, "U") || change.status == "AA" || change.status == "DD"
}

func gitReviewBlob(ctx context.Context, git, root, revision, path string) (document, error) {
	if revision == "" {
		return document{path: path}, nil
	}
	value, err := runGitCommandInput(ctx, git, root, "", localHistoryFileSizeLimit, "show", revision+":"+filepath.ToSlash(path))
	if err != nil {
		return document{}, err
	}
	return decodeHistoryComparison(path, []byte(value))
}

func gitReviewPatch(ctx context.Context, git string, data gitReviewData) (string, error) {
	args := []string{"diff", "--no-color", "--no-ext-diff", "--no-textconv", "--no-renames", "--unified=4"}
	if data.staged {
		args = append(args, "--cached")
	}
	args = append(args, "--", data.path)
	return runGitCommandInput(ctx, git, data.root, "", localHistoryFileSizeLimit, args...)
}

func readGitReview(ctx context.Context, root string, change gitChange, staged bool, revision, parent string) (gitReviewData, error) {
	data := gitReviewData{root: root, path: change.path, change: change, staged: staged, revision: revision}
	git, err := exec.LookPath("git.exe")
	if err != nil {
		return data, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if change.conflicted() {
		return data, errors.New(uiText("此文件存在冲突，请打开冲突处理"))
	}
	leftPath := change.path
	if change.original != "" && staged {
		leftPath = change.original
	}
	left, right := "HEAD", ":0"
	if !staged {
		left, right = ":0", ""
	}
	if revision != "" {
		left, right = parent, revision
	}
	code := byte('M')
	if len(change.status) > 0 {
		code = change.status[0]
	}
	if revision == "" && !staged && len(change.status) == 2 {
		code = change.status[1]
	}
	if code == 'A' || code == '?' {
		left = ""
	}
	if code == 'D' {
		right = ""
	}
	// Git index syntax is :0:path, while tree objects use revision:path.
	data.comparison.old, err = gitReviewBlob(ctx, git, root, left, leftPath)
	if err != nil {
		return data, err
	}
	if revision == "" && !staged && code != 'D' {
		data.comparison.current, err = readHistoryCurrentFile(ctx, filepath.Join(root, change.path))

	} else {
		data.comparison.current, err = gitReviewBlob(ctx, git, root, right, change.path)
	}
	if err != nil {
		return data, err
	}
	data.comparison.compareLines()
	// Whole-file staging remains available for additions, deletions, renames and binary files.
	if revision == "" && code == 'M' && change.original == "" {
		data.patch, err = gitReviewPatch(ctx, git, data)
		if err != nil {
			return data, err
		}
		data.hunks = parseGitPatch(data.patch)
	}
	return data, nil
}

func readGitWorkingReview(ctx context.Context, root string, change gitChange) (gitReviewData, error) {
	data := gitReviewData{root: root, path: change.path, change: change, wholeWorking: true}
	git, err := exec.LookPath("git.exe")
	if err != nil {
		return data, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if change.conflicted() {
		return data, errors.New(uiText("此文件存在冲突，请打开冲突处理"))
	}
	left, leftPath := "HEAD", change.path
	if change.original != "" {
		leftPath = change.original
	}
	if change.status == "??" || strings.HasPrefix(change.status, "A") {
		left = ""
	}
	data.comparison.old, err = gitReviewBlob(ctx, git, root, left, leftPath)
	if err != nil {
		return data, err
	}
	deleted := strings.HasSuffix(change.status, "D") || strings.HasPrefix(change.status, "D") && !change.unstaged()
	if !deleted {
		data.comparison.current, err = readHistoryCurrentFile(ctx, filepath.Join(root, change.path))
		if err != nil {
			return data, err
		}
	}
	data.comparison.compareLines()
	return data, nil
}

func parseGitPatch(patch string) []gitPatchHunk {
	header, start, offset := "", 0, 0
	var hunks []gitPatchHunk
	for line := range strings.SplitAfterSeq(patch, "\n") {
		if strings.HasPrefix(line, "@@ ") {
			if len(hunks) == 0 {
				header = patch[:offset]
			} else {
				hunks[len(hunks)-1].patch = header + patch[start:offset]
			}
			start = offset
			fields := strings.Fields(line)
			if len(fields) < 4 {
				return nil
			}
			oldLine, oldCount, ok := gitPatchRange(fields[1], '-')
			newLine, newCount, valid := gitPatchRange(fields[2], '+')
			if !ok || !valid {
				return nil
			}
			hunks = append(hunks, gitPatchHunk{oldLine: oldLine, oldCount: oldCount, newLine: newLine, newCount: newCount})
		}
		offset += len(line)
	}
	if len(hunks) > 0 {
		hunks[len(hunks)-1].patch = header + patch[start:]
	}
	return hunks
}

func gitPatchRange(value string, prefix byte) (int, int, bool) {
	if len(value) < 2 || value[0] != prefix {
		return 0, 0, false
	}
	line, count, exists := strings.Cut(value[1:], ",")
	n, err := strconv.Atoi(line)
	c := 1
	if exists {
		var e error
		c, e = strconv.Atoi(count)
		if e != nil {
			return 0, 0, false
		}
	}
	return n, c, err == nil && n >= 0 && c >= 0
}

func (h gitPatchHunk) rows(result components.CodeDiffResult) components.CodeDiffHunk {
	start, end := -1, 0
	for i, row := range result.Rows {
		if row.Kind == components.CodeDiffUnchanged {
			continue
		}
		inside := row.LeftLine >= max(0, h.oldLine-1) && row.LeftLine < h.oldLine-1+h.oldCount || row.RightLine >= max(0, h.newLine-1) && row.RightLine < h.newLine-1+h.newCount
		if inside {
			if start < 0 {
				start = i
			}
			end = i + 1
		}
	}
	return components.CodeDiffHunk{Start: max(0, start), End: end}
}

func applyGitHunk(ctx context.Context, data gitReviewData, index int) error {
	if index < 0 || index >= len(data.hunks) || data.revision != "" {
		return errors.New(uiText("请选择可暂存的区别"))
	}
	git, err := exec.LookPath("git.exe")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	patch, err := gitReviewPatch(ctx, git, data)
	if err != nil {
		return err
	}
	if patch != data.patch {
		return errors.New(uiText("文件或暂存区已变化，请刷新差异后重试"))
	}
	args := []string{"apply", "--cached", "--whitespace=nowarn", "--recount"}
	if data.staged {
		args = append(args, "--reverse")
	}
	_, err = runGitCommandInput(ctx, git, data.root, data.hunks[index].patch, localHistoryFileSizeLimit, append(args, "-")...)
	return err
}

func readGitLog(ctx context.Context, root string) ([]gitCommitRecord, error) {
	page, err := readGitLogPage(ctx, root, gitLogQuery{})
	return page.commits, err
}

func readGitCommitFiles(ctx context.Context, root, revision string) (gitCommitFiles, error) {
	var result gitCommitFiles
	git, err := exec.LookPath("git.exe")
	if err != nil {
		return result, err
	}
	parents, err := runGitCommand(ctx, git, root, "rev-list", "--parents", "-n", "1", revision)
	if err != nil {
		return result, err
	}
	if fields := strings.Fields(parents); len(fields) > 1 {
		result.parent = fields[1]
	}
	args := []string{"diff-tree", "--root", "--no-commit-id", "--name-status", "-z", "-r", "--no-renames"}
	if result.parent != "" {
		args = append(args, result.parent)
	}
	args = append(args, revision, "--")
	output, err := runGitCommand(ctx, git, root, args...)
	if err != nil {
		return result, err
	}
	fields := strings.Split(output, "\x00")
	for i := 0; i+1 < len(fields); i += 2 {
		if fields[i] == "" || fields[i+1] == "" {
			continue
		}
		result.changes = append(result.changes, gitChange{status: fields[i], path: fields[i+1]})
	}
	return result, nil
}

func gitReviewTitle(data gitReviewData) string {
	if data.revision != "" {
		return fmt.Sprintf("%s · %s", data.revision[:min(8, len(data.revision))], data.path)
	}
	if data.wholeWorking {
		return uiText("整文件变更 · HEAD → 当前磁盘文件 · ") + data.path
	}
	if data.staged {
		return "已暂存 · HEAD → 暂存区 · " + data.path
	}
	return "工作区 · 暂存区 → 磁盘文件 · " + data.path
}
