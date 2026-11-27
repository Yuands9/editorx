package editor

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type gitChange struct {
	status, path, original string
}

func (change gitChange) staged() bool {
	return len(change.status) == 2 && change.status[0] != ' ' && change.status[0] != '?' && change.status[0] != '!'
}

func (change gitChange) unstaged() bool {
	return len(change.status) == 2 && change.status[1] != ' ' && change.status[1] != '!'
}

func (change gitChange) paths() []string {
	if change.original != "" {
		return []string{change.path, change.original}
	}
	return []string{change.path}
}

type gitStatus struct {
	root, branch, tracking string
	remotes                []string
	changes                []gitChange
	hasCommit              bool
}

func (status gitStatus) hasStaged() bool {
	for _, change := range status.changes {
		if change.staged() {
			return true
		}
	}
	return false
}

type gitAction string

const (
	gitRefresh        gitAction = "刷新"
	gitInit           gitAction = "初始化"
	gitStage          gitAction = "暂存"
	gitStageAll       gitAction = "全部暂存"
	gitUnstage        gitAction = "取消暂存"
	gitCommit         gitAction = "提交"
	gitAmend          gitAction = "修改上次提交"
	gitCommitSelected gitAction = "提交所选文件"
	gitAmendSelected  gitAction = "用所选文件修改上次提交"
	gitListMessages   gitAction = "读取提交说明"
	gitListIgnored    gitAction = "读取忽略文件"
	gitListStashes    gitAction = "读取贮藏"
	gitStashSave      gitAction = "贮藏变更"
	gitStashApply     gitAction = "应用贮藏"
	gitStashPop       gitAction = "弹出贮藏"
	gitFetch          gitAction = "获取"
	gitPull           gitAction = "拉取"
	gitPush           gitAction = "推送"
	gitAddRemote      gitAction = "添加远程"
	gitListBranches   gitAction = "读取分支"
	gitCreateBranch   gitAction = "新建分支"
	gitSwitchBranch   gitAction = "切换分支"
)

func (action gitAction) updatesOpenFiles() bool {
	return action == gitPull || action == gitSwitchBranch || action == gitStashSave || action == gitStashApply || action == gitStashPop
}

type gitResult struct {
	status      gitStatus
	action      gitAction
	updated     map[string]document
	missing     []string
	branches    []string
	items       []string
	fail        string
	committed   bool
	statusValid bool
}

func runGitCommand(ctx context.Context, git, directory string, args ...string) (string, error) {
	return runGitCommandInput(ctx, git, directory, "", 4<<20, args...)
}

func runGitCommandInput(ctx context.Context, git, directory, input string, limit int, args ...string) (string, error) {
	return runGitCommandEnvironment(ctx, git, directory, input, limit, nil, args...)
}

func runGitCommandEnvironment(ctx context.Context, git, directory, input string, limit int, environment []string, args ...string) (string, error) {
	options := []string{"--no-pager", "--literal-pathspecs", "-c", "core.quotepath=false", "-C", directory}
	cmd := exec.CommandContext(ctx, git, append(options, args...)...)
	cmd.Dir = directory
	cmd.Env = append(cmd.Environ(), "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=Never", "LC_ALL=C")
	cmd.Env = append(cmd.Env, environment...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	cmd.WaitDelay = 2 * time.Second
	cmd.Stdin = strings.NewReader(input)
	stdout, stderr := aiCLICapture{limit: limit}, aiCLICapture{limit: 16 << 10}
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return "", errors.New(detail)
		}
		return "", err
	}
	if stdout.overflow {
		return "", errors.New(uiText("Git 输出过大，请缩小项目范围"))
	}
	return stdout.String(), nil
}

func gitRepositoryRoot(ctx context.Context, git, directory string) (string, error) {
	root, err := runGitCommand(ctx, git, directory, "rev-parse", "--show-toplevel")
	if err != nil {
		if strings.Contains(err.Error(), "not a git repository") {
			return "", nil
		}
		return "", err
	}
	return filepath.Clean(strings.TrimSpace(root)), nil
}

