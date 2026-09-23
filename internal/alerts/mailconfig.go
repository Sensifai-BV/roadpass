package alerts

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sensifai/roadpass/internal/secrets"
)

// The platform's outgoing mail server: set by the platform manager in the
// console, else taken from the environment (SMTP_*). Every mail the platform
// sends (invitations, password resets, alerts, reports, invoices) goes through
// Senders.Mail, so a change applies to the app and the worker within a minute.

// MailSettings is the stored form (the password lives in the secrets store).
type MailSettings struct {
	Host     string `json:"host"`
	Port     string `json:"port"`
	TLS      string `json:"tls"` // starttls | tls | none
	Username string `json:"username"`
	From     string `json:"from"`
	ReplyTo  string `json:"replyTo,omitempty"`
}

const mailCacheTTL = 30 * time.Second

type mailCache struct {
	mu  sync.Mutex
	at  time.Time
	cfg SMTPConfig
	ok  bool
}

// Mail is the mail server to use now: the platform's saved settings, else the
// environment default.
func (s *Senders) Mail(ctx context.Context) SMTPConfig {
	if s == nil {
		return SMTPConfig{}
	}
	if s.Load == nil {
		return s.SMTP
	}
	s.mail.mu.Lock()
	defer s.mail.mu.Unlock()
	if time.Since(s.mail.at) > mailCacheTTL {
		cfg, ok, err := s.Load(ctx)
		if err == nil {
			s.mail.cfg, s.mail.ok, s.mail.at = cfg, ok, time.Now()
		}
	}
	if s.mail.ok {
		return s.mail.cfg
	}
	return s.SMTP
}

// MailSource says where the mail settings come from: "platform", "environment" or "".
func (s *Senders) MailSource(ctx context.Context) string {
	cfg := s.Mail(ctx)
	switch {
	case !cfg.Configured():
		return ""
	case s.mail.ok:
		return "platform"
	}
	return "environment"
}

// ForgetMail drops the cached settings after the platform manager saved new ones.
func (s *Senders) ForgetMail() {
	s.mail.mu.Lock()
	s.mail.at = time.Time{}
	s.mail.mu.Unlock()
}

// PlatformMail loads the saved mail settings from the database.
func PlatformMail(db *pgxpool.Pool, sec *secrets.Store) func(context.Context) (SMTPConfig, bool, error) {
	return func(ctx context.Context) (SMTPConfig, bool, error) {
		ms, ref, err := LoadMailSettings(ctx, db)
		if errors.Is(err, pgx.ErrNoRows) {
			return SMTPConfig{}, false, nil
		}
		if err != nil {
			return SMTPConfig{}, false, err
		}
		cfg := SMTPConfig{Host: ms.Host, Port: ms.Port, Username: ms.Username, From: ms.From, TLS: ms.TLS, ReplyTo: ms.ReplyTo}
		if ref != "" && sec != nil {
			if v, err := sec.Get(ctx, ref); err == nil {
				cfg.Password = v["password"]
			}
		}
		return cfg, cfg.Configured(), nil
	}
}

// LoadMailSettings reads the stored settings and their secret reference.
func LoadMailSettings(ctx context.Context, db *pgxpool.Pool) (MailSettings, string, error) {
	var raw string
	var ref *string
	var ms MailSettings
	if err := db.QueryRow(ctx, `SELECT value, secret_ref FROM platform_settings WHERE key='mail'`).Scan(&raw, &ref); err != nil {
		return ms, "", err
	}
	if err := json.Unmarshal([]byte(raw), &ms); err != nil {
		return ms, "", err
	}
	if ref != nil {
		return ms, *ref, nil
	}
	return ms, "", nil
}

// SaveMailSettings stores the settings; password "" keeps the saved one.
func SaveMailSettings(ctx context.Context, db *pgxpool.Pool, sec *secrets.Store, ms MailSettings, password, by string) error {
	_, ref, err := LoadMailSettings(ctx, db)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if password != "" {
		if ref != "" {
			_ = sec.Delete(ctx, ref)
		}
		if ref, err = sec.Put(ctx, "platform-mail", map[string]string{"password": password}); err != nil {
			return err
		}
	}
	raw, _ := json.Marshal(ms)
	_, err = db.Exec(ctx, `INSERT INTO platform_settings(key, value, secret_ref, updated_by) VALUES ('mail', $1, NULLIF($2,''), $3)
		ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value, secret_ref=EXCLUDED.secret_ref, updated_at=now(), updated_by=EXCLUDED.updated_by`, string(raw), ref, by)
	return err
}

// DeleteMailSettings returns to the environment default.
func DeleteMailSettings(ctx context.Context, db *pgxpool.Pool, sec *secrets.Store) error {
	if _, ref, err := LoadMailSettings(ctx, db); err == nil && ref != "" {
		_ = sec.Delete(ctx, ref)
	}
	_, err := db.Exec(ctx, `DELETE FROM platform_settings WHERE key='mail'`)
	return err
}
