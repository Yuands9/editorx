package editor

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"reflect"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

type officeColor struct {
	RGB string `json:"rgb,omitempty"`
}
type officeDecoration struct {
	S int `json:"s"`
}
type officeNumberFormat struct {
	Pattern string `json:"pattern"`
}
type officeBorder struct {
	Style int         `json:"s"`
	Color officeColor `json:"cl"`
}
type officeRotation struct {
	Angle    int `json:"a"`
	Vertical int `json:"v,omitempty"`
}
type officeCellStyle struct {
	Border     map[string]officeBorder `json:"bd,omitempty"`
	Rotation   *officeRotation         `json:"tr,omitempty"`
	Shrink     int                     `json:"stf,omitempty"`
	Font       string                  `json:"ff,omitempty"`
	Size       float64                 `json:"fs,omitempty"`
	Bold       int                     `json:"bl,omitempty"`
	Italic     int                     `json:"it,omitempty"`
	Underline  *officeDecoration       `json:"ul,omitempty"`
	Strike     *officeDecoration       `json:"st,omitempty"`
	Color      *officeColor            `json:"cl,omitempty"`
	Background *officeColor            `json:"bg,omitempty"`
	Horizontal int                     `json:"ht,omitempty"`
	Vertical   int                     `json:"vt,omitempty"`
	Wrap       int                     `json:"tb,omitempty"`
	Number     *officeNumberFormat     `json:"n,omitempty"`
}
type officeCell struct {
	Value   any             `json:"v,omitempty"`
	Type    int             `json:"t,omitempty"`
	Formula string          `json:"f,omitempty"`
	Style   json.RawMessage `json:"s,omitempty"`
	Rich    json.RawMessage `json:"p,omitempty"`
}
type officeRange struct {
	StartRow    int `json:"startRow"`
	EndRow      int `json:"endRow"`
	StartColumn int `json:"startColumn"`
	EndColumn   int `json:"endColumn"`
}
type officeRow struct {
	Height float64 `json:"h,omitempty"`
	Hidden int     `json:"hd,omitempty"`
}
type officeColumn struct {
	Width  float64 `json:"w,omitempty"`
	Hidden int     `json:"hd,omitempty"`
}
type officeSheet struct {
	ID                 string                      `json:"id"`
	Name               string                      `json:"name"`
	Hidden             int                         `json:"hidden"`
	RowCount           int                         `json:"rowCount"`
	ColumnCount        int                         `json:"columnCount"`
	Cells              map[int]map[int]*officeCell `json:"cellData"`
	Merges             []officeRange               `json:"mergeData"`
	Rows               map[int]officeRow           `json:"rowData"`
	Columns            map[int]officeColumn        `json:"columnData"`
	DefaultColumnWidth float64                     `json:"defaultColumnWidth"`
	DefaultRowHeight   float64                     `json:"defaultRowHeight"`
	Gridlines          int                         `json:"showGridlines"`
}
type officeWorkbook struct {
	ID         string                      `json:"id"`
	Name       string                      `json:"name"`
	Version    string                      `json:"appVersion"`
	Locale     string                      `json:"locale"`
	DateSystem int                         `json:"dateSystem,omitempty"`
	Order      []string                    `json:"sheetOrder"`
	Sheets     map[string]*officeSheet     `json:"sheets"`
	Styles     map[string]*officeCellStyle `json:"styles"`
}

