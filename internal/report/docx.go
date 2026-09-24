package report

import (
	"archive/zip"
	"fmt"
	"html"
	"io"
	"strings"
	"time"
)

// DOCX renders a Word document (Office Open XML) with real heading styles, so
// the outline and table of contents work in Word.
func (r *Report) DOCX(w io.Writer) error {
	z := zip.NewWriter(w)
	put := func(name, body string) error {
		f, err := z.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: r.Generated})
		if err != nil {
			return err
		}
		_, err = io.WriteString(f, body)
		return err
	}
	files := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>
<Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/>
</Types>`,
		"_rels/.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>
</Relationships>`,
		"word/_rels/document.xml.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
</Relationships>`,
		"docProps/core.xml": fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
<dc:title>%s</dc:title><dc:creator>ROADPASS</dc:creator><dc:description>Fingerprint sha256:%s</dc:description>
<dcterms:created xsi:type="dcterms:W3CDTF">%s</dcterms:created>
</cp:coreProperties>`, x(r.Title), r.Fingerprint(), r.Generated.UTC().Format(time.RFC3339)),
		"word/styles.xml":   docxStyles,
		"word/document.xml": r.docxBody(),
	}
	for _, name := range []string{"[Content_Types].xml", "_rels/.rels", "word/_rels/document.xml.rels", "docProps/core.xml", "word/styles.xml", "word/document.xml"} {
		if err := put(name, files[name]); err != nil {
			return err
		}
	}
	return z.Close()
}

func x(s string) string { return html.EscapeString(s) }

func para(style, text string) string {
	ps := ""
	if style != "" {
		ps = `<w:pPr><w:pStyle w:val="` + style + `"/></w:pPr>`
	}
	return `<w:p>` + ps + `<w:r><w:t xml:space="preserve">` + x(text) + `</w:t></w:r></w:p>`
}

func (r *Report) docxBody() string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)
	b.WriteString(para("Small", "ROADPASS"+prefixed(" · ", strings.Join(nonEmpty(r.Org, r.Period), " · "))))
	if br := r.Brand; br != nil {
		b.WriteString(para("Small", strings.Join(nonEmpty(br.Name, br.Legal, strings.ReplaceAll(br.Address, "\n", ", "), br.Contact), " · ")))
	}
	b.WriteString(para("Title", r.Title))
	if r.Subtitle != "" {
		b.WriteString(para("Subtitle", r.Subtitle))
	}
	if r.Link != nil && r.Link.URL != "" {
		b.WriteString(para("Small", r.Link.Caption+": "+r.Link.URL))
	}
	if r.Notice != "" {
		b.WriteString(para("Notice", r.Notice))
	}
	if len(r.KPIs) > 0 {
		t := &Table{Columns: []Column{{Title: "Figure", Weight: 2}, {Title: "Value", Align: Right}, {Title: "", Weight: 2}}}
		for _, k := range r.KPIs {
			t.Rows = append(t.Rows, []any{k.Label, k.Value, k.Hint})
		}
		b.WriteString(docxTable(t))
	}
	for _, s := range r.Sections {
		b.WriteString(para("Heading1", s.Heading))
		for _, p := range s.Text {
			b.WriteString(para("", p))
		}
		if s.Chart != nil && s.Table == nil {
			t := &Table{Columns: []Column{{Title: "Period"}, {Title: s.Chart.Unit, Align: Right}}}
			for i, v := range s.Chart.Values {
				if i < len(s.Chart.Labels) {
					t.Rows = append(t.Rows, []any{s.Chart.Labels[i], v})
				}
			}
			b.WriteString(docxTable(t))
		}
		if s.Table != nil {
			b.WriteString(docxTable(s.Table))
		}
	}
	if so := r.SignOff; so != nil {
		b.WriteString(para("Heading1", "Sign-off"))
		b.WriteString(para("", so.Statement))
		b.WriteString(para("", fmt.Sprintf("%s, %s · %s", so.Name, so.Role, so.At.UTC().Format("2 January 2006 15:04 UTC"))))
		if so.Signature != "" {
			b.WriteString(para("Mono", fmt.Sprintf("Ed25519 key %s · signature %s", so.KeyID, so.Signature)))
		}
	}
	b.WriteString(para("Mono", "Fingerprint sha256:"+r.Fingerprint()+" · generated "+r.Generated.UTC().Format("2006-01-02 15:04 UTC")))
	b.WriteString(`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1134" w:right="1021" w:bottom="1134" w:left="1021" w:header="567" w:footer="567" w:gutter="0"/></w:sectPr></w:body></w:document>`)
	return b.String()
}

func prefixed(p, s string) string {
	if s == "" {
		return ""
	}
	return p + s
}

