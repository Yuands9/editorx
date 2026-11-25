package editor

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// serveLocalDocument gives each viewer a private loopback route for its assets and document.
func serveLocalDocument(assets fs.FS, route func(http.ResponseWriter, *http.Request, string) bool) (*http.Server, string, error) {
	var secret [24]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return nil, "", err
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, "", err
	}
	host := listener.Addr().String()
	prefix := "/" + hex.EncodeToString(secret[:]) + "/"
	static := http.StripPrefix(prefix, http.FileServer(http.FS(assets)))
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, ok := strings.CutPrefix(r.URL.Path, prefix)
		if r.Host != host || !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if !route(w, r, path) && !serveCompressedOfficeAsset(w, r, assets, path) {
			static.ServeHTTP(w, r)
		}
	})}
	go server.Serve(listener)
	return server, "http://" + host + prefix, nil
}

func serveCompressedOfficeAsset(w http.ResponseWriter, r *http.Request, assets fs.FS, path string) bool {
	if (r.Method != http.MethodGet && r.Method != http.MethodHead) || !fs.ValidPath(path) {
		return false
	}
	file, err := assets.Open(path + ".gz")
	if err != nil {
		return false
	}
	defer file.Close()
	content, ok := file.(io.ReadSeeker)
	if !ok {
		http.Error(w, "办公预览资源读取失败", http.StatusInternalServerError)
		return true
	}
	contentType := mime.TypeByExtension(filepath.Ext(path))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Add("Vary", "Accept-Encoding")
	if r.Header.Get("Range") == "" && acceptsOfficeGzip(strings.Join(r.Header.Values("Accept-Encoding"), ",")) {
		info, err := file.Stat()
		if err != nil {
			http.Error(w, "办公预览资源读取失败", http.StatusInternalServerError)
			return true
		}
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
		if r.Method != http.MethodHead {
			_, _ = io.Copy(w, content)
		}
		return true
	}

	// The last four bytes of a gzip member give the decoded size. All bundled assets fit in uint32.
	var size [4]byte
	if _, err = content.Seek(-int64(len(size)), io.SeekEnd); err == nil {
		_, err = io.ReadFull(content, size[:])
	}
	if err == nil {
		_, err = content.Seek(0, io.SeekStart)
	}
	if err != nil {
		http.Error(w, "办公预览资源读取失败", http.StatusInternalServerError)
		return true
	}
	decoded, err := gzip.NewReader(content)
	if err != nil {
		http.Error(w, "办公预览资源解压失败", http.StatusInternalServerError)
		return true
	}
	defer decoded.Close()
	if r.Header.Get("Range") != "" {
		body, err := io.ReadAll(decoded)
		if err != nil {
			http.Error(w, "办公预览资源解压失败", http.StatusInternalServerError)
			return true
		}
		http.ServeContent(w, r, path, time.Time{}, bytes.NewReader(body))
		return true
	}
	w.Header().Set("Content-Length", strconv.FormatUint(uint64(binary.LittleEndian.Uint32(size[:])), 10))
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, decoded)
	}
	return true
}

func acceptsOfficeGzip(header string) bool {
	wildcard := false
	for option := range strings.SplitSeq(header, ",") {
		encoding, parameters, _ := strings.Cut(option, ";")
		encoding = strings.TrimSpace(encoding)
		if !strings.EqualFold(encoding, "gzip") && encoding != "*" {
			continue
		}
		allowed := true
		for parameter := range strings.SplitSeq(parameters, ";") {
			key, value, _ := strings.Cut(parameter, "=")
			if strings.EqualFold(strings.TrimSpace(key), "q") {
				quality, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
				allowed = err == nil && quality > 0 && quality <= 1
				break
			}
		}
		if strings.EqualFold(encoding, "gzip") {
			return allowed
		}
		wildcard = allowed
	}
	return wildcard
}

func serveEditorPage(w http.ResponseWriter, r *http.Request, assets fs.FS, path string) {
	page, err := fs.ReadFile(assets, path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if r.URL.Query().Get("theme") == "light" {
		page = bytes.Replace(page, []byte("<html "), []byte(`<html class="light" `), 1)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeContent(w, r, path, time.Time{}, bytes.NewReader(page))
}
