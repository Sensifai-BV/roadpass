package api

import (
	"context"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sensifai/roadpass/internal/alerts"
	"github.com/sensifai/roadpass/internal/auth"
	"github.com/sensifai/roadpass/internal/store"
)

// The public contact form: stored for the platform operator, who reads it under
// Platform → Messages. When the platform has a mail server the operator's admins
// are also e-mailed. The sender gets no mail back, so the form cannot be used to
// send mail to somebody else's address.

var contactTopics = map[string]bool{"general": true, "demo": true, "pilot": true, "integration": true, "support": true, "press": true}

func (s *Server) contactSend(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name      string `json:"name"`
		Email     string `json:"email"`
		Company   string `json:"company"`
		Topic     string `json:"topic"`
		Message   string `json:"message"`
		Website   string `json:"website"` // honeypot: real people leave it empty
		Turnstile string `json:"turnstileToken"`
	}
	if !decode(w, r, &in, 16<<10) {
		return
	}
	ctx := r.Context()
	if ok, _, _ := s.Cache.Allow(ctx, "contact:"+clientIP(r), 5, time.Hour); !ok {
		writeProblem(w, r, http.StatusTooManyRequests, "Too many messages", "Please wait a while before sending another.")
		return
	}
	if in.Website != "" { // a bot: pretend it worked
		w.WriteHeader(http.StatusNoContent)
		return
	}
	in.Name, in.Email, in.Company = strings.TrimSpace(in.Name), strings.TrimSpace(in.Email), strings.TrimSpace(in.Company)
	in.Message = strings.TrimSpace(in.Message)
	if in.Topic == "" {
		in.Topic = "general"
	}
	var errs []fieldErr
	if n := utf8.RuneCountInString(in.Name); n < 1 || n > 120 {
		errs = append(errs, fieldErr{"name", "Tell us your name (up to 120 characters)."})
	}
	if a, err := mail.ParseAddress(in.Email); err != nil || a.Address != in.Email || len(in.Email) > 200 {
		errs = append(errs, fieldErr{"email", "Enter a valid e-mail address."})
	}
	if utf8.RuneCountInString(in.Company) > 160 {
		errs = append(errs, fieldErr{"company", "Keep the organisation name under 160 characters."})
	}
	if !contactTopics[in.Topic] {
		errs = append(errs, fieldErr{"topic", "Choose one of the topics."})
	}
	if n := utf8.RuneCountInString(in.Message); n < 10 || n > 5000 {
		errs = append(errs, fieldErr{"message", "Write between 10 and 5000 characters."})
	}
	if len(errs) > 0 {
		writeProblemErrs(w, r, http.StatusUnprocessableEntity, "Message not sent", "", errs)
		return
	}
	if !s.passTurnstile(w, r, in.Turnstile, "Message not sent") {
		return
	}
	m := store.ContactMessage{ID: auth.ID("msg_"), Name: in.Name, Email: in.Email, Company: in.Company, Topic: in.Topic, Message: in.Message}
	if err := s.St.AddContactMessage(ctx, m); err != nil {
		fail(w, r, err)
		return
	}
	go s.notifyContact(context.WithoutCancel(ctx), m)
	w.WriteHeader(http.StatusNoContent)
}

// notifyContact e-mails the operator's admins about a new message, best effort.
func (s *Server) notifyContact(ctx context.Context, m store.ContactMessage) {
	if !s.mailReady() {
		return
	}
	admins, err := s.St.OperatorAdmins(ctx)
	if err != nil {
		slog.Warn("contact e-mail: admins", "err", err)
		return
	}
	text := "New message from the ROADPASS contact form.\n\nFrom: " + m.Name + " <" + m.Email + ">\n"
	if m.Company != "" {
		text += "Organisation: " + m.Company + "\n"
	}
	text += "Topic: " + m.Topic + "\n\n" + m.Message
	for _, to := range admins {
		if err := alerts.SendMail(ctx, s.Notify.Senders.Mail(ctx), to, alerts.Message{Subject: "Contact form: " + m.Topic + " from " + m.Name, Text: text,
			Severity: "info", State: "contact", Link: strings.TrimRight(s.Cfg.PublicURL, "/") + "/platform/messages"}); err != nil {
			slog.Warn("contact e-mail failed", "err", err)
		}
	}
}

func (s *Server) platformContact(w http.ResponseWriter, r *http.Request) {
	list, err := s.St.ContactMessages(r.Context(), 200)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) platformContactHandled(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Handled bool `json:"handled"`
	}
	if !decode(w, r, &in, 256) {
		return
	}
	ok, err := s.St.SetContactHandled(r.Context(), r.PathValue("id"), in.Handled)
	if err != nil {
		fail(w, r, err)
		return
	}
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "Message not found", "")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
