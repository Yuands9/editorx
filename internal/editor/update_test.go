package editor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

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

	release, err := fetchEditorRelease(context.Background(), server.Client(), server.URL, "0.32.9")
	if err != nil || release.comparison != 1 || release.checksum != checksum {
		t.Fatalf("release = %+v, %v", release, err)
	}
	release.download = server.URL + "/file"
	directory := t.TempDir()
	current := filepath.Join(directory, updateExecutable)
	staged := filepath.Join(directory, "verified.exe")
	for _, file := range []string{current, staged} {
		if err := os.WriteFile(file, []byte("previous"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := downloadEditorUpdate(context.Background(), server.Client(), release, staged); err != nil {
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
	release.checksum[0] ^= 1
	if err := downloadEditorUpdate(context.Background(), server.Client(), release, invalid); err == nil {
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
}
