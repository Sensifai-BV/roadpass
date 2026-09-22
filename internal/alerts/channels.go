package alerts

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"mime"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Message is what a channel sends.
type Message struct {
	// ID is the stable id of the event ("evt_14"); a webhook sends it in the body and in
	// X-Roadpass-Delivery, and keeps it across retries so receivers can de-duplicate.
	ID       string
	Subject  string
	ReplyTo  string // set by SendMail from the mail server's reply-to
	Text     string // plain text, one or more lines
	Severity string
	State    string // firing | reminder | escalated | resolved | test | verify
	Link     string // absolute URL into ROADPASS
	Data     map[string]any
	// Attachments go with e-mail only (e.g. the monthly compliance PDF).
	Attachments []Attachment
}

// Attachment is a file sent with an e-mail.
type Attachment struct {
	Name, Type string
	Data       []byte
}

// Senders configures the operator's defaults and the outbound HTTP client.
type Senders struct {
	SMTP SMTPConfig // the environment default; use Mail(ctx)
	// Load returns the platform's saved mail server (nil: environment only).
	Load         func(context.Context) (SMTPConfig, bool, error)
	mail         mailCache
	Twilio       TwilioConfig
	TelegramBase string // https://api.telegram.org
	AllowPrivate bool   // tests and local stacks: allow webhooks to private addresses
	HTTP         *http.Client
	Now          func() time.Time
}

// SMTPConfig is an SMTP relay.
type SMTPConfig struct {
	Host, Port, Username, Password, From string
	ReplyTo                              string // optional
	TLS                                  string // starttls | tls | none
}

// TwilioConfig is a Twilio account.
type TwilioConfig struct{ Base, AccountSID, AuthToken, From string }

// Configured reports whether an operator default exists.
func (c SMTPConfig) Configured() bool { return c.Host != "" && c.From != "" }
func (c TwilioConfig) Configured() bool {
	return c.AccountSID != "" && c.AuthToken != "" && c.From != ""
}

// ErrBlockedAddress is a webhook that resolves to a private network.
var ErrBlockedAddress = errors.New("refusing to call a private or local address")

// SafeClient returns an HTTP client that refuses to connect to private,
// loopback and link-local addresses (checked at connect time, so DNS rebinding
// cannot sneak past), unless allowPrivate.
func SafeClient(allowPrivate bool, timeout time.Duration) *http.Client {
	d := &net.Dialer{Timeout: timeout, Control: func(_, address string, _ syscall.RawConn) error {
		if allowPrivate {
			return nil
		}
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		ip := net.ParseIP(host)
		if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
			ip.IsUnspecified() || ip.IsMulticast() || ip.Equal(net.IPv4bcast) || (ip.To4() != nil && ip.To4()[0] == 100 && ip.To4()[1]&0xc0 == 64) {
			return ErrBlockedAddress
		}
		return nil
	}}
	tr := &http.Transport{DialContext: d.DialContext, TLSHandshakeTimeout: timeout, ResponseHeaderTimeout: timeout, MaxIdleConns: 20}
	return &http.Client{Transport: tr, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// CheckURL validates a channel URL before it is stored.
func CheckURL(raw string, allowPrivate bool) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return errors.New("not a valid URL")
	}
	if u.Scheme != "https" && !(allowPrivate && u.Scheme == "http") {
		return errors.New("use an https:// URL")
	}
	if u.User != nil {
		return errors.New("credentials in the URL are not allowed; put them in the secret")
	}
	return nil
}

// Send delivers m through channel c (secret holds its credentials).
func (s *Senders) Send(ctx context.Context, c Channel, secret map[string]string, m Message) error {
	switch c.Type {
	case "email":
		return s.email(ctx, c, secret, m)
	case "webhook":
		return s.webhook(ctx, c, secret, m)
	case "slack":
		return s.postJSON(ctx, secret["url"], slackPayload(m), nil)
	case "teams":
		return s.postJSON(ctx, secret["url"], teamsPayload(m), nil)
	case "sms":
		return s.sms(ctx, c, secret, m)
	case "telegram":
		return s.telegram(ctx, c, secret, m)
	}
	return fmt.Errorf("unknown channel type %q", c.Type)
}

func (s *Senders) client() *http.Client {
	if s.HTTP != nil {
		return s.HTTP
	}
	return SafeClient(s.AllowPrivate, 10*time.Second)
}

