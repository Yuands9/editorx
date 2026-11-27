package editor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/vivaui/viva/ui"
)

type gitCommitAttempt struct {
	directory, root, branch, pushRemote, message string
	action                                       gitAction
	paths                                        []string
	warning                                      string
	open                                         bool
}

type gitCommitFile struct {
	path string
	hash [32]byte
	info os.FileInfo
}

func (m *editorApp) startGitCommit(action gitAction, message string, paths []string, pushRemote string, localOnly bool) {
	git := &m.tools.git
	if git.request != nil || git.snapshot.root == "" || git.snapshot.hasConflicts() {
		return
	}
	git.commitProblem, git.commitAttempt = "", nil
	var files []gitCommitFile
	if action == gitCommitSelected || action == gitAmendSelected {
		for _, tab := range m.tabs {
			if tab.doc.path == "" || !slices.ContainsFunc(paths, func(path string) bool {
				return strings.EqualFold(filepath.Join(git.snapshot.root, path), tab.doc.path)
			}) {
				continue
			}
			if tab.modified() || tab.hasResultDraft() || tab.savingText != nil || tab.source.text != nil && tab.source.text.CompositionActive() {
				git.commitProblem = fmt.Sprintf(uiText("「%s」有未保存的内容，请保存后再提交。"), tab.tabTitle())
				return
			}
			file := gitCommitFile{path: tab.doc.path, hash: tab.doc.diskHash}
			if file.hash == ([32]byte{}) && tab.largeText != nil {
				file.info = tab.largeText.file
			}
			if file.hash != ([32]byte{}) || file.info != nil {
				files = append(files, file)
			}
		}
	}
	attempt := &gitCommitAttempt{directory: git.directory, root: git.snapshot.root, branch: git.snapshot.branch,
		pushRemote: pushRemote, action: action, message: message, paths: slices.Clone(paths)}
	git.commitAttempt, git.requestAction = attempt, action
	git.commitAI = gitCommitAIState{}
	git.issue, git.message = "", ""
	git.request = ui.NewEffect[gitResult, int](func(ctx context.Context) (gitResult, error) {
		check, cancel := context.WithTimeout(ctx, 30*time.Second)
		problem, warning := checkGitCommit(check, *attempt, files, localOnly)
		cancel()
		if problem != "" || warning != "" {
			return gitResult{action: action, commitProblem: problem, commitWarning: warning}, nil
		}
		return runProjectGit(ctx, attempt.directory, action, message, attempt.paths, nil), nil
	})
}

func checkGitCommit(ctx context.Context, attempt gitCommitAttempt, files []gitCommitFile, localOnly bool) (problem, warning string) {
	git, err := exec.LookPath("git.exe")
	if err != nil {
		return err.Error(), ""
	}
	root, err := gitRepositoryRoot(ctx, git, attempt.directory)
	if err != nil || !strings.EqualFold(root, attempt.root) {
		return uiText("仓库已变化，请刷新后重新提交。"), ""
	}
	branch := attempt.branch
	if strings.HasPrefix(branch, "HEAD ") {
		branch = ""
	}
	current, err := runGitCommand(ctx, git, root, "branch", "--show-current")
	if err != nil || strings.TrimSpace(current) != branch {
		return uiText("当前分支已变化，请刷新后重新提交。"), ""
	}
	if !localOnly && branch != "" {
		warning = checkGitUpstream(ctx, git, root, branch, attempt.pushRemote)
		current, err = runGitCommand(ctx, git, root, "branch", "--show-current")
		if err != nil || strings.TrimSpace(current) != branch {
			return uiText("当前分支已变化，请刷新后重新提交。"), ""
		}
	}
	for _, file := range files {
		var changed bool
		var err error
		if file.info != nil {
			var current os.FileInfo
			current, err = os.Stat(file.path)
			changed = err == nil && !sameTextFile(file.info, current)
		} else {
			var current diskReadResult
			current, err = inspectDiskFile(ctx, file.path)
			changed = err == nil && (current.missing || current.digest != file.hash)
		}
		if changed || errors.Is(err, os.ErrNotExist) {
			return fmt.Sprintf(uiText("「%s」的磁盘内容已变化，请先比对或重新读取后再提交。"), filepath.Base(file.path)), ""
		}
		if err != nil {
			return fmt.Sprintf(uiText("无法检查「%s」的磁盘内容：%s"), filepath.Base(file.path), err.Error()), ""
		}
	}
	return "", warning
}

func checkGitUpstream(ctx context.Context, git, root, branch, pushRemote string) string {
	compare, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	upstream, err := runGitCommand(compare, git, root, "for-each-ref", "--format=%(upstream:remotename)%09%(upstream:remoteref)", "refs/heads/"+branch)
	unavailable := uiText("暂时无法确认远程分支是否最新。")
	if err != nil {
		return unavailable
	}
	remote, ref, _ := strings.Cut(strings.TrimRight(upstream, "\r\n"), "\t")
	tracking := remote != "" && ref != ""
	if pushRemote != "" && (!tracking || remote != pushRemote) {
		remote, ref, tracking = pushRemote, "refs/heads/"+branch, false
	}
	if remote == "" || ref == "" {
		return ""
	}
	refs, err := runGitCommand(compare, git, root, "ls-remote", "--refs", "--", remote, ref)
	if err != nil {
		return unavailable
	}
	for line := range strings.SplitSeq(refs, "\n") {
		oid, name, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if !ok || name != ref || (len(oid) != 40 && len(oid) != 64) || strings.Trim(oid, "0123456789abcdef") != "" {
			continue
		}
		// A remote commit absent from our object database cannot be in local HEAD.
		if _, err := runGitCommand(compare, git, root, "merge-base", "--is-ancestor", oid, "HEAD"); err != nil {
			if compare.Err() != nil {
				return unavailable
			}
			return uiText("本地分支尚未包含远程提交，请先拉取并处理差异。")
		}
		return ""
	}
	if tracking {
		return uiText("远程上游分支已不存在，请检查上游设置。")
	}
	return ""
}

func (m *editorApp) gitCommitCheckDialog(c ui.C) {
	git := &m.tools.git
	attempt := git.commitAttempt
	if attempt == nil || !attempt.open {
		return
	}
	closeDialog := func() { git.commitAttempt = nil }
	dismissed := ui.Modal(c, &attempt.open, uiText("提交前检查"), func() {
		ui.Wrap(c, attempt.warning).FillWidth()
		ui.Wrap(c, uiText("可以返回处理，或只创建本地提交。")).FillWidth().Muted()
	}, func() {
		if ui.Button(c, uiText("返回")).Ghost().Clicked() {
			c.After(closeDialog)
		}
		if ui.Button(c, uiText("仅提交到本地")).Outline().Disabled(git.request != nil).Clicked() {
			c.After(func() {
				closeDialog()
				if git.directory != attempt.directory || git.snapshot.branch != attempt.branch || strings.TrimSpace(git.commit.Text()) != attempt.message ||
					(attempt.action == gitCommitSelected || attempt.action == gitAmendSelected) && !slices.Equal(git.selectedCommitPaths(), attempt.paths) {
					git.commitProblem = uiText("提交信息已变化，请确认后重新提交。")
					return
				}
				m.startGitCommit(attempt.action, attempt.message, attempt.paths, "", true)
			})
		}
	})
	if dismissed {
		c.After(closeDialog)
	}
}
