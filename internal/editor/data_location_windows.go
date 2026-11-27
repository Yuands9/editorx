package editor

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	editorDataDirectoryEnv = "VIVA_EDITOR_DATA_DIR"
	editorDataLocationFile = "editorx-data-location.json"
)

// The location record stays next to the executable so it is available before
// opening or locking a data directory. An environment override has no record.
type editorDataLocation struct {
	Directory string `json:"directory"`
	Pending   string `json:"pending,omitempty"`
	path      string
}

func editorDataDirectory() (string, error) {
	location, err := currentEditorDataLocation()
	return location.Directory, err
}

func currentEditorDataLocation() (editorDataLocation, error) {
	if configured := os.Getenv(editorDataDirectoryEnv); configured != "" {
		directory, err := filepath.Abs(configured)
		return editorDataLocation{Directory: directory}, err
	}
	executable, err := os.Executable()
	if err != nil {
		return editorDataLocation{}, err
	}
	return readEditorDataLocation(filepath.Join(filepath.Dir(executable), editorDataLocationFile))
}

func readEditorDataLocation(path string) (editorDataLocation, error) {
	location := editorDataLocation{path: path, Directory: filepath.Join(filepath.Dir(path), "data")}
	data, err := readTextAsset(path, 64<<10)
	if errors.Is(err, os.ErrNotExist) {
		return location, nil
	}
	if err == nil {
		err = json.Unmarshal(data, &location)
	}
	if err == nil && (!filepath.IsAbs(location.Directory) || location.Pending != "" && !filepath.IsAbs(location.Pending)) {
		err = errors.New(uiText("数据目录必须是绝对路径"))
	}
	if err == nil {
		var info os.FileInfo
		info, err = os.Stat(location.Directory)
		if err == nil && !info.IsDir() {
			err = fmt.Errorf(uiText("数据路径不是文件夹：%s"), location.Directory)
		}
	}
	if err != nil {
		return editorDataLocation{}, fmt.Errorf(uiText("无法读取数据目录设置 %s：%w"), path, err)
	}
	location.Directory = filepath.Clean(location.Directory)
	return location, nil
}

func (location editorDataLocation) save() error {
	data, err := json.MarshalIndent(location, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(location.path, data)
}

func claimEditorDataLocation(location editorDataLocation, paths []string) (editorDataLocation, func(), string, error) {
	for {
		release, err := claimEditorInstance(location.Directory, paths)
		if err != nil || release == nil || location.path == "" {
			return location, release, "", err
		}
		// Another launch may have completed the migration while this one was
		// waiting on the source lock.
		latest, err := readEditorDataLocation(location.path)
		if err != nil {
			release()
			return location, nil, "", err
		}
		if !strings.EqualFold(location.Directory, latest.Directory) {
			release()
			location = latest
			continue
		}
		location = latest
		if location.Pending == "" {
			return location, release, "", nil
		}
		migrated, releaseTarget, err := location.migrate()
		if err != nil {
			return location, release, "无法迁移数据，已继续使用原目录：" + err.Error(), nil
		}
		release()
		return migrated, releaseTarget, "数据已迁移至 " + migrated.Directory, nil
	}
}

func editorDataDestination(source, destination string) (string, error) {
	resolvedSource, err := filepath.EvalSymlinks(source)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(destination)
	if err != nil {
		return "", err
	}
	if strings.EqualFold(resolved, resolvedSource) {
		return "", errors.New(uiText("所选文件夹是当前数据目录"))
	}
	if gitPathWithin(resolvedSource, resolved) || gitPathWithin(resolved, resolvedSource) {
		return "", errors.New(uiText("请选择与当前数据目录互不包含的文件夹"))
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return "", err
	}
	if len(entries) != 0 {
		return "", errors.New(uiText("请选择一个空文件夹"))
	}
	return resolved, nil
}

func (location editorDataLocation) migrate() (editorDataLocation, func(), error) {
	target, err := editorDataDestination(location.Directory, location.Pending)
	if err != nil {
		return location, nil, err
	}
	if !strings.EqualFold(target, location.Pending) {
		return location, nil, errors.New(uiText("目标文件夹的位置已变化，请重新选择"))
	}
	release, err := lockSessionDirectory(target)
	if err != nil {
		return location, nil, err
	}
	migrated := location
	migrated.Directory, migrated.Pending = target, ""
	if err := copyEditorData(location.Directory, migrated); err != nil {
		release()
		return location, nil, err
	}
	removeLegacyEditorWebCache(location.Directory)
	return migrated, release, nil
}

func copyEditorData(source string, destination editorDataLocation) error {
	root, err := os.OpenRoot(source)
	if err != nil {
		return err
	}
	defer root.Close()
	staged, err := os.MkdirTemp(destination.Directory, ".editorx-migration-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staged)
	if err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || name == "." {
			return err
		}
		if entry.IsDir() {
			switch strings.ToLower(name) {
			case "webview2", "preview", "cli":
				return fs.SkipDir
			}
			return os.Mkdir(filepath.Join(staged, filepath.FromSlash(name)), 0700)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf(uiText("数据目录包含无法复制的条目：%s"), name)
		}
		file, err := root.Open(filepath.FromSlash(name))
		if err != nil {
			return err
		}
		defer file.Close()
		return writeFileFrom(filepath.Join(staged, filepath.FromSlash(name)), file, info.Size())
	}); err != nil {
		return err
	}

	entries, err := os.ReadDir(staged)
	if err != nil {
		return err
	}
	targetEntries, err := os.ReadDir(destination.Directory)
	if err != nil {
		return err
	}
	if len(targetEntries) != 1 || targetEntries[0].Name() != filepath.Base(staged) {
		return errors.New(uiText("目标文件夹已有其他内容"))
	}
	moved := 0
	for _, entry := range entries {
		// MoveFileW refuses to overwrite an entry created since validation.
		err = syscall.Rename(filepath.Join(staged, entry.Name()), filepath.Join(destination.Directory, entry.Name()))
		if err != nil {
			break
		}
		moved++
	}
	if err == nil {
		err = destination.save()
	}
	if err != nil {
		for _, entry := range entries[:moved] {
			err = errors.Join(err, syscall.Rename(filepath.Join(destination.Directory, entry.Name()), filepath.Join(staged, entry.Name())))
		}
	}
	return err
}
