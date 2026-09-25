package report

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

func sample() *Report {
	r := &Report{Title: "Monthly compliance report", Subtitle: "Recycled plastic in asphalt · limits, exceedances and data completeness",
		Org: "Nuevafilm Reciclados S.L.", Period: "September 2026", Generated: time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC), By: "Ana García",
		Notice: "Simulated demonstration data: figures do not describe real operations.",
		KPIs:   []KPI{{"Limits", "4", "configured"}, {"Exceedances", "7", "2 critical"}, {"Data completeness", "98.6 %", "readings received"}, {"Recycled plastic", "1,240 t", "this month"}}}
	tbl := &Table{Name: "Exceedances", Columns: []Column{{Title: "When", Weight: 1.4}, {Title: "Limit", Weight: 2}, {Title: "Value", Align: Right, Format: "0.0"}, {Title: "Note", Weight: 2.5}}}
	for i := range 80 {
		tbl.Rows = append(tbl.Rows, []any{time.Date(2026, 9, 1+i%30, 8, i%60, 0, 0, time.UTC), "Melt temperature ≤ 250 °C", 251.5 + float64(i)/10, "Line 2 — operator notified"})
	}
	tbl.Rows = append(tbl.Rows, []any{time.Time{}, "Formula probe", 1.0, "=HYPERLINK(\"http://evil\",\"x\")"})
	tbl.Total = []any{"", "Total", 81.0, ""}
	r.Sections = []Section{
		{Heading: "Summary", Text: []string{"Seven readings exceeded their limits; all were acknowledged within the hour.", "Second paragraph with accents: Cantábrico, Logroño, Göteborg."}},
		{Heading: "Exceedance log", Table: tbl},
		{Heading: "Throughput", Chart: &Chart{Kind: "bars", Labels: []string{"1 Sep", "2 Sep", "3 Sep", "4 Sep"}, Values: []float64{40, 52, 47, 61}, Unit: "t"}},
	}
	r.SignOff = &SignOff{Statement: "I confirm this report reflects the records of the period.", Name: "Ana García", Role: "Quality manager",
		At: time.Date(2026, 10, 16, 9, 5, 0, 0, time.UTC), KeyID: "usr_x:k1", Signature: "c2lnbmF0dXJl"}
	return r
}

func TestFingerprintIgnoresWhenAndWho(t *testing.T) {
	a, b := sample(), sample()
	b.Generated, b.By, b.SignOff = b.Generated.Add(time.Hour), "someone else", nil
	if a.Fingerprint() != b.Fingerprint() {
		t.Fatal("same data must share a fingerprint")
	}
	b.KPIs[1].Value = "8"
	if a.Fingerprint() == b.Fingerprint() {
		t.Fatal("different data must not")
	}
}

func TestEveryFormatRenders(t *testing.T) {
	r := sample()
	out := os.Getenv("ROADPASS_REPORT_SAMPLES") // set to a directory to look at the files
	for f := range Formats {
		var buf bytes.Buffer
		if err := r.Render(&buf, f); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		b := buf.Bytes()
		switch f {
		case "pdf":
			if !bytes.HasPrefix(b, []byte("%PDF-")) || !bytes.Contains(b, []byte("/Count 3")) && !bytes.Contains(b, []byte("/Count 2")) && !bytes.Contains(b, []byte("/Count 4")) {
				t.Fatalf("pdf: not a multi-page PDF (%d bytes)", len(b))
			}
		case "docx":
			z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
			if err != nil {
				t.Fatal(err)
			}
			for _, zf := range z.File {
				if strings.HasSuffix(zf.Name, ".xml") || strings.HasSuffix(zf.Name, ".rels") {
					rc, _ := zf.Open()
					d := xml.NewDecoder(rc)
					for {
						if _, err := d.Token(); err == io.EOF {
							break
						} else if err != nil {
							t.Fatalf("docx %s: %v", zf.Name, err)
						}
					}
					rc.Close()
				}
			}
		case "xlsx":
			x, err := excelize.OpenReader(bytes.NewReader(b))
			if err != nil {
				t.Fatal(err)
			}
			if v, _ := x.GetCellValue("Exceedances", "B2"); v != "Melt temperature ≤ 250 °C" {
				t.Fatalf("xlsx cell: %q", v)
			}
			if typ, _ := x.GetCellType("Exceedances", "C2"); typ != excelize.CellTypeNumber && typ != excelize.CellTypeUnset {
				t.Fatalf("numbers must stay numbers in Excel, got %v", typ)
			}
		case "csv":
			if !bytes.HasPrefix(b, []byte("\xEF\xBB\xBF")) {
				t.Fatal("csv: BOM missing")
			}
			rows, err := csv.NewReader(bytes.NewReader(b[3:])).ReadAll()
			if err != nil || len(rows) != 82 || rows[0][0] != "When" {
				t.Fatalf("csv: %v rows=%d", err, len(rows))
			}
			if last := rows[len(rows)-1][3]; !strings.HasPrefix(last, "'=") {
				t.Fatalf("csv must neutralise formulas, got %q", last)
			}
		}
		if out != "" {
			_ = os.WriteFile(filepath.Join(out, "sample."+Formats[f].Ext), b, 0o644)
		}
	}
}

func TestFileName(t *testing.T) {
	r := sample()
	if got := r.FileName("pdf"); got != "roadpass-monthly-compliance-report-2026-10-16.pdf" {
		t.Fatal(got)
	}
}
