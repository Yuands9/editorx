package editor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestUpdateChecksPublicPageWhenAPIIsLimited(t *testing.T) {
	var apiCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api":
			apiCalls.Add(1)
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(http.StatusForbidden)
		case "/releases/latest":
			http.Redirect(w, r, "/releases/tag/v0.41.0", http.StatusFound)
		case "/redirect/latest":
			http.Redirect(w, r, "https://example.com/releases/tag/v0.42.0", http.StatusFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	cache := editorUpdateCache{}
	for range 2 {
		release, err := checkEditorRelease(context.Background(), server.Client(), server.URL+"/api", server.URL+"/releases/latest", "0.40.0", &cache)
		if err != nil || !release.PageOnly || release.comparison != 1 || release.Version != "0.41.0" || release.Download != "" {
			t.Fatalf("unexpected fallback result: %+v, %v", release, err)
		}
		cache.Release = release
	}
	if apiCalls.Load() != 1 || time.Until(cache.APIRetryAfter) < time.Minute {
		t.Fatal("the client retried the API before its rate limit reset")
	}
	if _, err := fetchEditorReleasePage(context.Background(), server.Client(), server.URL+"/redirect/latest", "0.40.0"); err == nil {
		t.Fatal("accepted a release location on another host")
	}
	state := &editorUpdateState{cache: cache}
	dir := t.TempDir()
	if err := state.save(dir); err != nil {
		t.Fatal(err)
	}
	if saved, err := readEditorUpdateCache(dir, "0.40.0"); err != nil || !saved.Release.PageOnly || saved.Release.comparison != 1 {
		t.Fatalf("cached fallback could not be restored: %+v, %v", saved, err)
	}
}

func TestUpdateDownloadsVerifiedExecutableAndPreservesPrevious(t *testing.T) {
	binary, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	checksum := sha256.Sum256(binary)
	record := githubEditorRelease{Tag: "v0.32.10", Assets: []githubReleaseAsset{{
		Name: updateExecutable, Digest: "sha256:" + hex.EncodeToString(checksum[:]), State: "uploaded", Size: int64(len(binary)),
		URL: editorRepositoryURL + "/releases/download/v0.32.10/EditorX.exe",
	}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/file" {
			_, _ = w.Write(binary)
			return
		}
		_ = json.NewEncoder(w).Encode(record)
	}))
	defer server.Close()

	release, err := fetchEditorRelease(context.Background(), server.Client(), server.URL, "0.32.9", &editorUpdateCache{})
	if err != nil || release.comparison != 1 || release.Checksum != checksum {
		t.Fatalf("release = %+v, %v", release, err)
	}
	release.Download = server.URL + "/file"
	directory := t.TempDir()
	current := filepath.Join(directory, updateExecutable)
	staged := filepath.Join(directory, "verified.exe")
	for _, file := range []string{current, staged} {
		if err := os.WriteFile(file, []byte("previous"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := downloadEditorUpdate(context.Background(), server.Client(), release, staged, io.Discard); err != nil {
		t.Fatal(err)
	}
	update, err := replaceEditorExecutable(current, staged)
	if err != nil {
		t.Fatal(err)
	}
	if prior, err := os.ReadFile(update.backup); err != nil || string(prior) != "previous" {
		t.Fatalf("update did not preserve the previous copy: %q, %v", prior, err)
	}

	invalid := filepath.Join(directory, "invalid.exe")
	if err := os.WriteFile(invalid, nil, 0600); err != nil {
		t.Fatal(err)
	}
	release.Checksum[0] ^= 1
	if err := downloadEditorUpdate(context.Background(), server.Client(), release, invalid, io.Discard); err == nil {
		t.Fatal("download accepted a file with a different SHA-256")
	}
	if _, err := os.Stat(invalid); !os.IsNotExist(err) {
		t.Fatalf("failed download left an executable behind: %v", err)
	}
	if _, err := replaceEditorExecutable(current, invalid); err == nil {
		t.Fatal("replacing with a missing download should fail")
	}
	if restored, err := os.ReadFile(current); err != nil || sha256.Sum256(restored) != checksum {
		t.Fatal("failed install did not restore the working binary")
	}
	if prior, err := os.ReadFile(update.backup); err != nil || string(prior) != "previous" {
		t.Fatal("failed install removed the previous backup")
	}
	cleanupPreviousEditorExecutable(current)
	if _, err := os.Stat(update.backup); !os.IsNotExist(err) {
		t.Fatalf("a successful startup kept the previous executable: %v", err)
	}
	if currentData, err := os.ReadFile(current); err != nil || sha256.Sum256(currentData) != checksum {
		t.Fatal("cleanup changed the current executable")
	}
}
