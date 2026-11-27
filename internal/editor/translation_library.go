package editor

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

const (
	translationLibraryFile  = "translations.csv"
	translationLibraryLimit = 2 << 20
	translationTextLimit    = 8 << 10
	translationMatchLimit   = 12
	translationTerm         = "term"
	translationSentence     = "sentence"
)

var translationColumns = [...]string{"source_lang", "target_lang", "type", "source", "target", "context"}

type translationEntry struct {
	sourceLang, targetLang string
	kind                   string
	source, target         string
	context                string
}

type translationKey struct {
	sourceLang, targetLang, kind, source, context string
}

func (entry translationEntry) key() translationKey {
	source := entry.source
	if entry.kind == translationTerm {
		source = strings.ToLower(source)
	}
	return translationKey{entry.sourceLang, entry.targetLang, entry.kind, source, entry.context}
}

func languageCode(value string) string { return strings.ToLower(strings.TrimSpace(value)) }

func validLanguageCode(code string) bool {
	if len(code) < 2 || len(code) > 32 || code[0] < 'a' || code[0] > 'z' {
		return false
	}
	for _, part := range strings.Split(code, "-") {
		if part == "" || strings.IndexFunc(part, func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
		}) >= 0 {
			return false
		}
	}
	return true
}

func (entry *translationEntry) validate() error {
	entry.sourceLang, entry.targetLang = languageCode(entry.sourceLang), languageCode(entry.targetLang)
	entry.context = strings.TrimSpace(entry.context)
	if !validLanguageCode(entry.sourceLang) || !validLanguageCode(entry.targetLang) || entry.sourceLang == entry.targetLang {
		return errors.New(uiText("请输入不同的语言代码，如 en 和 zh-CN"))
	}
	if entry.kind != translationTerm && entry.kind != translationSentence {
		return errors.New(uiText("词条类型须为 term 或 sentence"))
	}
	if strings.TrimSpace(entry.source) == "" || strings.TrimSpace(entry.target) == "" {
		return errors.New(uiText("请填写原文和译文"))
	}
	if len(entry.source) > translationTextLimit || len(entry.target) > translationTextLimit || len(entry.context) > 400 {
		return errors.New(uiText("原文和译文各限 8 KiB，用法备注限 400 字节"))
	}
	for _, value := range []string{entry.source, entry.target, entry.context} {
		if !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
			return errors.New(uiText("词条须为 UTF-8 文本"))
		}
	}
	return nil
}

func readTranslationLibrary(path string, allowMissing bool) ([]translationEntry, error) {
	var entries []translationEntry
	err := scanTranslationLibrary(path, allowMissing, func(entry translationEntry) {
		entries = append(entries, entry)
	})
	if err != nil {
		return nil, err
	}
	return entries, nil
}

func scanTranslationLibrary(path string, allowMissing bool, visit func(translationEntry)) error {
	file, err := os.Open(path)
	if allowMissing && errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New(uiText("翻译库须为普通 CSV 文件"))
	}
	if info.Size() > translationLibraryLimit {
		return errors.New(uiText("翻译库超过 2 MiB"))
	}
	bounded := &io.LimitedReader{R: file, N: translationLibraryLimit + 1}
	buffer := bufio.NewReader(bounded)
	if prefix, _ := buffer.Peek(3); bytes.Equal(prefix, []byte{0xef, 0xbb, 0xbf}) {
		_, _ = buffer.Discard(3)
	}
	reader := csv.NewReader(buffer)
	reader.FieldsPerRecord = len(translationColumns)
	reader.ReuseRecord = true
	columns, err := reader.Read()
	if err != nil || !slices.Equal(columns, translationColumns[:]) {
		return fmt.Errorf(uiText("CSV 表头须为 %s"), strings.Join(translationColumns[:], ","))
	}
	for {
		record, err := reader.Read()
		if bounded.N == 0 {
			return errors.New(uiText("翻译库超过 2 MiB"))
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		entry := translationEntry{record[0], record[1], record[2], record[3], record[4], record[5]}
		if err := entry.validate(); err != nil {
			row, _ := reader.FieldPos(0)
			return fmt.Errorf(uiText("CSV 第 %d 行：%w"), row, err)
		}
		visit(entry)
	}
}

func encodeTranslationLibrary(entries []translationEntry) ([]byte, error) {
	var output bytes.Buffer
	writer := csv.NewWriter(&output)
	if err := writer.Write(translationColumns[:]); err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if err := writer.Write([]string{entry.sourceLang, entry.targetLang, entry.kind, entry.source, entry.target, entry.context}); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	return output.Bytes(), writer.Error()
}

