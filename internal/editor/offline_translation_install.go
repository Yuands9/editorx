package editor

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const offlineTranslationDirectory = "translation-offline"

type offlineInstallation struct {
	ID    string `json:"id"`
	Bytes int64  `json:"bytes"`
}

func offlineComponentPath(directory string, component offlineComponent) string {
	return filepath.Join(directory, offlineTranslationDirectory, component.ID)
}

func readOfflineInstallation(directory string, component offlineComponent) (*offlineInstallation, error) {
	path := offlineComponentPath(directory, component)
	data, err := readTextAsset(filepath.Join(path, "installed.json"), 4096)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var info offlineInstallation
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, err
	}
	if info.ID != component.ID || info.Bytes <= 0 {
		return nil, errors.New("离线组件信息损坏，请移除后重新下载")
	}
	for _, name := range component.Required {
		file, err := os.Stat(filepath.Join(path, name))
		if err != nil || !file.Mode().IsRegular() || file.Size() == 0 {
			return nil, errors.New("离线组件不完整，请移除后重新下载")
		}
	}
	return &info, nil
}

func installOfflineComponent(ctx context.Context, client *http.Client, directory string, component offlineComponent, progress *dictionaryProgress) (*offlineInstallation, error) {
	if info, err := readOfflineInstallation(directory, component); info != nil || err != nil {
		return info, err
	}
	destination := offlineComponentPath(directory, component)
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return nil, err
	}
	temporary, err := os.MkdirTemp(parent, ".install-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(temporary)
	info := &offlineInstallation{ID: component.ID}
	for _, archive := range component.Archives {
		path := filepath.Join(temporary, "download.zip")
		if err := downloadOfflineArchive(ctx, client, archive, path, progress); err != nil {
			return nil, err
		}
		bytes, err := unpackOfflineArchive(ctx, path, temporary, component)
		if err != nil {
			return nil, err
		}
		info.Bytes += bytes
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	}
	if component.ID == offlineRuntime.ID {
		// SentencePiece's official wheel already carries this redistributable DLL.
		// CTranslate2 imports its standard name; keep an app-local copy so no system
		// Visual C++ installation or administrator rights are required.
		data, err := os.ReadFile(filepath.Join(temporary, "sentencepiece.libs", "msvcp140-a4c2229bdc2a2a630acdc095b4d86008.dll"))
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(temporary, "msvcp140.dll"), data, 0600); err != nil {
			return nil, err
		}
		info.Bytes += int64(len(data))
	}
	for _, name := range component.Required {
		if file, err := os.Stat(filepath.Join(temporary, name)); err != nil || !file.Mode().IsRegular() || file.Size() == 0 {
			return nil, errors.New("下载包缺少运行文件")
		}
	}
	data, err := json.Marshal(info)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(temporary, "installed.json"), data, 0600); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for attempt := 0; ; attempt++ {
		err := os.Rename(temporary, destination)
		if err == nil {
			break
		}
		// Another window may have finished installing the same immutable version.
		if existing, readErr := readOfflineInstallation(directory, component); readErr == nil && existing != nil {
			return existing, nil
		}
		// Windows antivirus can briefly hold a newly extracted DLL directory.
		if !os.IsPermission(err) || attempt >= 50 {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return info, nil
}

func downloadOfflineArchive(ctx context.Context, client *http.Client, archive offlineArchive, path string, progress *dictionaryProgress) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, archive.URL, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("组件下载返回 HTTP %d", response.StatusCode)
	}
	if response.ContentLength > 0 && response.ContentLength != archive.Bytes {
		return errors.New("下载大小与发布版本不符")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	digest := sha256.New()
	count, copyErr := io.Copy(io.MultiWriter(file, digest, progress), io.LimitReader(response.Body, archive.Bytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if count != archive.Bytes || hex.EncodeToString(digest.Sum(nil)) != archive.SHA256 {
		return errors.New("离线组件校验失败，请重试下载")
	}
	return nil
}

func unpackOfflineArchive(ctx context.Context, path, destination string, component offlineComponent) (int64, error) {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return 0, err
	}
	defer archive.Close()
	var total int64
	for _, entry := range archive.File {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		name := strings.TrimPrefix(entry.Name, component.Prefix)
		if entry.FileInfo().IsDir() {
			continue
		}
		// Models need no Stanza/PyTorch tokenizer; retain model, tokenizer and attribution.
		if component.Prefix != "" && (!strings.HasPrefix(entry.Name, component.Prefix) || !slices.Contains(component.Required, name)) {
			continue
		}
		if !filepath.IsLocal(name) || strings.ContainsAny(name, "\\:") || entry.Mode()&os.ModeSymlink != 0 || name == "installed.json" || name == "download.zip" {
			return 0, errors.New("离线组件包含无效路径")
		}
		total += int64(entry.UncompressedSize64)
		if entry.UncompressedSize64 > 200<<20 || total > 400<<20 {
			return 0, errors.New("离线组件解压超过大小限制")
		}
		target := filepath.Join(destination, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return 0, err
		}
		reader, err := entry.Open()
		if err != nil {
			return 0, err
		}
		file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			reader.Close()
			return 0, err
		}
		_, copyErr := io.Copy(file, reader)
		reader.Close()
		closeErr := file.Close()
		if copyErr != nil {
			return 0, copyErr
		}
		if closeErr != nil {
			return 0, closeErr
		}
	}
	return total, nil
}

func removeOfflineComponents(directory string) error {
	components := append([]offlineComponent{offlineRuntime}, offlineModels[:]...)
	for _, component := range components {
		path := offlineComponentPath(directory, component)
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("离线组件路径不是普通目录")
		}
		// Only these fixed, versioned component directories belong to this installer.
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	return nil
}
