package api

import (
	"errors"
	"net/http"
	"net/mail"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/sensifai/roadpass/internal/alerts"
)

// The platform's outgoing mail server, set by the platform manager. It sends
// every system e-mail: invitations, password resets, alerts, reports, invoices.

type mailSettingsOut struct {
	alerts.MailSettings
	PasswordSet bool   `json:"passwordSet"`
	Source      string `json:"source"` // platform | environment | "" (none)
	EnvHost     string `json:"envHost,omitempty"`
}

func (s *Server) platformMail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ms, ref, err := alerts.LoadMailSettings(ctx, s.St.DB)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		fail(w, r, err)
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		ms = alerts.MailSettings{TLS: "starttls"}
	}
	env := s.Notify.Senders.SMTP
	writeJSON(w, http.StatusOK, mailSettingsOut{MailSettings: ms, PasswordSet: ref != "", Source: s.Notify.Senders.MailSource(ctx), EnvHost: env.Host})
}

func (s *Server) platformSaveMail(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	var in struct {
		alerts.MailSettings
		Password string `json:"password"`
	}
	if !decode(w, r, &in, 8192) {
		return
	}
	in.Host, in.From, in.Username = strings.TrimSpace(in.Host), strings.TrimSpace(in.From), strings.TrimSpace(in.Username)
	var errs []fieldErr
	if in.Host == "" || strings.ContainsAny(in.Host, " /\r\n") {
		errs = append(errs, fieldErr{"host", "Enter the mail server's host name, e.g. smtp.example.com."})
	}
	if in.Port != "" {
		if n, err := strconv.Atoi(in.Port); err != nil || n < 1 || n > 65535 {
			errs = append(errs, fieldErr{"port", "A port between 1 and 65535, or empty for the usual one."})
		}
	}
	if in.TLS != "starttls" && in.TLS != "tls" && in.TLS != "none" {
		errs = append(errs, fieldErr{"tls", "Choose STARTTLS, TLS or none."})
	}
	if a, err := mail.ParseAddress(in.From); err != nil || a.Address == "" {
		errs = append(errs, fieldErr{"from", "A sender address, e.g. ROADPASS <no-reply@example.com>."})
	}
	if in.ReplyTo != "" {
		if _, err := mail.ParseAddress(in.ReplyTo); err != nil {
			errs = append(errs, fieldErr{"replyTo", "A valid e-mail address, or leave it empty."})
		}
	}
	if errs != nil {
		writeProblemErrs(w, r, http.StatusUnprocessableEntity, "Mail settings not saved", "Fix the highlighted fields.", errs)
		return
	}
	if err := alerts.SaveMailSettings(r.Context(), s.St.DB, s.Notify.Secrets, in.MailSettings, in.Password, p.Actor()); err != nil {
		fail(w, r, err)
		return
	}
	s.Notify.Senders.ForgetMail()
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "platform.mail.update", "mail", map[string]any{"host": in.Host, "from": in.From, "passwordChanged": in.Password != ""})
	s.platformMail(w, r)
}

func (s *Server) platformDeleteMail(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	if err := alerts.DeleteMailSettings(r.Context(), s.St.DB, s.Notify.Secrets); err != nil {
		fail(w, r, err)
		return
	}
	s.Notify.Senders.ForgetMail()
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "platform.mail.reset", "mail", nil)
	s.platformMail(w, r)
}

// platformTestMail sends a test message with the settings in force now.
func (s *Server) platformTestMail(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	var in struct {
		To string `json:"to"`
	}
	if !decode(w, r, &in, 1024) {
		return
	}
	to := strings.TrimSpace(in.To)
	if to == "" && p.User != nil {
		to = p.User.Email
	}
	cfg := s.Notify.Senders.Mail(r.Context())
	if !cfg.Configured() {
		writeProblem(w, r, http.StatusConflict, "No mail server", "Save the mail settings first.")
		return
	}
	err := alerts.SendMail(r.Context(), cfg, to, alerts.Message{Subject: "ROADPASS test e-mail", Severity: "info", State: "test",
		Text: "This is a test from your ROADPASS platform. Invitations, password resets, alerts, reports and invoices are sent through this mail server.",
		Link: s.Cfg.PublicURL})
	if err != nil {
		writeProblem(w, r, http.StatusBadGateway, "Test e-mail not sent", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"sentTo": to})
}
