package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sensifai/roadpass/internal/config"
)

func TestSPASEO(t *testing.T) {
	s := &Server{Cfg: config.Config{PublicURL: "https://roadpass.example/"}}
	h := s.spa()
	get := func(p string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, p, nil))
		return w
	}
	if b := get("/robots.txt").Body.String(); !strings.Contains(b, "Sitemap: https://roadpass.example/sitemap.xml") {
		t.Fatalf("robots: %s", b)
	}
	if b := get("/sitemap.xml").Body.String(); !strings.Contains(b, "<loc>https://roadpass.example/contact</loc>") {
		t.Fatalf("sitemap: %s", b)
	}
	if w := get("/"); w.Header().Get("X-Robots-Tag") != "" || strings.Contains(w.Body.String(), "__ORIGIN__") {
		t.Fatalf("landing must be indexable with the origin filled in: %v", w.Header())
	}
	if get("/passports").Header().Get("X-Robots-Tag") == "" {
		t.Fatal("app pages must be noindex")
	}
}
