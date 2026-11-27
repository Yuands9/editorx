package editor

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"html/template"
	"io"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
)

//go:embed assets/word-preview.html
var wordPreviewPage string

var wordPreviewTemplate = template.Must(template.New("word").Parse(wordPreviewPage))

const wordMainNamespace = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
const wordStrictNamespace = "http://purl.oclc.org/ooxml/wordprocessingml/main"

type wordPreviewHeading struct {
	ID, Depth int
	Text      string
}

type wordPreviewContent struct {
	Title    string
	Light    bool
	Body     template.HTML
	Headings []wordPreviewHeading
}

type wordPreviewReader struct {
	parts      map[string]*zip.File
	styles     map[string]int
	images     map[string]*zip.File
	imageBytes int
}

func readWordPreview(filename string) (wordPreviewContent, error) {
	book, err := zip.OpenReader(filename)
	if book != nil {
		defer book.Close()
	}
	if err != nil {
		return wordPreviewContent{}, err
	}
	return decodeWordPreview(&book.Reader)
}

func decodeWordPreview(book *zip.Reader) (wordPreviewContent, error) {
	reader := wordPreviewReader{
		parts: make(map[string]*zip.File, len(book.File)), styles: make(map[string]int),
		images: make(map[string]*zip.File), imageBytes: 20 << 20,
	}
	for _, part := range book.File {
		reader.parts[part.Name] = part
	}
	data, err := readWordPart(reader.parts["word/document.xml"], 24<<20)
	if err != nil {
		return wordPreviewContent{}, fmt.Errorf(uiText("无法读取 Word 正文：%w"), err)
	}
	reader.readStyles()
	reader.readImages()
	return reader.readBody(xml.NewDecoder(bytes.NewReader(data)))
}

func readWordPart(part *zip.File, limit int) ([]byte, error) {
	if part == nil {
		return nil, os.ErrNotExist
	}
	if part.UncompressedSize64 > uint64(limit) {
		return nil, errors.New(uiText("内容超过预览大小限制"))
	}
	stream, err := part.Open()
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	data, err := io.ReadAll(io.LimitReader(stream, int64(limit)+1))
	if len(data) > limit {
		return nil, errors.New(uiText("内容超过预览大小限制"))
	}
	return data, err
}

type wordValue struct {
	Value string `xml:"val,attr"`
}

func (reader *wordPreviewReader) readStyles() {
	data, err := readWordPart(reader.parts["word/styles.xml"], 4<<20)
	if err != nil {
		return
	}
	var document struct {
		Styles []struct {
			ID      string     `xml:"styleId,attr"`
			Name    wordValue  `xml:"name"`
			Outline *wordValue `xml:"pPr>outlineLvl"`
		} `xml:"style"`
	}
	if xml.Unmarshal(data, &document) != nil {
		return
	}
	for _, style := range document.Styles {
		level := max(wordStyleHeading(style.ID), wordStyleHeading(style.Name.Value))
		if style.Outline != nil {
			level = wordOutlineLevel(style.Outline.Value)
		}
		reader.styles[style.ID] = level
	}
}

func wordStyleHeading(style string) int {
	style = strings.ToLower(strings.TrimSpace(style))
	for _, prefix := range []string{"heading", "标题"} {
		if suffix, ok := strings.CutPrefix(style, prefix); ok {
			if level, err := strconv.Atoi(strings.TrimSpace(suffix)); err == nil && level >= 1 && level <= 9 {
				return level
			}
		}
	}
	return 0
}

func wordOutlineLevel(value string) int {
	level, err := strconv.Atoi(value)
	if err != nil || level < 0 || level >= 9 {
		return 0
	}
	return level + 1
}

func (reader *wordPreviewReader) readImages() {
	data, err := readWordPart(reader.parts["word/_rels/document.xml.rels"], 4<<20)
	if err != nil {
		return
	}
	var relations struct {
		Items []struct {
			ID     string `xml:"Id,attr"`
			Type   string `xml:"Type,attr"`
			Target string `xml:"Target,attr"`
			Mode   string `xml:"TargetMode,attr"`
		} `xml:"Relationship"`
	}
	if xml.Unmarshal(data, &relations) != nil {
		return
	}
	for _, item := range relations.Items {
		if !strings.HasSuffix(item.Type, "/image") || strings.EqualFold(item.Mode, "External") {
			continue
		}
		name := path.Join("word", item.Target)
		if strings.HasPrefix(item.Target, "/") {
			name = path.Clean(strings.TrimPrefix(item.Target, "/"))
		}
		if strings.HasPrefix(name, "word/media/") {
			reader.images[item.ID] = reader.parts[name]
		}
	}
}

