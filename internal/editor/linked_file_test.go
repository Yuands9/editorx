package editor

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

func TestLinkedFileWithHTTPSRedirectAndDifferentFormats(t *testing.T) {
	files := map[string]struct {
		mediaType, name string
		data            []byte
	}{
		"/json":      {"application/json", "中文配置.json", []byte(`{"ready":true}`)},
		"/image":     {"image/png", "image.png", applicationIconPNG},
		"/pdf":       {"application/pdf", "pdf.pdf", previewFixturePDF()},
		"/word":      {"application/octet-stream", "word.docx", sampleDocx(t)},
		"/README.md": {"text/plain", "README.md", []byte("# 中文标题")},
		"/tool":      {"text/javascript", "tool.js", []byte("const enabled = true;\n")},
		"/settings":  {"text/plain", "settings", []byte("port=8080\n")},
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/json", http.StatusFound)
			return
		}
		if r.URL.Path == "/too-big" {
			w.Header().Set("Content-Length", fmt.Sprint(linkedFileLimit+1))
			w.WriteHeader(http.StatusOK)
			return
		}
		item, found := files[r.URL.Path]
		if !found {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/json" {
			w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''%E4%B8%AD%E6%96%87%E9%85%8D%E7%BD%AE.json")
		}
		w.Header().Set("Content-Type", item.mediaType)
		w.Write(item.data)
	}))
	defer server.Close()
	directory := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for route, item := range files {
		t.Run(item.name, func(t *testing.T) {
			if route == "/json" {
				route = "/redirect"
			}
			address, err := linkedFileURL(server.URL + route + "?example=1#part")
			if err != nil {
				t.Fatal(err)
			}
			file, err := readLinkedFile(ctx, server.Client(), address, directory)
			if err != nil {
				t.Fatal(err)
			}
			if filepath.Base(file.doc.path) != item.name || !gitPathWithin(directory, file.doc.path) {
				t.Fatalf("unexpected download path: %q", file.doc.path)
			}
			switch filepath.Ext(item.name) {
			case ".png":
				if file.picture == nil {
					t.Fatal("image preview was not loaded")
				}
			case ".pdf":
				if file.preview == nil || file.preview.kind != "PDF" {
					t.Fatal("PDF preview was not loaded")
				}
			case ".docx":
				if hasWordEditor {
					if file.office == nil || file.office.state.Kind != "docx" {
						t.Fatal("local Word editor was not loaded")
					}
				} else if file.preview == nil || !strings.Contains(string(file.preview.word.Body), "中文文档测试") {
					t.Fatal("public Word preview did not load the document")
				}
			default:
				if file.doc.saved != string(item.data) {
					t.Fatalf("the linked text was not decoded: %q", file.doc.saved)
				}
			}
		})
	}
	for _, route := range []string{"/missing", "/too-big"} {
		address, err := linkedFileURL(server.URL + route)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := readLinkedFile(ctx, server.Client(), address, directory); err == nil {
			t.Fatalf("invalid response accepted: %s", route)
		}
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != len(files) {
		t.Fatalf("failed requests left files behind: %v, %v", entries, err)
	}
	for _, raw := range []string{"file:///test", "://", "https://name:password@example.invalid/a.txt"} {
		if _, err := linkedFileURL(raw); err == nil {
			t.Fatalf("invalid address accepted: %q", raw)
		}
	}
}

func TestLinkedFileShortcutOpensEditableLocalCopy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, "# 从链接打开\n")
	}))
	defer server.Close()
	store, err := openSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := newEditorApp()
	store.attach(m)
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	chord := func(mods gfx.Modifiers, key rune) {
		_, err := s.Driver().ShortcutChord(mods, key)
		check(err)
	}
	defaultMods := gfx.ModifierControl | gfx.ModifierShift
	chord(defaultMods, 'l')
	if !m.linkedFile.open {
		t.Fatal("default shortcut did not open the link dialog")
	}
	check(s.ClickLabel("button:取消"))
	check(s.ClickLabel("button:设置"))
	check(s.ClickLabel("button:快捷键"))
	check(s.ClickLabel("button:Ctrl+Shift+L"))
	chord(gfx.ModifierControl|gfx.ModifierAlt, 'l')
	check(s.ClickLabel("button:关闭设置"))
	chord(defaultMods, 'l')
	if m.linkedFile.open {
		t.Fatal("the old shortcut still opened the dialog")
	}
	preferences, err := readApplicationPreferences(store.directory)
	check(err)
	if preferences.Shortcuts.chord(shortcutOpenLink).Label() != "Ctrl+Alt+L" {
		t.Fatal("custom link shortcut did not persist")
	}
	chord(gfx.ModifierControl|gfx.ModifierAlt, 'l')
	check(s.ClickLabel("文件链接"))
	check(s.Type(server.URL + "/说明.md"))
	check(s.Driver().Key(gfx.KeyEnter, 0))
	check(s.WaitFor(func(app.Snapshot) bool { return m.active != nil || m.linkedFile.problem != "" }, 5*time.Second))
	if m.active == nil || m.linkedFile.open || m.active.format() != "Markdown" {
		t.Fatalf("the link did not open an editable Markdown tab: %q", m.linkedFile.problem)
	}
	tab := m.active
	tab.source.replace("# 本地修改\n")
	if !m.saveTab(tab, false) {
		t.Fatal("the local copy did not save")
	}
	saved, err := os.ReadFile(tab.doc.path)
	check(err)
	if !strings.Contains(string(saved), "本地修改") || !gitPathWithin(filepath.Join(store.directory, "downloads"), tab.doc.path) {
		t.Fatal("editing did not save to the local copy")
	}
}
