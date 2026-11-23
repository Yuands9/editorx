package editor

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWordPreviewRendersTextTableImagesAndOutline(t *testing.T) {
	directory := t.TempDir()
	document := filepath.Join(directory, "preview.docx")
	check := func(data []byte) wordPreviewContent {
		t.Helper()
		if err := os.WriteFile(document, data, 0600); err != nil {
			t.Fatal(err)
		}
		content, err := readWordPreview(document)
		if err != nil {
			t.Fatal(err)
		}
		return content
	}
	sample := check(sampleDocx(t))
	for _, fragment := range []string{"中文文档测试", "<table>", "data:image/png;base64,"} {
		if !strings.Contains(string(sample.Body), fragment) {
			t.Fatalf("Word preview is missing %q", fragment)
		}
	}
	outline := check(outlineWordFixture(t))
	if len(outline.Headings) != 3 || outline.Headings[0].Text != "第一章" ||
		outline.Headings[1].Text != "章节细节" || outline.Headings[1].Depth != 1 {
		t.Fatalf("unexpected headings: %+v", outline.Headings)
	}
	outline.Title = "<example>.docx"
	var rendered bytes.Buffer
	if err := wordPreviewTemplate.Execute(&rendered, outline); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered.String(), `href="#heading-2" style="--depth:1"`) ||
		!strings.Contains(rendered.String(), "&lt;example&gt;.docx") {
		t.Fatal("Word preview lost the title hierarchy or did not escape the filename")
	}
	if !hasWordEditor {
		m := newEditorApp()
		m.load(document)
		session := openTestSession(t, m)
		if !session.HasText("正在打开 Word 预览…") || !session.HasText("Word · 只读预览") {
			t.Fatal("the public Word tab did not mount its preview panel")
		}
	}
}