func docxTable(t *Table) string {
	var b strings.Builder
	total := 0.0
	for _, c := range t.Columns {
		total += weight(c)
	}
	const width = 9864 // twentieths of a point between the margins
	b.WriteString(`<w:tbl><w:tblPr><w:tblStyle w:val="Grid"/><w:tblW w:w="5000" w:type="pct"/><w:tblLook w:val="04A0" w:firstRow="1"/></w:tblPr><w:tblGrid>`)
	for _, c := range t.Columns {
		fmt.Fprintf(&b, `<w:gridCol w:w="%d"/>`, int(width*weight(c)/total))
	}
	b.WriteString(`</w:tblGrid>`)
	cell := func(v any, c Column, head, bold bool) string {
		text := colText(v, c)
		if head {
			text = cellText(v)
		}
		jc := ""
		if c.Align == Right {
			jc = `<w:jc w:val="right"/>`
		}
		shade, rpr := "", ""
		if head {
			shade = `<w:shd w:val="clear" w:color="auto" w:fill="F2F2F4"/>`
		}
		if head || bold {
			rpr = `<w:rPr><w:b/></w:rPr>`
		}
		return `<w:tc><w:tcPr>` + shade + `</w:tcPr><w:p><w:pPr><w:pStyle w:val="Cell"/>` + jc + `</w:pPr><w:r>` + rpr + `<w:t xml:space="preserve">` + x(text) + `</w:t></w:r></w:p></w:tc>`
	}
	b.WriteString(`<w:tr><w:trPr><w:tblHeader/></w:trPr>`)
	for _, c := range t.Columns {
		b.WriteString(cell(c.Title, c, true, false))
	}
	b.WriteString(`</w:tr>`)
	row := func(r []any, bold bool) {
		b.WriteString(`<w:tr>`)
		for i, c := range t.Columns {
			var v any
			if i < len(r) {
				v = r[i]
			}
			b.WriteString(cell(v, c, false, bold))
		}
		b.WriteString(`</w:tr>`)
	}
	for _, r := range t.Rows {
		row(r, false)
	}
	if len(t.Total) > 0 {
		row(t.Total, true)
	}
	b.WriteString(`</w:tbl>`)
	b.WriteString(para("", ""))
	return b.String()
}

const docxStyles = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Inter" w:hAnsi="Inter" w:cs="Arial"/><w:sz w:val="20"/><w:color w:val="1A1A1F"/></w:rPr></w:rPrDefault>
<w:pPrDefault><w:pPr><w:spacing w:after="100" w:line="276" w:lineRule="auto"/></w:pPr></w:pPrDefault></w:docDefaults>
<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/></w:style>
<w:style w:type="paragraph" w:styleId="Title"><w:name w:val="Title"/><w:basedOn w:val="Normal"/><w:pPr><w:spacing w:before="120" w:after="80"/></w:pPr><w:rPr><w:b/><w:sz w:val="44"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Subtitle"><w:name w:val="Subtitle"/><w:basedOn w:val="Normal"/><w:rPr><w:color w:val="52525B"/><w:sz w:val="22"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:pPr><w:keepNext/><w:spacing w:before="320" w:after="100"/><w:outlineLvl w:val="0"/></w:pPr><w:rPr><w:b/><w:sz w:val="28"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Small"><w:name w:val="Small"/><w:basedOn w:val="Normal"/><w:rPr><w:color w:val="80808A"/><w:sz w:val="16"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Mono"><w:name w:val="Mono"/><w:basedOn w:val="Normal"/><w:rPr><w:rFonts w:ascii="Consolas" w:hAnsi="Consolas"/><w:color w:val="80808A"/><w:sz w:val="14"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Notice"><w:name w:val="Notice"/><w:basedOn w:val="Normal"/><w:pPr><w:shd w:val="clear" w:color="auto" w:fill="FEF3C7"/><w:spacing w:before="120" w:after="160"/></w:pPr><w:rPr><w:color w:val="92400E"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Cell"><w:name w:val="Cell"/><w:basedOn w:val="Normal"/><w:pPr><w:spacing w:after="0"/></w:pPr><w:rPr><w:sz w:val="17"/></w:rPr></w:style>
<w:style w:type="table" w:styleId="Grid"><w:name w:val="Grid"/><w:tblPr><w:tblBorders><w:bottom w:val="single" w:sz="4" w:color="E4E4E7"/><w:insideH w:val="single" w:sz="4" w:color="E4E4E7"/></w:tblBorders><w:tblCellMar><w:top w:w="50" w:type="dxa"/><w:left w:w="90" w:type="dxa"/><w:bottom w:w="50" w:type="dxa"/><w:right w:w="90" w:type="dxa"/></w:tblCellMar></w:tblPr></w:style>
</w:styles>`