func readGitStatus(ctx context.Context, git, directory string) (gitStatus, error) {
	var status gitStatus
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type statusOutput struct {
		text string
		err  error
	}
	// Porcelain paths are relative to the repository root even when opened from a subfolder.
	changes := make(chan statusOutput, 1)
	go func() {
		output, err := runGitCommand(ctx, git, directory, "--no-optional-locks", "status", "--porcelain=v1", "-z", "--branch", "--untracked-files=all")
		changes <- statusOutput{text: output, err: err}
	}()

	remotes := make(chan statusOutput, 1)
	go func() {
		output, err := runGitCommand(ctx, git, directory, "remote")
		remotes <- statusOutput{text: output, err: err}
	}()

	root, err := gitRepositoryRoot(ctx, git, directory)
	if err != nil || root == "" {
		return status, err
	}
	status.root = root
	output := <-changes
	if output.err != nil {
		return status, output.err
	}
	entries := strings.Split(output.text, "\x00")
	if len(entries) > 0 && strings.HasPrefix(entries[0], "## ") {
		heading := strings.TrimPrefix(entries[0], "## ")
		status.hasCommit = !strings.HasPrefix(heading, "No commits yet on ") && !strings.HasPrefix(heading, "Initial commit on ")
		heading = strings.TrimPrefix(heading, "No commits yet on ")
		heading = strings.TrimPrefix(heading, "Initial commit on ")
		status.branch, status.tracking, _ = strings.Cut(heading, "...")
		entries = entries[1:]
	}
	for i := 0; i < len(entries); i++ {
		entry := entries[i]
		if len(entry) < 4 || entry[2] != ' ' {
			continue
		}
		change := gitChange{status: entry[:2], path: entry[3:]}
		if strings.ContainsAny(change.status, "RC") && i+1 < len(entries) {
			i++
			change.original = entries[i]
		}
		status.changes = append(status.changes, change)
	}
	remote := <-remotes
	if remote.err != nil {
		return status, remote.err
	}
	status.remotes = strings.Fields(remote.text)
	return status, nil
}

