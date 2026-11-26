package editor

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/text/transform"
)

const largeEditIndexStride = 16 << 10

// The scratch file is append-only. A revision owns only piece offsets and line
// counts, so a search or undo can keep a revision without copying the document.
type largeEditStorage struct {
	file   *os.File
	mu     sync.RWMutex
	size   int64
	breaks int
	index  []int
}

type largeEditPiece struct {
	start, size, end int64
	breaks, lineEnd  int
}

type largeEditRevision struct {
	store  *largeEditStorage
	pieces []largeEditPiece
	size   int64
	breaks int
}

type largeEditLoad struct {
	doc      document
	revision *largeEditRevision
	start    int64
}

func newLargeEditStorage() (*largeEditStorage, error) {
	flags := os.O_CREATE | os.O_EXCL | os.O_RDWR
	if runtime.GOOS == "windows" {
		const fileFlagDeleteOnClose = 0x04000000
		flags |= fileFlagDeleteOnClose
	}
	file, err := os.OpenFile(filepath.Join(os.TempDir(), "editorx-text-"+rand.Text()), flags, 0600)
	if err != nil {
		return nil, err
	}
	if runtime.GOOS != "windows" {
		if err := os.Remove(file.Name()); err != nil {
			file.Close()
			return nil, err
		}
	}
	return &largeEditStorage{file: file, index: []int{0}}, nil
}

func (s *largeEditStorage) close() { _ = s.file.Close() }

func (s *largeEditStorage) append(data []byte) (largeEditPiece, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := largeEditPiece{start: s.size, size: int64(len(data))}
	if _, err := s.file.WriteAt(data, s.size); err != nil {
		return largeEditPiece{}, err
	}
	for len(data) > 0 {
		n := min(len(data), largeEditIndexStride-int(s.size%largeEditIndexStride))
		breaks := bytes.Count(data[:n], []byte{'\n'})
		s.breaks += breaks
		p.breaks += breaks
		s.size += int64(n)
		if s.size%largeEditIndexStride == 0 {
			s.index = append(s.index, s.breaks)
		}
		data = data[n:]
	}
	return p, nil
}

func (s *largeEditStorage) countBefore(offset int64) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if offset == s.size {
		return s.breaks, nil
	}
	block := offset / largeEditIndexStride
	count := s.index[block]
	length := offset % largeEditIndexStride
	if length == 0 {
		return count, nil
	}
	var data [largeEditIndexStride]byte
	if _, err := s.file.ReadAt(data[:length], block*largeEditIndexStride); err != nil {
		return 0, err
	}
	return count + bytes.Count(data[:length], []byte{'\n'}), nil
}

// afterBreak returns the byte immediately after the nth newline in storage.
func (s *largeEditStorage) afterBreak(n int) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	block := sort.Search(len(s.index), func(i int) bool { return s.index[i] >= n }) - 1
	start := int64(block) * largeEditIndexStride
	var data [largeEditIndexStride]byte
	buffer := data[:min(s.size-start, largeEditIndexStride)]
	if _, err := s.file.ReadAt(buffer, start); err != nil {
		return 0, err
	}
	pos := 0
	for i := s.index[block]; i < n; i++ {
		pos += bytes.IndexByte(buffer[pos:], '\n') + 1
	}
	return start + int64(pos), nil
}

