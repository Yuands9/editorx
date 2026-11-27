package editor

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
)

func TestNativeDocumentOutline(t *testing.T) {
	if os.Getenv("VIVA_NATIVE_SMOKE") != "1" {
		t.Skip("native document outline check is opt-in")
	}
	directory := t.TempDir()
	setNativeWebViewProfile(t, directory)
	type outlineSample struct {
		name  string
		data  []byte
		probe string
	}
	samples := []outlineSample{
		{"书签.pdf", outlinePDFFixture(false), checkPDFOutlineJS},
		{"标记标题.pdf", outlinePDFFixture(true), checkPDFOutlineJS},
		{"无大纲.pdf", previewFixturePDF(), `await waitFor(() => status.textContent === "这份 PDF 没有书签或已标记的标题。");`},
	}
	if hasWordEditor {
		samples = append(samples, outlineSample{"标题.docx", outlineWordFixture(t), checkWordOutlineJS})
	}
	for _, sample := range samples {
		if err := os.WriteFile(filepath.Join(directory, sample.name), sample.data, 0600); err != nil {
			t.Fatal(err)
		}
	}

	m := newEditorApp()
	m.dataDirectory = directory
	var failure error
	var started time.Time
	index, checking, completed := 0, false, false
	stop := make(chan struct{})
	defer close(stop)
	err := app.RunApplication(func(application *app.Application) error {
		if err := openEditorWindow(application, m); err != nil {
			return err
		}
		finish := func(err error) {
			failure, completed = err, true
			m.releaseWindowState()
			application.Quit()
		}
		go func() {
			ticker := time.NewTicker(40 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-stop:
					return
				case <-ticker.C:
					application.Post(func() {
						if completed || !m.webReady {
							return
						}
						sample := samples[index]
						if started.IsZero() {
							started = time.Now()
							m.load(filepath.Join(directory, sample.name))
							m.invalidator.Invalidate()
						}
						if m.showError || m.active == nil {
							finish(fmt.Errorf("open %s: %s", sample.name, m.errorText))
							return
						}
						var view *app.WebView
						var ready *bool
						var problem string
						if word := m.active.office; word != nil {
							view, ready, problem = word.view, &word.ready, word.errorText
						} else if pdf := m.active.preview; pdf != nil {
							view, ready, problem = pdf.view, &pdf.ready, pdf.issue
						}
						if ready == nil || problem != "" || time.Since(started) > 20*time.Second {
							finish(fmt.Errorf("outline check for %s, checking=%v: %s", sample.name, checking, problem))
							return
						}
						if !*ready || view == nil {
							return
						}
						if !checking {
							checking = true
							*ready = false
							view.Eval(outlineProbeStart + sample.probe + outlineProbeEnd)
							return
						}
						t.Logf("native outline passed: %s", sample.name)
						index++
						started, checking = time.Time{}, false
						if index == len(samples) {
							finish(nil)
						}
					})
				}
			}
		}()
		return nil
	})
	if err != nil || failure != nil || !completed {
		t.Fatalf("native document outline: %v, %v, completed=%v", err, failure, completed)
	}
}

const outlineProbeStart = `(async () => {
	const panel = document.querySelector("#document-outline");
	const status = document.querySelector("#outline-status");
	const titles = () => [...panel.querySelectorAll(".outline-entry")];
	const waitFor = async (condition) => {
		const deadline = Date.now() + 6500;
		while (!condition()) {
			if (Date.now() >= deadline) {
				throw new Error("outline wait timed out: " + status.textContent + " [" + titles().map(button => button.textContent).join(", ") + "]");
			}
			await new Promise(resolve => setTimeout(resolve, 25));
		}
	};
	try {
		const toggle = document.querySelector("#outline-toggle");
		if (!panel.hidden || toggle.hidden || toggle.disabled) throw new Error("incorrect outline toolbar state");
		toggle.click();
		if (panel.hidden) throw new Error("outline did not open");
`
const outlineProbeEnd = `
		window.editorxSetLanguage("en");
		await waitFor(() => document.documentElement.lang === "en" && toggle.textContent === "Outline");
		window.editorxSetLanguage("zh-CN");
		await waitFor(() => document.documentElement.lang === "zh-CN" && toggle.textContent === "标题");
		window.viva.postMessage(JSON.stringify({kind:"ready"}));
	} catch (error) {
		window.viva.postMessage(JSON.stringify({kind:"error", message:error.message, text:error.message}));
	}
})()`

