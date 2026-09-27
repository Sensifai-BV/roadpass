// Package api is the HTTP surface: the JSON API (/api/v1), the NGSI-LD API
// (/ngsi-ld/v1), live events (SSE), and the embedded single-page dashboard.
//
// One API serves both the dashboard (session cookie) and partner systems (API key);
// handlers never know which, they get a Principal.
package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/sensifai/roadpass/internal/alerts"
	"github.com/sensifai/roadpass/internal/analytics"
	"github.com/sensifai/roadpass/internal/cache"
	"github.com/sensifai/roadpass/internal/config"
	"github.com/sensifai/roadpass/internal/ingest"
	"github.com/sensifai/roadpass/internal/keyring"
	"github.com/sensifai/roadpass/internal/ledger"
	"github.com/sensifai/roadpass/internal/passport"
	"github.com/sensifai/roadpass/internal/store"
)

// Server holds the dependencies of every handler.
type Server struct {
	Cfg      config.Config
	St       *store.Store
	CH       *analytics.DB
	Cache    *cache.Cache
	Ingest   *ingest.Service
	Passport *passport.Builder
	Ledger   ledger.Client
	Keyring  *keyring.Keyring
	Notify   *alerts.Notifier
	Version  string
	Started  time.Time

	keys *keyCache
}

// Handler builds the router.
func (s *Server) Handler() http.Handler {
	s.keys = newKeyCache()
	mux := http.NewServeMux()
	s.routes(mux)
	var h http.Handler = mux
	h = s.securityHeaders(h)
	h = recoverer(h)
	h = logRequests(h)
	h = requestID(h)
	return h
}

// ---------------------------------------------------------------- middleware

type ctxKey int

const (
	keyRequestID ctxKey = iota
	keyPrincipal
)

func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" || len(id) > 64 {
			b := make([]byte, 8)
			_, _ = rand.Read(b)
			id = hex.EncodeToString(b)
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), keyRequestID, id)))
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusWriter) WriteHeader(code int) { w.status = code; w.ResponseWriter.WriteHeader(code) }
func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		if strings.HasPrefix(r.URL.Path, "/assets/") || r.URL.Path == "/healthz" {
			return
		}
		lvl := slog.LevelInfo
		if sw.status >= 500 {
			lvl = slog.LevelError
		}
		slog.Log(r.Context(), lvl, "http", "method", r.Method, "path", r.URL.Path, "status", sw.status,
			"bytes", sw.bytes, "ms", time.Since(start).Milliseconds(), "req", r.Context().Value(keyRequestID))
	})
}

func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				slog.Error("panic", "err", v, "stack", string(debug.Stack()))
				writeProblem(w, r, http.StatusInternalServerError, "Internal error", "")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if s.Cfg.Production() {
			h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/ngsi-ld/") {
			tiles := ""
			if s.Cfg.TileURL != "" {
				tiles = " " + originOf(s.Cfg.TileURL)
			}
			// The Turnstile widget (contact form) loads a script and a frame from Cloudflare, only when it is configured.
			script, frame, connect := "'self'", "", "'self'"
			if s.turnstileOn() {
				script += " https://challenges.cloudflare.com"
				connect += " https://challenges.cloudflare.com"
				frame = " frame-src https://challenges.cloudflare.com;"
			}
			h.Set("Content-Security-Policy", "default-src 'self'; script-src "+script+"; style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data: blob:"+tiles+"; font-src 'self'; connect-src "+connect+";"+frame+" frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		}
		next.ServeHTTP(w, r)
	})
}

func originOf(u string) string {
	if i := strings.Index(u, "://"); i >= 0 {
		rest := u[i+3:]
		if j := strings.IndexByte(rest, '/'); j >= 0 {
			rest = rest[:j]
		}
		return u[:i+3] + strings.NewReplacer("{s}", "*", "{a-c}", "*").Replace(rest)
	}
	return ""
}

// ---------------------------------------------------------------- responses

// Problem is an RFC 9457 problem document.
type Problem struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail,omitempty"`
	Instance string `json:"instance,omitempty"`
	Errors   any    `json:"errors,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	// Encode first: a value JSON cannot carry (NaN, ±Inf) must not become an
	// empty 200 that the dashboard silently reads as "no data".
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		slog.Error("response not encodable", "err", err)
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(Problem{Type: "about:blank", Title: "Server error", Status: http.StatusInternalServerError, Detail: "The response could not be encoded."})
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

