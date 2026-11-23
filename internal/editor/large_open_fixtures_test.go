package editor

import (
	"archive/zip"
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestMakeLargeOpenFixtures(t *testing.T) {
	directory := os.Getenv("VIVA_LARGE_FIXTURES")
	if directory == "" {
		t.Skip("opt-in fixture generation")
	}

	t.Run("xlsx", func(t *testing.T) {
		book := excelize.NewFile()
		defer book.Close()
		for _, sheet := range []string{"Sheet1", "Operations"} {
			if sheet != "Sheet1" {
				if _, err := book.NewSheet(sheet); err != nil {
					t.Fatal(err)
				}
			}
			writer, err := book.NewStreamWriter(sheet)
			if err != nil {
				t.Fatal(err)
			}
			for row := 1; row <= 6000; row++ {
				values := []any{
					fmt.Sprintf("CN-%08d", row*8191), "示例记录", fmt.Sprintf("customer-%d", row*4093),
					row, row * 37, row * 19, excelize.Cell{Formula: fmt.Sprintf("E%d+F%d", row, row)},
					"已核对", row % 12, fmt.Sprintf("inventory/2026/%d", row), row + 184, row * row,
				}
				if err := writer.SetRow(fmt.Sprintf("A%d", row), values); err != nil {
					t.Fatal(err)
				}
			}
			if err := writer.Flush(); err != nil {
				t.Fatal(err)
			}
		}
		if err := book.SaveAs(filepath.Join(directory, "large.xlsx")); err != nil {
			t.Fatal(err)
		}
	})

	pictures := largeOpenPictures(t)
	t.Run("docx", func(t *testing.T) {
		data := makeLargeDOCX(t, pictures[:24])
		if err := os.WriteFile(filepath.Join(directory, "large.docx"), data, 0600); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("pdf", func(t *testing.T) {
		data := makeLargePDF(pictures)
		if err := os.WriteFile(filepath.Join(directory, "large.pdf"), data, 0600); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("json", func(t *testing.T) {
		var data bytes.Buffer
		data.WriteString("[\n")
		for index := 0; index < 65000; index++ {
			if index != 0 {
				data.WriteString(",\n")
			}
			fmt.Fprintf(&data, `  {"id":%d,"name":"项目配置样例 %08d","enabled":true,"endpoint":"/catalog/v1/example/%d","protocol":"https","timeoutMs":%d,"note":"本地打开验证用数据，记录办公文件与代码配置的编辑启动耗时。"}`, index, index*7187, index*97, 200+index%2000)
		}
		data.WriteString("\n]\n")
		if err := os.WriteFile(filepath.Join(directory, "large.json"), data.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("html", func(t *testing.T) {
		var source strings.Builder
		source.WriteString("<!doctype html>\n<html lang=\"zh-CN\">\n<meta charset=\"utf-8\">\n<title>本地操作记录</title>\n<main><h1>操作记录</h1><ol>\n")
		for index := 1; index <= 55000; index++ {
			fmt.Fprintf(&source, "  <li id=\"record-%d\" data-batch=\"%d\"><strong>工作记录 %06d</strong><span>用来验证一次打开后能直接修改源码，并保留右侧页面预览的本地样例。</span></li>\n", index, index/128, index)
		}
		source.WriteString("</ol></main>\n</html>\n")
		if err := os.WriteFile(filepath.Join(directory, "large.html"), []byte(source.String()), 0600); err != nil {
			t.Fatal(err)
		}
	})
}

func largeOpenPictures(t *testing.T) [][]byte {
	t.Helper()
	random := rand.New(rand.NewPCG(29, 41))
	pictures := make([][]byte, 48)
	picture := image.NewRGBA(image.Rect(0, 0, 960, 540))
	for index := range pictures {
		for y := 0; y < picture.Bounds().Dy(); y++ {
			for x := 0; x < picture.Bounds().Dx(); x++ {
				noise := random.Uint32N(192)
				picture.SetRGBA(x, y, color.RGBA{uint8(uint32(x/8) + noise), uint8(uint32(y/5) + noise), uint8(uint32(index*13) + noise), 255})
			}
		}
		var data bytes.Buffer
		if err := jpeg.Encode(&data, picture, &jpeg.Options{Quality: 86}); err != nil {
			t.Fatal(err)
		}
		pictures[index] = data.Bytes()
	}
	return pictures
}

func makeLargeDOCX(t *testing.T, pictures [][]byte) []byte {
	t.Helper()
	sample := sampleDocx(t)
	original := string(zipPart(t, sample, "word/document.xml"))
	header, content, found := strings.Cut(original, "<w:body>")
	body, section, hasSection := strings.Cut(content, "<w:sectPr>")
	_, imageBody, hasImage := strings.Cut(body, "<w:drawing>")
	drawing, _, hasEnd := strings.Cut(imageBody, "</w:drawing>")
	if !found || !hasSection || !hasImage || !hasEnd {
		t.Fatal("the small DOCX template is missing its page or drawing")
	}

	var document strings.Builder
	document.WriteString(header)
	document.WriteString("<w:body>")
	for page := 0; page < 72; page++ {
		document.WriteString("<w:p>")
		if page > 0 {
			document.WriteString("<w:pPr><w:pageBreakBefore/></w:pPr>")
		}
		fmt.Fprintf(&document, "<w:r><w:t>项目操作记录 第 %d 页</w:t></w:r></w:p>", page+1)
		for paragraph := 1; paragraph <= 16; paragraph++ {
			fmt.Fprintf(&document, "<w:p><w:r><w:t>记录 %03d-%02d。此次检查覆盖文件打开、中文编辑与表格资料的日常整理。This is a synthetic paragraph for a local editing test.</w:t></w:r></w:p>", page+1, paragraph)
		}
		if page%3 == 0 {
			index := page/3 + 1
			imageXML := strings.ReplaceAll(drawing, "rIdImage", fmt.Sprintf("rIdQA%d", index))
			imageXML = strings.ReplaceAll(imageXML, `id="1"`, fmt.Sprintf(`id="%d"`, index))
			imageXML = strings.ReplaceAll(imageXML, `cx="914400" cy="914400"`, `cx="3657600" cy="2057400"`)
			fmt.Fprintf(&document, "<w:p><w:r><w:drawing>%s</w:drawing></w:r></w:p>", imageXML)
		}
	}
	document.WriteString("<w:sectPr>")
	document.WriteString(section)

	var relationships strings.Builder
	relationships.WriteString(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for index := range pictures {
		fmt.Fprintf(&relationships, `<Relationship Id="rIdQA%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/page-%d.jpg"/>`, index+1, index+1)
	}
	relationships.WriteString("</Relationships>")
	types := strings.Replace(string(zipPart(t, sample, "[Content_Types].xml")), "</Types>", `<Default Extension="jpg" ContentType="image/jpeg"/></Types>`, 1)
	var result bytes.Buffer
	archive := zip.NewWriter(&result)
	add := func(path string, data []byte) {
		t.Helper()
		file, err := archive.Create(path)
		if err == nil {
			_, err = file.Write(data)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	add("[Content_Types].xml", []byte(types))
	add("_rels/.rels", zipPart(t, sample, "_rels/.rels"))
	add("word/_rels/document.xml.rels", []byte(relationships.String()))
	add("word/document.xml", []byte(document.String()))
	for index, picture := range pictures {
		add(fmt.Sprintf("word/media/page-%d.jpg", index+1), picture)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return result.Bytes()
}

func makeLargePDF(pictures [][]byte) []byte {
	const pages = 120
	firstPage := 4 + len(pictures)
	offsets := make([]int, firstPage+2*pages)
	var data bytes.Buffer
	data.WriteString("%PDF-1.4\n")
	begin := func(number int) {
		offsets[number] = data.Len()
		fmt.Fprintf(&data, "%d 0 obj\n", number)
	}
	begin(1)
	data.WriteString("<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	begin(2)
	fmt.Fprintf(&data, "<< /Type /Pages /Count %d /Kids [", pages)
	for page := 0; page < pages; page++ {
		fmt.Fprintf(&data, " %d 0 R", firstPage+2*page)
	}
	data.WriteString(" ] >>\nendobj\n")
	begin(3)
	data.WriteString("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n")
	for index, picture := range pictures {
		begin(4 + index)
		fmt.Fprintf(&data, "<< /Type /XObject /Subtype /Image /Width 960 /Height 540 /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n", len(picture))
		data.Write(picture)
		data.WriteString("\nendstream\nendobj\n")
	}
	for page := 0; page < pages; page++ {
		number := firstPage + 2*page
		begin(number)
		fmt.Fprintf(&data, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 600 800] /Resources << /Font << /F1 3 0 R >> /XObject << /Im1 %d 0 R >> >> /Contents %d 0 R >>\nendobj\n", 4+page%len(pictures), number+1)
		begin(number + 1)
		content := fmt.Sprintf("BT /F1 24 Tf 55 730 Td (Viva Editor synthetic report, page %d) Tj ET q 480 0 0 270 60 410 cm /Im1 Do Q", page+1)
		fmt.Fprintf(&data, "<< /Length %d >>\nstream\n%s\nendstream\nendobj\n", len(content), content)
	}
	xref := data.Len()
	fmt.Fprintf(&data, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&data, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&data, "trailer\n<< /Root 1 0 R /Size %d >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	return data.Bytes()
}
