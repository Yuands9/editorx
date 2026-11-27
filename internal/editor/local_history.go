package editor

import (
	"cmp"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	localHistoryDirectory     = "local-history"
	localHistorySource        = "source.txt"
	localHistoryTime          = "20060102T150405.000000000Z"
	localHistoryPathLimit     = 128 << 10
	localHistoryFileSizeLimit = 32 << 20
	defaultHistoryDays        = 30
	defaultHistoryCount       = 20
	maxHistoryDays            = 36500
	maxHistoryCount           = 100000
)

type localHistoryPreferences struct {
	Disabled bool `json:"disabled,omitempty"`
	Days     int  `json:"days,omitempty"`
	Versions int  `json:"versions,omitempty"`
}

func (p localHistoryPreferences) limits() (days, versions int) {
	days, versions = p.Days, p.Versions
	if days == 0 || days < -1 || days > maxHistoryDays {
		days = defaultHistoryDays
	}
	if versions == 0 || versions < -1 || versions > maxHistoryCount {
		versions = defaultHistoryCount
	}
	return days, versions
}

type localHistoryVersion struct {
	fileID, name string
	at           time.Time
	bytes        int64
	digest       [sha256.Size]byte
}

func (v localHistoryVersion) key() string { return filepath.Join(v.fileID, v.name) }

type localHistoryFile struct {
	id, source string
	versions   []localHistoryVersion
	bytes      int64
}

type localHistoryCatalog struct {
	files    []localHistoryFile
	bytes    int64
	count    int
	revision uint64
	problem  string
}

type localHistoryStore struct {
	directory string
	mu        sync.Mutex
	policy    localHistoryPreferences
	epoch     uint64
	problem   string
	revision  atomic.Uint64
}

func (s *localHistoryStore) setPolicy(policy localHistoryPreferences) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.policy.Disabled != policy.Disabled {
		s.epoch++
	}
	s.policy = policy
}

func (s *localHistoryStore) noteProblem(err error) {
	problem := ""
	if err != nil {
		problem = uiText("本地历史记录失败：") + err.Error()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.problem != problem {
		s.problem = problem
		s.revision.Add(1)
	}
}

func localHistoryFileID(path string) string {
	digest := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(path))))
	return hex.EncodeToString(digest[:])
}

func historyDigest(value string) ([sha256.Size]byte, bool) {
	var digest [sha256.Size]byte
	if len(value) != hex.EncodedLen(len(digest)) {
		return digest, false
	}
	_, err := hex.Decode(digest[:], []byte(value))
	return digest, err == nil
}

func parseHistoryVersion(fileID, name string) (localHistoryVersion, bool) {
	key, ok := strings.CutSuffix(name, ".gz")
	if !ok {
		return localHistoryVersion{}, false
	}
	when, hash, ok := strings.Cut(key, "-")
	if !ok {
		return localHistoryVersion{}, false
	}
	at, err := time.Parse(localHistoryTime, when)
	digest, valid := historyDigest(hash)
	return localHistoryVersion{fileID: fileID, name: name, at: at, digest: digest}, err == nil && valid
}

func (s *localHistoryStore) openRoot() (*os.Root, error) {
	info, err := os.Lstat(s.directory)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New(uiText("本地历史目录不是普通文件夹"))
	}
	return os.OpenRoot(s.directory)
}

type localHistoryDraft struct {
	temporary string
	source    string
	version   localHistoryVersion
	epoch     uint64
}