func mergeTranslationLibrary(path string, additions []translationEntry) (added, duplicates int, err error) {
	entries, err := readTranslationLibrary(path, true)
	if err != nil {
		return 0, 0, err
	}
	existing := make(map[translationKey]string, len(entries)+len(additions))
	for _, entry := range entries {
		existing[entry.key()] = entry.target
	}
	for _, entry := range additions {
		if err := entry.validate(); err != nil {
			return 0, 0, err
		}
		if target, ok := existing[entry.key()]; ok {
			if target != entry.target {
				return 0, 0, errors.New(uiText("库中已有相同语言、原文和用法的不同译文，请在库文件中核对或填写不同的用法备注"))
			}
			duplicates++
			continue
		}
		existing[entry.key()] = entry.target
		entries = append(entries, entry)
		added++
	}
	if added == 0 {
		return 0, duplicates, nil
	}
	data, err := encodeTranslationLibrary(entries)
	if err == nil {
		err = writeTextAsset(path, data, translationLibraryLimit)
	}
	if err != nil {
		return 0, 0, err
	}
	return added, duplicates, nil
}

func termMatches(source, term string) bool {
	if strings.ContainsFunc(term, unspacedScript) {
		return strings.Contains(source, term)
	}
	word := func(r rune) bool {
		return !unspacedScript(r) && (unicode.IsLetter(r) || unicode.IsMark(r) || unicode.IsDigit(r) || r == '_')
	}
	for position := 0; position < len(source); {
		offset := strings.Index(source[position:], term)
		if offset < 0 {
			return false
		}
		start := position + offset
		end := start + len(term)
		before, _ := utf8.DecodeLastRuneInString(source[:start])
		after, _ := utf8.DecodeRuneInString(source[end:])
		if !word(before) && !word(after) {
			return true
		}
		_, size := utf8.DecodeRuneInString(source[start:])
		position = start + size
	}
	return false
}

func unspacedScript(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul)
}

// 首次校验时才编译。%%、命名变量、printf 参数、代码片段和链接目标都保留原样。
var translationMarkerPattern = sync.OnceValue(func() *regexp.Regexp {
	return regexp.MustCompile(`\x60[^\x60\r\n]+\x60|\]\([^)\r\n]+\)|\{\{[ \t]*[\pL\pN_.-]+[ \t]*\}\}|[$%]?\{[\pL\pN_.-]+\}|%%|%(?:[1-9][0-9]*\$|\[[1-9][0-9]*\])?[-+#0']*(?:[0-9]+|\*)?(?:\.(?:[0-9]+|\*))?(?:hh|ll|[hlLzjt])?[bcdeEfFgGiopqstTuvxX]`)
})

func validateTranslationMarkers(source, translated string) error {
	pattern := translationMarkerPattern()
	original, result := pattern.FindAllString(source, -1), pattern.FindAllString(translated, -1)
	counts := make(map[string]int, len(original))
	for _, marker := range original {
		counts[marker]++
	}
	for _, marker := range result {
		if counts[marker] == 0 {
			return fmt.Errorf(uiText("译文包含额外或重复的标记 %s"), marker)
		}
		counts[marker]--
	}
	for _, marker := range original {
		if counts[marker] != 0 {
			return fmt.Errorf(uiText("译文缺少原文标记 %s"), marker)
		}
	}
	orderedPrintf := func(markers []string) []string {
		var ordered []string
		for _, marker := range markers {
			if strings.HasPrefix(marker, "%") && marker != "%%" &&
				!strings.Contains(marker, "$") && !strings.HasPrefix(marker, "%[") {
				ordered = append(ordered, marker)
			}
		}
		return ordered
	}
	if !slices.Equal(orderedPrintf(original), orderedPrintf(result)) {
		return errors.New(uiText("未编号的格式参数顺序已改变"))
	}
	return nil
}

// JSON 源码只改引号内的值，排除键、跨值选区和被截断的转义序列。
func jsonTranslationReplacement(value string, start, end int, translated string) (string, error) {
	for offset := 0; offset <= start && offset < len(value); offset++ {
		if value[offset] != '"' {
			continue
		}
		contentStart := offset + 1
		escapedSelection := false
		for offset++; offset < len(value) && value[offset] != '"'; offset++ {
			if value[offset] == '\\' {
				escapeEnd := offset + 2
				if offset+1 < len(value) && value[offset+1] == 'u' {
					escapeEnd += 4
				}
				escapedSelection = escapedSelection || start < escapeEnd && end > offset
				offset = escapeEnd - 1
			}
		}
		if !escapedSelection && start >= contentStart && end <= offset && offset < len(value) &&
			!strings.HasPrefix(strings.TrimLeft(value[offset+1:], " \t\r\n"), ":") {
			encoded, _ := json.Marshal(translated)
			return string(encoded[1 : len(encoded)-1]), nil
		}
	}
	return "", errors.New(uiText("JSON 中请选择单个字符串值内的普通文本。仍可复制译文"))
}