func (reader *wordPreviewReader) writeImage(output *strings.Builder, id string) {
	part := reader.images[id]
	if part == nil || reader.imageBytes == 0 {
		return
	}
	data, err := readWordPart(part, min(reader.imageBytes, 8<<20))
	if err != nil {
		return
	}
	mime := http.DetectContentType(data)
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "image/bmp":
		reader.imageBytes -= len(data)
		output.WriteString(`<img alt="" src="data:` + mime + ";base64," + base64.StdEncoding.EncodeToString(data) + `">`)
	}
}

func wordElement(name xml.Name) bool {
	return name.Space == wordMainNamespace || name.Space == wordStrictNamespace
}

func wordAttribute(element xml.StartElement, name string) string {
	for _, attribute := range element.Attr {
		if attribute.Name.Local == name {
			return attribute.Value
		}
	}
	return ""
}

func (reader *wordPreviewReader) readBody(decoder *xml.Decoder) (wordPreviewContent, error) {
	var content wordPreviewContent
	var output strings.Builder
	body := false
	for {
		token, err := decoder.Token()
		if err != nil {
			if errors.Is(err, io.EOF) && body {
				// Only renderer-generated markup and escaped document text reach the template.
				content.Body = template.HTML(output.String())
				return content, nil
			}
			return wordPreviewContent{}, fmt.Errorf(uiText("Word 正文格式无效：%w"), err)
		}
		switch element := token.(type) {
		case xml.StartElement:
			if !wordElement(element.Name) {
				continue
			}
			if element.Name.Local == "body" {
				body = true
			}
			if !body {
				continue
			}
			switch element.Name.Local {
			case "p":
				if err := reader.writeParagraph(decoder, &output, &content); err != nil {
					return wordPreviewContent{}, err
				}
			case "tbl":
				output.WriteString("<table>")
			case "tr":
				output.WriteString("<tr>")
			case "tc":
				output.WriteString("<td>")
			}
		case xml.EndElement:
			if !body || !wordElement(element.Name) {
				continue
			}
			switch element.Name.Local {
			case "tbl":
				output.WriteString("</table>")
			case "tr":
				output.WriteString("</tr>")
			case "tc":
				output.WriteString("</td>")
			}
		}
	}
}

func (reader *wordPreviewReader) writeParagraph(decoder *xml.Decoder, output *strings.Builder, content *wordPreviewContent) error {
	var paragraph, title strings.Builder
	styleLevel, outlineLevel := 0, -1
	for depth := 1; depth > 0; {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		switch element := token.(type) {
		case xml.StartElement:
			depth++
			if wordElement(element.Name) {
				switch element.Name.Local {
				case "pStyle":
					style := wordAttribute(element, "val")
					var found bool
					if styleLevel, found = reader.styles[style]; !found {
						styleLevel = wordStyleHeading(style)
					}
				case "outlineLvl":
					outlineLevel = wordOutlineLevel(wordAttribute(element, "val"))
				case "t":
					var value string
					if err := decoder.DecodeElement(&value, &element); err != nil {
						return err
					}
					depth--
					paragraph.WriteString(html.EscapeString(value))
					title.WriteString(value)
				case "br", "cr":
					paragraph.WriteString("<br>")
				case "tab":
					paragraph.WriteByte('\t')
					title.WriteByte(' ')
				}
			} else if element.Name.Local == "blip" {
				reader.writeImage(&paragraph, wordAttribute(element, "embed"))
			}
		case xml.EndElement:
			depth--
		}
	}
	level := styleLevel
	if outlineLevel >= 0 {
		level = outlineLevel
	}
	if label := strings.TrimSpace(title.String()); level > 0 && label != "" {
		id := len(content.Headings) + 1
		content.Headings = append(content.Headings, wordPreviewHeading{ID: id, Depth: level - 1, Text: label})
		tag := min(level, 6)
		fmt.Fprintf(output, `<h%d id="heading-%d">%s</h%d>`, tag, id, paragraph.String(), tag)
		return nil
	}
	output.WriteString("<p>" + paragraph.String() + "</p>")
	return nil
}
