package editor

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/encoding/unicode"
)

type textEncoding string

const (
	utf8Encoding        textEncoding = "UTF-8"
	utf16LEEncoding     textEncoding = "UTF-16 LE"
	utf16BEEncoding     textEncoding = "UTF-16 BE"
	gbkEncoding         textEncoding = "GBK"
	gb18030Encoding     textEncoding = "GB18030"
	big5Encoding        textEncoding = "Big5"
	shiftJISEncoding    textEncoding = "Shift-JIS"
	windows1252Encoding textEncoding = "Windows-1252"
	latin1Encoding      textEncoding = "ISO-8859-1"
	textEncodingSample               = 64 << 10
)

var textEncodings = [...]textEncoding{utf8Encoding, utf16LEEncoding, utf16BEEncoding, gbkEncoding, gb18030Encoding, big5Encoding, shiftJISEncoding, windows1252Encoding, latin1Encoding}
var errTextEncoding = errors.New("无法按所选编码读取文件，请尝试其他编码")
var errTextPE = errors.New("文件内容是 Windows 可执行程序（PE），无法作为文本编辑")

func (enc textEncoding) charset() encoding.Encoding {
	switch enc {
	case utf16LEEncoding:
		return unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM)
	case utf16BEEncoding:
		return unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM)
	case gbkEncoding:
		return simplifiedchinese.GBK
	case gb18030Encoding:
		return simplifiedchinese.GB18030
	case big5Encoding:
		return traditionalchinese.Big5
	case shiftJISEncoding:
		return japanese.ShiftJIS
	case windows1252Encoding:
		return charmap.Windows1252
	case latin1Encoding:
		return charmap.ISO8859_1
	}
	return nil
}

func (enc textEncoding) label(bom bool) string {
	if enc == "" {
		enc = utf8Encoding
	}
	if bom {
		return string(enc) + " BOM"
	}
	return string(enc)
}

func (enc textEncoding) bomPrefix() []byte {
	switch enc {
	case "", utf8Encoding:
		return []byte{0xef, 0xbb, 0xbf}
	case utf16LEEncoding:
		return []byte{0xff, 0xfe}
	case utf16BEEncoding:
		return []byte{0xfe, 0xff}
	}
	return nil
}

func (enc textEncoding) utf16Order() binary.ByteOrder {
	switch enc {
	case utf16LEEncoding:
		return binary.LittleEndian
	case utf16BEEncoding:
		return binary.BigEndian
	}
	return nil
}

func inspectTextEncoding(file io.ReaderAt, size int64, requested textEncoding) (textEncoding, bool, error) {
	sample := make([]byte, min(size, textEncodingSample))
	if len(sample) > 0 {
		if _, err := file.ReadAt(sample, 0); err != nil {
			return "", false, err
		}
	}
	return detectTextEncoding(sample, int64(len(sample)) == size, requested)
}

func detectTextEncoding(sample []byte, complete bool, requested textEncoding) (textEncoding, bool, error) {
	if len(sample) >= 64 && bytes.HasPrefix(sample, []byte("MZ")) {
		offset := int64(binary.LittleEndian.Uint32(sample[0x3c:]))
		if offset >= 64 && offset+4 <= int64(len(sample)) && bytes.Equal(sample[offset:offset+4], []byte("PE\x00\x00")) {
			return "", false, errTextPE
		}
	}
	for _, enc := range []textEncoding{utf8Encoding, utf16LEEncoding, utf16BEEncoding} {
		if bytes.HasPrefix(sample, enc.bomPrefix()) {
			if requested != "" && requested != enc {
				return "", false, fmt.Errorf("文件头指定了 %s", enc.label(true))
			}
			return enc, true, nil
		}
	}
	if requested.utf16Order() != nil {
		return requested, false, nil
	}
	if count := bytes.Count(sample, []byte{0}); count > 0 {
		if requested == "" && count >= 4 {
			even := 0
			for i := 0; i < len(sample); i += 2 {
				if sample[i] == 0 {
					even++
				}
			}
			odd := count - even
			if odd >= even*8+4 {
				return utf16LEEncoding, false, nil
			}
			if even >= odd*8+4 {
				return utf16BEEncoding, false, nil
			}
		}
		return "", false, errTextHasNUL
	}
	if requested != "" {
		return requested, false, nil
	}
	if sampleMatchesEncoding(sample, complete, utf8Encoding) {
		return utf8Encoding, false, nil
	}
	for _, enc := range []textEncoding{gbkEncoding, gb18030Encoding, windows1252Encoding} {
		if sampleMatchesEncoding(sample, complete, enc) {
			return enc, false, nil
		}
	}
	return latin1Encoding, false, nil
}

func sampleMatchesEncoding(sample []byte, complete bool, enc textEncoding) bool {
	tail := 0
	if !complete {
		tail = min(3, len(sample))
	}
	for trim := 0; trim <= tail; trim++ {
		if _, err := decodeTextEncoding(sample[:len(sample)-trim], enc); err == nil {
			return true
		}
		// Only try trimming a truncated character at a sampled file boundary.
	}
	return false
}

func decodeTextEncoding(data []byte, enc textEncoding) ([]byte, error) {
	if codec := enc.charset(); codec != nil {
		decoded, err := codec.NewDecoder().Bytes(data)
		if err != nil {
			return nil, errTextEncoding
		}
		if bytes.Contains(decoded, []byte("\xef\xbf\xbd")) {
			// The decoders may replace invalid input. A literal replacement
			// character is valid if it round-trips to the original bytes.
			encoded, err := codec.NewEncoder().Bytes(decoded)
			if err != nil || !bytes.Equal(encoded, data) {
				return nil, errTextEncoding
			}
		}
		data = decoded
	} else if !utf8.Valid(data) {
		return nil, errTextEncoding
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return nil, errTextHasNUL
	}
	return data, nil
}

func encodeText(value string, enc textEncoding) ([]byte, error) {
	if codec := enc.charset(); codec != nil {
		encoded, err := codec.NewEncoder().Bytes([]byte(value))
		if err != nil {
			return nil, fmt.Errorf("内容包含 %s 无法保存的字符", enc)
		}
		return encoded, nil
	}
	return []byte(value), nil
}
