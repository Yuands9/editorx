package editor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/vivaui/viva/app"
)

const historyCompareLineLimit = 1_050_000

var (
	errHistoryCompareBytes = fmt.Errorf("文件超过 %d MiB", localHistoryFileSizeLimit>>20)
	errHistoryCompareLines = fmt.Errorf("文件超过 %d 行", historyCompareLineLimit)
)

type localHistoryComparison struct {
	old, current            document
	oldProblem, diskProblem string
	fromEditor              bool
	diff                    historyDiff
}

type historyPreviewBuffer struct{ bytes.Buffer }

func (b *historyPreviewBuffer) Write(data []byte) (int, error) {
	if b.Len()+len(data) > localHistoryFileSizeLimit {
		return 0, errHistoryCompareBytes
	}
	return b.Buffer.Write(data)
}

func historyCompareSize(value string) error {
	if len(value) > localHistoryFileSizeLimit {
		return errHistoryCompareBytes
	}
	if strings.Count(value, "\n") >= historyCompareLineLimit {
		return errHistoryCompareLines
	}
	return nil
}

func decodeHistoryComparison(path string, data []byte) (document, error) {
	doc, err := decodeDocument(path, data)
	if err != nil {
		return document{}, err
	}
	if err := historyCompareSize(doc.saved); err != nil {
		return document{}, err
	}
	return doc, nil
}

func historyComparison(ctx context.Context, store *localHistoryStore, source string, version localHistoryVersion, working *document) localHistoryComparison {
	var comparison localHistoryComparison
	if officeKind(source) != "" || previewFormat(source) != "" || isImageFile(source) {
		comparison.oldProblem = "可另存此版本后用对应的应用查看。"
		return comparison
	}
	var data historyPreviewBuffer
	err := store.copyVersion(ctx, version, &data)
	if err == nil {
		comparison.old, err = decodeHistoryComparison(source, data.Bytes())
	}
	if err != nil {
		comparison.oldProblem = "无法比对历史版本：" + err.Error()
		return comparison
	}
	if working != nil {
		comparison.fromEditor = true
		err = historyCompareSize(working.saved)
		if err == nil {
			comparison.current = *working
		}
	} else {
		comparison.current, err = readHistoryCurrentFile(ctx, source)
	}
	if errors.Is(err, os.ErrNotExist) {
		comparison.diskProblem = "原文件已不存在，可另存历史副本。"
	} else if err != nil {
		comparison.diskProblem = "无法比对当前文件：" + err.Error()
	}
	if ctx.Err() == nil {
		comparison.compareLines()
	}
	return comparison
}

func readHistoryCurrentFile(ctx context.Context, path string) (document, error) {
	file, err := os.Open(path)
	if err != nil {
		return document{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return document{}, err
	}
	if info.Size() > localHistoryFileSizeLimit {
		return document{}, errHistoryCompareBytes
	}
	data, err := io.ReadAll(io.LimitReader(largeEditContextReader{ctx: ctx, Reader: file}, localHistoryFileSizeLimit+1))
	if err != nil {
		return document{}, err
	}
	if len(data) > localHistoryFileSizeLimit {
		return document{}, errHistoryCompareBytes
	}
	return decodeHistoryComparison(path, data)
}

func (comparison *localHistoryComparison) compareLines() {
	previous := comparison.old.saved
	if comparison.diskProblem == "" && comparison.current.eol != "混合" {
		previous = strings.ReplaceAll(previous, "\r\n", "\n")
	}
	comparison.diff = compareHistoryLines(previous, comparison.current.saved)
}

func chooseHistoryCopyPath(store *localHistoryStore, source string, version localHistoryVersion) (string, error) {
	name := filepath.Base(source)
	if source == "" {
		name = "历史版本.bin"
	}
	ext := filepath.Ext(name)
	name = strings.TrimSuffix(name, ext) + "（历史 " + version.at.Local().Format("20060102-150405") + "）" + ext
	path, err := app.SaveFile(app.SaveFileOptions{Title: "另存历史版本", SuggestedName: name})
	if err != nil {
		return "", err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if gitPathWithin(store.directory, path) {
		return "", errors.New("请选择本地历史目录之外的位置")
	}
	if source != "" && strings.EqualFold(filepath.Clean(path), filepath.Clean(source)) {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return "", errors.New("请选择一个新文件名保存副本")
		}
	}
	return path, nil
}
