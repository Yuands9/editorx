package editor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

const gitCommitAIContextLimit = 96 << 10
const gitCommitAIFileLimit = 8 << 20
const gitCommitAIFileCount = 100

func readGitCommitAIContext(ctx context.Context, root string, changes []gitChange) (string, error) {
	if len(changes) == 0 {
		return "", errors.New(uiText("请先勾选需要生成提交说明的文件"))
	}
	if len(changes) > gitCommitAIFileCount {
		return "", errors.New(uiText("AI 提交说明最多处理 100 个文件，请减少所选文件"))
	}
	git, err := exec.LookPath("git.exe")
	if err != nil {
		return "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	var content strings.Builder
	var tracked []string
	for _, change := range changes {
		if change.conflicted() || change.status == "!!" {
			return "", errors.New(uiText("请先解决冲突并仅选择可提交的文件"))
		}
		for _, name := range change.paths() {
			info, err := gitCommitAIFileInfo(root, name)
			if err != nil {
				return "", err
			}
			if info != nil && !info.Mode().IsRegular() {
				return "", errors.New(uiText("AI 提交说明暂不支持目录项，请展开后选择文件"))
			}
			if info != nil && info.Size() > gitCommitAIFileLimit {
				return "", errors.New(uiText("AI 提交说明暂不读取超过 8 MiB 的文件，请减少所选文件"))
			}
		}
		entry := fmt.Sprintf("File %q, Git status %q", change.path, change.status)
		if change.original != "" {
			entry += fmt.Sprintf(", renamed from %q", change.original)
		}
		if err := appendGitCommitAIContext(&content, entry+"\n"); err != nil {
			return "", err
		}
		if change.status != "??" {
			tracked = append(tracked, change.paths()...)
		}
	}
	head, headErr := runGitCommand(ctx, git, root, "rev-parse", "--verify", "--quiet", "HEAD")
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if headErr != nil {
		var exit *exec.ExitError
		if !errors.As(headErr, &exit) || exit.ExitCode() != 1 {
			return "", headErr
		}
	}
	hasCommit := headErr == nil && strings.TrimSpace(head) != ""
	changed := false
	if hasCommit && len(tracked) != 0 {
		// Bound both sides before asking Git to compute the selected diff. No
		// external diff or textconv process is allowed to inspect other files.
		args := append([]string{"ls-tree", "-l", "-z", "HEAD", "--"}, tracked...)
		objects, err := runGitCommandInput(ctx, git, root, "", gitCommitAIContextLimit, args...)
		if err != nil {
			return "", err
		}
		for record := range strings.SplitSeq(objects, "\x00") {
			metadata, _, _ := strings.Cut(record, "\t")
			fields := strings.Fields(metadata)
			if len(fields) == 4 && fields[1] != "blob" {
				return "", errors.New(uiText("AI 提交说明暂不支持目录项，请展开后选择文件"))
			}
			if len(fields) == 4 && fields[1] == "blob" {
				size, err := strconv.ParseInt(fields[3], 10, 64)
				if err != nil || size > gitCommitAIFileLimit {
					return "", errors.New(uiText("AI 提交说明暂不读取超过 8 MiB 的文件，请减少所选文件"))
				}
			}
		}
		args = append([]string{"--no-optional-locks", "diff", "--no-color", "--no-ext-diff", "--no-textconv", "--no-renames", "--submodule=short", "--unified=3", "HEAD", "--"}, tracked...)
		patch, err := runGitCommandInput(ctx, git, root, "", gitCommitAIContextLimit, args...)
		if err != nil {
			if err.Error() == uiText("Git 输出过大，请缩小项目范围") {
				return "", errors.New(uiText("所选改动超过 AI 提交说明的 96 KiB 限制，请减少所选文件"))
			}
			return "", err
		}
		if !utf8.ValidString(patch) {
			return "", errors.New(uiText("所选改动包含非 UTF-8 文本，请先转换编码或减少所选文件"))
		}
		if strings.ContainsRune(patch, 0) {
			return "", errors.New(uiText("AI 提交说明暂不支持此二进制差异，请减少所选文件"))
		}
		if err := appendGitCommitAIContext(&content, "\nHEAD to saved working files:\n"+patch); err != nil {
			return "", err
		}
		changed = patch != ""
	}
	for _, change := range changes {
		if hasCommit && change.status != "??" {
			continue
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		name := filepath.Join(root, change.path)
		info, err := gitCommitAIFileInfo(root, change.path)
		if err != nil {
			return "", err
		}
		if info == nil {
			return "", errors.New(uiText("所选文件已变化，请刷新 Git 状态后重试"))
		}
		if !info.Mode().IsRegular() {
			return "", errors.New(uiText("AI 提交说明暂不支持目录项，请展开后选择文件"))
		}
		file, err := os.Open(name)
		if err != nil {
			return "", err
		}
		data, err := io.ReadAll(io.LimitReader(largeEditContextReader{ctx: ctx, Reader: file}, gitCommitAIContextLimit+1))
		_ = file.Close()
		if err != nil {
			return "", err
		}
		if len(data) > gitCommitAIContextLimit {
			return "", errors.New(uiText("所选改动超过 AI 提交说明的 96 KiB 限制，请减少所选文件"))
		}
		doc, err := decodeDocument(change.path, data)
		entry := fmt.Sprintf("\nNew file %q:\n", change.path)
		if err != nil {
			entry += "Binary or unsupported text encoding; contents omitted.\n"
		} else {
			entry += doc.saved + "\n"
		}
		if err := appendGitCommitAIContext(&content, entry); err != nil {
			return "", err
		}
		changed = true
	}
	if !changed {
		return "", errors.New(uiText("所选文件没有可生成提交说明的已保存改动"))
	}
	return content.String(), nil
}

func appendGitCommitAIContext(content *strings.Builder, value string) error {
	if content.Len()+len(value) > gitCommitAIContextLimit {
		return errors.New(uiText("所选改动超过 AI 提交说明的 96 KiB 限制，请减少所选文件"))
	}
	content.WriteString(value)
	return nil
}

func gitCommitAIFileInfo(root, name string) (os.FileInfo, error) {
	if !filepath.IsLocal(name) || strings.ContainsRune(name, 0) || filepath.Clean(name) == "." || !gitPathWithin(root, filepath.Join(root, name)) {
		return nil, errors.New(uiText("Git 路径不属于当前仓库"))
	}
	current := root
	var info os.FileInfo
	for _, part := range strings.Split(filepath.ToSlash(filepath.Clean(name)), "/") {
		if strings.EqualFold(part, ".git") {
			return nil, errors.New(uiText("Git 路径不属于当前仓库"))
		}
		current = filepath.Join(current, part)
		var err error
		info, err = os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New(uiText("AI 提交说明暂不支持链接文件或链接目录"))
		}
	}
	return info, nil
}
