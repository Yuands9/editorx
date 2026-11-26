package editor

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTextEncodingsPreserveOriginalBytes(t *testing.T) {
	cases := []struct {
		encoding    textEncoding
		raw, source string
		bom         bool
	}{
		{utf8Encoding, "\xef\xbb\xbf中文\r\n", "中文", true},
		{utf16LEEncoding, "\xff\xfe\x2d\x4e\x87\x65\x0d\x00\x0a\x00", "中文", true},
		{utf16BEEncoding, "\xfe\xff\x4e\x2d\x65\x87\x00\x0d\x00\x0a", "中文", true},
		{utf16LEEncoding, "L\x00i\x00n\x00e\x00\x0d\x00\x0a\x00", "Line", false},
		{utf16BEEncoding, "\x00L\x00i\x00n\x00e\x00\x0d\x00\x0a", "Line", false},
		{gbkEncoding, "\xd6\xd0\xce\xc4\r\n", "中文", false},
		{gb18030Encoding, "\x94\x39\xfc\x36\r\n", "😀", false},
		{windows1252Encoding, "Caf\xe9 \x80\r\n", "Café €", false},
	}
	for _, sample := range cases {
		t.Run(sample.encoding.label(sample.bom)+"/"+sample.source, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "text.txt")
			if err := os.WriteFile(path, []byte(sample.raw), 0600); err != nil {
				t.Fatal(err)
			}
			doc, err := readDocument(path)
			if err != nil {
				t.Fatal(err)
			}
			if doc.encoding != sample.encoding || doc.bom != sample.bom || doc.eol != "CRLF" || doc.saved != sample.source+"\n" {
				t.Fatalf("decoded %s, BOM %t, EOL %s, content %q", doc.encoding, doc.bom, doc.eol, doc.saved)
			}
			expected := []byte(sample.raw)
			rawSuffix := []byte("!\x0d\x0a")
			suffixStart := len(expected) - 2
			if sample.encoding == utf16LEEncoding {
				rawSuffix, suffixStart = []byte("!\x00\x0d\x00\x0a\x00"), len(expected)-4
			} else if sample.encoding == utf16BEEncoding {
				rawSuffix, suffixStart = []byte("\x00!\x00\x0d\x00\x0a"), len(expected)-4
			}
			expected = append(expected[:suffixStart], rawSuffix...)
			if err := doc.save(path, sample.source+"!\n", nil); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(got, expected) {
				t.Fatalf("saved bytes %x, want %x, error %v", got, expected, err)
			}
			if sample.encoding == gbkEncoding || sample.encoding == windows1252Encoding {
				if err := doc.save(path, "😀\n", nil); err == nil {
					t.Fatal("unsupported characters were accepted")
				}
				got, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(got, expected) {
					t.Fatal("a failed save changed the original file")
				}
			}
		})
	}
}

func TestReopenTextEncodingThroughStatusMenu(t *testing.T) {
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "日本語.txt")
	check(os.WriteFile(path, []byte("\x82\xa0\x82\xa2\r\n"), 0600))
	store, err := openSessionStore(filepath.Join(directory, "data"))
	check(err)
	m := newEditorApp()
	store.attach(m)
	m.load(path)
	tab := m.active
	if tab == nil {
		t.Fatal(m.errorText)
	}
	s := openTestSession(t, m)
	check(s.ClickLabel("button:" + tab.doc.encoding.label(tab.doc.bom)))
	check(s.ClickLabel("menuitem:Shift-JIS"))
	if tab.doc.encoding != shiftJISEncoding || tab.source.text.String() != "あい\n" || tab.modified() {
		t.Fatal("choosing Shift-JIS did not reopen the document")
	}
	tab.source.replace("あい!\n")
	if !m.saveTab(tab, false) {
		t.Fatal(m.errorText)
	}
	raw, err := os.ReadFile(path)
	check(err)
	if string(raw) != "\x82\xa0\x82\xa2!\r\n" {
		t.Fatalf("Save used a different encoding: %x", raw)
	}
	check(store.save())
	restored, err := openSessionStore(store.directory)
	check(err)
	actual := restored.windows[0].active
	if actual == nil || actual.doc.encoding != shiftJISEncoding || actual.source.text.String() != "あい!\n" {
		t.Fatal("session lost the chosen encoding")
	}
}

