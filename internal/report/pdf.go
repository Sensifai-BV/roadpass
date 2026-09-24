package report

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/go-pdf/fpdf"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gomedium"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
	"rsc.io/qr"
)

type rgb struct{ r, g, b int }

var (
	ink    = rgb{17, 17, 19}
	ink2   = rgb{82, 82, 91}
	muted  = rgb{128, 128, 136}
	line   = rgb{228, 228, 231}
	panel  = rgb{246, 246, 248}
	accent = rgb{31, 157, 85}
	warnBg = rgb{254, 243, 199}
	warnFg = rgb{146, 64, 14}
)

// pdfText keeps characters the embedded Go fonts lack readable.
var pdfText = strings.NewReplacer("₂", "2", "→", "->", "≥", ">=", "≤", "<=", "…", "...", "–", "-", "—", "-", "“", "\"", "”", "\"", "‘", "'", "’", "'", "·", "·")

// PDF renders an A4 report: header, notice, figures, sections with tables and
// charts, sign-off, and on every page the fingerprint and page number.
func (r *Report) PDF(w io.Writer) error {
	const margin = 16.0
	p := fpdf.New("P", "mm", "A4", "")
	p.SetMargins(margin, 18, margin)
	p.SetAutoPageBreak(true, 18)
	p.AddUTF8FontFromBytes("go", "", goregular.TTF)
	p.AddUTF8FontFromBytes("go", "B", gobold.TTF)
	p.AddUTF8FontFromBytes("gom", "", gomedium.TTF)
	p.AddUTF8FontFromBytes("mono", "", gomono.TTF)
	p.SetTitle(r.Title, true)
	p.SetAuthor("ROADPASS", true)
	p.SetCreator("ROADPASS", true)
	fp := r.Fingerprint()
	pageW, _ := p.GetPageSize()
	inner := pageW - 2*margin
	color := func(c rgb) { p.SetTextColor(c.r, c.g, c.b) }
	fill := func(c rgb) { p.SetFillColor(c.r, c.g, c.b) }
	draw := func(c rgb) { p.SetDrawColor(c.r, c.g, c.b) }
	t := func(s string) string { return pdfText.Replace(s) }

	p.SetFooterFunc(func() {
		if b := r.Brand; b != nil && b.Footer != "" {
			p.SetY(-16)
			p.SetFont("go", "", 7)
			color(muted)
			p.CellFormat(inner, 4, t(b.Footer), "", 0, "L", false, 0, "")
		}
		p.SetY(-12)
		p.SetFont("mono", "", 6.5)
		color(muted)
		p.CellFormat(inner*0.8, 4, t(fmt.Sprintf("Fingerprint sha256:%s  ·  generated %s", fp, r.Generated.UTC().Format("2006-01-02 15:04 UTC"))), "", 0, "L", false, 0, "")
		p.SetFont("go", "", 7.5)
		p.CellFormat(inner*0.2, 4, fmt.Sprintf("%d / {nb}", p.PageNo()), "", 0, "R", false, 0, "")
	})
	p.AliasNbPages("{nb}")
	p.AddPage()

	// Header: brand, title, meta.
	// The ROADPASS mark: a dark tile, a white road in perspective, a lime centre line.
	fill(ink)
	p.RoundedRect(margin, 14, 7, 7, 1.6, "1234", "F")
	p.SetDrawColor(255, 255, 255)
	p.SetLineWidth(0.55)
	p.SetLineCapStyle("round")
	p.Line(margin+2.0, 19.2, margin+3.2, 15.8)
	p.Line(margin+5.0, 19.2, margin+3.8, 15.8)
	p.SetDrawColor(147, 213, 0)
	p.SetLineWidth(0.45)
	p.Line(margin+3.5, 16.6, margin+3.5, 17.2)
	p.Line(margin+3.5, 17.9, margin+3.5, 18.6)
	p.SetLineCapStyle("butt")
	p.SetFont("go", "B", 9)
	color(ink)
	p.Text(margin+9, 19.2, "ROADPASS")
	// The issuing organisation's letterhead, when it has one: logo, then name.
	brandX := margin + 30.0
	if b := r.Brand; b != nil {
		if len(b.Logo) > 0 {
			typ := map[string]string{"image/png": "PNG", "image/jpeg": "JPG"}[b.LogoType]
			if typ != "" {
				opt := fpdf.ImageOptions{ImageType: typ}
				if info := p.RegisterImageOptionsReader("brandlogo", opt, bytes.NewReader(b.Logo)); info != nil && p.Ok() {
					h := 8.0
					wd := math.Min(28, h*info.Width()/math.Max(1, info.Height()))
					p.ImageOptions("brandlogo", brandX, 13.5, wd, h, false, opt, 0, "")
					brandX += wd + 2.5
				} else {
					p.ClearError()
				}
			}
		}
		p.SetFont("go", "B", 9)
		color(ink)
		p.Text(brandX, 19.2, t(b.Name))
	}
	p.SetFont("go", "", 8)
	color(muted)
	right := strings.Join(nonEmpty(r.Org, r.Period), "  ·  ")
	p.SetXY(margin, 15)
	p.CellFormat(inner, 4, t(right), "", 0, "R", false, 0, "")
	// A QR code to the item online, top right; the title leaves room for it.
	titleW := inner
	qrBottom := 0.0
	if r.Link != nil && r.Link.URL != "" {
		if code, err := qr.Encode(r.Link.URL, qr.M); err == nil {
			const size = 24.0
			opt := fpdf.ImageOptions{ImageType: "PNG"}
			p.RegisterImageOptionsReader("qr", opt, bytes.NewReader(code.PNG()))
			x := margin + inner - size
			p.ImageOptions("qr", x, 24, size, size, false, opt, 0, r.Link.URL)
			p.SetFont("go", "", 6.5)
			color(muted)
			p.SetXY(x-6, 24+size+0.5)
			p.MultiCell(size+6, 3, t(r.Link.Caption), "", "C", false)
			titleW, qrBottom = inner-size-6, p.GetY()
		}
	}
	p.SetY(28)
	p.SetFont("go", "B", 19)
	color(ink)
	p.MultiCell(titleW, 8, t(r.Title), "", "L", false)
	if r.Subtitle != "" {
		p.SetFont("go", "", 10)
		color(ink2)
		p.MultiCell(titleW, 5, t(r.Subtitle), "", "L", false)
	}
	if b := r.Brand; b != nil {
		if who := strings.Join(nonEmpty(b.Legal, strings.ReplaceAll(b.Address, "\n", ", "), b.Contact), "  ·  "); who != "" {
			p.SetFont("go", "", 8)
			color(muted)
			p.MultiCell(titleW, 4, t(who), "", "L", false)
		}
	}
	p.Ln(3)
	if p.GetY() < qrBottom {
		p.SetY(qrBottom + 2)
	}
	if r.Notice != "" {
		y := p.GetY()
		p.SetFont("gom", "", 8.5)
		lines := p.SplitText(t(r.Notice), inner-8)
		h := float64(len(lines))*4.4 + 5
		fill(warnBg)
		p.RoundedRect(margin, y, inner, h, 2, "1234", "F")
		color(warnFg)
		p.SetXY(margin+4, y+2.5)
		p.MultiCell(inner-8, 4.4, t(r.Notice), "", "L", false)
		p.SetY(y + h + 3)
	}

	// Figures: up to four tiles per row.
	if len(r.KPIs) > 0 {
		per := min(4, len(r.KPIs))
		gap := 3.0
		tw := (inner - gap*float64(per-1)) / float64(per)
		for i, k := range r.KPIs {
			if i%per == 0 {
				if i > 0 {
					p.Ln(22 + gap)
				}
				if p.GetY()+22 > 279 {
					p.AddPage()
				}
			}
			x := margin + float64(i%per)*(tw+gap)
			y := p.GetY()
			fill(panel)
			p.RoundedRect(x, y, tw, 22, 2.5, "1234", "F")
			p.SetFont("go", "", 7.5)
			color(muted)
			p.SetXY(x+3.5, y+3)
			p.CellFormat(tw-7, 4, t(k.Label), "", 0, "L", false, 0, "")
			p.SetFont("go", "B", 14)
			color(ink)
			p.SetXY(x+3.5, y+8.5)
			p.CellFormat(tw-7, 7, t(k.Value), "", 0, "L", false, 0, "")
			if k.Hint != "" {
				p.SetFont("go", "", 7)
				color(muted)
				p.SetXY(x+3.5, y+16)
				p.CellFormat(tw-7, 4, t(clip(k.Hint, 60)), "", 0, "L", false, 0, "")
			}
			p.SetY(y)
		}
		p.Ln(22 + 6)
	}

	for _, s := range r.Sections {
		if p.GetY() > 255 {
			p.AddPage()
		}
		p.SetFont("go", "B", 12)
		color(ink)
		p.MultiCell(inner, 6, t(s.Heading), "", "L", false)
		p.Ln(1)
		for _, para := range s.Text {
			p.SetFont("go", "", 9)
			color(ink2)
			p.MultiCell(inner, 4.6, t(para), "", "L", false)
			p.Ln(1.2)
		}
		if s.Chart != nil && len(s.Chart.Values) > 0 {
			r.pdfChart(p, s.Chart, margin, inner)
		}
		if s.Table != nil {
			pdfTable(p, s.Table, margin, inner, t, color, fill, draw)
		}
		p.Ln(5)
	}

	if so := r.SignOff; so != nil {
		if p.GetY() > 230 {
			p.AddPage()
		}
		y := p.GetY()
		draw(line)
		p.SetLineWidth(0.3)
		p.RoundedRect(margin, y, inner, 42, 2.5, "1234", "D")
		p.SetXY(margin+5, y+4)
		p.SetFont("go", "B", 10)
		color(ink)
		p.CellFormat(inner-10, 5, "Sign-off", "", 2, "L", false, 0, "")
		p.SetX(margin + 5)
		p.SetFont("go", "", 8.5)
		color(ink2)
		p.MultiCell(inner-10, 4.3, t(so.Statement), "", "L", false)
		p.SetX(margin + 5)
		p.SetFont("gom", "", 9)
		color(ink)
		p.CellFormat(inner-10, 6, t(fmt.Sprintf("%s, %s  ·  %s", so.Name, so.Role, so.At.UTC().Format("2 Jan 2006 15:04 UTC"))), "", 2, "L", false, 0, "")
		if so.Signature != "" {
			p.SetX(margin + 5)
			p.SetFont("mono", "", 6.5)
			color(muted)
			p.MultiCell(inner-10, 3.2, t(fmt.Sprintf("Ed25519 key %s · signature %s", so.KeyID, so.Signature)), "", "L", false)
		}
		p.SetY(y + 45)
	}
	return p.Output(w)
}

