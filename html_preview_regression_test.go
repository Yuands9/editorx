package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreviewHTMLBaseAndRawText(t *testing.T) {
	source := `<!DOCTYPE html><HTML><HEAD><!-- <base href="bad/"> --><script>const s='<base href="fake/">';</script><BASE target="_blank" HREF="../assets/?a=1&amp;b=2" data-test="中文"></HEAD><body><base href="later/"><p>中文</p></body></HTML>`
	output := previewHTML(source, `D:\项目 中文\index.html`)
	for _, expected := range []string{
		`<meta charset="utf-8">`, `href="file:///D:/assets/?a=1&amp;b=2"`, `target="_blank"`, `data-test="中文"`,
		`const s='<base href="fake/">';`, `<base href="later/">`,
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("missing %s in %s", expected, output)
		}
	}
	if strings.Contains(output, `href="file:///D:/项目`) {
		t.Fatal("added a second base instead of resolving first existing href")
	}
	lateBase := previewHTML(`<html><head></head><body><BaSe href="../assets/"></body></html>`, `D:\项目\index.html`)
	if strings.Count(strings.ToLower(lateBase), "<base ") != 1 || !strings.Contains(lateBase, `href="file:///D:/assets/"`) {
		t.Fatal("explicit base outside head was ignored", lateBase)
	}
	fragment := `<div data-html="<head>">中文 &amp; 正文</div>`
	if output := previewHTML(fragment, ""); output != `<meta charset="utf-8">`+fragment {
		t.Fatal(output)
	}
}

func TestHTMLPreviewWriteKeepsCompleteUnicodeAndCleansTemporary(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "preview.html")
	if err := os.WriteFile(path, []byte("原来内容"), 0600); err != nil {
		t.Fatal(err)
	}
	source := strings.Repeat("<p>中文😀</p>\n", 1000)
	if err := writeHTMLPreview(path, source); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != source {
		t.Fatal("preview data lost", err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 {
		t.Fatal("temporary preview retained", err)
	}
	if err := writeHTMLPreview(directory, "should fail"); err == nil {
		t.Fatal("directory replaced")
	}
	data, err = os.ReadFile(path)
	if err != nil || string(data) != source {
		t.Fatal("failed replacement damaged previous preview", err)
	}
}