func TestLargeTextEncodedBoundariesAndSearch(t *testing.T) {
	samples := []struct {
		enc                 textEncoding
		prefix, body, query string
	}{
		{utf16LEEncoding, strings.Repeat("x", (largeTextPageBytes-4)/2), strings.Repeat("😀", 38000), "😀end"},
		{utf16BEEncoding, strings.Repeat("x", (largeTextPageBytes-4)/2), strings.Repeat("😀", 38000), "😀end"},
		{gb18030Encoding, strings.Repeat("x", largeTextPageBytes-2), strings.Repeat("😀", 38000), "😀end"},
		{big5Encoding, strings.Repeat("x", largeTextPageBytes-1), strings.Repeat("中文", 38000), "中文end"},
		{shiftJISEncoding, strings.Repeat("x", largeTextPageBytes-1), strings.Repeat("あい", 38000), "あいend"},
	}
	for _, sample := range samples {
		t.Run(string(sample.enc), func(t *testing.T) {
			value := sample.prefix + sample.query + sample.body + "\r\n" + sample.query
			raw, err := (document{encoding: sample.enc, bom: sample.enc.utf16Order() != nil}).fileBytes(value)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "large.log")
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			var rendered strings.Builder
			for index := int64(0); index < largeTextPageCount(int64(len(raw))); index++ {
				page, err := readLargeTextPage(path, index, -1, "", sample.enc)
				if err != nil {
					t.Fatal(err)
				}
				rendered.WriteString(page.value)
			}
			if rendered.String() != strings.ReplaceAll(value, "\r\n", "\n") {
				t.Fatal("a page dropped, duplicated or split characters")
			}
			for _, direction := range []int{1, -1} {
				start := int64(0)
				if direction < 0 {
					start = int64(len(raw))
				}
				found, err := findLargeText(context.Background(), path, sample.query, start, direction, sample.enc)
				if err != nil || found < 0 {
					t.Fatalf("could not find match in direction %d: %v", direction, err)
				}
				page, err := readLargeTextPage(path, 0, found, sample.query, sample.enc)
				if err != nil {
					t.Fatal(err)
				}
				if string([]rune(page.value)[page.selectionStart:page.selectionEnd]) != sample.query {
					t.Fatal("search did not select the expected decoded text")
				}
			}
		})
	}
}

func TestOpenWithExplicitEncodingThroughMainMenu(t *testing.T) {
	path := filepath.Join(t.TempDir(), "text.txt")
	raw := []byte("\x2d\x4e\x41\x00")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	m := newEditorApp()
	s := openTestSession(t, m)
	for _, action := range []func() error{
		func() error { return s.Driver().QueueOpenFiles([]string{path}, nil) },
		func() error { return s.ClickLabel("button:主菜单") },
		func() error { return s.ClickLabel("menuitem:按编码打开") },
		func() error { return s.ClickLabel("menuitem:UTF-16 LE") },
	} {
		if err := action(); err != nil {
			t.Fatal(err)
		}
	}
	if m.active == nil || m.active.doc.encoding != utf16LEEncoding || m.active.source.text.String() != "中A" {
		t.Fatalf("explicit open failed: %s", m.errorText)
	}
}

func TestExecutableWithTextExtensionIsRejected(t *testing.T) {
	raw := make([]byte, 256)
	copy(raw, "MZ")
	binary.LittleEndian.PutUint32(raw[0x3c:], 128)
	copy(raw[128:], "PE\x00\x00")
	path := filepath.Join(t.TempDir(), "executable.txt")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readDocument(path); !errors.Is(err, errTextPE) {
		t.Fatalf("editable path misidentified the executable: %v", err)
	}
	if _, err := readLargeTextPage(path, 0, -1, "", windows1252Encoding); !errors.Is(err, errTextPE) {
		t.Fatalf("forcing an encoding bypassed executable detection: %v", err)
	}
}
