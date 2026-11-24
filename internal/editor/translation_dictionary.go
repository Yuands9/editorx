package editor

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"unicode/utf8"
)

const dictionaryDirectory = "dictionaries/ecdict-common"

type dictionaryDownload struct {
	url, checksum string
	size          int64
}

// Pin a reviewed data revision. The CSV is streamed into common-word shards;
// neither the source CSV nor a dictionary index is retained in memory.
var englishChineseDictionary = dictionaryDownload{
	url:      "https://raw.githubusercontent.com/skywind3000/ECDICT/39cfb9672c0906b63569e0453c2e6abfb9ab182a/ecdict.csv",
	checksum: "d1ecbdf9ee2b920afb6f430ca6b6bb9f559588cf0dff86e04bd255f9390843cc",
	size:     66009548,
}

type dictionaryInfo struct {
	Entries       int    `json:"entries"`
	Bytes         int64  `json:"bytes"`
	Source        string `json:"source"`
	DownloadBytes int64  `json:"downloadBytes,omitempty"`
}

type dictionaryProgress struct {
	received   atomic.Int64
	total      atomic.Int64
	notified   int64
	invalidate func()
}

func (progress *dictionaryProgress) Write(data []byte) (int, error) {
	completed := progress.received.Add(int64(len(data)))
	if completed-progress.notified >= 256<<10 {
		progress.notified = completed
		if progress.invalidate != nil {
			progress.invalidate()
		}
	}
	return len(data), nil
}

func readDictionaryInfo(directory string) (*dictionaryInfo, error) {
	data, err := readTextAsset(filepath.Join(directory, "manifest.json"), 4096)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var info dictionaryInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, err
	}
	if info.Entries <= 0 || info.Bytes <= 0 {
		return nil, errors.New("词库信息损坏，请移除后重新下载")
	}
	return &info, nil
}

func installTranslationDictionary(ctx context.Context, client *http.Client, spec dictionaryDownload, destination string, progress *dictionaryProgress) (dictionaryInfo, error) {
	var info dictionaryInfo
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, spec.url, nil)
	if err != nil {
		return info, err
	}
	// Handle gzip explicitly so progress reports transferred bytes, not the
	// much larger decompressed CSV that Go's default transport would expose.
	request.Header.Set("Accept-Encoding", "gzip")
	response, err := client.Do(request)
	if err != nil {
		return info, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return info, fmt.Errorf("词库下载返回 HTTP %d", response.StatusCode)
	}
	if response.ContentLength > spec.size {
		return info, errors.New("词库大小与发布版本不符")
	}
	progress.total.Store(response.ContentLength)
	transfer := &io.LimitedReader{R: response.Body, N: spec.size + 1}
	var source io.Reader = io.TeeReader(transfer, progress)
	if response.Header.Get("Content-Encoding") == "gzip" {
		compressed, err := gzip.NewReader(source)
		if err != nil {
			return info, fmt.Errorf("压缩词库无效：%w", err)
		}
		defer compressed.Close()
		source = compressed
	} else if encoding := response.Header.Get("Content-Encoding"); encoding != "" {
		return info, fmt.Errorf("词库使用不支持的压缩方式：%s", encoding)
	}
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return info, err
	}
	temporary, err := os.MkdirTemp(parent, ".ecdict-download-")
	if err != nil {
		return info, err
	}
	defer os.RemoveAll(temporary) // Only the unique directory created by this download.
	var files [26]*os.File
	var writers [26]*csv.Writer
	defer func() {
		for _, file := range files {
			if file != nil {
				file.Close()
			}
		}
	}()
	digest := sha256.New()
	bounded := &io.LimitedReader{R: source, N: spec.size + 1}
	reader := csv.NewReader(io.TeeReader(bounded, digest))
	reader.ReuseRecord = true
	columns, err := reader.Read()
	if err != nil || len(columns) < 11 || columns[0] != "word" || columns[3] != "translation" || columns[8] != "bnc" || columns[9] != "frq" {
		return info, errors.New("下载文件不是 ECDICT 词库")
	}
	for {
		if err := ctx.Err(); err != nil {
			return info, err
		}
		record, err := reader.Read()
		if bounded.N == 0 {
			return info, errors.New("词库超过下载大小限制")
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return info, fmt.Errorf("词库数据不完整：%w", err)
		}
		bnc, _ := strconv.Atoi(record[8])
		frequency, _ := strconv.Atoi(record[9])
		common := bnc > 0 && bnc <= 30000 || frequency > 0 && frequency <= 30000 || record[7] != "" || record[6] == "1"
		word := strings.ToLower(strings.TrimSpace(record[0]))
		translation := strings.ReplaceAll(record[3], `\n`, "\n")
		if !common || word == "" || word[0] < 'a' || word[0] > 'z' || len(word) > 80 || strings.TrimSpace(translation) == "" {
			continue
		}
		entry := translationEntry{"en", "zh-cn", translationTerm, word, translation, "ECDICT"}
		if record[1] != "" && len(record[1]) < 160 {
			entry.context += " · /" + record[1] + "/"
		}
		if err := entry.validate(); err != nil {
			continue
		}
		entries := []translationEntry{entry}
		seen := map[string]bool{word: true}
		// ECDICT supplies real inflections (files, saved, running, ...).
		// Store their base-word definitions instead of guessing stems at lookup time.
		for _, exchange := range strings.Split(record[10], "/") {
			kind, form, ok := strings.Cut(exchange, ":")
			if !ok || len(kind) != 1 || !strings.Contains("pdi3rts", kind) {
				continue
			}
			form = strings.ToLower(strings.TrimSpace(form))
			if form == "" || len(form) > 80 || form[0] < 'a' || form[0] > 'z' || seen[form] {
				continue
			}
			variant := entry
			variant.source, variant.context = form, "ECDICT · 原形 "+word
			if err := variant.validate(); err != nil {
				continue
			}
			seen[form] = true
			entries = append(entries, variant)
		}
		for _, entry := range entries {
			bucket := entry.source[0] - 'a'
			if files[bucket] == nil {
				file, err := os.Create(filepath.Join(temporary, string(entry.source[0])+".csv"))
				if err != nil {
					return info, err
				}
				files[bucket], writers[bucket] = file, csv.NewWriter(file)
				if err := writers[bucket].Write(translationColumns[:]); err != nil {
					return info, err
				}
			}
			if err := writers[bucket].Write([]string{entry.sourceLang, entry.targetLang, entry.kind, entry.source, entry.target, entry.context}); err != nil {
				return info, err
			}
			info.Entries++
		}
	}
	if transfer.N == 0 || spec.size+1-bounded.N != spec.size || hex.EncodeToString(digest.Sum(nil)) != spec.checksum {
		return info, errors.New("词库校验失败，请重试下载")
	}
	for index, file := range files {
		if file == nil {
			continue
		}
		writers[index].Flush()
		if err := writers[index].Error(); err != nil {
			return info, err
		}
		stat, err := file.Stat()
		if err != nil {
			return info, err
		}
		if stat.Size() > translationLibraryLimit {
			return info, errors.New("词库分片超过 2 MiB，未安装")
		}
		info.Bytes += stat.Size()
		if err := file.Close(); err != nil {
			return info, err
		}
		files[index] = nil
	}
	if info.Entries == 0 {
		return info, errors.New("下载文件没有可用词条")
	}
	info.Source = spec.url
	info.DownloadBytes = progress.received.Load()
	metadata, err := json.Marshal(info)
	if err != nil {
		return info, err
	}
	if err := os.WriteFile(filepath.Join(temporary, "LICENSE.txt"), []byte(dictionaryLicense), 0600); err != nil {
		return info, err
	}
	if err := os.WriteFile(filepath.Join(temporary, "manifest.json"), metadata, 0600); err != nil {
		return info, err
	}
	if err := ctx.Err(); err != nil {
		return info, err
	}
	// An existing install (including another window's completed download) is never overwritten.
	if err := os.Rename(temporary, destination); err != nil {
		return info, fmt.Errorf("无法安装词库（已有词库时请先移除）：%w", err)
	}
	return info, nil
}

