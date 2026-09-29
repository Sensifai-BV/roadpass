package api

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// dist is the built dashboard (web/ → npm run build → internal/api/dist).
//
//go:embed all:dist
var dist embed.FS

// indexable are the public marketing pages; every other path is app UI or a private link.
var indexable = map[string]bool{"/": true, "/contact": true, "/verify": true, "/explorer": true}

// seoLastmod is the sitemap's lastmod for the marketing pages; bump it when their copy changes.
const seoLastmod = "2026-10-20"

func (s *Server) origin() string { return strings.TrimRight(s.Cfg.PublicURL, "/") }

func (s *Server) robotsTxt(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write([]byte("User-agent: *\nAllow: /$\nAllow: /contact\nAllow: /verify\nAllow: /explorer\nDisallow: /api/\nDisallow: /ngsi-ld/\nDisallow: /p/\nDisallow: /join/\nDisallow: /reset/\nDisallow: /login\n\nSitemap: " + s.origin() + "/sitemap.xml\n"))
}

func (s *Server) sitemapXML(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n" + `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
	for _, u := range []struct{ path, freq, prio string }{{"/", "monthly", "1.0"}, {"/explorer", "daily", "0.7"}, {"/verify", "yearly", "0.6"}, {"/contact", "yearly", "0.6"}} {
		b.WriteString("  <url><loc>" + s.origin() + u.path + "</loc><lastmod>" + seoLastmod + "</lastmod><changefreq>" + u.freq + "</changefreq><priority>" + u.prio + "</priority></url>\n")
	}
	b.WriteString("</urlset>\n")
	_, _ = w.Write(b.Bytes())
}

// spa serves the dashboard: hashed assets are immutable for a year; every other
// path that is not a file returns index.html (client-side routing), never cached.
func (s *Server) spa() http.Handler {
	root, _ := fs.Sub(dist, "dist")
	files := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "")
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/ngsi-ld/") {
			writeProblem(w, r, http.StatusNotFound, "Not found", "")
			return
		}
		switch r.URL.Path {
		case "/robots.txt":
			s.robotsTxt(w, r)
			return
		case "/sitemap.xml":
			s.sitemapXML(w, r)
			return
		}
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p != "" && p != "index.html" {
			if st, err := fs.Stat(root, p); err == nil && !st.IsDir() {
				switch {
				case strings.HasPrefix(p, "assets/"):
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				case p == "sw.js": // a service worker must update with each release
					w.Header().Set("Cache-Control", "no-cache")
					w.Header().Set("Service-Worker-Allowed", "/")
				default:
					w.Header().Set("Cache-Control", "public, max-age=3600")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if !indexable[path.Clean(r.URL.Path)] {
			w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		}
		b, err := fs.ReadFile(root, "index.html")
		if err != nil {
			http.Error(w, "dashboard not built", http.StatusNotFound)
			return
		}
		_, _ = w.Write(bytes.ReplaceAll(b, []byte("__ORIGIN__"), []byte(s.origin())))
	})
}
