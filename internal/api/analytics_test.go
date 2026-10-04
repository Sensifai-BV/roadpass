package api

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// Analytics answers depend on the signed-in company, so a browser cache must not
// hand one account's figures to the next one on the same machine.
func TestAnalyticsAnswerVariesBySession(t *testing.T) {
	w := httptest.NewRecorder()
	answer(w, httptest.NewRequest("GET", "/api/v1/analytics/overview", nil), map[string]int{"n": 1}, nil)
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "private") {
		t.Fatalf("Cache-Control = %q, want private", cc)
	}
	vary := w.Header().Get("Vary")
	for _, h := range []string{"Cookie", "Authorization"} {
		if !strings.Contains(vary, h) {
			t.Fatalf("Vary = %q, missing %s", vary, h)
		}
	}
}
