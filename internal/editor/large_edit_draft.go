package editor

import (
	"archive/zip"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	largeDraftInlineLimit = 512 << 10
	largeDraftDirectory   = "large-drafts"
	largeDraftMagic       = "EditorX text draft\x01"
)

type largeEditSpan struct {
	start, end             int64
	insertStart, insertEnd int64
}

// Shared ranges have the same logical order in both revisions. Everything
// between those anchors is a single patch, including gaps made by undo.
func largeEditSpans(saved, current *largeEditRevision) []largeEditSpan {
	if saved == current {
		return nil
	}
	anchors := append([]largeEditPiece(nil), saved.pieces...)
	sort.Slice(anchors, func(i, j int) bool { return anchors[i].start < anchors[j].start })
	var spans []largeEditSpan
	var oldEnd, newEnd int64
	for _, part := range current.pieces {
		i := sort.Search(len(anchors), func(i int) bool { return anchors[i].start+anchors[i].size > part.start })
		for ; i < len(anchors) && anchors[i].start < part.start+part.size; i++ {
			anchor := anchors[i]
			start, end := max(part.start, anchor.start), min(part.start+part.size, anchor.start+anchor.size)
			oldAt, newAt := anchor.end-anchor.size+start-anchor.start, part.end-part.size+start-part.start
			if oldAt > oldEnd || newAt > newEnd {
				spans = append(spans, largeEditSpan{oldEnd, oldAt, newEnd, newAt})
			}
			oldEnd, newEnd = oldAt+end-start, newAt+end-start
		}
	}
	if oldEnd < saved.size || newEnd < current.size {
		spans = append(spans, largeEditSpan{oldEnd, saved.size, newEnd, current.size})
	}
	return spans
}

func inlineLargeEditChanges(current *largeEditRevision, spans []largeEditSpan) ([]largeEditChange, error) {
	if len(spans) == 0 {
		return nil, nil
	}
	changes := make([]largeEditChange, 0, len(spans))
	for _, span := range spans {
		value, err := current.read(span.insertStart, span.insertEnd)
		if err != nil {
			return nil, err
		}
		changes = append(changes, largeEditChange{Start: span.start, End: span.end, Text: value})
	}
	return changes, nil
}

func validLargeDraftID(id string) bool {
	if len(id) < 16 || len(id) > 64 {
		return false
	}
	return strings.IndexFunc(id, func(c rune) bool {
		return (c < 'A' || c > 'Z') && (c < '2' || c > '7')
	}) < 0
}

func largeDraftPath(directory, id string) string {
	if directory == "" || !validLargeDraftID(id) {
		return ""
	}
	return filepath.Join(directory, largeDraftDirectory, id+".patch.gz")
}

func persistLargeEditDraft(directory string, current *largeEditRevision, spans []largeEditSpan) (string, string, error) {
	if directory == "" {
		return "", "", errors.New("找不到 EditorX 会话目录")
	}
	id := rand.Text()
	path := largeDraftPath(directory, id)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", "", err
	}
	err := writeFileAtomically(path, func(output io.Writer) error {
		writer, err := gzip.NewWriterLevel(output, gzip.BestSpeed)
		if err != nil {
			return err
		}
		if _, err = writer.Write([]byte(largeDraftMagic)); err != nil {
			return err
		}
		buffer := make([]byte, largeTextPageBytes)
		var header [24]byte
		for _, span := range spans {
			binary.LittleEndian.PutUint64(header[:8], uint64(span.start))
			binary.LittleEndian.PutUint64(header[8:16], uint64(span.end))
			length := span.insertEnd - span.insertStart
			binary.LittleEndian.PutUint64(header[16:], uint64(length))
			if _, err = writer.Write(header[:]); err != nil {
				return err
			}
			if _, err = io.CopyBuffer(writer, io.NewSectionReader(current, span.insertStart, length), buffer); err != nil {
				return err
			}
		}
		return writer.Close()
	})
	if err != nil {
		return "", "", err
	}
	return id, path, nil
}

