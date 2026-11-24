package editor

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
)

const testDictionaryCSV = "word,phonetic,definition,translation,pos,collins,oxford,tag,bnc,frq,exchange,detail,audio\n" +
	"hello,həˈləʊ,,你好；喂,,3,1,cet4,100,100,,,\n" +
	"world,wɜːld,,世界,,3,1,,100,100,,,\n" +
	"file,,,文件,,3,1,,100,100,s:files/p:filed/d:filed,,\n" +
	"obscureword,,,不应安装的罕用词,,,,,40000,40000,,,\n"

func testDictionarySpec(url, body string) dictionaryDownload {
	digest := sha256.Sum256([]byte(body))
	return dictionaryDownload{url: url, checksum: hex.EncodeToString(digest[:]), size: int64(len(body))}
}

func TestDictionaryCompressedTransfer(t *testing.T) {
	var buffer bytes.Buffer
	compressor := gzip.NewWriter(&buffer)
	if _, err := compressor.Write([]byte(testDictionaryCSV)); err != nil {
		t.Fatal(err)
	}
	if err := compressor.Close(); err != nil {
		t.Fatal(err)
	}
	for _, truncated := range []bool{false, true} {
		body := buffer.Bytes()
		if truncated {
			body = body[:len(body)-8]
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Accept-Encoding") != "gzip" {
				t.Error("download did not request a compressed transfer")
			}
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			_, _ = w.Write(body)
		}))
		progress := &dictionaryProgress{}
		directory := filepath.Join(t.TempDir(), "dictionary")
		info, err := installTranslationDictionary(context.Background(), server.Client(), testDictionarySpec(server.URL, testDictionaryCSV), directory, progress)
		server.Close()
		if truncated {
			if err == nil {
				t.Fatal("truncated gzip was installed")
			}
			entries, _ := os.ReadDir(filepath.Dir(directory))
			if len(entries) != 0 {
				t.Fatal("truncated gzip left installation files", entries)
			}
			continue
		}
		if err != nil || info.Entries != 5 || info.DownloadBytes != int64(len(body)) || progress.total.Load() != int64(len(body)) {
			t.Fatalf("compressed transfer = %+v, total=%d: %v", info, progress.total.Load(), err)
		}
	}
}

func TestDictionaryDownloadIntegrityAndCleanup(t *testing.T) {
	var body = testDictionaryCSV
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
	defer server.Close()
	parent := t.TempDir()
	destination := filepath.Join(parent, "ecdict-common")
	spec := testDictionarySpec(server.URL, testDictionaryCSV)
	info, err := installTranslationDictionary(context.Background(), server.Client(), spec, destination, &dictionaryProgress{})
	if err != nil || info.Entries != 5 {
		t.Fatalf("install = %+v, %v", info, err)
	}
	if _, err := os.Stat(filepath.Join(destination, "LICENSE.txt")); err != nil {
		t.Fatal("the installed dictionary must carry its license", err)
	}
	entries, err := readTranslationLibrary(filepath.Join(destination, "h.csv"), false)
	if err != nil || len(entries) != 1 || entries[0].target != "你好；喂" {
		t.Fatalf("lookup shard = %+v, %v", entries, err)
	}
	for _, scenario := range []string{"checksum", "truncated", "cancel", "existing"} {
		t.Run(scenario, func(t *testing.T) {
			candidate := filepath.Join(parent, scenario)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			broken := spec
			body = testDictionaryCSV
			switch scenario {
			case "checksum":
				broken.checksum = strings.Repeat("0", 64)
			case "truncated":
				body = testDictionaryCSV[:len(testDictionaryCSV)-25]
			case "cancel":
				cancel()
			case "existing":
				candidate = destination
			}
			if _, err := installTranslationDictionary(ctx, server.Client(), broken, candidate, &dictionaryProgress{}); err == nil {
				t.Fatal("unsafe/incomplete download was accepted")
			}
			if scenario != "existing" {
				if _, err := os.Stat(candidate); !os.IsNotExist(err) {
					t.Fatal("failed download exposed a partial dictionary", err)
				}
			}
			temporary, _ := filepath.Glob(filepath.Join(parent, ".ecdict-download-*"))
			if len(temporary) != 0 {
				t.Fatal("temporary download files were not released")
			}
		})
	}
	if err := removeTranslationDictionary(destination); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatal("dictionary was not removed")
	}
}