func (s *Senders) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Senders) postJSON(ctx context.Context, target string, payload any, headers map[string]string) error {
	if err := CheckURL(target, s.AllowPrivate); err != nil {
		return err
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return s.post(ctx, target, "application/json", b, headers)
}

func (s *Senders) post(ctx context.Context, target, ctype string, body []byte, headers map[string]string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", ctype)
	req.Header.Set("User-Agent", "ROADPASS-alerts/1")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := s.client().Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	snippet, _ := io.ReadAll(io.LimitReader(res.Body, 300))
	if res.StatusCode >= 300 {
		return &HTTPError{Status: res.StatusCode, Body: strings.TrimSpace(string(snippet))}
	}
	return nil
}

// HTTPError is a non-2xx answer from a channel's endpoint.
type HTTPError struct {
	Status int
	Body   string
}

func (e *HTTPError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.Status, e.Body) }

// Transient reports whether sending again may succeed: the endpoint answered
// 5xx, 408 or 429, or the call timed out or could not connect. A refusal (4xx), a
// blocked address or an invalid URL will not change on a retry.
func Transient(err error) bool {
	if err == nil || errors.Is(err, ErrBlockedAddress) {
		return false
	}
	var he *HTTPError
	if errors.As(err, &he) {
		return he.Status >= 500 || he.Status == 408 || he.Status == 429
	}
	var ne net.Error
	return errors.Is(err, context.DeadlineExceeded) || errors.As(err, &ne)
}

// Signature is the webhook signature header value: t=<unix>,v1=HMAC-SHA256(secret, "<t>.<body>").
func Signature(secret string, t int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.", t)
	mac.Write(body)
	return fmt.Sprintf("t=%d,v1=%s", t, hex.EncodeToString(mac.Sum(nil)))
}

