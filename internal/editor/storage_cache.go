package editor

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	editorPreviewCacheDirectory = "preview"
	editorPreviewCachePattern   = "page-*.html"
	editorPreviewWritePattern   = ".viva-preview-*"
	editorCLICacheDirectory     = "cli"
	editorCLICachePattern       = "reply-*"
)

// Editors and CLI workers register a temporary path before cleanup can see it.
var activeEditorCache = struct {
	sync.Mutex
	paths map[string]struct{}
}{paths: make(map[string]struct{})}

func editorCacheKey(path string) string {
	return strings.ToLower(filepath.Clean(path))
}

func createEditorPreviewCache(directory, pattern string) (*os.File, error) {
	activeEditorCache.Lock()
	defer activeEditorCache.Unlock()
	file, err := os.CreateTemp(directory, pattern)
	if err == nil {
		activeEditorCache.paths[editorCacheKey(file.Name())] = struct{}{}
	}
	return file, err
}

func removeEditorPreviewCache(path string) {
	_ = os.Remove(path)
	releaseEditorCache(path)
}

func releaseEditorCache(path string) {
	activeEditorCache.Lock()
	delete(activeEditorCache.paths, editorCacheKey(path))
	activeEditorCache.Unlock()
}

func editorCacheOwner(path string) string {
	directory, name, _ := strings.Cut(path, "/")
	switch directory {
	case editorPreviewCacheDirectory:
		if matchesEditorCachePattern(name, editorPreviewCachePattern) || matchesEditorCachePattern(name, editorPreviewWritePattern) {
			return path
		}
	case editorCLICacheDirectory:
		job, _, nested := strings.Cut(name, "/")
		if nested && matchesEditorCachePattern(job, editorCLICachePattern) {
			return directory + "/" + job
		}
	}
	return ""
}

func matchesEditorCachePattern(name, pattern string) bool {
	prefix, suffix, _ := strings.Cut(pattern, "*")
	rest, prefixOK := strings.CutPrefix(name, prefix)
	number, suffixOK := strings.CutSuffix(rest, suffix)
	return prefixOK && suffixOK && number != "" && strings.IndexFunc(number, func(r rune) bool { return r < '0' || r > '9' }) < 0
}
