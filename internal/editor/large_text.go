package editor

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

const (
	largeTextThreshold  = 64 << 20
	largeTextPageBytes  = 128 << 10
	largeTextQueryLimit = 1024
)

type largeTextPage struct {
	index, size, start           int64
	value                        string
	encoding                     textEncoding
	bom                          bool
	file                         os.FileInfo
	selectionStart, selectionEnd int
}

func largeTextFile(path string) bool {
	kind := documentFormat(path)
	if path == "" || kind != "纯文本" && kind != "日志" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Size() >= largeTextThreshold
}

func largeTextPageCount(size int64) int64 {
	return max(0, size-1)/largeTextPageBytes + 1
}

// Prefer complete lines. Long lines still split on character boundaries.
func largeTextBoundary(file io.ReaderAt, offset, size int64, enc textEncoding) (int64, error) {
	if offset <= 0 || offset >= size {
		return min(offset, size), nil
	}
	var window [4100]byte
	if order := enc.utf16Order(); order != nil {
		offset &^= 1
		start := max(0, offset-4096)
		data := window[:min(offset+2, size)-start]
		if _, err := file.ReadAt(data, start); err != nil {
			return 0, err
		}
		i := int(offset - start)
		for j := i - 2; j >= 0; j -= 2 {
			if order.Uint16(data[j:]) == '\n' {
				return start + int64(j+2), nil
			}
		}
		if i >= 2 && len(data) >= i+2 {
			unit := order.Uint16(data[i:])
			if unit >= 0xdc00 && unit <= 0xdfff || unit == '\n' && order.Uint16(data[i-2:]) == '\r' {
				return offset - 2, nil
			}
		}
		return offset, nil
	}

	start := max(0, offset-4096)
	data := window[:offset-start+1]
	if _, err := file.ReadAt(data, start); err != nil {
		return 0, err
	}
	i := len(data) - 1
	if newline := bytes.LastIndexByte(data[:i], '\n'); newline >= 0 {
		return start + int64(newline+1), nil
	}
	if enc.legacyMultibyte() {
		aligned, err := legacyTextBoundary(file, offset, size, enc)
		if err != nil || aligned != offset {
			return aligned, err
		}
	}
	if enc == utf8Encoding {
		for i > 0 && !utf8.RuneStart(data[i]) {
			offset--
			i--
		}
	}
	if i > 0 && data[i] == '\n' && data[i-1] == '\r' {
		offset--
	}
	return offset, nil
}

func (enc textEncoding) legacyMultibyte() bool {
	return enc == gbkEncoding || enc == gb18030Encoding || enc == big5Encoding || enc == shiftJISEncoding
}

// A byte below 0x30 cannot be a trail byte in these encodings. Start at
// the nearest such byte and scan forward without retaining the line.
func legacyTextBoundary(file io.ReaderAt, offset, size int64, enc textEncoding) (int64, error) {
	anchor := int64(0)
	var window [4096]byte
scan:
	for end := offset; end > 0; {
		start := max(0, end-int64(len(window)))
		data := window[:end-start]
		if _, err := file.ReadAt(data, start); err != nil {
			return 0, err
		}
		for i := len(data) - 1; i >= 0; i-- {
			if data[i] < 0x30 {
				anchor = start + int64(i) + 1
				break scan
			}
		}
		end = start
	}
	if anchor == offset {
		return anchor, nil
	}
	reader := bufio.NewReader(io.NewSectionReader(file, anchor, min(size, offset+4)-anchor))
	for anchor < offset {
		data, _ := reader.Peek(2)
		if len(data) == 0 {
			return 0, io.ErrUnexpectedEOF
		}
		width := 1
		lead := data[0]
		if enc == shiftJISEncoding {
			if lead >= 0x81 && lead <= 0x9f || lead >= 0xe0 && lead <= 0xfc {
				width = 2
			}
		} else if lead >= 0x81 && lead <= 0xfe {
			width = 2
			if enc == gb18030Encoding && len(data) == 2 && data[1] >= '0' && data[1] <= '9' {
				width = 4
			}
		}
		if anchor+int64(width) > offset {
			break
		}
		if _, err := reader.Discard(width); err != nil {
			return 0, err
		}
		anchor += int64(width)
	}
	return anchor, nil
}

