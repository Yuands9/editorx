package editor

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"io/fs"
	"net"
	"net/http"
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
		if !route(w, r, path) {
			static.ServeHTTP(w, r)
		}
	})}
	go server.Serve(listener)
	return server, "http://" + host + prefix, nil
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