func readWorkbook(data []byte, name string) (*officeWorkbook, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	parts := map[string]*zip.File{}
	for _, entry := range archive.File {
		parts[entry.Name] = entry
	}
	decode := func(name string, value any) error {
		entry := parts[name]
		if entry == nil {
			return fmt.Errorf("缺少工作簿内容 %s", name)
		}
		stream, e := entry.Open()
		if e != nil {
			return e
		}
		defer stream.Close()
		return xml.NewDecoder(stream).Decode(value)
	}
	var metadata struct {
		Sheets []struct {
			Name  string `xml:"name,attr"`
			ID    string `xml:"id,attr"`
			State string `xml:"state,attr"`
		} `xml:"sheets>sheet"`
	}
	var relations struct {
		Items []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if err = decode("xl/workbook.xml", &metadata); err != nil {
		return nil, err
	}
	if err = decode("xl/_rels/workbook.xml.rels", &relations); err != nil {
		return nil, err
	}
	targets := map[string]string{}
	for _, rel := range relations.Items {
		target := path.Clean(path.Join("xl", rel.Target))
		if strings.HasPrefix(rel.Target, "/") {
			target = strings.TrimPrefix(rel.Target, "/")
		}
		targets[rel.ID] = target
	}
	book := &officeWorkbook{ID: "viva-workbook", Name: name, Version: "1.5.2", Locale: "zhCN", Sheets: map[string]*officeSheet{}, Styles: map[string]*officeCellStyle{}}
	if props, e := f.GetWorkbookProps(); e == nil && props.Date1904 != nil && *props.Date1904 {
		book.DateSystem = 1
	}
	for index, meta := range metadata.Sheets {
		var sheetXML struct {
			Rows []struct {
				Number int     `xml:"r,attr"`
				Height float64 `xml:"ht,attr"`
				Hidden bool    `xml:"hidden,attr"`
				Cells  []struct {
					Ref   string `xml:"r,attr"`
					Style int    `xml:"s,attr"`
				} `xml:"c"`
			} `xml:"sheetData>row"`
			Columns []struct {
				Min    int     `xml:"min,attr"`
				Max    int     `xml:"max,attr"`
				Width  float64 `xml:"width,attr"`
				Hidden bool    `xml:"hidden,attr"`
			} `xml:"cols>col"`
		}
		if err = decode(targets[meta.ID], &sheetXML); err != nil {
			return nil, err
		}
		id := strconv.Itoa(index)
		sheet := &officeSheet{ID: id, Name: meta.Name, RowCount: 1000, ColumnCount: 26, DefaultColumnWidth: 96, DefaultRowHeight: 24, Gridlines: 1, Cells: map[int]map[int]*officeCell{}, Rows: map[int]officeRow{}, Columns: map[int]officeColumn{}, Merges: []officeRange{}}
		if meta.State == "hidden" {
			sheet.Hidden = 1
		}
		if meta.State == "veryHidden" {
			sheet.Hidden = 2
		}
		for _, row := range sheetXML.Rows {
			if row.Height > 0 || row.Hidden {
				sheet.Rows[row.Number-1] = officeRow{Height: row.Height * 4 / 3, Hidden: boolNumber(row.Hidden)}
			}
			sheet.RowCount = min(1048576, max(sheet.RowCount, row.Number+100))
			for _, item := range row.Cells {
				column, r, e := excelize.CellNameToCoordinates(item.Ref)
				if e != nil {
					return nil, e
				}
				sheet.ColumnCount = min(16384, max(sheet.ColumnCount, column+5))
				value, e := f.GetCellValue(meta.Name, item.Ref, excelize.Options{RawCellValue: true})
				if e != nil {
					return nil, e
				}
				cell := &officeCell{Value: value, Type: 1}
				kind, e := f.GetCellType(meta.Name, item.Ref)
				if e != nil {
					return nil, e
				}
				if kind == excelize.CellTypeNumber || kind == excelize.CellTypeUnset {
					if number, e := strconv.ParseFloat(value, 64); e == nil {
						cell.Value, cell.Type = number, 2
					}
				}
				if kind == excelize.CellTypeBool {
					cell.Value, cell.Type = value == "1" || value == "TRUE", 3
				}
				formula, e := f.GetCellFormula(meta.Name, item.Ref)
				if e != nil {
					return nil, e
				}
				if formula != "" {
					cell.Formula = "=" + formula
					if value == "" {
						cell.Value, cell.Type = nil, 0
					}
				}
				key := strconv.Itoa(item.Style)
				cell.Style, _ = json.Marshal(key)
				if _, exists := book.Styles[key]; !exists {
					style, e := f.GetStyle(item.Style)
					if e != nil {
						return nil, e
					}
					book.Styles[key] = importCellStyle(style)
				}
				if sheet.Cells[r-1] == nil {
					sheet.Cells[r-1] = map[int]*officeCell{}
				}
				sheet.Cells[r-1][column-1] = cell
			}
		}
		for _, col := range sheetXML.Columns {
			for c := col.Min; c <= col.Max; c++ {
				sheet.Columns[c-1] = officeColumn{Width: col.Width*7 + 5, Hidden: boolNumber(col.Hidden)}
			}
		}
		merges, e := f.GetMergeCells(meta.Name, true)
		if e != nil {
			return nil, e
		}
		for _, merged := range merges {
			c1, r1, e := excelize.CellNameToCoordinates(merged.GetStartAxis())
			if e != nil {
				return nil, e
			}
			c2, r2, e := excelize.CellNameToCoordinates(merged.GetEndAxis())
			if e != nil {
				return nil, e
			}
			sheet.Merges = append(sheet.Merges, officeRange{r1 - 1, r2 - 1, c1 - 1, c2 - 1})
		}
		book.Order = append(book.Order, id)
		book.Sheets[id] = sheet
	}
	return book, nil
}
func boolNumber(value bool) int {
	if value {
		return 1
	}
	return 0
}
func importCellStyle(style *excelize.Style) *officeCellStyle {
	out := &officeCellStyle{}
	for _, border := range style.Border {
		key := map[string]string{"left": "l", "right": "r", "top": "t", "bottom": "b", "diagonalDown": "tl_br", "diagonalUp": "bl_tr"}[border.Type]
		if key != "" && border.Style != 0 {
			if out.Border == nil {
				out.Border = map[string]officeBorder{}
			}
			out.Border[key] = officeBorder{Style: map[int]int{1: 1, 2: 8, 3: 4, 4: 3, 5: 13, 6: 7, 7: 2, 8: 9, 9: 5, 10: 10, 11: 6, 12: 11, 13: 12}[border.Style], Color: officeColor{RGB: "#" + border.Color}}
		}
	}
	if style.Font != nil {
		font := style.Font
		out.Font, out.Size, out.Bold, out.Italic = font.Family, font.Size, boolNumber(font.Bold), boolNumber(font.Italic)
		if font.Color != "" {
			out.Color = &officeColor{"#" + font.Color}
		}
		if font.Underline != "" {
			out.Underline = &officeDecoration{1}
		}
		if font.Strike {
			out.Strike = &officeDecoration{1}
		}
	}
	if style.Fill.Type == "pattern" && style.Fill.Pattern == 1 && len(style.Fill.Color) > 0 {
		out.Background = &officeColor{"#" + style.Fill.Color[0]}
	}
	if a := style.Alignment; a != nil {
		out.Shrink = boolNumber(a.ShrinkToFit)
		if a.TextRotation != 0 {
			out.Rotation = &officeRotation{Angle: a.TextRotation}
			if a.TextRotation == 255 {
				out.Rotation.Angle, out.Rotation.Vertical = 0, 1
			}
		}
		out.Horizontal = map[string]int{"left": 1, "center": 2, "right": 3, "justify": 4, "distributed": 6}[a.Horizontal]
		out.Vertical = map[string]int{"top": 1, "center": 2, "bottom": 3}[a.Vertical]
		if a.WrapText {
			out.Wrap = 3
		}
	}
	if style.CustomNumFmt != nil {
		out.Number = &officeNumberFormat{*style.CustomNumFmt}
	} else if pattern := map[int]string{1: "0", 2: "0.00", 3: "#,##0", 4: "#,##0.00", 9: "0%", 10: "0.00%", 14: "yyyy-mm-dd", 15: "d-mmm-yy", 20: "h:mm", 21: "h:mm:ss", 22: "m/d/yy h:mm", 49: "@"}[style.NumFmt]; pattern != "" {
		out.Number = &officeNumberFormat{pattern}
	}
	return out
}
func (book *officeWorkbook) style(cell *officeCell) *officeCellStyle {
	if cell == nil || len(cell.Style) == 0 {
		return &officeCellStyle{}
	}
	var key string
	if json.Unmarshal(cell.Style, &key) == nil {
		if style := book.Styles[key]; style != nil {
			return style
		}
		return &officeCellStyle{}
	}
	var style officeCellStyle
	_ = json.Unmarshal(cell.Style, &style)
	return &style
}
func exportCellStyle(base *excelize.Style, style *officeCellStyle) *excelize.Style {
	if base == nil {
		base = &excelize.Style{}
	}
	if base.Font == nil {
		base.Font = &excelize.Font{}
	}
	base.Font.Family, base.Font.Size, base.Font.Bold, base.Font.Italic = style.Font, style.Size, style.Bold == 1, style.Italic == 1
	base.Font.Strike = style.Strike != nil && style.Strike.S == 1
	base.Font.Underline = ""
	if style.Underline != nil && style.Underline.S == 1 {
		base.Font.Underline = "single"
	}
	if style.Color != nil {
		base.Font.Color = strings.TrimPrefix(style.Color.RGB, "#")
	}
	if style.Background != nil {
		base.Fill = excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{strings.TrimPrefix(style.Background.RGB, "#")}}
	} else {
		base.Fill = excelize.Fill{}
	}
	if base.Alignment == nil {
		base.Alignment = &excelize.Alignment{}
	}
	base.Alignment.Horizontal = map[int]string{1: "left", 2: "center", 3: "right", 4: "justify", 6: "distributed"}[style.Horizontal]
	base.Alignment.Vertical = map[int]string{1: "top", 2: "center", 3: "bottom"}[style.Vertical]
	base.Alignment.WrapText = style.Wrap == 3
	base.Alignment.ShrinkToFit = style.Shrink == 1
	base.Alignment.TextRotation = 0
	if style.Rotation != nil {
		base.Alignment.TextRotation = style.Rotation.Angle
		if style.Rotation.Vertical == 1 {
			base.Alignment.TextRotation = 255
		}
	}
	base.Border = nil
	for key, border := range style.Border {
		name := map[string]string{"l": "left", "r": "right", "t": "top", "b": "bottom", "tl_br": "diagonalDown", "bl_tr": "diagonalUp"}[key]
		if name != "" && border.Style != 0 {
			base.Border = append(base.Border, excelize.Border{Type: name, Color: strings.TrimPrefix(border.Color.RGB, "#"), Style: map[int]int{1: 1, 8: 2, 4: 3, 3: 4, 13: 5, 7: 6, 2: 7, 9: 8, 5: 9, 10: 10, 6: 11, 11: 12, 12: 13}[border.Style]})
		}
	}
	if style.Number != nil {
		base.CustomNumFmt = &style.Number.Pattern
		base.NumFmt = 0
	}
	return base
}
func writeWorkbook(original []byte, baseline, current *officeWorkbook) ([]byte, error) {
	if !reflect.DeepEqual(baseline.Order, current.Order) {
		return nil, fmt.Errorf("当前版本不支持增删或移动工作表")
	}
	f, err := excelize.OpenReader(bytes.NewReader(original))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	for _, id := range baseline.Order {
		before, after := baseline.Sheets[id], current.Sheets[id]
		if after == nil || before.Name != after.Name {
			return nil, fmt.Errorf("当前版本不支持重命名工作表")
		}
		positions := map[[2]int]bool{}
		for r, row := range before.Cells {
			for c := range row {
				positions[[2]int{r, c}] = true
			}
		}
		for r, row := range after.Cells {
			for c := range row {
				positions[[2]int{r, c}] = true
			}
		}
		for position := range positions {
			r, c := position[0], position[1]
			old, next := before.Cells[r][c], after.Cells[r][c]
			if old == nil {
				old = &officeCell{}
			}
			if next == nil {
				next = &officeCell{}
			}
			axis, e := excelize.CoordinatesToCellName(c+1, r+1)
			if e != nil {
				return nil, e
			}
			if !reflect.DeepEqual(old.Rich, next.Rich) && len(next.Rich) > 0 && string(next.Rich) != "null" {
				return nil, fmt.Errorf("%s!%s 含单元格内富文本，请使用普通文字编辑后保存", before.Name, axis)
			}
			valueChanged := old.Formula != next.Formula || (next.Formula == "" && (!reflect.DeepEqual(old.Value, next.Value) || old.Type != next.Type))
			if valueChanged {
				if err = f.SetCellFormula(before.Name, axis, ""); err != nil {
					return nil, err
				}
				if next.Formula != "" {
					err = f.SetCellFormula(before.Name, axis, strings.TrimPrefix(next.Formula, "="))
				} else {
					err = f.SetCellValue(before.Name, axis, next.Value)
				}
				if err != nil {
					return nil, err
				}
			}
			if !reflect.DeepEqual(baseline.style(old), current.style(next)) {
				oldID, e := f.GetCellStyle(before.Name, axis)
				if e != nil {
					return nil, e
				}
				style, e := f.GetStyle(oldID)
				if e != nil {
					return nil, e
				}
				styleID, e := f.NewStyle(exportCellStyle(style, current.style(next)))
				if e != nil {
					return nil, e
				}
				if e = f.SetCellStyle(before.Name, axis, axis, styleID); e != nil {
					return nil, e
				}
			}
		}
		if !reflect.DeepEqual(before.Merges, after.Merges) {
			for _, merge := range before.Merges {
				a, _ := excelize.CoordinatesToCellName(merge.StartColumn+1, merge.StartRow+1)
				b, _ := excelize.CoordinatesToCellName(merge.EndColumn+1, merge.EndRow+1)
				if err = f.UnmergeCell(before.Name, a, b); err != nil {
					return nil, err
				}
			}
			for _, merge := range after.Merges {
				a, e := excelize.CoordinatesToCellName(merge.StartColumn+1, merge.StartRow+1)
				if e != nil {
					return nil, e
				}
				b, e := excelize.CoordinatesToCellName(merge.EndColumn+1, merge.EndRow+1)
				if e != nil {
					return nil, e
				}
				if err = f.MergeCell(before.Name, a, b); err != nil {
					return nil, err
				}
			}
		}
		for r, row := range after.Rows {
			if before.Rows[r] != row {
				if row.Height > 0 {
					if err = f.SetRowHeight(before.Name, r+1, row.Height*3/4); err != nil {
						return nil, err
					}
				}
				if err = f.SetRowVisible(before.Name, r+1, row.Hidden != 1); err != nil {
					return nil, err
				}
			}
		}
		for c, col := range after.Columns {
			if before.Columns[c] != col {
				axis, e := excelize.ColumnNumberToName(c + 1)
				if e != nil {
					return nil, e
				}
				if col.Width > 0 {
					if err = f.SetColWidth(before.Name, axis, axis, max(0, (col.Width-5)/7)); err != nil {
						return nil, err
					}
				}
				if err = f.SetColVisible(before.Name, axis, col.Hidden != 1); err != nil {
					return nil, err
				}
			}
		}
	}
	recalculate := true
	if err = f.SetCalcProps(&excelize.CalcPropsOptions{FullCalcOnLoad: &recalculate, ForceFullCalc: &recalculate}); err != nil {
		return nil, err
	}
	buffer, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func validateDocx(data []byte) error {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("无效的 DOCX 文件：%w", err)
	}
	for _, entry := range reader.File {
		if entry.Name == "word/document.xml" {
			stream, e := entry.Open()
			if e != nil {
				return e
			}
			defer stream.Close()
			decoder := xml.NewDecoder(io.LimitReader(stream, 64<<20))
			for {
				_, e = decoder.Token()
				if e == io.EOF {
					return nil
				}
				if e != nil {
					return e
				}
			}
		}
	}
	return fmt.Errorf("文件缺少 Word 正文 word/document.xml")
}