// stage returns the source's file info even when its size rules out a snapshot,
// so the caller can still check the destination before overwriting it.
func (s *localHistoryStore) stage(ctx context.Context, source string, now time.Time) (*localHistoryDraft, os.FileInfo, error) {
	s.mu.Lock()
	epoch, enabled := s.epoch, !s.policy.Disabled
	s.mu.Unlock()
	if !enabled || gitPathWithin(s.directory, source) {
		return nil, nil, nil
	}
	info, err := os.Lstat(source)
	if errors.Is(err, os.ErrNotExist) || err == nil && !info.Mode().IsRegular() {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if info.Size() > localHistoryFileSizeLimit {
		return nil, info, nil
	}
	input, err := os.Open(source)
	if err != nil {
		return nil, nil, err
	}
	defer input.Close()
	opened, err := input.Stat()
	if err != nil {
		return nil, nil, err
	}
	if !sameTextFile(info, opened) {
		return nil, nil, errDocumentChanged
	}
	if err := os.MkdirAll(s.directory, 0700); err != nil {
		return nil, nil, err
	}
	root, err := s.openRoot()
	if err != nil {
		return nil, nil, err
	}
	defer root.Close()
	temporary, err := os.CreateTemp(root.Name(), ".version-*.tmp")
	if err != nil {
		return nil, nil, err
	}
	draft := &localHistoryDraft{temporary: temporary.Name(), source: source, epoch: epoch}
	draft.version.fileID, draft.version.at = localHistoryFileID(source), now.UTC()
	digest := sha256.New()
	writer, err := gzip.NewWriterLevel(temporary, gzip.BestSpeed)
	if err == nil {
		reader := io.LimitReader(largeEditContextReader{ctx: ctx, Reader: input}, info.Size()+1)
		copied, copyErr := io.Copy(io.MultiWriter(writer, digest), reader)
		if copyErr == nil && copied != info.Size() {
			copyErr = errDocumentChanged
		}
		err = errors.Join(copyErr, writer.Close())
	}
	if err == nil {
		err = temporary.Sync()
	}
	err = errors.Join(err, temporary.Close())
	if err != nil {
		os.Remove(draft.temporary)
		return nil, nil, err
	}
	copy(draft.version.digest[:], digest.Sum(nil))
	draft.version.name = draft.version.at.Format(localHistoryTime) + "-" + hex.EncodeToString(draft.version.digest[:]) + ".gz"
	return draft, info, nil
}

func (s *localHistoryStore) commit(draft *localHistoryDraft) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.policy.Disabled || draft.epoch != s.epoch {
		return nil
	}
	root, err := s.openRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	id := draft.version.fileID
	if err := root.Mkdir(id, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	metadata := filepath.Join(id, localHistorySource)
	if _, err := historyFilePath(root, id); err != nil {
		if err := root.WriteFile(metadata, []byte(draft.source), 0600); err != nil {
			return err
		}
	}
	if err := root.Rename(filepath.Base(draft.temporary), draft.version.key()); err != nil {
		return err
	}
	s.revision.Add(1)
	versions, err := historyVersions(root, id)
	if err != nil {
		return err
	}
	_, err = s.removeVersions(root, s.expiredVersions(versions, draft.version.at))
	return err
}

func historyVersions(root *os.Root, id string) ([]localHistoryVersion, error) {
	entries, err := fs.ReadDir(root.FS(), id)
	if err != nil {
		return nil, err
	}
	var versions []localHistoryVersion
	for _, entry := range entries {
		version, ok := parseHistoryVersion(id, entry.Name())
		if !ok || !entry.Type().IsRegular() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		version.bytes = info.Size()
		versions = append(versions, version)
	}
	slices.Reverse(versions) // filenames sort by UTC time
	return versions, nil
}

func (s *localHistoryStore) versionsFor(id string) ([]localHistoryVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	root, err := s.openRoot()
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	versions, err := historyVersions(root, id)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return versions, err
}

func historyFilePath(root *os.Root, id string) (string, error) {
	file, err := root.Open(filepath.Join(id, localHistorySource))
	if err != nil {
		return "", err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, localHistoryPathLimit+1))
	if err != nil {
		return "", err
	}
	path := string(data)
	if len(data) > localHistoryPathLimit || !filepath.IsAbs(path) || localHistoryFileID(path) != id {
		return "", errors.New(uiText("本地历史的文件路径无效"))
	}
	return path, nil
}