func loadLargeEdit(ctx context.Context, store *largeEditStorage, path string, requested textEncoding, initialStart int64) (largeEditLoad, error) {
	file, err := os.Open(path)
	if err != nil {
		return largeEditLoad{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return largeEditLoad{}, err
	}
	if !info.Mode().IsRegular() {
		return largeEditLoad{}, errors.New("请选择普通文本文件")
	}
	size := info.Size()
	enc, bom, err := inspectTextEncoding(file, size, requested)
	if err != nil {
		return largeEditLoad{}, err
	}

	doc := document{path: path, encoding: enc, bom: bom, eol: "LF"}
	hash := sha256.New()
	raw := make([]byte, largeTextPageBytes+4096)
	var start int64
	var crlf int
	var trailingCR bool
	initialPending := initialStart > 0
	for offset := int64(0); offset < size; {
		if err := ctx.Err(); err != nil {
			return largeEditLoad{}, err
		}
		limit := offset + largeTextPageBytes
		reached := initialPending && initialStart <= limit
		if reached {
			limit = initialStart
		}
		end, err := largeEditReadBoundary(file, offset, limit, size, enc, raw)
		if err != nil {
			return largeEditLoad{}, err
		}
		if reached && end <= offset {
			initialPending = false
			start = store.size
			continue
		}
		if end <= offset || end-offset > int64(len(raw)) {
			return largeEditLoad{}, errTextEncoding
		}
		chunk := raw[:end-offset]
		if _, err := file.ReadAt(chunk, offset); err != nil {
			return largeEditLoad{}, err
		}
		_, _ = hash.Write(chunk)
		if offset == 0 && bom {
			chunk = chunk[len(enc.bomPrefix()):]
		}
		decoded, err := decodeTextEncoding(chunk, enc)
		if err != nil {
			return largeEditLoad{}, err
		}
		if len(decoded) > 0 {
			crlf += bytes.Count(decoded, []byte("\r\n"))
			if trailingCR && decoded[0] == '\n' {
				crlf++
			}
			trailingCR = decoded[len(decoded)-1] == '\r'
		}
		if _, err := store.append(decoded); err != nil {
			return largeEditLoad{}, err
		}
		if reached {
			initialPending = false
			start = store.size
		}
		offset = end
	}
	current, err := os.Stat(path)
	if err != nil {
		return largeEditLoad{}, err
	}
	if !sameTextFile(info, current) {
		return largeEditLoad{}, errors.New("文件在打开期间发生变化，请重试")
	}
	if crlf > 0 {
		doc.eol = "混合"
		if crlf == store.breaks {
			doc.eol = "CRLF"
		}
	}
	copy(doc.diskHash[:], hash.Sum(nil))
	revision := &largeEditRevision{store: store}
	revision.addPiece(largeEditPiece{size: store.size, breaks: store.breaks})
	return largeEditLoad{doc: doc, revision: revision, start: start}, nil
}

func largeEditReadBoundary(file io.ReaderAt, start, offset, size int64, enc textEncoding, scratch []byte) (int64, error) {
	if offset >= size {
		return size, nil
	}
	if enc == utf8Encoding {
		var edge [4]byte
		start := max(0, offset-3)
		buf := edge[:offset-start+1]
		if _, err := file.ReadAt(buf, start); err != nil {
			return 0, err
		}
		// The next chunk must begin on a rune boundary.
		i := len(buf) - 1
		for i > 0 && !utf8.RuneStart(buf[i]) {
			i--
		}
		return start + int64(i), nil
	}
	if order := enc.utf16Order(); order != nil {
		offset -= offset % 2
		if offset > 0 && offset+2 <= size {
			var edge [2]byte
			if _, err := file.ReadAt(edge[:], offset); err != nil {
				return 0, err
			}
			if unit := order.Uint16(edge[:]); unit >= 0xdc00 && unit <= 0xdfff {
				offset -= 2
			}
		}
		return offset, nil
	}
	if enc == windows1252Encoding || enc == latin1Encoding {
		return offset, nil
	}
	// Start at the previous verified boundary. A legacy multibyte trail byte
	// can also be a lead byte, so inspecting only the end is ambiguous.
	data := scratch[:min(size, offset+3)-start]
	if _, err := file.ReadAt(data, start); err != nil {
		return 0, err
	}
	pos, limit := 0, int(offset-start)
	for pos < limit {
		width := 1
		lead := data[pos]
		if enc == shiftJISEncoding {
			if lead >= 0x81 && lead <= 0x9f || lead >= 0xe0 && lead <= 0xfc {
				width = 2
			}
		} else if lead >= 0x81 && lead <= 0xfe {
			width = 2
			if enc == gb18030Encoding && pos+1 < len(data) && data[pos+1] >= '0' && data[pos+1] <= '9' {
				width = 4
			}
		}
		if pos+width > limit {
			break
		}
		pos += width
	}
	return start + int64(pos), nil
}

func (r *largeEditRevision) addPiece(p largeEditPiece) {
	if p.size == 0 {
		return
	}
	r.size += p.size
	r.breaks += p.breaks
	p.end, p.lineEnd = r.size, r.breaks
	if n := len(r.pieces); n > 0 {
		last := &r.pieces[n-1]
		if last.start+last.size == p.start {
			last.size += p.size
			last.breaks += p.breaks
			last.end, last.lineEnd = p.end, p.lineEnd
			return
		}
	}
	r.pieces = append(r.pieces, p)
}

func (r *largeEditRevision) pieceAt(offset int64) int {
	return sort.Search(len(r.pieces), func(i int) bool { return r.pieces[i].end > offset })
}

// ReadAt uses UTF-8 byte offsets in this revision, even for files saved in other encodings.
func (r *largeEditRevision) ReadAt(dst []byte, offset int64) (int, error) {
	if offset < 0 {
		return 0, errors.New("文档偏移越界")
	}
	written := 0
	for i := r.pieceAt(offset); i < len(r.pieces) && written < len(dst); i++ {
		p := r.pieces[i]
		skip := offset - (p.end - p.size)
		n := int(min(int64(len(dst)-written), p.size-skip))
		got, err := r.store.file.ReadAt(dst[written:written+n], p.start+skip)
		written += got
		if err != nil {
			return written, err
		}
		offset += int64(n)
	}
	if written != len(dst) {
		return written, io.EOF
	}
	return written, nil
}

func (r *largeEditRevision) read(start, end int64) (string, error) {
	if start < 0 || end < start || end > r.size {
		return "", errors.New("文档偏移越界")
	}
	if start == end {
		return "", nil
	}
	if end-start <= 4096 {
		data := make([]byte, end-start)
		_, err := r.ReadAt(data, start)
		return string(data), err
	}
	var dst strings.Builder
	dst.Grow(int(end - start))
	_, err := io.Copy(&dst, io.NewSectionReader(r, start, end-start))
	return dst.String(), err
}

// lineAt uses zero-based logical lines. CRLF contributes only its LF.
func (r *largeEditRevision) lineAt(offset int64) (int, error) {
	i := r.pieceAt(offset)
	if i == len(r.pieces) {
		return r.breaks, nil
	}
	p := r.pieces[i]
	before, err := r.store.countBefore(p.start)
	if err != nil {
		return 0, err
	}
	inside, err := r.store.countBefore(p.start + offset - (p.end - p.size))
	return p.lineEnd - p.breaks + inside - before, err
}

func (r *largeEditRevision) lineStart(line int) (int64, error) {
	if line <= 0 {
		return 0, nil
	}
	if line > r.breaks {
		return r.size, nil
	}
	i := sort.Search(len(r.pieces), func(i int) bool { return r.pieces[i].lineEnd >= line })
	p := r.pieces[i]
	before, err := r.store.countBefore(p.start)
	if err != nil {
		return 0, err
	}
	after, err := r.store.afterBreak(before + line - (p.lineEnd - p.breaks))
	return p.end - p.size + after - p.start, err
}

func (r *largeEditRevision) copyPieces(next *largeEditRevision, from, to int64) error {
	for i := r.pieceAt(from); from < to; i++ {
		p := r.pieces[i]
		skip := from - (p.end - p.size)
		end := min(to, p.end)
		if skip != 0 || end != p.end {
			p.start += skip
			p.size = end - from
			first, err := r.store.countBefore(p.start)
			if err != nil {
				return err
			}
			last, err := r.store.countBefore(p.start + p.size)
			if err != nil {
				return err
			}
			p.breaks = last - first
		}
		next.addPiece(p)
		from = end
	}
	return nil
}

func (r *largeEditRevision) boundary(offset int64) bool {
	if offset == 0 || offset == r.size {
		return true
	}
	if offset < 0 || offset > r.size {
		return false
	}
	var char [1]byte
	_, err := r.ReadAt(char[:], offset)
	return err == nil && utf8.RuneStart(char[0])
}

type largeEditChange struct {
	Start, End int64
	Text       string
}

func (r *largeEditRevision) apply(ctx context.Context, changes []largeEditChange) (*largeEditRevision, error) {
	next := &largeEditRevision{store: r.store, pieces: make([]largeEditPiece, 0, len(r.pieces)+2*len(changes))}
	var start int64
	for _, change := range changes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if change.Start < start || change.End < change.Start || !r.boundary(change.Start) || !r.boundary(change.End) {
			return nil, errors.New("编辑位置已变化")
		}
		if !utf8.ValidString(change.Text) || strings.ContainsRune(change.Text, 0) {
			return nil, errTextEncoding
		}
		if err := r.copyPieces(next, start, change.Start); err != nil {
			return nil, err
		}
		if change.Text != "" {
			p, err := r.store.append([]byte(change.Text))
			if err != nil {
				return nil, err
			}
			next.addPiece(p)
		}
		start = change.End
	}
	if err := r.copyPieces(next, start, r.size); err != nil {
		return nil, err
	}
	return next, nil
}

type largeEditContextReader struct {
	ctx context.Context
	io.Reader
}

func (r largeEditContextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(data)
}

func saveLargeEdit(ctx context.Context, doc document, path string, r *largeEditRevision, history *localHistoryStore) (document, error) {
	original, err := doc.checkDisk(ctx, path)
	if err != nil {
		return doc, err
	}
	digest, err := writeDocumentAtomically(ctx, path, original, history, func(output io.Writer) error {
		if doc.bom {
			if _, err := output.Write(doc.encoding.bomPrefix()); err != nil {
				return err
			}
		}
		input := largeEditContextReader{ctx, io.NewSectionReader(r, 0, r.size)}
		if codec := doc.encoding.charset(); codec != nil {
			writer := transform.NewWriter(output, codec.NewEncoder())
			if _, err := io.Copy(writer, input); err != nil {
				return err
			}
			if err := writer.Close(); err != nil {
				return err
			}
		} else if _, err := io.Copy(output, input); err != nil {
			return err
		}
		return ctx.Err()
	})
	if err != nil {
		return doc, err
	}
	doc.path, doc.diskHash = path, digest
	return doc, nil
}