func (s *Senders) webhook(ctx context.Context, c Channel, secret map[string]string, m Message) error {
	payload := map[string]any{
		"event": m.State, "severity": m.Severity, "title": m.Subject, "text": m.Text, "link": m.Link, "data": m.Data,
		"sentAt": s.now().UTC().Format(time.RFC3339),
	}
	if m.ID != "" {
		payload["id"] = m.ID
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	target := c.Config["url"]
	if err := CheckURL(target, s.AllowPrivate); err != nil {
		return err
	}
	h := map[string]string{
		"X-Roadpass-Event":     m.State,
		"X-Roadpass-Signature": Signature(secret["signingSecret"], s.now().Unix(), body),
	}
	if m.ID != "" {
		h["X-Roadpass-Delivery"] = m.ID
	}
	return s.post(ctx, target, "application/json", body, h)
}

func slackPayload(m Message) map[string]any {
	esc := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	icon := map[string]string{"critical": ":red_circle:", "warning": ":large_orange_circle:", "info": ":large_blue_circle:"}[m.Severity]
	if m.State == "resolved" {
		icon = ":white_check_mark:"
	}
	text := fmt.Sprintf("%s *%s*\n%s", icon, esc.Replace(m.Subject), esc.Replace(m.Text))
	if m.Link != "" {
		text += fmt.Sprintf("\n<%s|Open in ROADPASS>", m.Link)
	}
	return map[string]any{"text": esc.Replace(m.Subject), "blocks": []any{map[string]any{"type": "section", "text": map[string]any{"type": "mrkdwn", "text": text}}}}
}

func teamsPayload(m Message) map[string]any {
	color := map[string]string{"critical": "Attention", "warning": "Warning", "info": "Accent"}[m.Severity]
	if m.State == "resolved" {
		color = "Good"
	}
	body := []any{
		map[string]any{"type": "TextBlock", "text": m.Subject, "weight": "Bolder", "size": "Medium", "color": color, "wrap": true},
		map[string]any{"type": "TextBlock", "text": m.Text, "wrap": true},
	}
	card := map[string]any{"$schema": "http://adaptivecards.io/schemas/adaptive-card.json", "type": "AdaptiveCard", "version": "1.4", "body": body}
	if m.Link != "" {
		card["actions"] = []any{map[string]any{"type": "Action.OpenUrl", "title": "Open in ROADPASS", "url": m.Link}}
	}
	return map[string]any{"type": "message", "attachments": []any{map[string]any{"contentType": "application/vnd.microsoft.card.adaptive", "content": card}}}
}

func (s *Senders) sms(ctx context.Context, c Channel, secret map[string]string, m Message) error {
	tw := s.Twilio
	if secret["accountSid"] != "" { // the organisation's own Twilio account
		tw = TwilioConfig{Base: tw.Base, AccountSID: secret["accountSid"], AuthToken: secret["authToken"], From: c.Config["from"]}
	}
	if !tw.Configured() {
		return errors.New("no SMS provider configured")
	}
	base := tw.Base
	if base == "" {
		base = "https://api.twilio.com"
	}
	text := m.Subject
	if m.Text != "" {
		text += " — " + m.Text
	}
	if len([]rune(text)) > 300 {
		text = string([]rune(text)[:297]) + "…"
	}
	form := url.Values{"To": {c.Config["to"]}, "From": {tw.From}, "Body": {text}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/2010-04-01/Accounts/%s/Messages.json", strings.TrimRight(base, "/"), url.PathEscape(tw.AccountSID)),
		strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.SetBasicAuth(tw.AccountSID, tw.AuthToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	cl := s.HTTP
	if cl == nil {
		cl = &http.Client{Timeout: 10 * time.Second}
	}
	res, err := cl.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 300))
		return fmt.Errorf("Twilio HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

func (s *Senders) telegram(ctx context.Context, c Channel, secret map[string]string, m Message) error {
	if secret["botToken"] == "" {
		return errors.New("no Telegram bot token")
	}
	base := s.TelegramBase
	if base == "" {
		base = "https://api.telegram.org"
	}
	text := "<b>" + html.EscapeString(m.Subject) + "</b>\n" + html.EscapeString(m.Text)
	if m.Link != "" {
		text += "\n<a href=\"" + html.EscapeString(m.Link) + "\">Open in ROADPASS</a>"
	}
	b, _ := json.Marshal(map[string]any{"chat_id": c.Config["chatId"], "text": text, "parse_mode": "HTML", "disable_web_page_preview": true})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/bot"+secret["botToken"]+"/sendMessage", bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	cl := s.HTTP
	if cl == nil {
		cl = &http.Client{Timeout: 10 * time.Second}
	}
	res, err := cl.Do(req)
	if err != nil {
		return errors.New("telegram: request failed") // the URL carries the token: never echo it
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return fmt.Errorf("Telegram HTTP %d", res.StatusCode)
	}
	return nil
}

func (s *Senders) email(ctx context.Context, c Channel, secret map[string]string, m Message) error {
	cfg := s.Mail(ctx)
	if c.Config["smtpHost"] != "" { // the organisation's own mail server
		cfg = SMTPConfig{Host: c.Config["smtpHost"], Port: c.Config["smtpPort"], Username: c.Config["smtpUser"], Password: secret["smtpPassword"],
			From: c.Config["from"], TLS: c.Config["smtpTls"]}
	}
	if !cfg.Configured() {
		return errors.New("no e-mail server configured")
	}
	return SendMail(ctx, cfg, c.Config["to"], m)
}

// SendMail sends one plain-text and HTML message through an SMTP relay.
func SendMail(ctx context.Context, cfg SMTPConfig, to string, m Message) error {
	if strings.ContainsAny(to, "\r\n") || !strings.Contains(to, "@") {
		return errors.New("invalid recipient")
	}
	port := cfg.Port
	if port == "" {
		port = map[string]string{"tls": "465", "none": "25"}[cfg.TLS]
		if port == "" {
			port = "587"
		}
	}
	addr := net.JoinHostPort(cfg.Host, port)
	d := net.Dialer{Timeout: 10 * time.Second}
	var conn net.Conn
	var err error
	if cfg.TLS == "tls" {
		conn, err = tls.DialWithDialer(&d, "tcp", addr, &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12})
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return err
	}
	cl, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		conn.Close()
		return err
	}
	defer cl.Close()
	if cfg.TLS == "" || cfg.TLS == "starttls" {
		if ok, _ := cl.Extension("STARTTLS"); ok {
			if err := cl.StartTLS(&tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
				return err
			}
		} else if cfg.TLS == "starttls" {
			return errors.New("the mail server does not offer STARTTLS")
		}
	}
	if cfg.Username != "" {
		if err := cl.Auth(smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)); err != nil {
			return err
		}
	}
	if err := cl.Mail(addressOf(cfg.From)); err != nil {
		return err
	}
	if err := cl.Rcpt(to); err != nil {
		return err
	}
	w, err := cl.Data()
	if err != nil {
		return err
	}
	if m.ReplyTo == "" {
		m.ReplyTo = cfg.ReplyTo
	}
	if _, err := io.WriteString(w, mailBody(cfg.From, to, m, time.Now())); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return cl.Quit()
}

