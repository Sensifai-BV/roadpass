package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Cloudflare Turnstile keeps bots off the public contact form. The page renders
// the widget with the site key and sends the token it produces; the server checks
// that token with Cloudflare using the secret key. Both keys unset: no challenge.

func (s *Server) turnstileOn() bool {
	return s.Cfg.TurnstileSiteKey != "" && s.Cfg.TurnstileSecret != ""
}

// passTurnstile runs the challenge check of a public form when Turnstile is
// configured. It has answered the request when it returns false.
func (s *Server) passTurnstile(w http.ResponseWriter, r *http.Request, token, title string) bool {
	if !s.turnstileOn() {
		return true
	}
	ok, err := verifyTurnstile(r.Context(), s.Cfg.TurnstileVerifyURL, s.Cfg.TurnstileSecret, token, clientIP(r))
	switch {
	case err != nil:
		// Fail closed: without the check the form is an open door for bots.
		slog.Warn("turnstile verification failed", "err", err)
		writeProblem(w, r, http.StatusServiceUnavailable, "Spam check unavailable", "We could not run the spam check. Please try again in a moment.")
	case !ok:
		writeProblemErrs(w, r, http.StatusUnprocessableEntity, title, "", []fieldErr{{"turnstile", "Complete the spam check and try again."}})
	default:
		return true
	}
	return false
}

// contactConfig tells the contact page which challenge to show.
func (s *Server) contactConfig(w http.ResponseWriter, r *http.Request) {
	key := ""
	if s.turnstileOn() {
		key = s.Cfg.TurnstileSiteKey
	}
	w.Header().Set("Cache-Control", "no-cache")
	writeJSON(w, http.StatusOK, map[string]string{"turnstileSiteKey": key})
}

var turnstileClient = &http.Client{Timeout: 6 * time.Second}

// verifyTurnstile asks Cloudflare whether token is a valid, unused solution. An
// empty token is simply not valid; an unreachable or malformed answer is an error.
func verifyTurnstile(ctx context.Context, verifyURL, secret, token, ip string) (bool, error) {
	token = strings.TrimSpace(token)
	if token == "" || len(token) > 2048 {
		return false, nil
	}
	form := url.Values{"secret": {secret}, "response": {token}}
	if ip != "" {
		form.Set("remoteip", ip)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, verifyURL, strings.NewReader(form.Encode()))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := turnstileClient.Do(req)
	if err != nil {
		return false, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return false, fmt.Errorf("siteverify answered %d", res.StatusCode)
	}
	var out struct {
		Success bool     `json:"success"`
		Errors  []string `json:"error-codes"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 16<<10)).Decode(&out); err != nil {
		return false, err
	}
	for _, e := range out.Errors {
		// A wrong secret or malformed request is our fault, not the visitor's.
		if e == "invalid-input-secret" || e == "missing-input-secret" {
			return false, errors.New("turnstile secret rejected: " + e)
		}
	}
	return out.Success, nil
}
