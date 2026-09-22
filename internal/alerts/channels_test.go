package alerts

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"strings"
	"testing"
	"time"
)

func TestWebhookIsSignedAndVerifiable(t *testing.T) {
	var got struct {
		body []byte
		sig  string
		evt  string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.body, _ = io.ReadAll(r.Body)
		got.sig, got.evt = r.Header.Get("X-Roadpass-Signature"), r.Header.Get("X-Roadpass-Event")
	}))
	defer srv.Close()
	now := time.Unix(1_760_000_000, 0)
	s := &Senders{AllowPrivate: true, Now: func() time.Time { return now }}
	err := s.Send(context.Background(), Channel{Type: "webhook", Config: map[string]string{"url": srv.URL}}, map[string]string{"signingSecret": "k"},
		Message{Subject: "Melt temperature high", Text: "Line 2 at 251 °C", Severity: "critical", State: "firing", Data: map[string]any{"value": 251.0}})
	if err != nil {
		t.Fatal(err)
	}
	if got.evt != "firing" || got.sig != Signature("k", now.Unix(), got.body) {
		t.Fatalf("signature %q does not verify against the body", got.sig)
	}
	var p map[string]any
	if json.Unmarshal(got.body, &p) != nil || p["severity"] != "critical" || p["title"] != "Melt temperature high" {
		t.Fatalf("payload: %s", got.body)
	}
}

func TestSafeClientRefusesPrivateNetworks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	_, err := SafeClient(false, 2*time.Second).Get(srv.URL) // 127.0.0.1
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("loopback must be refused, got %v", err)
	}
	if _, err := SafeClient(true, 2*time.Second).Get(srv.URL); err != nil {
		t.Fatalf("allowed in tests: %v", err)
	}
	for _, bad := range []string{"http://example.com/x", "https://user:pw@example.com/", "not a url", "ftp://example.com"} {
		if CheckURL(bad, false) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if CheckURL("https://hooks.slack.com/services/T/B/x", false) != nil {
		t.Error("a plain https URL must pass")
	}
}

func TestTwilioAndTelegramRequests(t *testing.T) {
	var form, auth, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		if strings.Contains(path, "/Accounts/") {
			_ = r.ParseForm()
			form = r.PostForm.Encode()
			u, p, _ := r.BasicAuth()
			auth = u + ":" + p
			w.WriteHeader(201)
			return
		}
		if strings.Contains(path, "/botBAD/") {
			w.WriteHeader(401)
		}
	}))
	defer srv.Close()
	s := &Senders{Twilio: TwilioConfig{Base: srv.URL, AccountSID: "AC1", AuthToken: "tok", From: "+3100"}, TelegramBase: srv.URL}
	if err := s.Send(context.Background(), Channel{Type: "sms", Config: map[string]string{"to": "+3161"}}, nil, Message{Subject: "Hi", Text: "there"}); err != nil {
		t.Fatal(err)
	}
	if auth != "AC1:tok" || !strings.Contains(form, "To=%2B3161") || !strings.Contains(form, "Body=Hi") {
		t.Fatalf("twilio request: %s %s", auth, form)
	}
	if err := s.Send(context.Background(), Channel{Type: "telegram", Config: map[string]string{"chatId": "42"}}, map[string]string{"botToken": "T0K"}, Message{Subject: "Hi"}); err != nil {
		t.Fatal(err)
	}
	if path != "/botT0K/sendMessage" {
		t.Fatalf("telegram path %s", path)
	}
	err := s.Send(context.Background(), Channel{Type: "telegram"}, map[string]string{"botToken": "BAD"}, Message{Subject: "x"})
	if err == nil || strings.Contains(err.Error(), "BAD") {
		t.Fatalf("errors must not leak the bot token: %v", err)
	}
}

func TestQuietHours(t *testing.T) {
	from, to := 22*60, 7*60
	p := Prefs{QuietFrom: &from, QuietTo: &to}
	at := func(h int) time.Time { return time.Date(2026, 10, 16, h, 0, 0, 0, time.UTC) }
	for h, want := range map[int]bool{23: true, 3: true, 7: false, 12: false, 21: false} {
		if p.Quiet(at(h), time.UTC) != want {
			t.Errorf("%02d:00 quiet=%v", h, !want)
		}
	}
}