func TestDownloadedDictionaryThroughUI(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(testDictionaryCSV))
	}))
	defer server.Close()
	previous := englishChineseDictionary
	englishChineseDictionary = testDictionarySpec(server.URL, testDictionaryCSV)
	defer func() { englishChineseDictionary = previous }()
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	m.newDocument()
	session := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	if m.translationEnabled || m.translation != nil {
		t.Fatal("translation should start disabled")
	}
	m.openTranslation(false)
	if m.translation != nil || requests.Load() != 0 {
		t.Fatal("disabled translation initialized/downloaded resources")
	}
	m.translationEnabled = true
	m.openTranslation(false)
	check(session.Frame())
	if requests.Load() != 0 {
		t.Fatal("opening translation must not download a dictionary")
	}
	local, err := session.Locate("button:查找")
	check(err)
	ai, err := session.Locate("button:AI 翻译")
	check(err)
	download, err := session.Locate("button:下载英汉词库")
	check(err)
	if ai.Bounds.Y != local.Bounds.Y || ai.Bounds.X <= local.Bounds.X || download.Bounds.Y <= local.Bounds.Y {
		t.Fatal("local search, AI, and the inline download are out of order")
	}
	check(session.ClickLabel("button:下载英汉词库"))
	check(session.WaitFor(func(app.Snapshot) bool { return m.translation.download == nil }, 2*time.Second))
	if m.translation.dictionary == nil || requests.Load() != 1 {
		t.Fatal("dictionary download did not complete", m.translation.dictionaryProblem)
	}
	state := m.translation
	state.query.SetText("hello")
	check(session.Frame())
	check(session.ClickLabel("button:查找"))
	if state.output.Text() != "你好；喂" || state.target.tab != nil || len(state.suggestions) != 1 {
		t.Fatalf("typed dictionary lookup = %q, %s", state.output.Text(), state.problem)
	}
	check(session.ClickLabel("button:保存到库…"))
	check(session.ClickLabel("button:保存词条"))
	for _, query := range []string{"HELLO", "hello world", "shellow"} {
		state.query.SetText(query)
		check(session.Frame())
		check(session.ClickLabel("button:查找"))
		switch query {
		case "HELLO":
			if state.output.Text() != "你好；喂" || state.suggestions[0].scope != "个人" {
				t.Fatal("case-insensitive lookup or personal-library priority failed")
			}
		case "hello world":
			if state.output.Text() != "" || len(state.terms) < 2 {
				t.Fatal("dictionary should supply word meanings, not invent a sentence translation")
			}
		case "shellow":
			if len(state.suggestions) != 0 || len(state.terms) != 0 {
				t.Fatal("dictionary matched inside another word")
			}
		}
	}
	if m.active.modified() || requests.Load() != 1 {
		t.Fatal("offline lookup changed the document or made a network request")
	}
	if _, err := session.Locate("button:移除下载词库…"); err == nil {
		t.Fatal("dictionary removal is outside the library manager")
	}
	for _, label := range []string{"词库管理", "移除下载词库…", "确认移除", "返回翻译"} {
		check(session.ClickLabel("button:" + label))
	}
	if _, err := os.Stat(filepath.Join(m.dataDirectory, dictionaryDirectory)); !os.IsNotExist(err) {
		t.Fatal("the library manager did not remove the downloaded dictionary", err)
	}
	state.query.SetText("hello")
	check(session.Frame())
	check(session.ClickLabel("button:查找"))
	if state.output.Text() != "你好；喂" || len(state.suggestions) != 1 || state.suggestions[0].scope != "个人" {
		t.Fatal("removing the downloaded dictionary lost a saved translation")
	}
	m.toggleTranslation()
	check(session.Frame())
	if m.translation != nil {
		t.Fatal("closed translation retained panel state")
	}
}

func TestInstalledDictionaryFromRealSource(t *testing.T) {
	source := os.Getenv("EDITORX_DICTIONARY_SOURCE")
	compressed := os.Getenv("EDITORX_DICTIONARY_GZIP")
	if compressed != "" {
		source = compressed
	}
	if source == "" {
		t.Skip("real dictionary verification is opt-in and uses a previously downloaded file")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if compressed != "" {
			w.Header().Set("Content-Encoding", "gzip")
		}
		http.ServeFile(w, r, source)
	}))
	defer server.Close()
	spec := englishChineseDictionary
	spec.url = server.URL
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	started := time.Now()
	info, err := installTranslationDictionary(context.Background(), server.Client(), spec, filepath.Join(m.dataDirectory, dictionaryDirectory), &dictionaryProgress{})
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Stat(source)
	if err != nil || info.DownloadBytes != file.Size() {
		t.Fatal("download byte count differs from the actual transfer", info.DownloadBytes, err)
	}
	t.Logf("downloaded %d bytes; installed %d entries, %d bytes (%.2f MiB), %s", info.DownloadBytes, info.Entries, info.Bytes, float64(info.Bytes)/(1<<20), time.Since(started))
	m.translationEnabled = true
	m.openTranslation(false)
	for _, query := range []string{"hello", "world", "file", "permission", "translate", "files", "saved", "failed", "configuration", "connection"} {
		started = time.Now()
		m.translation.query.SetText(query)
		m.lookupTypedTranslation()
		if m.translation.output.Text() == "" {
			t.Errorf("real dictionary missed %q: %s", query, m.translation.problem)
		}
		t.Logf("lookup %s: %s", query, time.Since(started))
	}
}

func TestDictionaryDownloadStopsWithPanel(t *testing.T) {
	for _, action := range []string{"取消下载", "关闭翻译"} {
		t.Run(action, func(t *testing.T) {
			var started, stopped atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(strings.Split(testDictionaryCSV, "\n")[0] + "\n"))
				w.(http.Flusher).Flush()
				started.Store(true)
				<-r.Context().Done()
				stopped.Store(true)
			}))
			defer server.Close()
			previous := englishChineseDictionary
			englishChineseDictionary = testDictionarySpec(server.URL, testDictionaryCSV)
			defer func() { englishChineseDictionary = previous }()
			m := newEditorApp()
			m.dataDirectory = t.TempDir()
			m.translationEnabled = true
			m.openTranslation(false)
			s := openTestSession(t, m)
			if err := s.ClickLabel("button:词库管理"); err != nil {
				t.Fatal(err)
			}
			if err := s.ClickLabel("button:下载英汉词库"); err != nil {
				t.Fatal(err)
			}
			if err := s.WaitFor(func(app.Snapshot) bool { return started.Load() }, time.Second); err != nil {
				t.Fatal(err)
			}
			if err := s.ClickLabel("button:" + action); err != nil {
				t.Fatal(err)
			}
			if err := s.WaitFor(func(app.Snapshot) bool {
				temporary, _ := filepath.Glob(filepath.Join(m.dataDirectory, "dictionaries", ".ecdict-download-*"))
				return stopped.Load() && len(temporary) == 0
			}, 2*time.Second); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(m.dataDirectory, dictionaryDirectory)); !os.IsNotExist(err) {
				t.Fatal("canceled download published a dictionary", err)
			}
		})
	}
}