const checkWordOutlineJS = `
		await waitFor(() => titles().length === 3);
		const [first, child, last] = titles();
		if (first.textContent !== "第一章" || child.textContent !== "章节细节" || last.textContent !== "最后一章") {
			throw new Error("Word headings were incomplete or out of order");
		}
		if (!first.closest("li").contains(child) || first.closest("li").contains(last)) {
			throw new Error("Word heading levels were not preserved");
		}
		last.click();
		await waitFor(() => last.getAttribute("aria-current") === "location");
		const change = await window.office.editor.activeEditor.doc.create.heading({
			at: {kind: "documentEnd"}, level: 2, text: "新加的标题"
		});
		if (!change.success) throw new Error("could not add a heading for the refresh check");
		await waitFor(() => titles().some(button => button.textContent === "新加的标题"));
		toggle.click();
		if (!panel.hidden) throw new Error("Word outline did not close");
`

const checkPDFOutlineJS = `
		await waitFor(() => titles().length === 2);
		const [first, second] = titles();
		if (first.textContent !== "Outline overview" || second.textContent !== "Outline details") {
			throw new Error("incorrect PDF title text");
		}
		const branch = first.closest("li");
		if (!branch.contains(second)) throw new Error("PDF nested headings were flat");
		const disclosure = branch.querySelector(".outline-disclosure");
		disclosure.click();
		if (!second.closest("ul").hidden || disclosure.getAttribute("aria-expanded") !== "false") {
			throw new Error("PDF outline branch did not collapse");
		}
		disclosure.click();
		second.click();
		await waitFor(() => document.querySelector("#page").value === "2" &&
			second.getAttribute("aria-current") === "location");
		toggle.click();
		if (!panel.hidden) throw new Error("PDF outline did not close");
`

func outlineWordFixture(t *testing.T) []byte {
	t.Helper()
	var data bytes.Buffer
	archive := zip.NewWriter(&data)
	parts := map[string]string{
		"[Content_Types].xml":          `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/><Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/></Types>`,
		"_rels/.rels":                  `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/_rels/document.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rIdStyles" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`,
		"word/styles.xml":              `<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:pPr><w:outlineLvl w:val="0"/></w:pPr></w:style><w:style w:type="paragraph" w:styleId="Heading2"><w:name w:val="heading 2"/><w:pPr><w:outlineLvl w:val="1"/></w:pPr></w:style></w:styles>`,
		"word/document.xml": `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>
<w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>第一章</w:t></w:r></w:p>
<w:p><w:pPr><w:pStyle w:val="Heading2"/></w:pPr><w:r><w:t>章节细节</w:t></w:r></w:p>
<w:p><w:r><w:t>这是正文，不应出现在标题列表。</w:t><w:br w:type="page"/></w:r></w:p>
<w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>最后一章</w:t></w:r></w:p>
<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440"/></w:sectPr>
</w:body></w:document>`,
	}
	for name, content := range parts {
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func outlinePDFFixture(tagged bool) []byte {
	catalog := "<< /Type /Catalog /Pages 2 0 R /Outlines 8 0 R /Dests << /Details [4 0 R /Fit] >> >>"
	pages := [2]string{}
	streams := [2]string{}
	for i, title := range []string{"Outline overview", "Outline details"} {
		attributes := ""
		body := fmt.Sprintf("BT /F1 24 Tf 60 350 Td (%s) Tj ET", title)
		if tagged {
			attributes = fmt.Sprintf("/StructParents %d", i)
			body = fmt.Sprintf("/H%d <</MCID 0>> BDC %s EMC", i+1, body)
		}
		pages[i] = fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 500 420] /Resources << /Font << /F1 5 0 R >> >> /Contents %d 0 R %s >>", i+6, attributes)
		streams[i] = fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body)
	}

	outline := []string{
		"<< /Type /Outlines /First 9 0 R /Last 9 0 R /Count 2 >>",
		"<< /Title (Outline overview) /Parent 8 0 R /Dest [3 0 R /Fit] /First 10 0 R /Last 10 0 R /Count 1 >>",
		"<< /Title (Outline details) /Parent 9 0 R /Dest /Details >>",
	}
	if tagged {
		catalog = "<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 8 0 R /MarkInfo << /Marked true >> >>"
		outline = []string{
			"<< /Type /StructTreeRoot /K [9 0 R 10 0 R] /ParentTree 11 0 R >>",
			"<< /Type /StructElem /S /H1 /P 8 0 R /Pg 3 0 R /K 0 >>",
			"<< /Type /StructElem /S /H2 /P 8 0 R /Pg 4 0 R /K 0 >>",
			"<< /Nums [0 [9 0 R] 1 [10 0 R]] >>",
		}
	}
	return makeFixturePDF(append([]string{
		catalog, "<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		pages[0], pages[1], "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>", streams[0], streams[1],
	}, outline...)...)
}