func (s *localHistoryStore) catalog() (localHistoryCatalog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	catalog := localHistoryCatalog{revision: s.revision.Load(), problem: s.problem}
	root, err := s.openRoot()
	if errors.Is(err, os.ErrNotExist) {
		return catalog, nil
	}
	if err != nil {
		return catalog, err
	}
	defer root.Close()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return catalog, err
	}
	for _, entry := range entries {
		id := entry.Name()
		if _, ok := historyDigest(id); !ok || !entry.IsDir() {
			continue
		}
		versions, err := historyVersions(root, id)
		if err != nil {
			catalog.problem = uiText("部分历史记录无法读取：") + err.Error()
			continue
		}
		if len(versions) == 0 {
			continue
		}
		path, err := historyFilePath(root, id)
		if err != nil {
			catalog.problem = uiText("部分历史记录的文件路径无法读取：") + err.Error()
		}
		file := localHistoryFile{id: id, source: path, versions: versions}
		for _, version := range versions {
			file.bytes += version.bytes
		}
		catalog.files = append(catalog.files, file)
		catalog.bytes += file.bytes
		catalog.count += len(versions)
	}
	slices.SortFunc(catalog.files, func(a, b localHistoryFile) int {
		if order := cmp.Compare(b.bytes, a.bytes); order != 0 {
			return order
		}
		return cmp.Compare(a.source, b.source)
	})
	return catalog, nil
}

func (s *localHistoryStore) expiredVersions(versions []localHistoryVersion, now time.Time) []localHistoryVersion {
	days, count := s.policy.limits()
	cutoff := now.Add(-time.Duration(days) * 24 * time.Hour)
	var expired []localHistoryVersion
	for index, version := range versions {
		if (days > 0 && version.at.Before(cutoff)) || (count > 0 && index >= count) {
			expired = append(expired, version)
		}
	}
	return expired
}

func (s *localHistoryStore) clean(now time.Time, all bool) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if all {
		s.epoch++ // Versions being staged when the user clears all are discarded.
	}
	root, err := s.openRoot()
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer root.Close()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return 0, err
	}
	removed := 0
	var problem error
	for _, entry := range entries {
		id := entry.Name()
		if _, valid := historyDigest(id); !valid || !entry.IsDir() {
			if entry.Type().IsRegular() && strings.HasPrefix(id, ".version-") && strings.HasSuffix(id, ".tmp") {
				if info, err := entry.Info(); err == nil && now.Sub(info.ModTime()) > 24*time.Hour {
					_ = root.Remove(id)
				}
			}
			continue
		}
		versions, err := historyVersions(root, id)
		if err != nil {
			problem = errors.Join(problem, err)
			continue
		}
		if !all {
			versions = s.expiredVersions(versions, now)
		}
		count, err := s.removeVersions(root, versions)
		removed += count
		problem = errors.Join(problem, err)
	}
	return removed, problem
}

func (s *localHistoryStore) remove(versions []localHistoryVersion) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	root, err := s.openRoot()
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer root.Close()
	return s.removeVersions(root, versions)
}

func (s *localHistoryStore) removeVersions(root *os.Root, versions []localHistoryVersion) (int, error) {
	removed := 0
	groups := map[string]bool{}
	var problem error
	for _, version := range versions {
		if err := root.Remove(version.key()); err == nil {
			groups[version.fileID] = true
			removed++
		} else if !errors.Is(err, os.ErrNotExist) {
			problem = errors.Join(problem, err)
		}
	}
	for id := range groups {
		if entries, err := fs.ReadDir(root.FS(), id); err == nil &&
			len(entries) == 1 && entries[0].Name() == localHistorySource && entries[0].Type().IsRegular() {
			if err := root.Remove(filepath.Join(id, localHistorySource)); err == nil {
				_ = root.Remove(id)
			}
		}
	}
	if removed != 0 {
		s.revision.Add(1)
	}
	return removed, problem
}

func (s *localHistoryStore) copyVersion(ctx context.Context, version localHistoryVersion, output io.Writer) error {
	root, err := s.openRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	file, err := root.Open(version.key())
	if err != nil {
		return err
	}
	defer file.Close()
	input, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer input.Close()
	digest := sha256.New()
	if _, err := io.Copy(io.MultiWriter(output, digest), largeEditContextReader{ctx: ctx, Reader: input}); err != nil {
		return err
	}
	var actual [sha256.Size]byte
	copy(actual[:], digest.Sum(nil))
	if actual != version.digest {
		return fmt.Errorf(uiText("历史版本校验失败：%s"), version.at.Local().Format(time.DateTime))
	}
	return nil
}