// mailBody is the MIME message: text and HTML alternatives, wrapped with any
// attachments (base64, 76-character lines) in a multipart/mixed envelope.
func mailBody(from, to string, m Message, now time.Time) string {
	boundary := "rp" + strconv.FormatInt(now.UnixNano(), 36)
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nDate: %s\r\n", from, to,
		mime.QEncoding.Encode("utf-8", m.Subject), now.UTC().Format(time.RFC1123Z))
	if m.ReplyTo != "" && !strings.ContainsAny(m.ReplyTo, "\r\n") {
		fmt.Fprintf(&b, "Reply-To: %s\r\n", m.ReplyTo)
	}
	alt := boundary
	if len(m.Attachments) > 0 {
		alt = boundary + "a"
		fmt.Fprintf(&b, "Content-Type: multipart/mixed; boundary=%s\r\n\r\n--%s\r\n", boundary, boundary)
	}
	fmt.Fprintf(&b, "Content-Type: multipart/alternative; boundary=%s\r\n\r\n", alt)
	fmt.Fprintf(&b, "--%s\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s\r\n", alt, plainBody(m))
	fmt.Fprintf(&b, "--%s\r\nContent-Type: text/html; charset=utf-8\r\n\r\n%s\r\n--%s--\r\n", alt, htmlBody(m), alt)
	for _, a := range m.Attachments {
		name := mime.QEncoding.Encode("utf-8", strings.Map(func(r rune) rune {
			if r == '"' || r == '\r' || r == '\n' {
				return '_'
			}
			return r
		}, a.Name))
		fmt.Fprintf(&b, "\r\n--%s\r\nContent-Type: %s\r\nContent-Transfer-Encoding: base64\r\nContent-Disposition: attachment; filename=\"%s\"\r\n\r\n", boundary, a.Type, name)
		enc := base64.StdEncoding.EncodeToString(a.Data)
		for len(enc) > 76 {
			b.WriteString(enc[:76] + "\r\n")
			enc = enc[76:]
		}
		b.WriteString(enc + "\r\n")
	}
	if len(m.Attachments) > 0 {
		fmt.Fprintf(&b, "--%s--\r\n", boundary)
	}
	return b.String() // smtp's DotWriter escapes leading dots
}

func addressOf(from string) string {
	if i := strings.LastIndex(from, "<"); i >= 0 {
		return strings.TrimSuffix(from[i+1:], ">")
	}
	return from
}

// footer explains why the e-mail came, and the button what it opens.
func footer(m Message) (note, button string) {
	switch m.State {
	case "invite":
		return "You received this because an administrator invited you. Ignore it if you did not expect it; the link expires on its own.", "Accept the invitation"
	case "report":
		return "Sent on your organisation's monthly compliance schedule. An administrator can change it under Compliance → Reports.", "Open in ROADPASS"
	}
	return "ROADPASS alerts. Change what you receive under Settings → Notifications.", "Open in ROADPASS"
}

func plainBody(m Message) string {
	note, button := footer(m)
	s := m.Text
	if m.Link != "" {
		s += "\n\n" + button + ": " + m.Link
	}
	return s + "\n\n— " + note
}

func htmlBody(m Message) string {
	color := map[string]string{"critical": "#d92d20", "warning": "#dc6803", "info": "#1f9d55"}[m.Severity]
	if m.State == "resolved" {
		color = "#1f9d55"
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<div style="font-family:-apple-system,Segoe UI,Inter,sans-serif;max-width:560px;margin:0 auto;color:#111">`)
	fmt.Fprintf(&b, `<div style="border-left:4px solid %s;padding:4px 0 4px 14px;margin:16px 0"><div style="font-size:17px;font-weight:600">%s</div></div>`, color, html.EscapeString(m.Subject))
	for _, line := range strings.Split(m.Text, "\n") {
		fmt.Fprintf(&b, `<p style="font-size:14px;line-height:1.5;margin:6px 0">%s</p>`, html.EscapeString(line))
	}
	note, button := footer(m)
	if m.Link != "" {
		fmt.Fprintf(&b, `<p style="margin:18px 0"><a href="%s" style="background:#111;color:#fff;padding:9px 14px;border-radius:9px;text-decoration:none;font-size:14px">%s</a></p>`, html.EscapeString(m.Link), html.EscapeString(button))
	}
	fmt.Fprintf(&b, `<p style="font-size:12px;color:#888;margin-top:24px">%s</p></div>`, html.EscapeString(note))
	return b.String()
}