func gitPathWithin(root, file string) bool {
	if file == "" {
		return false
	}
	relative, err := filepath.Rel(root, file)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// Commit selection is local UI state until the user submits. Git's --only
// transaction commits each selected working-tree file while retaining unrelated
// staged entries. An alternate index keeps the real index unchanged on failure.
func commitSelectedGitFiles(ctx context.Context, git, root, message string, paths []string, amend, hasCommit bool) (bool, error) {
	if strings.TrimSpace(message) == "" || len(paths) == 0 && !amend {
		return false, errors.New(uiText("请选择文件并填写提交说明"))
	}
	for _, name := range paths {
		if name == "" || strings.ContainsRune(name, 0) || filepath.VolumeName(name) != "" || filepath.IsAbs(name) || filepath.Clean(name) == "." || !gitPathWithin(root, filepath.Join(root, name)) {
			return false, errors.New(uiText("Git 路径不属于当前仓库"))
		}
	}
	value, err := runGitCommand(ctx, git, root, "rev-parse", "--path-format=absolute", "--git-path", "index")
	if err != nil {
		return false, err
	}
	indexPath := strings.TrimSpace(value)
	lock, err := os.OpenFile(indexPath+".lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return false, fmt.Errorf("%s: %w", uiText("无法锁定 Git 暂存区，请等待其他 Git 操作完成"), err)
	}
	lockOwned := true
	defer func() {
		_ = lock.Close()
		if lockOwned {
			_ = os.Remove(indexPath + ".lock")
		}
	}()
	temporary, err := os.CreateTemp(filepath.Dir(indexPath), "editorx-commit-index-*")
	if err != nil {
		return false, err
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
		_ = os.Remove(temporaryPath + ".lock")
	}()
	original, err := os.Open(indexPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if original != nil {
		_, err = io.Copy(temporary, original)
		_ = original.Close()
		if err != nil {
			return false, err
		}
	}
	if err := temporary.Close(); err != nil {
		return false, err
	}
	if original == nil {
		// Git initializes an absent index, but rejects an empty index file.
		if err := os.Remove(temporaryPath); err != nil {
			return false, err
		}
	}
	environment := []string{"GIT_INDEX_FILE=" + temporaryPath}
	previousHead := ""
	if hasCommit {
		previousHead, err = runGitCommand(ctx, git, root, "rev-parse", "--verify", "HEAD")
		if err != nil {
			return false, err
		}
	}
	pathspec := strings.Join(paths, "\x00") + "\x00"
	if len(paths) > 0 {
		// Restore HEAD entries only in the temporary index before adding. An
		// already-staged deletion or rename no longer has its old path in index.
		if hasCommit {
			if _, err := runGitCommandEnvironment(ctx, git, root, pathspec, 4<<20, environment,
				"reset", "-q", "HEAD", "--pathspec-from-file=-", "--pathspec-file-nul"); err != nil {
				return false, err
			}
		}
		if _, err := runGitCommandEnvironment(ctx, git, root, pathspec, 4<<20, environment,
			"add", "-A", "--pathspec-from-file=-", "--pathspec-file-nul"); err != nil {
			return false, err
		}
	}
	args := []string{"commit", "--only", "-m", strings.TrimSpace(message)}
	if amend {
		args = append(args, "--amend")
	}
	if len(paths) > 0 {
		args = append(args, "--pathspec-from-file=-", "--pathspec-file-nul")
	} else {
		pathspec = ""
	}
	_, commitErr := runGitCommandEnvironment(ctx, git, root, pathspec, 4<<20, environment, args...)
	if commitErr != nil {
		// A post-commit hook can time out after Git has updated HEAD and index.
		// Verify with a fresh bounded context before reporting a failed commit.
		verifyContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		head, verifyErr := runGitCommand(verifyContext, git, root, "rev-parse", "--verify", "HEAD")
		cancel()
		if verifyErr != nil || strings.TrimSpace(head) == strings.TrimSpace(previousHead) {
			return false, commitErr
		}
	}
	updated, err := os.Open(temporaryPath)
	if err != nil {
		return true, fmt.Errorf("%s: %w", uiText("提交已完成，但暂存区同步失败，请刷新后检查"), err)
	}
	_, err = io.Copy(lock, updated)
	_ = updated.Close()
	if err == nil {
		err = lock.Close()
	}
	if err == nil {
		err = os.Rename(indexPath+".lock", indexPath)
	}
	if err != nil {
		return true, fmt.Errorf("%s: %w", uiText("提交已完成，但暂存区同步失败，请刷新后检查"), err)
	}
	lockOwned = false
	if commitErr != nil {
		return true, fmt.Errorf("%s: %w", uiText("提交已完成，但后续处理失败，请刷新后检查"), commitErr)
	}
	return true, nil
}

func runProjectGit(ctx context.Context, directory string, action gitAction, input string, paths, openFiles []string) gitResult {
	result := gitResult{action: action}
	git, err := exec.LookPath("git.exe")
	if err != nil {
		result.fail = "找不到 Git，请安装 Git for Windows"
		return result
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if action == gitRefresh || action == gitPull || action == gitPush || action == gitCommitSelected || action == gitAmendSelected {
		result.status, err = readGitStatus(ctx, git, directory)
		result.statusValid = err == nil
	} else {
		result.status.root, err = gitRepositoryRoot(ctx, git, directory)
	}
	if err != nil {
		result.fail = err.Error()
		return result
	}
	if action == gitRefresh {
		return result
	}
	if action != gitInit && result.status.root == "" {
		result.fail = "此目录还没有 Git 仓库"
		return result
	}
	branch := result.status.branch
	repo := result.status.root
	switch action {
	case gitListIgnored, gitListMessages, gitListStashes:
		var args []string
		switch action {
		case gitListIgnored:
			args = []string{"ls-files", "--others", "--ignored", "--exclude-standard", "--directory", "-z"}
		case gitListMessages:
			args = []string{"log", "-12", "--format=%B", "-z"}
		case gitListStashes:
			args = []string{"stash", "list", "--format=%gd%x00%H%x00%gs", "-z"}
		}
		output, err := runGitCommand(ctx, git, repo, args...)
		if err != nil {
			result.fail = err.Error()
		} else {
			result.items = strings.Split(strings.TrimRight(output, "\x00"), "\x00")
			if output == "" {
				result.items = nil
			}
		}
		return result
	}
	if action == gitListBranches {
		names, err := runGitCommand(ctx, git, repo, "for-each-ref", "--format=%(refname:lstrip=2)", "refs/heads")
		if err != nil {
			result.fail = err.Error()
		} else {
			result.branches = strings.Fields(names)
		}
		return result
	}
	if action == gitInit {
		repo = directory
	}
	var args []string
	switch action {
	case gitCommitSelected, gitAmendSelected:
		result.committed, err = commitSelectedGitFiles(ctx, git, repo, input, paths, action == gitAmendSelected, result.status.hasCommit)
		if err != nil {
			result.fail = err.Error()
		}
		refresh, refreshErr := readGitStatus(ctx, git, directory)
		result.statusValid = refreshErr == nil
		if refreshErr == nil {
			result.status = refresh
		} else if result.fail == "" {
			result.fail = "操作完成，刷新状态失败：" + refreshErr.Error()
		}
		return result
	case gitInit:
		if result.status.root == "" {
			args = []string{"init"}
		}
	case gitCreateBranch:
		if input == "" || strings.HasPrefix(input, "-") || strings.HasPrefix(input, "@{") {
			result.fail = "请输入有效的新分支名称"
			return result
		}
		args = []string{"switch", "-c", input}
	case gitSwitchBranch:
		if input == "" || strings.HasPrefix(input, "-") {
			result.fail = "请选择要切换的本地分支"
			return result
		}
		args = []string{"switch", "--no-guess", "--", input}
	case gitStage, gitUnstage:
		for _, name := range paths {
			if filepath.IsAbs(name) || !gitPathWithin(repo, filepath.Join(repo, name)) {
				result.fail = "Git 路径不属于当前仓库"
				return result
			}
		}
		if len(paths) > 0 && action == gitStage {
			args = append([]string{"add", "-A", "--"}, paths...)
		}
		if len(paths) > 0 && action == gitUnstage {
			args = append([]string{"reset", "-q", "--"}, paths...)
		}
	case gitStageAll:
		args = []string{"add", "-A"}
	case gitCommit, gitAmend:
		if message := strings.TrimSpace(input); message != "" {
			args = []string{"commit"}
			if action == gitAmend {
				args = append(args, "--amend")
			}
			args = append(args, "-m", message)
		}
	case gitStashSave:
		args = []string{"stash", "push", "-u"}
		if input := strings.TrimSpace(input); input != "" {
			args = append(args, "-m", input)
		}
	case gitStashApply, gitStashPop:
		ref, oid, _ := strings.Cut(input, "\x00")
		number := strings.TrimSuffix(strings.TrimPrefix(ref, "stash@{"), "}")
		index, refErr := strconv.Atoi(number)
		_, hashErr := hex.DecodeString(oid)
		if refErr != nil || index < 0 || ref != fmt.Sprintf("stash@{%d}", index) || hashErr != nil || len(oid) != 40 && len(oid) != 64 {
			result.fail = uiText("请选择有效的贮藏记录")
			return result
		}
		current, err := runGitCommand(ctx, git, repo, "rev-parse", "--verify", ref)
		if err != nil || strings.TrimSpace(current) != oid {
			result.fail = uiText("贮藏列表已变更，请刷新后重试")
			return result
		}
		args = []string{"stash", "apply", "--", oid}
		if action == gitStashPop {
			args = []string{"stash", "pop", "--", ref}
		}
	case gitFetch:
		args = []string{"fetch", "--all"}
	case gitPull:
		if result.status.tracking == "" {
			result.fail = "当前分支没有上游，请先推送此分支或在终端设置上游"
			return result
		}
		args = []string{"pull", "--ff-only"}
	case gitPush:
		if branch == "" || strings.HasPrefix(branch, "HEAD ") {
			result.fail = "请先在终端切换到要推送的分支"
			return result
		}
		remote := input
		if remote == "" {
			result.fail = "请先选择远程仓库"
			return result
		}
		if !strings.HasPrefix(remote, "-") {
			target := branch
			if qualified, _, ok := strings.Cut(result.status.tracking, " "); ok || qualified != "" {
				if name, ok := strings.CutPrefix(qualified, remote+"/"); ok {
					target = name
				}
			}
			args = []string{"push", "--set-upstream", "--", remote, "HEAD:refs/heads/" + target}
		}
	case gitAddRemote:
		address := strings.TrimSpace(input)
		if address != "" && !strings.HasPrefix(address, "-") && !strings.ContainsAny(address, "\x00\r\n") {
			args = []string{"remote", "add", "origin", address}
		}
	}
	if len(args) == 0 {
		result.fail = fmt.Sprintf(uiText("缺少%s所需的参数"), action)
		return result
	}
	if _, err := runGitCommand(ctx, git, repo, args...); err != nil {
		result.fail = err.Error()
	} else if action == gitCommit || action == gitAmend {
		result.committed = true
	}
	refresh, refreshErr := readGitStatus(ctx, git, directory)
	result.statusValid = refreshErr == nil
	if refreshErr == nil {
		result.status = refresh
	} else if result.fail == "" {
		result.fail = "操作完成，刷新状态失败：" + refreshErr.Error()
	}
	if action.updatesOpenFiles() && result.fail == "" {
		result.updated = make(map[string]document)
		for _, file := range openFiles {
			if !gitPathWithin(repo, file) {
				continue
			}
			if doc, err := readDocument(file); err == nil {
				result.updated[file] = doc
			} else if (action == gitSwitchBranch || action == gitStashSave) && errors.Is(err, os.ErrNotExist) {
				result.missing = append(result.missing, file)
			}
		}
	}
	return result
}
