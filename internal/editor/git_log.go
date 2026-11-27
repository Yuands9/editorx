package editor

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const gitLogPageSize = 100

type gitLogQuery struct {
	reference, text, author, since string
	offset                         int
	scope                          string
}

type gitLogPage struct {
	root       string
	commits    []gitCommitRecord
	references []string
	hasMore    bool
}

func readGitLogPage(ctx context.Context, root string, query gitLogQuery) (gitLogPage, error) {
	var page gitLogPage
	git, err := exec.LookPath("git.exe")
	if err != nil {
		return page, err
	}
	var relative string
	if query.scope != "" {
		root, err = gitRepositoryRoot(ctx, git, root)
		if err != nil {
			return page, err
		}
		if root == "" {
			return page, errors.New(uiText("此目录还没有 Git 仓库"))
		}
		relative, err = filepath.Rel(root, query.scope)
		if err != nil || !filepath.IsLocal(relative) {
			return page, errors.New(uiText("Git 路径不属于当前仓库"))
		}
		relative = filepath.ToSlash(relative)
	}
	page.root = root
	if query.offset == 0 {
		refs, err := runGitCommand(ctx, git, root, "for-each-ref", "--sort=refname", "--format=%(refname)", "refs/heads", "refs/remotes", "refs/tags")
		if err != nil {
			return page, err
		}
		for ref := range strings.SplitSeq(strings.TrimSpace(refs), "\n") {
			ref = strings.TrimSpace(ref)
			if ref != "" && !strings.HasSuffix(ref, "/HEAD") {
				page.references = append(page.references, ref)
			}
		}
		if len(page.references) == 0 {
			if _, err := runGitCommand(ctx, git, root, "rev-parse", "--verify", "HEAD"); err != nil {
				return page, nil
			}
		}
	}
	args := []string{"log", "--date-order", "--no-color", "--fixed-strings", "--regexp-ignore-case", "--max-count=101", "--skip=" + strconv.Itoa(max(0, query.offset)),
		"--format=%H%x00%an%x00%aI%x00%s%x00%D%x00%B%x00%P%x00"}
	if query.author != "" {
		args = append(args, "--author="+query.author)
	}
	if query.since != "" {
		args = append(args, "--since="+query.since)
	}
	search := strings.TrimSpace(query.text)
	revision := query.reference
	if search != "" {
		isHash := len(search) >= 4 && len(search) <= 40 && strings.Trim(search, "0123456789abcdefABCDEF") == ""
		if isHash {
			resolved, err := runGitCommand(ctx, git, root, "rev-parse", "--verify", "--end-of-options", search+"^{commit}")
			if err == nil {
				revision, search = strings.TrimSpace(resolved), ""
				args = append(args, "--max-count=1")
			}
		}
		if search != "" {
			args = append(args, "--fixed-strings", "--regexp-ignore-case", "--grep="+search)
		}
	}
	if revision == "all" {
		args = append(args, "--all")
	} else if revision == "" {
		args = append(args, "HEAD")
	} else {
		args = append(args, "--end-of-options", revision)
	}
	args = append(args, "--")
	if relative != "" && relative != "." {
		args = append(args, relative)
	}
	output, err := runGitCommand(ctx, git, root, args...)
	if err != nil {
		return page, err
	}
	fields := strings.Split(output, "\x00")
	for i := 0; i+6 < len(fields); i += 7 {
		page.commits = append(page.commits, gitCommitRecord{hash: strings.TrimSpace(fields[i]), author: fields[i+1], at: fields[i+2],
			subject: fields[i+3], refs: fields[i+4], body: strings.TrimSpace(fields[i+5]), parents: fields[i+6]})
	}
	page.hasMore = len(page.commits) > gitLogPageSize
	if page.hasMore {
		page.commits = page.commits[:gitLogPageSize]
	}
	return page, nil
}
