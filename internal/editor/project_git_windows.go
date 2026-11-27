package editor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	gitRefresh      gitAction = "刷新"
	gitInit         gitAction = "初始化"
	gitStage        gitAction = "暂存"
	gitStageAll     gitAction = "全部暂存"
	gitUnstage      gitAction = "取消暂存"
	gitCommit       gitAction = "提交"
	gitFetch        gitAction = "获取"
	gitPull         gitAction = "拉取"
	gitPush         gitAction = "推送"
	gitAddRemote    gitAction = "添加远程"
	gitListBranches gitAction = "读取分支"
	gitCreateBranch gitAction = "新建分支"
	gitSwitchBranch gitAction = "切换分支"
)

func (action gitAction) updatesOpenFiles() bool {
	return action == gitPull || action == gitSwitchBranch
}

type gitResult struct {
	status   gitStatus
	action   gitAction
	updated  map[string]document
	missing  []string
	branches []string
	fail     string
}

func runGitCommand(ctx context.Context, git, directory string, args ...string) (string, error) {
	return runGitCommandInput(ctx, git, directory, "", 4<<20, args...)
}

func runGitCommandInput(ctx context.Context, git, directory, input string, limit int, args ...string) (string, error) {
	options := []string{"--no-pager", "--literal-pathspecs", "-c", "core.quotepath=false", "-C", directory}
	cmd := exec.CommandContext(ctx, git, append(options, args...)...)
	cmd.Dir = directory
	cmd.Env = append(cmd.Environ(), "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=Never", "LC_ALL=C")
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
		return "", errors.New("Git 输出过大，请缩小项目范围")
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

func runProjectGit(ctx context.Context, directory string, action gitAction, input string, paths, openFiles []string) gitResult {
	result := gitResult{action: action}
	git, err := exec.LookPath("git.exe")
	if err != nil {
		result.fail = "找不到 Git，请安装 Git for Windows"
		return result
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if action == gitRefresh || action == gitPull || action == gitPush {
		result.status, err = readGitStatus(ctx, git, directory)
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
	case gitCommit:
		if message := strings.TrimSpace(input); message != "" {
			args = []string{"commit", "-m", message}
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
		result.fail = fmt.Sprintf("缺少%s所需的参数", action)
		return result
	}
	if _, err := runGitCommand(ctx, git, repo, args...); err != nil {
		result.fail = err.Error()
	}
	refresh, refreshErr := readGitStatus(ctx, git, directory)
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
			} else if action == gitSwitchBranch && errors.Is(err, os.ErrNotExist) {
				result.missing = append(result.missing, file)
			}
		}
	}
	return result
}
