package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func fakeSiteverify(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f, _ := url.ParseQuery(string(b))
		w.Header().Set("Content-Type", "application/json")
		switch {
		case f.Get("secret") == "bad-secret":
			_, _ = w.Write([]byte(`{"success":false,"error-codes":["invalid-input-secret"]}`))
		case f.Get("response") == "good-token" && f.Get("remoteip") == "203.0.113.9":
			_, _ = w.Write([]byte(`{"success":true}`))
		default:
			_, _ = w.Write([]byte(`{"success":false,"error-codes":["invalid-input-response"]}`))
		}
	}))
}

func TestVerifyTurnstile(t *testing.T) {
	srv := fakeSiteverify(t)
	defer srv.Close()
	ctx := context.Background()
	if ok, err := verifyTurnstile(ctx, srv.URL, "s", "good-token", "203.0.113.9"); !ok || err != nil {
		t.Fatalf("good token: ok=%v err=%v", ok, err)
	}
	if ok, err := verifyTurnstile(ctx, srv.URL, "s", "forged", "203.0.113.9"); ok || err != nil {
		t.Fatalf("forged token must be refused cleanly: ok=%v err=%v", ok, err)
	}
	if ok, err := verifyTurnstile(ctx, srv.URL, "s", "", ""); ok || err != nil {
		t.Fatalf("empty token must be refused without calling Cloudflare: ok=%v err=%v", ok, err)
	}
	if _, err := verifyTurnstile(ctx, srv.URL, "bad-secret", "good-token", ""); err == nil {
		t.Fatal("a rejected secret is a configuration error, not a visitor failure")
	}
	srv.Close()
	if _, err := verifyTurnstile(ctx, srv.URL, "s", "good-token", ""); err == nil {
		t.Fatal("an unreachable Cloudflare must be an error so the form fails closed")
	}
}
