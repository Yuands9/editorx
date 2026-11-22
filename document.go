package main

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

type document struct {
	path     string
	saved    string
	bom      bool
	eol      string
	diskHash [32]byte
}

var (
	errTextNotUTF8 = errors.New("此原型支持 UTF-8 文件，请先将文件转换为 UTF-8")
	errTextHasNUL  = errors.New("文件含有空字符，请选择纯文本文件")
)

func readDocument(path string) (document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return document{}, err
	}
	doc := document{path: path, diskHash: sha256.Sum256(data), eol: "LF"}
	doc.bom = bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf})
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	if !utf8.Valid(data) {
		return document{}, errTextNotUTF8
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return document{}, errTextHasNUL
	}
	value := string(data)
	crlf := strings.Count(value, "\r\n")
	if crlf > 0 {
		if strings.Count(value, "\n") == crlf {
			doc.eol = "CRLF"
			value = strings.ReplaceAll(value, "\r\n", "\n")
		} else {
			doc.eol = "混合"
		}
	}
	doc.saved = value
	return doc, nil
}

func (doc *document) save(path, value string) error {
	if doc.path != "" && strings.EqualFold(filepath.Clean(path), filepath.Clean(doc.path)) {
		current, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("无法核对原文件，请另存为：%w", err)
		}
		if sha256.Sum256(current) != doc.diskHash {
			return errors.New("磁盘文件已被其他程序修改，请另存为以保留两个版本")
		}
	}
	output := value
	if doc.eol == "CRLF" {
		output = strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\n", "\r\n")
	}
	if doc.bom {
		output = "\ufeff" + output
	}
	data := []byte(output)
	if err := writeFile(path, data); err != nil {
		return err
	}
	doc.path, doc.saved, doc.diskHash = path, value, sha256.Sum256(data)
	return nil
}

// 先写入同目录临时文件，完成同步后再替换目标，写入失败时保留原文件。
func writeFile(path string, data []byte) error {
	return writeFileFrom(path, bytes.NewReader(data), int64(len(data)))
}

func writeFileFrom(path string, data io.Reader, size int64) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".viva-save-*")
	if err != nil {
		return err
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)
	if _, err = io.CopyN(file, data, size); err != nil {
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
	return os.Rename(tempPath, path)
}
