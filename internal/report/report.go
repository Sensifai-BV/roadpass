// Package report renders one report model to PDF, Word (DOCX), Excel (XLSX) and
// CSV, so every page that can be downloaded is downloaded the same way. A report
// carries a fingerprint (SHA-256 of its data) printed on every format, and may
// carry a sign-off: the signer's personal Ed25519 signature over that fingerprint.
package report

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// Report is a document's content, independent of its format.
type Report struct {
	Title     string    `json:"title"`
	Subtitle  string    `json:"subtitle,omitempty"`
	Org       string    `json:"org,omitempty"`
	Period    string    `json:"period,omitempty"`
	Generated time.Time `json:"generated"`
	By        string    `json:"by,omitempty"`
	// Notice is shown prominently, e.g. that the data are simulated.
	Notice   string    `json:"notice,omitempty"`
	KPIs     []KPI     `json:"kpis,omitempty"`
	Sections []Section `json:"sections"`
	SignOff  *SignOff  `json:"signOff,omitempty"`
	// Link is a page to open the item online; the PDF prints it as a QR code.
	Link *Link `json:"link,omitempty"`
	// Brand is the issuing organisation's letterhead. It is presentation only:
	// it is left out of the fingerprint, so a new logo never breaks a signature.
	Brand *Brand `json:"brand,omitempty"`
}

// Brand is an organisation's letterhead on its reports.
type Brand struct {
	Name     string `json:"name"`
	Legal    string `json:"legal,omitempty"`   // legal name, VAT or registration number
	Address  string `json:"address,omitempty"` // postal address, one or more lines
	Contact  string `json:"contact,omitempty"` // e-mail · phone · website
	Footer   string `json:"footer,omitempty"`
	Logo     []byte `json:"-"`
	LogoType string `json:"-"` // image/png | image/jpeg
}

// Link is a URL with a short caption.
type Link struct {
	URL     string `json:"url"`
	Caption string `json:"caption"`
}

// KPI is one headline figure.
type KPI struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Hint  string `json:"hint,omitempty"`
}

// Section is a heading with text, and optionally a table and a chart.
type Section struct {
	Heading string   `json:"heading"`
	Text    []string `json:"text,omitempty"`
	Table   *Table   `json:"table,omitempty"`
	Chart   *Chart   `json:"chart,omitempty"`
}

// Align of a column.
type Align string

// Alignments.
const (
	Left  Align = "left"
	Right Align = "right"
)

// Column of a table.
type Column struct {
	Title string `json:"title"`
	Align Align  `json:"align,omitempty"`
	// Weight is the relative width in PDF and DOCX (default 1).
	Weight float64 `json:"weight,omitempty"`
	// Format is the Excel number format for numeric cells (e.g. "#,##0.0").
	Format string `json:"format,omitempty"`
}

// Table is rows of cells: strings, numbers (float64/int) or times.
type Table struct {
	Name    string   `json:"name,omitempty"` // sheet / file name
	Columns []Column `json:"columns"`
	Rows    [][]any  `json:"rows"`
	// Total is an optional last row, rendered bold.
	Total []any `json:"total,omitempty"`
}

// Chart is a small bar or line chart (PDF only; other formats carry the table).
type Chart struct {
	Kind   string    `json:"kind"` // bars | line
	Labels []string  `json:"labels"`
	Values []float64 `json:"values"`
	Unit   string    `json:"unit,omitempty"`
}

// SignOff is the statement a named person signed.
type SignOff struct {
	Statement string    `json:"statement"`
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	At        time.Time `json:"at"`
	KeyID     string    `json:"keyId,omitempty"`
	Signature string    `json:"signature,omitempty"` // base64 Ed25519 over "roadpass/v1/report\n"+fingerprint
}

