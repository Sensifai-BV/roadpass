package report

import (
	"encoding/csv"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

// XLSX renders a workbook: a Summary sheet (title, notice, figures,
// fingerprint) and one sheet per table, with typed cells, a frozen and
// filtered header row, and number formats.
func (r *Report) XLSX(w io.Writer) error {
	f := excelize.NewFile()
	defer f.Close()
	_ = f.SetDocProps(&excelize.DocProperties{Title: r.Title, Creator: "ROADPASS", Description: "Fingerprint sha256:" + r.Fingerprint()})
	title, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Size: 16}})
	bold, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	mutedSt, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Color: "80808A", Size: 9}})
	head, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Color: "3F3F46"}, Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"F2F2F4"}},
		Border: []excelize.Border{{Type: "bottom", Color: "D4D4D8", Style: 1}}})
	notice, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Color: "92400E"}, Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"FEF3C7"}}})
	dateSt, _ := f.NewStyle(&excelize.Style{NumFmt: 22}) // m/d/yy h:mm, localised by Excel

	const summary = "Summary"
	_ = f.SetSheetName("Sheet1", summary)
	set := func(cell string, v any, style int) {
		_ = f.SetCellValue(summary, cell, v)
		if style != 0 {
			_ = f.SetCellStyle(summary, cell, cell, style)
		}
	}
	set("A1", r.Title, title)
	row := 2
	if r.Subtitle != "" {
		set("A2", r.Subtitle, 0)
		row++
	}
	meta := nonEmpty(r.Org, r.Period)
	if b := r.Brand; b != nil {
		meta = append(meta, nonEmpty(b.Legal, strings.ReplaceAll(b.Address, "\n", ", "), b.Contact, b.Footer)...)
	}
	for _, m := range meta {
		set(fmt.Sprintf("A%d", row), m, mutedSt)
		row++
	}
	if r.Notice != "" {
		set(fmt.Sprintf("A%d", row), r.Notice, notice)
		row++
	}
	row++
	for _, k := range r.KPIs {
		set(fmt.Sprintf("A%d", row), k.Label, bold)
		set(fmt.Sprintf("B%d", row), k.Value, 0)
		set(fmt.Sprintf("C%d", row), k.Hint, mutedSt)
		row++
	}
	row++
	set(fmt.Sprintf("A%d", row), "Generated "+r.Generated.UTC().Format("2006-01-02 15:04 UTC")+" · fingerprint sha256:"+r.Fingerprint(), mutedSt)
	_ = f.SetColWidth(summary, "A", "A", 34)
	_ = f.SetColWidth(summary, "B", "B", 22)
	_ = f.SetColWidth(summary, "C", "C", 48)

	used := map[string]bool{summary: true}
	for i, s := range r.Sections {
		t := s.Table
		if t == nil && s.Chart != nil {
			t = &Table{Columns: []Column{{Title: "Label"}, {Title: s.Chart.Unit}}}
			for j, v := range s.Chart.Values {
				if j < len(s.Chart.Labels) {
					t.Rows = append(t.Rows, []any{s.Chart.Labels[j], v})
				}
			}
		}
		if t == nil {
			continue
		}
		name := sheetName(t.Name, s.Heading, i, used)
		if _, err := f.NewSheet(name); err != nil {
			return err
		}
		for c, col := range t.Columns {
			cell, _ := excelize.CoordinatesToCellName(c+1, 1)
			_ = f.SetCellValue(name, cell, col.Title)
			_ = f.SetCellStyle(name, cell, cell, head)
			colName, _ := excelize.ColumnNumberToName(c + 1)
			wd := 14.0 * weight(col)
			if wd > 60 {
				wd = 60
			}
			_ = f.SetColWidth(name, colName, colName, wd)
			if col.Format != "" {
				st, _ := f.NewStyle(&excelize.Style{CustomNumFmt: &col.Format})
				_ = f.SetColStyle(name, colName, st)
			}
		}
		write := func(rowIdx int, cells []any, isTotal bool) {
			for c := range t.Columns {
				var v any
				if c < len(cells) {
					v = cells[c]
				}
				cell, _ := excelize.CoordinatesToCellName(c+1, rowIdx)
				switch x := v.(type) {
				case time.Time:
					if !x.IsZero() {
						_ = f.SetCellValue(name, cell, x.UTC())
						_ = f.SetCellStyle(name, cell, cell, dateSt)
					}
				case *time.Time:
					if x != nil {
						_ = f.SetCellValue(name, cell, x.UTC())
						_ = f.SetCellStyle(name, cell, cell, dateSt)
					}
				case nil:
				default:
					_ = f.SetCellValue(name, cell, x)
				}
				if isTotal {
					_ = f.SetCellStyle(name, cell, cell, bold)
				}
			}
		}
		for j, cells := range t.Rows {
			write(j+2, cells, false)
		}
		if len(t.Total) > 0 {
			write(len(t.Rows)+2, t.Total, true)
		}
		last, _ := excelize.CoordinatesToCellName(len(t.Columns), max(2, len(t.Rows)+1))
		_ = f.AutoFilter(name, "A1:"+last, nil)
		_ = f.SetPanes(name, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
	}
	return f.Write(w)
}

