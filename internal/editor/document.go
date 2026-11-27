package editor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type document struct {
	formatDirty bool
	path        string
	saved       string
	bom         bool
	encoding    textEncoding
	eol         string
	diskHash    [32]byte
}

var (
	errTextHasNUL      = errors.New("文件含有空字符，无法作为文本编辑")
	errDocumentChanged = errors.New("磁盘文件已被其他程序修改，请另存为以保留两个版本")
)

func readDocument(path string) (document, error) { return readDocumentAs(path, "") }

func readDocumentAs(path string, requested textEncoding) (document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return document{}, err
	}
	return decodeDocumentAs(path, data, requested)
}

func decodeDocument(path string, data []byte) (document, error) {
	return decodeDocumentAs(path, data, "")
}

func decodeDocumentAs(path string, data []byte, requested textEncoding) (document, error) {
	enc, bom, err := detectTextEncoding(data, true, requested)
	if err != nil {
		return document{}, err
	}
	doc := document{path: path, diskHash: sha256.Sum256(data), eol: "LF", encoding: enc, bom: bom}
	if bom {
		data = data[len(enc.bomPrefix()):]
	}
	decoded, err := decodeTextEncoding(data, enc)
	if err != nil {
		return document{}, err
	}
	value := string(decoded)
	crlf, cr, lf := strings.Count(value, "\r\n"), strings.Count(value, "\r"), strings.Count(value, "\n")
	switch {
	case crlf > 0 && cr == crlf && lf == crlf:
		doc.eol = "CRLF"
		value = strings.ReplaceAll(value, "\r\n", "\n")
	case cr > 0 && lf == 0:
		doc.eol = "CR"
		value = strings.ReplaceAll(value, "\r", "\n")
	case cr > 0:
		doc.eol = "混合"
	}
	doc.saved = value
	return doc, nil
}

func (doc document) checkDisk(ctx context.Context, path string) (os.FileInfo, error) {
	if doc.path == "" || !strings.EqualFold(filepath.Clean(path), filepath.Clean(doc.path)) {
		return nil, nil
	}
	input, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf(uiText("无法核对原文件，请另存为：%w"), err)
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return nil, err
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, largeEditContextReader{ctx: ctx, Reader: input}); err != nil {
		return nil, err
	}
	if !bytes.Equal(digest.Sum(nil), doc.diskHash[:]) {
		return nil, errDocumentChanged
	}
	return info, nil
}

func (doc *document) save(path, value string, history *localHistoryStore) error {
	original, err := doc.checkDisk(context.Background(), path)
	if err != nil {
		return err
	}
	if original != nil && value == doc.saved && !doc.formatDirty {
		return nil
	}
	data, err := doc.fileBytes(value)
	if err != nil {
		return err
	}
	digest, err := writeDocumentBytes(path, data, original, history)
	if err != nil {
		return err
	}
	doc.path, doc.saved, doc.diskHash = path, value, digest
	doc.formatDirty = false
	return nil
}

func (doc document) fileBytes(value string) ([]byte, error) {
	output := value
	if doc.eol == "LF" || doc.eol == "CRLF" || doc.eol == "CR" {
		output = strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
		if doc.eol == "CRLF" {
			output = strings.ReplaceAll(output, "\n", "\r\n")
		} else if doc.eol == "CR" {
			output = strings.ReplaceAll(output, "\n", "\r")
		}
	}
	data, err := encodeText(output, doc.encoding)
	if err != nil {
		return nil, err
	}
	if doc.bom {
		data = append(doc.encoding.bomPrefix(), data...)
	}
	return data, nil
}

func (doc document) create(path, value string) error {
	data, err := doc.fileBytes(value)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0666)
	if err != nil {
		return err
	}
	_, err = file.Write(data)
	if err == nil {
		err = file.Sync()
	}
	err = errors.Join(err, file.Close())
	if err != nil {
		return errors.Join(err, os.Remove(path))
	}
	return err
}

// 先写入同目录临时文件，完成同步后再替换目标，写入失败时保留原文件。
func writeFile(path string, data []byte) error {
	return writeFileFrom(path, bytes.NewReader(data), int64(len(data)))
}

func writeFileFrom(path string, data io.Reader, size int64) error {
	return writeFileAtomically(path, func(output io.Writer) error {
		_, err := io.CopyN(output, data, size)
		return err
	})
}

func writeFileAtomically(path string, write func(io.Writer) error) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".viva-save-*")
	if err != nil {
		return err
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)
	if err = write(file); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return replaceSavedFile(tempPath, path)
}
