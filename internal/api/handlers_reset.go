package api

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/sensifai/roadpass/internal/alerts"
	"github.com/sensifai/roadpass/internal/auth"
)

// Password reset: a one-time link by e-mail, valid for an hour. The request
// never says whether an account exists; the token is stored hashed in Redis.

const (
	resetTTL     = time.Hour
	keyReset     = "rp:pwreset:%s" // sha256(token) → user id
	keyPwChanged = "rp:pwset:%s"   // user id → unix time of the last reset
)

func (s *Server) forgotPassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
	}
	if !decode(w, r, &in, 1024) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	ctx := r.Context()
	if ok, _, _ := s.Cache.Allow(ctx, "pwreset:"+clientIP(r), 10, time.Hour); !ok {
		writeProblem(w, r, http.StatusTooManyRequests, "Too many requests", "Wait a while and try again.")
		return
	}
	// The same answer whatever happens below.
	defer w.WriteHeader(http.StatusNoContent)
	if email == "" || !s.mailReady() {
		return
	}
	if ok, _, _ := s.Cache.Allow(ctx, "pwreset-mail:"+email, 3, time.Hour); !ok {
		return
	}
	u, err := s.St.UserByEmail(ctx, email)
	if err != nil || u.DisabledAt != nil || u.Demo {
		return
	}
	token := auth.Token("rst_", 32)
	if err := s.Cache.R.Set(ctx, fmt.Sprintf(keyReset, hex.EncodeToString(auth.Digest(token))), u.ID, resetTTL).Err(); err != nil {
		slog.Warn("password reset not stored", "err", err)
		return
	}
	link := strings.TrimRight(s.Cfg.PublicURL, "/") + "/reset/" + token
	text := strings.Join([]string{
		"Someone asked to reset the password of your ROADPASS account.",
		"Open the link to choose a new one. It works once and expires in an hour; your other sessions are signed out.",
		"If it was not you, ignore this e-mail: your password stays as it is.",
	}, "\n")
	if err := alerts.SendMail(context.WithoutCancel(ctx), s.Notify.Senders.Mail(ctx), u.Email, alerts.Message{Subject: "Reset your ROADPASS password", Text: text,
		Severity: "info", State: "reset", Link: link}); err != nil {
		slog.Warn("password reset e-mail failed", "err", err)
	}
	_ = s.St.Audit(ctx, u.OrgID, u.ID, "user.password.reset_requested", u.ID, nil)
}

func (s *Server) resetPassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in, 4096) {
		return
	}
	ctx := r.Context()
	key := fmt.Sprintf(keyReset, hex.EncodeToString(auth.Digest(in.Token)))
	userID, err := s.Cache.R.Get(ctx, key).Result()
	if err != nil || userID == "" {
		writeProblem(w, r, http.StatusNotFound, "Reset link not valid", "It may have expired or been used already. Ask for a new one from the sign-in page.")
		return
	}
	u, err := s.St.UserByID(ctx, userID)
	if err != nil || u.DisabledAt != nil {
		writeProblem(w, r, http.StatusNotFound, "Reset link not valid", "Ask for a new one from the sign-in page.")
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		writeProblemErrs(w, r, http.StatusUnprocessableEntity, "Password not changed", "", []fieldErr{{"password", fmt.Sprintf("Use at least %d characters.", auth.MinPassword)}})
		return
	}
	if err := s.St.SetPassword(ctx, u.ID, hash); err != nil {
		fail(w, r, err)
		return
	}
	// One use, and every earlier session of this person ends.
	_ = s.Cache.R.Del(ctx, key).Err()
	_ = s.Cache.R.Set(ctx, fmt.Sprintf(keyPwChanged, u.ID), time.Now().Unix(), s.Cfg.SessionTTL+time.Hour).Err()
	_ = s.St.Audit(ctx, u.OrgID, u.ID, "user.password.reset", u.ID, nil)
	if err := s.startSession(w, r, u); err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "signed-in"})
}

// sessionStale reports whether a session predates the person's last password reset.
func (s *Server) sessionStale(ctx context.Context, userID string, created time.Time) bool {
	at, err := s.Cache.R.Get(ctx, fmt.Sprintf(keyPwChanged, userID)).Int64()
	return err == nil && created.Unix() < at
}