func sheetName(name, heading string, i int, used map[string]bool) string {
	n := name
	if n == "" {
		n = heading
	}
	// Excel: at most 31 characters, none of []:*?/\
	clean := []rune{}
	for _, c := range n {
		switch c {
		case '[', ']', ':', '*', '?', '/', '\\':
			continue
		}
		clean = append(clean, c)
	}
	if len(clean) > 28 {
		clean = clean[:28]
	}
	n = string(clean)
	if n == "" {
		n = fmt.Sprintf("Table %d", i+1)
	}
	base := n
	for k := 2; used[n]; k++ {
		n = fmt.Sprintf("%s %d", base, k)
	}
	used[n] = true
	return n
}

// CSV writes one table (by index among the tables) as UTF-8 CSV with a BOM, so
// Excel opens accented names correctly.
func (r *Report) CSV(w io.Writer, index int) error {
	var tables []*Table
	for _, s := range r.Sections {
		if s.Table != nil {
			tables = append(tables, s.Table)
		}
	}
	if index < 0 || index >= len(tables) {
		if len(tables) == 0 {
			return fmt.Errorf("this report has no table to export as CSV")
		}
		index = 0
	}
	t := tables[index]
	if _, err := io.WriteString(w, "\xEF\xBB\xBF"); err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	head := make([]string, len(t.Columns))
	for i, c := range t.Columns {
		head[i] = c.Title
	}
	if err := cw.Write(head); err != nil {
		return err
	}
	for _, row := range t.Rows {
		rec := make([]string, len(t.Columns))
		for i := range t.Columns {
			if i < len(row) {
				rec[i] = csvCell(row[i])
			}
		}
		if err := cw.Write(rec); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// csvCell writes numbers plainly (no thousands separators) and dates in ISO 8601.
// Cells that a spreadsheet would run as formulas are neutralised.
func csvCell(v any) string {
	switch x := v.(type) {
	case float64:
		return fmt.Sprintf("%g", x)
	case float32:
		return fmt.Sprintf("%g", x)
	case int, int64:
		return fmt.Sprint(x)
	case time.Time:
		if x.IsZero() {
			return ""
		}
		return x.UTC().Format(time.RFC3339)
	case *time.Time:
		if x == nil {
			return ""
		}
		return x.UTC().Format(time.RFC3339)
	}
	s := cellText(v)
	if s != "" && (s[0] == '=' || s[0] == '+' || s[0] == '-' || s[0] == '@' || s[0] == '\t' || s[0] == '\r') {
		return "'" + s
	}
	return s
}