func pdfTable(p *fpdf.Fpdf, tb *Table, margin, inner float64, t func(string) string, color, fill, draw func(rgb)) {
	if len(tb.Columns) == 0 {
		return
	}
	total := 0.0
	for _, c := range tb.Columns {
		total += weight(c)
	}
	widths := make([]float64, len(tb.Columns))
	for i, c := range tb.Columns {
		widths[i] = inner * weight(c) / total
	}
	header := func() {
		p.SetFont("go", "B", 7.8)
		color(ink2)
		fill(panel)
		x := margin
		y := p.GetY()
		for i, c := range tb.Columns {
			p.SetXY(x, y)
			al := "L"
			if c.Align == Right {
				al = "R"
			}
			p.CellFormat(widths[i], 6.5, t(clipW(p, c.Title, widths[i]-2)), "", 0, al, true, 0, "")
			x += widths[i]
		}
		p.SetY(y + 6.5)
	}
	header()
	row := func(cells []any, bold bool) {
		h := 5.6
		if p.GetY()+h > 279 {
			p.AddPage()
			header()
		}
		if bold {
			p.SetFont("go", "B", 7.8)
		} else {
			p.SetFont("go", "", 7.8)
		}
		color(ink)
		x := margin
		y := p.GetY()
		for i := range tb.Columns {
			var v any
			if i < len(cells) {
				v = cells[i]
			}
			al := "L"
			if tb.Columns[i].Align == Right {
				al = "R"
			}
			p.SetXY(x, y)
			p.CellFormat(widths[i], h, t(clipW(p, colText(v, tb.Columns[i]), widths[i]-2)), "", 0, al, false, 0, "")
			x += widths[i]
		}
		draw(line)
		p.SetLineWidth(0.15)
		p.Line(margin, y+h, margin+inner, y+h)
		p.SetY(y + h)
	}
	for _, rw := range tb.Rows {
		row(rw, false)
	}
	if len(tb.Total) > 0 {
		row(tb.Total, true)
	}
}

func (r *Report) pdfChart(p *fpdf.Fpdf, c *Chart, margin, inner float64) {
	const h = 42.0
	if p.GetY()+h+6 > 279 {
		p.AddPage()
	}
	y0 := p.GetY() + 2
	maxV := 0.0
	for _, v := range c.Values {
		maxV = math.Max(maxV, v)
	}
	if maxV <= 0 {
		maxV = 1
	}
	p.SetDrawColor(line.r, line.g, line.b)
	p.SetLineWidth(0.15)