func readLargeTextPage(path string, index, match int64, query string, requested textEncoding) (largeTextPage, error) {
	file, err := openSharedTextFile(path)
	if err != nil {
		return largeTextPage{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return largeTextPage{}, err
	}
	if !info.Mode().IsRegular() {
		return largeTextPage{}, errors.New(uiText("请选择普通文本文件"))
	}
	size := info.Size()
	enc, bom, err := inspectTextEncoding(file, size, requested)
	if err != nil {
		return largeTextPage{}, err
	}
	page, err := readLargeTextChunk(file, size, index, match, query, enc, bom)
	page.file = info
	return page, err
}

func readLargeTextChunk(file io.ReaderAt, size, index, match int64, query string, enc textEncoding, bom bool) (largeTextPage, error) {
	var err error
	last := largeTextPageCount(size) - 1
	if index < 0 {
		index = last
	}
	index = min(index, last)
	var needle []byte
	if match >= 0 {
		needle, err = encodeText(query, enc)
		if err != nil {
			return largeTextPage{}, err
		}
		if match+int64(len(needle)) > size {
			return largeTextPage{}, errors.New(uiText("文件已变化，请重新查找"))
		}
		index = match / largeTextPageBytes
	}
	start, err := largeTextBoundary(file, index*largeTextPageBytes, size, enc)
	if err != nil {
		return largeTextPage{}, err
	}
	end, err := largeTextBoundary(file, (index+1)*largeTextPageBytes, size, enc)
	if err != nil {
		return largeTextPage{}, err
	}
	if match >= end && index < last {
		index++
		start = end
		end, err = largeTextBoundary(file, (index+1)*largeTextPageBytes, size, enc)
		if err != nil {
			return largeTextPage{}, err
		}
	}
	if match >= 0 {
		end = max(end, match+int64(len(needle)))
	}
	data := make([]byte, end-start)
	if len(data) > 0 {
		if _, err := file.ReadAt(data, start); err != nil {
			return largeTextPage{}, err
		}
	}
	page := largeTextPage{index: index, size: size, start: start, encoding: enc, bom: bom}
	if start == 0 && bom {
		start = int64(len(enc.bomPrefix()))
		data = data[start:]
	}
	decoded, err := decodeTextEncoding(data, enc)
	if err != nil {
		return largeTextPage{}, err
	}
	if match >= start {
		begin := int(match - start)
		stop := begin + len(needle)
		if !bytes.Equal(data[begin:stop], needle) {
			return largeTextPage{}, errors.New(uiText("文件已变化，请重新查找"))
		}
		prefix, err := decodeTextEncoding(data[:begin], enc)
		if err != nil {
			return largeTextPage{}, err
		}
		page.selectionStart = utf8.RuneCount(prefix) - bytes.Count(prefix, []byte("\r\n"))
		page.selectionEnd = page.selectionStart + utf8.RuneCountInString(query)
	}
	page.value = strings.ReplaceAll(string(decoded), "\r\n", "\n")
	return page, nil
}

func findLargeText(ctx context.Context, path, query string, from int64, direction int, requested textEncoding) (int64, error) {
	if len(query) == 0 || len(query) > largeTextQueryLimit {
		return -1, nil
	}
	file, err := openSharedTextFile(path)
	if err != nil {
		return -1, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return -1, err
	}
	size := info.Size()
	enc, bom, err := inspectTextEncoding(file, size, requested)
	if err != nil {
		return -1, err
	}
	needle, err := encodeText(query, enc)
	if err != nil {
		return -1, err
	}
	first := int64(0)
	if bom {
		first = int64(len(enc.bomPrefix()))
	}
	overlap := int64(len(needle) - 1)
	buffer := make([]byte, largeTextPageBytes+overlap)
	for position := min(max(from, first), size); ; {
		if err := ctx.Err(); err != nil {
			return -1, err
		}
		left, right := position, min(size, position+largeTextPageBytes+overlap)
		if direction < 0 {
			if position <= first {
				break
			}
			left, right = max(first, position-largeTextPageBytes), min(size, position+overlap)
		}
		if right-left < int64(len(needle)) {
			break
		}
		data := buffer[:right-left]
		if _, err := file.ReadAt(data, left); err != nil {
			return -1, err
		}
		candidateBase := left
		for len(data) >= len(needle) {
			if err := ctx.Err(); err != nil {
				return -1, err
			}
			index := 0
			if direction < 0 {
				index = bytes.LastIndex(data, needle)
			} else {
				index = bytes.Index(data, needle)
			}
			if index < 0 {
				break
			}
			candidate := candidateBase + int64(index)
			valid := enc.utf16Order() == nil || candidate%2 == 0
			if enc.legacyMultibyte() {
				boundary, err := legacyTextBoundary(file, candidate, size, enc)
				if err != nil {
					return -1, err
				}
				valid = boundary == candidate
			}
			if valid {
				return candidate, nil
			}
			if direction < 0 {
				data = data[:index+len(needle)-1]
			} else {
				data = data[index+1:]
				candidateBase = candidate + 1
			}
		}
		if direction < 0 {
			position = left
		} else {
			position += largeTextPageBytes
		}
	}
	return -1, nil
}
