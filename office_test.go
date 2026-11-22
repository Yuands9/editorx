package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

func sampleWorkbook(t *testing.T) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	for cell, value := range map[string]any{"A1": "中文表格", "A2": 12, "B2": 3, "A5": "删除我", "A6": "00123"} {
		if err := f.SetCellValue("Sheet1", cell, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.SetCellFormula("Sheet1", "C2", "SUM(A2:B2)"); err != nil {
		t.Fatal(err)
	}
	style, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Family: "Microsoft YaHei", Bold: true, Color: "2454AC", Size: 14}, Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"E8EEFF"}}, Border: []excelize.Border{{Type: "bottom", Color: "2454AC", Style: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	f.SetCellStyle("Sheet1", "A1", "C1", style)
	f.MergeCell("Sheet1", "A1", "C1")
	f.SetColWidth("Sheet1", "A", "A", 24)
	f.SetRowHeight("Sheet1", 1, 30)
	f.AddPictureFromBytes("Sheet1", "E5", &excelize.Picture{Extension: ".png", File: applicationIconPNG, Format: &excelize.GraphicOptions{ScaleX: 0.15, ScaleY: 0.15}})
	f.NewSheet("第二张表")
	f.SetCellStr("第二张表", "A1", "保留内容")
	data, err := f.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}
func sampleDocx(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	parts := map[string]string{
		"[Content_Types].xml":          `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Default Extension="png" ContentType="image/png"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels":                  `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/_rels/document.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rIdImage" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/image1.png"/></Relationships>`,
		"word/document.xml":            `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture"><w:body><w:p><w:r><w:rPr><w:b/><w:color w:val="235A97"/><w:sz w:val="32"/></w:rPr><w:t>中文文档测试</w:t></w:r></w:p><w:p><w:r><w:t>这是可以编辑的正文。</w:t></w:r></w:p><w:tbl><w:tblPr><w:tblW w:w="6000" w:type="dxa"/><w:tblBorders><w:top w:val="single" w:sz="4"/><w:left w:val="single" w:sz="4"/><w:bottom w:val="single" w:sz="4"/><w:right w:val="single" w:sz="4"/><w:insideH w:val="single" w:sz="4"/><w:insideV w:val="single" w:sz="4"/></w:tblBorders></w:tblPr><w:tblGrid><w:gridCol w:w="3000"/><w:gridCol w:w="3000"/></w:tblGrid><w:tr><w:tc><w:tcPr><w:tcW w:w="3000" w:type="dxa"/></w:tcPr><w:p><w:r><w:t>名称</w:t></w:r></w:p></w:tc><w:tc><w:tcPr><w:tcW w:w="3000" w:type="dxa"/></w:tcPr><w:p><w:r><w:t>数值</w:t></w:r></w:p></w:tc></w:tr></w:tbl><w:p><w:r><w:drawing><wp:inline><wp:extent cx="914400" cy="914400"/><wp:docPr id="1" name="Viva 图标"/><a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:pic><pic:nvPicPr><pic:cNvPr id="1" name="image1.png"/><pic:cNvPicPr/></pic:nvPicPr><pic:blipFill><a:blip r:embed="rIdImage"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill><pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="914400" cy="914400"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr></pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p><w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440"/></w:sectPr></w:body></w:document>`,
	}
	for name, value := range parts {
		writer, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		writer.Write([]byte(value))
	}
	writer, err := archive.Create("word/media/image1.png")
	if err != nil {
		t.Fatal(err)
	}
	writer.Write(applicationIconPNG)
	if err = archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
func zipPart(t *testing.T, data []byte, name string) []byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range reader.File {
		if entry.Name == name {
			stream, err := entry.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			data, err := io.ReadAll(stream)
			if err != nil {
				t.Fatal(err)
			}
			return data
		}
	}
	t.Fatalf("missing part %s", name)
	return nil
}
func TestOfficeWorkbookRoundTrip(t *testing.T) {
	original := sampleWorkbook(t)
	before, err := readWorkbook(original, "中文.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(before)
	var after officeWorkbook
	json.Unmarshal(data, &after)
	sheet := after.Sheets[after.Order[0]]
	sheet.Cells[1][0].Value = float64(24)
	sheet.Cells[1][2].Formula = "=A2*B2"
	sheet.Cells[4][0] = nil
	sheet.Cells[5][0].Value = "00999"
	output, err := writeWorkbook(original, before, &after)
	if err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(output))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for axis, expected := range map[string]string{"A2": "24", "A5": "", "A6": "00999"} {
		value, err := f.GetCellValue("Sheet1", axis)
		if err != nil || value != expected {
			t.Fatalf("%s=%q want %q: %v", axis, value, expected, err)
		}
	}
	formula, _ := f.GetCellFormula("Sheet1", "C2")
	if formula != "A2*B2" {
		t.Fatal(formula)
	}
	value, err := f.CalcCellValue("Sheet1", "C2")
	if err != nil || value != "72" {
		t.Fatalf("formula=%s %v", value, err)
	}
	styleID, _ := f.GetCellStyle("Sheet1", "A1")
	style, _ := f.GetStyle(styleID)
	if !style.Font.Bold || len(style.Border) == 0 {
		t.Fatal("format lost")
	}
	if !bytes.Equal(zipPart(t, original, "xl/media/image1.png"), zipPart(t, output, "xl/media/image1.png")) {
		t.Fatal("image changed")
	}
	merges, _ := f.GetMergeCells("Sheet1")
	if len(merges) != 1 {
		t.Fatal("merge lost")
	}
}
func TestOfficeSessionAndConflict(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "文档.docx")
	original := sampleDocx(t)
	os.WriteFile(path, original, 0600)
	m := newEditorApp()
	m.load(path)
	if m.showError || m.active.office == nil {
		t.Fatal(m.errorText)
	}
	m.active.office.state.Dirty = true
	m.active.office.state.Snapshot = original
	restored := newEditorApp()
	restored.restoreSession(m.snapshotSession())
	if restored.active.office == nil || !restored.active.modified() {
		t.Fatal("draft lost")
	}
	os.WriteFile(path, append(original, 0), 0600)
	if err := saveOfficeFile(&restored.active.doc, restored.active.office, path); err == nil {
		t.Fatal("overwrote external edit")
	}
	alternate := filepath.Join(directory, "另存.docx")
	if err := saveOfficeFile(&restored.active.doc, restored.active.office, alternate); err != nil {
		t.Fatal(err)
	}
	saved, _ := os.ReadFile(alternate)
	if !bytes.Equal(saved, original) {
		t.Fatal("save mismatch")
	}
}
func TestOfficeLoopback(t *testing.T) {
	office, err := restoreOffice(officeSession{Kind: "docx", Original: sampleDocx(t)}, "测试.docx")
	if err != nil {
		t.Fatal(err)
	}
	if err = office.serve("测试.docx"); err != nil {
		t.Fatal(err)
	}
	defer office.close()
	response, err := http.Get(office.url + "info")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.Status)
	}
	response, err = http.Post(office.url+"snapshot?revision=1", "application/octet-stream", bytes.NewReader(office.state.Original))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal("cross-origin snapshot allowed")
	}
	request, _ := http.NewRequest("POST", office.url+"snapshot?revision=1", bytes.NewReader(office.state.Original))
	request.Header.Set("Origin", strings.TrimSuffix(strings.Split(office.url, "/")[0]+"//"+strings.Split(office.url, "/")[2], "/"))
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 204 {
		t.Fatal(response.Status)
	}
}
func TestOfficeBrowserFixture(t *testing.T) {
	if os.Getenv("VIVA_OFFICE_BROWSER") != "1" {
		t.Skip("browser fixture is opt-in")
	}
	directory := filepath.Join(".dev", "office-qa")
	os.MkdirAll(directory, 0700)
	urls := map[string]string{}
	for kind, data := range map[string][]byte{"xlsx": sampleWorkbook(t), "docx": sampleDocx(t)} {
		os.WriteFile(filepath.Join(directory, "sample."+kind), data, 0600)
		office, err := restoreOffice(officeSession{Kind: kind, Original: data}, "sample."+kind)
		if err != nil {
			t.Fatal(err)
		}
		if err = office.serve("sample." + kind); err != nil {
			t.Fatal(err)
		}
		defer office.close()
		urls[kind] = office.url
		go func() {
			for {
				time.Sleep(time.Second)
				office.mu.Lock()
				snapshot := office.received
				office.mu.Unlock()
				if len(snapshot) > 0 {
					office.state.Snapshot = snapshot
					output, err := office.output()
					if err == nil {
						os.WriteFile(filepath.Join(directory, "edited."+office.state.Kind), output, 0600)
					} else {
						os.WriteFile(filepath.Join(directory, "error."+office.state.Kind), []byte(err.Error()), 0600)
					}
				}
			}
		}()
	}
	encoded, _ := json.Marshal(urls)
	os.WriteFile(filepath.Join(directory, "urls.json"), encoded, 0600)
	t.Log(string(encoded))
	<-time.After(20 * time.Minute)
}
func TestOfficeWorkbookFormatting(t *testing.T) {
	original := sampleWorkbook(t)
	before, err := readWorkbook(original, "样式.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(before)
	var after officeWorkbook
	if err = json.Unmarshal(data, &after); err != nil {
		t.Fatal(err)
	}
	cell := after.Sheets[after.Order[0]].Cells[1][0]
	cell.Style = []byte(`{"bl":1,"fs":16,"ff":"Microsoft YaHei","cl":{"rgb":"#123456"},"bg":{"rgb":"#EFEFEF"},"bd":{"b":{"s":7,"cl":{"rgb":"#336699"}}},"ht":2,"vt":2,"tb":3,"tr":{"a":45},"stf":1,"n":{"pattern":"0.00"}}`)
	output, err := writeWorkbook(original, before, &after)
	if err != nil {
		t.Fatal(err)
	}
	file, err := excelize.OpenReader(bytes.NewReader(output))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	id, err := file.GetCellStyle("Sheet1", "A2")
	if err != nil {
		t.Fatal(err)
	}
	style, err := file.GetStyle(id)
	if err != nil {
		t.Fatal(err)
	}
	if !style.Font.Bold || style.Font.Size != 16 || style.Font.Color != "123456" || style.Alignment.TextRotation != 45 || !style.Alignment.ShrinkToFit || len(style.Border) != 1 || style.Border[0].Style != 6 || style.CustomNumFmt == nil || *style.CustomNumFmt != "0.00" {
		t.Fatalf("format did not round-trip: %+v", style)
	}
}
