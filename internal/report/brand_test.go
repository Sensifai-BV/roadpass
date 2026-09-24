package report

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestBrandedPDFAndFingerprint(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 40, 20))
	for x := 0; x < 40; x++ {
		for y := 0; y < 20; y++ {
			img.Set(x, y, color.RGBA{0, 120, 60, 255})
		}
	}
	var logo bytes.Buffer
	if err := png.Encode(&logo, img); err != nil {
		t.Fatal(err)
	}
	r := &Report{Title: "Monthly report", Org: "GreenSort", Sections: []Section{{Heading: "A", Text: []string{"b"}}}}
	plain := r.Fingerprint()
	r.Brand = &Brand{Name: "GreenSort Rotterdam BV", Legal: "VAT NL001234567B01", Address: "Botlek 1\nRotterdam", Contact: "info@greensort.test",
		Footer: "GreenSort Rotterdam BV · KvK 12345678", Logo: logo.Bytes(), LogoType: "image/png"}
	if r.Fingerprint() != plain {
		t.Fatal("the letterhead must not change the fingerprint (signatures would break)")
	}
	var out bytes.Buffer
	if err := r.PDF(&out); err != nil {
		t.Fatal(err)
	}
	if out.Len() < 1000 || !bytes.HasPrefix(out.Bytes(), []byte("%PDF")) {
		t.Fatalf("not a PDF (%d bytes)", out.Len())
	}
	r.Brand.Logo = []byte("not an image")
	if err := r.PDF(&bytes.Buffer{}); err != nil {
		t.Fatalf("a broken logo must not break the report: %v", err)
	}
}