func TestMailBodyWithAttachment(t *testing.T) {
	pdf := bytes.Repeat([]byte("%PDF-1.7 binary\x00\xff"), 40)
	body := mailBody("ROADPASS <noreply@example.org>", "qa@example.org", Message{Subject: "Compliance report – September 2026", Text: ".starts with a dot",
		Attachments: []Attachment{{Name: `report "9".pdf`, Type: "application/pdf", Data: pdf}}}, time.Unix(1, 0))
	msg, err := mail.ReadMessage(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	mt, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || mt != "multipart/mixed" {
		t.Fatalf("content type %q: %v", mt, err)
	}
	r := multipart.NewReader(msg.Body, params["boundary"])
	alt, err := r.NextPart()
	if err != nil || !strings.HasPrefix(alt.Header.Get("Content-Type"), "multipart/alternative") {
		t.Fatalf("first part %v: %v", alt.Header, err)
	}
	att, err := r.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	if att.FileName() != "report _9_.pdf" {
		t.Errorf("file name %q", att.FileName())
	}
	got, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, att))
	if err != nil || !bytes.Equal(got, pdf) {
		t.Errorf("attachment did not round-trip: %v", err)
	}
	if _, err := r.NextPart(); err != io.EOF {
		t.Errorf("expected the end of the message, got %v", err)
	}
}

func TestWebhookCarriesStableDeliveryID(t *testing.T) {
	var hdr string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hdr = r.Header.Get("X-Roadpass-Delivery")
		_ = json.NewDecoder(r.Body).Decode(&body)
	}))
	defer srv.Close()
	s := &Senders{AllowPrivate: true}
	err := s.Send(context.Background(), Channel{Type: "webhook", Config: map[string]string{"url": srv.URL}}, map[string]string{"signingSecret": "k"},
		Message{ID: "evt_14", Subject: "x", State: "firing"})
	if err != nil || hdr != "evt_14" || body["id"] != "evt_14" {
		t.Fatalf("err=%v header=%q body id=%v", err, hdr, body["id"])
	}
}

func TestTransientClassification(t *testing.T) {
	for _, c := range []struct {
		err  error
		want bool
	}{
		{&HTTPError{Status: 503}, true}, {&HTTPError{Status: 500}, true}, {&HTTPError{Status: 429}, true}, {&HTTPError{Status: 408}, true},
		{&HTTPError{Status: 400}, false}, {&HTTPError{Status: 401}, false}, {&HTTPError{Status: 404}, false}, {&HTTPError{Status: 302}, false},
		{ErrBlockedAddress, false}, {errors.New("use an https:// URL"), false}, {context.DeadlineExceeded, true}, {nil, false},
	} {
		if got := Transient(c.err); got != c.want {
			t.Errorf("Transient(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

func TestRetryRecoversFromFlakyEndpointButNotFromRefusal(t *testing.T) {
	waits := []time.Duration{0, time.Millisecond, time.Millisecond}
	calls := 0
	err := retry(context.Background(), waits, func(context.Context) error {
		calls++
		if calls < 3 {
			return &HTTPError{Status: 503, Body: "try later"}
		}
		return nil
	}, nil)
	if err != nil || calls != 3 {
		t.Fatalf("flaky endpoint: err=%v after %d calls, want success on the 3rd", err, calls)
	}
	calls = 0
	err = retry(context.Background(), waits, func(context.Context) error { calls++; return &HTTPError{Status: 401, Body: "bad signature"} }, nil)
	if err == nil || calls != 1 {
		t.Fatalf("refusal: err=%v after %d calls, want one attempt", err, calls)
	}
	calls = 0
	err = retry(context.Background(), waits, func(context.Context) error { calls++; return &HTTPError{Status: 500} }, nil)
	if err == nil || calls != 3 {
		t.Fatalf("down endpoint: err=%v after %d calls, want 3 then give up", err, calls)
	}
}
