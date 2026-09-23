package alerts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sync"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/sensifai/roadpass/internal/secrets"
)

// Push sends Web Push notifications (RFC 8030/8291, VAPID RFC 8292) to the
// browsers people registered, so alerts reach them with ROADPASS closed.
type Push struct {
	DB      *DB
	Secrets *secrets.Store
	Contact string // mailto: or https: contact for push services

	mu      sync.Mutex
	pub     string
	private string
}

const vapidSecret = "platform/vapid"

// Keys returns the VAPID key pair, generating it once per installation. The
// public half is published to browsers; the private half lives in the vault.
func (p *Push) Keys(ctx context.Context) (string, string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pub != "" {
		return p.pub, p.private, nil
	}
	pub, priv, ok, err := p.load(ctx)
	if err != nil {
		return "", "", err
	}
	if !ok {
		gpriv, gpub, err := webpush.GenerateVAPIDKeys()
		if err != nil {
			return "", "", err
		}
		// Each pair gets its own secret, so a replica that loses the race below
		// never overwrites the winner's private key.
		sum := sha256.Sum256([]byte(gpub))
		ref, err := p.Secrets.Put(ctx, vapidSecret+"-"+hex.EncodeToString(sum[:6]), map[string]string{"private": gpriv})
		if err != nil {
			return "", "", err
		}
		// Replicas may race: the first pair stored wins and everyone loads it.
		if _, err := p.DB.P.Exec(ctx, `INSERT INTO platform_settings (key, value) VALUES ('vapid_private_ref', $1), ('vapid_public', $2) ON CONFLICT (key) DO NOTHING`, ref, gpub); err != nil {
			return "", "", err
		}
		if pub, priv, ok, err = p.load(ctx); err != nil || !ok {
			return "", "", errors.Join(err, errors.New("VAPID keys not stored"))
		}
	}
	p.pub, p.private = pub, priv
	return pub, priv, nil
}

func (p *Push) load(ctx context.Context) (pub, priv string, ok bool, err error) {
	var ref string
	err = p.DB.P.QueryRow(ctx, `SELECT (SELECT value FROM platform_settings WHERE key='vapid_public'), (SELECT value FROM platform_settings WHERE key='vapid_private_ref')`).Scan(&pub, &ref)
	if err != nil || pub == "" {
		return "", "", false, nil
	}
	s, err := p.Secrets.Get(ctx, ref)
	if err != nil {
		return "", "", false, err
	}
	return pub, s["private"], true, nil
}

// PushPayload is what the service worker receives.
type PushPayload struct {
	Title    string `json:"title"`
	Body     string `json:"body"`
	Link     string `json:"link,omitempty"`
	Severity string `json:"severity"`
	Tag      string `json:"tag,omitempty"` // replaces an earlier notification with the same tag
}

// Send delivers to every browser of a person; returns how many accepted it.
func (p *Push) Send(ctx context.Context, userID string, m PushPayload) (int, error) {
	subs, err := p.DB.Subscriptions(ctx, userID)
	if err != nil || len(subs) == 0 {
		return 0, err
	}
	pub, priv, err := p.Keys(ctx)
	if err != nil {
		return 0, err
	}
	body, _ := json.Marshal(m)
	urgency := webpush.UrgencyNormal
	if m.Severity == "critical" {
		urgency = webpush.UrgencyHigh
	}
	ok := 0
	var last error
	for _, s := range subs {
		res, err := webpush.SendNotificationWithContext(ctx, body, &webpush.Subscription{Endpoint: s.Endpoint, Keys: webpush.Keys{P256dh: s.P256dh, Auth: s.Auth}},
			&webpush.Options{Subscriber: p.Contact, VAPIDPublicKey: pub, VAPIDPrivateKey: priv, TTL: 24 * 3600, Urgency: urgency, Topic: topic(m.Tag)})
		if err != nil {
			last = err
			p.DB.PushResult(ctx, s.ID, false, false)
			continue
		}
		res.Body.Close()
		switch {
		case res.StatusCode == http.StatusGone || res.StatusCode == http.StatusNotFound:
			p.DB.PushResult(ctx, s.ID, false, true) // the browser unsubscribed
		case res.StatusCode >= 300:
			last = errors.New(res.Status)
			p.DB.PushResult(ctx, s.ID, false, false)
		default:
			ok++
			p.DB.PushResult(ctx, s.ID, true, false)
		}
	}
	return ok, last
}

// topic is a push-service topic: at most 32 URL-safe characters.
func topic(tag string) string {
	out := make([]rune, 0, 32)
	for _, r := range tag {
		if len(out) == 32 {
			break
		}
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			out = append(out, r)
		}
	}
	return string(out)
}