func applyLargeEditDraft(ctx context.Context, r *largeEditRevision, path string) (*largeEditRevision, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	stream, err := gzip.NewReader(file)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	reader := bufio.NewReaderSize(largeEditContextReader{ctx, stream}, largeTextPageBytes)
	magic := make([]byte, len(largeDraftMagic))
	if _, err := io.ReadFull(reader, magic); err != nil || string(magic) != largeDraftMagic {
		return nil, errors.New("无法读取大文件草稿")
	}
	next := &largeEditRevision{store: r.store}
	var offset int64
	var header [24]byte
	buffer := make([]byte, largeTextPageBytes+utf8.UTFMax)
	for {
		if _, err := io.ReadFull(reader, header[:]); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		start := int64(binary.LittleEndian.Uint64(header[:8]))
		end := int64(binary.LittleEndian.Uint64(header[8:16]))
		length := int64(binary.LittleEndian.Uint64(header[16:]))
		if start < offset || end < start || length < 0 || !r.boundary(start) || !r.boundary(end) {
			return nil, errors.New("大文件草稿位置无效")
		}
		if err := r.copyPieces(next, offset, start); err != nil {
			return nil, err
		}
		carry := 0
		for length > 0 {
			n := int(min(length, largeTextPageBytes))
			if _, err := io.ReadFull(reader, buffer[carry:carry+n]); err != nil {
				return nil, err
			}
			length -= int64(n)
			count := carry + n
			end := count
			if length > 0 {
				for first := count - 1; first >= max(0, count-utf8.UTFMax); first-- {
					if utf8.RuneStart(buffer[first]) {
						if !utf8.FullRune(buffer[first:count]) {
							end = first
						}
						break
					}
				}
			}
			data, err := decodeTextEncoding(buffer[:end], utf8Encoding)
			if err != nil {
				return nil, err
			}
			piece, err := r.store.append(data)
			if err != nil {
				return nil, err
			}
			next.addPiece(piece)
			carry = copy(buffer, buffer[end:count])
		}
		offset = end
	}
	if err := r.copyPieces(next, offset, r.size); err != nil {
		return nil, err
	}
	return next, nil
}

func (s savedSession) largeDrafts() []*largeEditSession {
	var patches []*largeEditSession
	seen := make(map[*largeEditSession]bool)
	for _, window := range s.Windows {
		for _, tab := range window.Tabs {
			draft := tab.LargeEdit
			if draft != nil && (draft.Patch != "" || draft.patchError != "") && !seen[draft] {
				seen[draft] = true
				patches = append(patches, draft)
			}
		}
	}
	return patches
}

func bindLargeEditDraftPaths(saved savedSession, directory string) {
	for _, draft := range saved.largeDrafts() {
		draft.patchPath = largeDraftPath(directory, draft.Patch)
	}
}

func includeLargeEditDrafts(ctx context.Context, archive *zip.Writer, saved savedSession) error {
	included := make(map[string]bool)
	for _, draft := range saved.largeDrafts() {
		if draft.patchError != "" {
			return errors.New(draft.patchError)
		}
		if included[draft.Patch] {
			continue
		}
		file, err := os.Open(draft.patchPath)
		if err != nil {
			return err
		}
		name := largeDraftDirectory + "/" + draft.Patch + ".patch.gz"
		entry, err := archive.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if err == nil {
			_, err = io.Copy(entry, largeEditContextReader{ctx, file})
		}
		file.Close()
		if err != nil {
			return err
		}
		included[draft.Patch] = true
	}
	return nil
}

func importLargeEditDrafts(archive *zip.ReadCloser, saved savedSession, directory string) error {
	sources := make(map[string]string)
	for _, draft := range saved.largeDrafts() {
		if !validLargeDraftID(draft.Patch) || directory == "" {
			return errors.New("备份中的大文件草稿无效")
		}
		if id := sources[draft.Patch]; id != "" {
			draft.Patch, draft.patchPath = id, largeDraftPath(directory, id)
			continue
		}
		entry, err := archive.Open(largeDraftDirectory + "/" + draft.Patch + ".patch.gz")
		if err != nil {
			return fmt.Errorf("备份中缺少大文件草稿：%w", err)
		}
		id := rand.Text()
		path := largeDraftPath(directory, id)
		err = os.MkdirAll(filepath.Dir(path), 0700)
		if err == nil {
			err = writeFileAtomically(path, func(output io.Writer) error { _, err := io.Copy(output, entry); return err })
		}
		entry.Close()
		if err != nil {
			return err
		}
		sources[draft.Patch] = id
		draft.Patch, draft.patchPath = id, path
	}
	return nil
}

func pruneLargeEditDrafts(directory string, current, previous savedSession) {
	folder := filepath.Join(directory, largeDraftDirectory)
	entries, err := os.ReadDir(folder)
	if err != nil {
		return
	}
	keep := make(map[string]bool)
	for _, saved := range []savedSession{current, previous} {
		for _, draft := range saved.largeDrafts() {
			keep[draft.Patch] = true
		}
	}
	for _, entry := range entries {
		id, ok := strings.CutSuffix(entry.Name(), ".patch.gz")
		if !ok || !validLargeDraftID(id) || keep[id] || entry.IsDir() {
			continue
		}
		if info, err := entry.Info(); err == nil && info.Mode().IsRegular() && time.Since(info.ModTime()) > 5*time.Minute {
			_ = os.Remove(filepath.Join(folder, entry.Name()))
		}
	}
}