func removeTranslationDictionary(directory string) error {
	info, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("词库目录不是普通文件夹，请先检查路径")
	}
	// Remove only files owned by this dictionary; never recurse through user files.
	names := []string{"manifest.json", "LICENSE.txt"}
	for letter := 'a'; letter <= 'z'; letter++ {
		names = append(names, string(letter)+".csv")
	}
	for _, name := range names {
		if err := os.Remove(filepath.Join(directory, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := os.Remove(directory); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (m *editorApp) lookupDownloadedDictionary(state *translationPanelState) error {
	if state.dictionary == nil || state.querySource != "en" || state.queryTarget != "zh-cn" {
		return nil
	}
	query := strings.ToLower(strings.TrimSpace(state.target.selected))
	var buckets [26]bool
	for index := 0; index < len(query); {
		letter := query[index]
		if letter >= 'a' && letter <= 'z' {
			buckets[letter-'a'] = true
			for index < len(query) && query[index] >= 'a' && query[index] <= 'z' {
				index++
			}
		} else {
			_, size := utf8.DecodeRuneInString(query[index:])
			index += size
		}
	}
	for index, used := range buckets {
		if !used {
			continue
		}
		path := filepath.Join(m.dataDirectory, dictionaryDirectory, string(byte('a'+index))+".csv")
		if err := scanTranslationLibrary(path, true, func(entry translationEntry) {
			item := translationSuggestion{entry: entry, scope: "下载词库"}
			if query == entry.source && len(state.suggestions) < translationMatchLimit {
				state.suggestions = append(state.suggestions, item)
			} else if len(state.terms) < translationMatchLimit && termMatches(query, entry.source) {
				state.terms = append(state.terms, item)
			}
		}); err != nil {
			return err
		}
	}
	return nil
}

const dictionaryLicense = `ECDICT — https://github.com/skywind3000/ECDICT
Common words selected by BNC / contemporary frequency (top 30,000), exam tags and Oxford flags.
Source revision: 39cfb9672c0906b63569e0453c2e6abfb9ab182a

MIT License

Copyright (c) 2025 Linwei

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
`