// Fingerprint is SHA-256 over the report's data (without the sign-off and the
// generation time), so two exports of the same figures share it.
func (r *Report) Fingerprint() string {
	c := *r
	c.SignOff, c.Generated, c.By, c.Brand = nil, time.Time{}, "", nil
	b, _ := json.Marshal(c)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// SigningMessage is what a sign-off signature covers.
func SigningMessage(fingerprint string) []byte { return []byte("roadpass/v1/report\n" + fingerprint) }

// Formats lists the supported output formats.
var Formats = map[string]struct{ Ext, Type string }{
	"pdf":  {"pdf", "application/pdf"},
	"docx": {"docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
	"xlsx": {"xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
	"csv":  {"csv", "text/csv; charset=utf-8"},
	"json": {"json", "application/json"},
}

// Render writes the report in a format.
func (r *Report) Render(w io.Writer, format string) error {
	switch format {
	case "pdf":
		return r.PDF(w)
	case "docx":
		return r.DOCX(w)
	case "xlsx":
		return r.XLSX(w)
	case "csv":
		return r.CSV(w, 0)
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			*Report
			Fingerprint string `json:"fingerprint"`
		}{r, r.Fingerprint()})
	}
	return fmt.Errorf("unknown format %q", format)
}

// FileName is a safe download name: "<slug>-<date>.<ext>".
func (r *Report) FileName(format string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(r.Title) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			b.WriteRune(c)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "-"):
			b.WriteByte('-')
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "report"
	}
	if len(slug) > 60 {
		slug = slug[:60]
	}
	return fmt.Sprintf("roadpass-%s-%s.%s", slug, r.Generated.Format("2006-01-02"), Formats[format].Ext)
}

// colText formats a cell in its column: a numeric format such as "0.0" or
// "#,##0.00" fixes the decimals, so a column reads consistently.
func colText(v any, c Column) string {
	f, ok := v.(float64)
	if !ok || c.Format == "" {
		return cellText(v)
	}
	dec := 0
	if i := strings.IndexByte(c.Format, '.'); i >= 0 {
		dec = len(strings.TrimRight(c.Format[i+1:], "%"))
	}
	s := fmtInt(int64(f))
	if f < 0 && int64(f) == 0 {
		s = "-0"
	}
	if dec > 0 {
		frac := fmt.Sprintf("%.*f", dec, abs(f-float64(int64(f))))
		if strings.HasPrefix(frac, "1") { // rounding carried over
			return colText(float64(int64(f))+sign(f), Column{Format: c.Format})
		}
		s += frac[1:]
	}
	if strings.HasSuffix(c.Format, "%") {
		s += " %"
	}
	return s
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func sign(f float64) float64 {
	if f < 0 {
		return -1
	}
	return 1
}

// cellText formats a cell for text formats (PDF, DOCX, CSV).
func cellText(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return fmtNum(x)
	case float32:
		return fmtNum(float64(x))
	case int:
		return fmtInt(int64(x))
	case int64:
		return fmtInt(x)
	case time.Time:
		if x.IsZero() {
			return ""
		}
		if x.Hour() == 0 && x.Minute() == 0 && x.Second() == 0 {
			return x.Format("2 Jan 2006")
		}
		return x.Format("2 Jan 2006 15:04")
	case *time.Time:
		if x == nil {
			return ""
		}
		return cellText(*x)
	case bool:
		if x {
			return "yes"
		}
		return "no"
	}
	return fmt.Sprint(v)
}

func fmtInt(n int64) string {
	s := fmt.Sprint(n)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var out []byte
	for i := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, s[i])
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}

func fmtNum(f float64) string {
	if f == float64(int64(f)) && f < 1e15 && f > -1e15 {
		return fmtInt(int64(f))
	}
	ip := int64(f)
	frac := f - float64(ip)
	if frac < 0 {
		frac = -frac
	}
	dec := fmt.Sprintf("%.2f", frac)[1:]
	if f < 0 && ip == 0 {
		return "-0" + dec
	}
	return fmtInt(ip) + dec
}
