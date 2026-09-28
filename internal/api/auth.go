package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/sensifai/roadpass/internal/auth"
	"github.com/sensifai/roadpass/internal/cache"
	"github.com/sensifai/roadpass/internal/domain"
	"github.com/sensifai/roadpass/internal/ingest"
	"github.com/sensifai/roadpass/internal/store"
)

const cookieName = "rp_session"

// Principal is the authenticated caller.
type Principal struct {
	User   *store.User
	Key    *store.APIKey
	Org    domain.Org
	Viewer store.Viewer
}

// Actor names the caller in the audit log.
func (p *Principal) Actor() string {
	if p.User != nil {
		return p.User.ID
	}
	if p.Key != nil {
		return p.Key.ID
	}
	return "anonymous"
}

// IngestActor is who signs a submission: a member signs with their personal key
// as well as the company key; an API key with the company key only.
func (p *Principal) IngestActor() ingest.Actor {
	a := ingest.Actor{ID: p.Actor()}
	if p.User != nil {
		a.UserID = p.User.ID
		if p.User.Role != "admin" {
			a.Steps = p.User.Steps
		}
	}
	return a
}

// Role is the user's role; API keys act as editors within their scopes.
func (p *Principal) Role() string {
	if p.User != nil {
		return p.User.Role
	}
	return "editor"
}

var roleRank = map[string]int{"viewer": 1, "editor": 2, "admin": 3}

// can reports whether the principal has at least role.
func (p *Principal) can(role string) bool { return roleRank[p.Role()] >= roleRank[role] }

// Duties are the jobs the caller may do. Admins hold every duty; editors the
// ones assigned to them; viewers none. API keys carry their own duties.
func (p *Principal) Duties() []string {
	if p.Org.Kind.ReadOnly() {
		return []string{}
	}
	if p.Key != nil {
		return p.Key.Duties
	}
	switch p.User.Role {
	case "admin":
		return domain.AllDuties()
	case "editor":
		return p.User.Duties
	}
	return []string{}
}

// Operator reports platform-operator staff.
func (p *Principal) Operator() bool { return p.Org.Kind == domain.KindOperator }

func principal(ctx context.Context) *Principal {
	p, _ := ctx.Value(keyPrincipal).(*Principal)
	return p
}

// guard describes what a route requires.
type guard struct {
	role     string // minimum role ("" = any signed-in caller)
	scope    string // API-key scope required ("" = sessions only)
	operator bool   // platform operator only
	optional bool   // anonymous callers allowed (principal may be nil)
}

// keyCache keeps API-key lookups for 15 s (positive answers only), so a revoked
// key stops working within 15 s and a burst of requests costs one query.
type keyCache struct {
	mu sync.Mutex
	m  map[string]keyEntry
}

type keyEntry struct {
	k   store.APIKey
	exp time.Time
}

func newKeyCache() *keyCache { return &keyCache{m: map[string]keyEntry{}} }

func (s *Server) lookupKey(ctx context.Context, raw string) (*store.APIKey, error) {
	h := string(auth.Digest(raw))
	s.keys.mu.Lock()
	e, ok := s.keys.m[h]
	s.keys.mu.Unlock()
	if ok && time.Now().Before(e.exp) {
		return &e.k, nil
	}
	k, err := s.St.APIKeyByHash(ctx, []byte(h))
	if err != nil {
		return nil, err
	}
	s.keys.mu.Lock()
	if len(s.keys.m) > 10000 {
		s.keys.m = map[string]keyEntry{}
	}
	s.keys.m[h] = keyEntry{k, time.Now().Add(15 * time.Second)}
	s.keys.mu.Unlock()
	go s.St.TouchAPIKey(context.WithoutCancel(ctx), k.ID) //nolint:errcheck // best effort
	return &k, nil
}

func bearer(r *http.Request) string {
	if k := r.Header.Get("X-API-Key"); k != "" {
		return k
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	return ""
}

// authenticate resolves the caller, or returns nil for an anonymous request.
func (s *Server) authenticate(r *http.Request) (*Principal, bool, error) {
	ctx := r.Context()
	if raw := bearer(r); raw != "" {
		k, err := s.lookupKey(ctx, raw)
		if errors.Is(err, store.ErrNotFound) {
			return nil, true, nil
		}
		if err != nil {
			return nil, true, err
		}
		o, err := s.St.Org(ctx, k.OrgID)
		if err != nil {
			return nil, true, err
		}
		return &Principal{Key: k, Org: o, Viewer: viewerOf(o)}, true, nil
	}
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" {
		return nil, false, nil
	}
	sess, err := s.Cache.GetSession(ctx, c.Value, s.Cfg.SessionTTL)
	if errors.Is(err, cache.ErrNoSession) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	u, err := s.St.UserByID(ctx, sess.UserID)
	if err != nil || u.DisabledAt != nil || s.sessionStale(ctx, u.ID, sess.CreatedAt) {
		return nil, false, nil
	}
	o, err := s.St.Org(ctx, u.OrgID)
	if err != nil {
		return nil, false, err
	}
	return &Principal{User: &u, Org: o, Viewer: viewerOf(o)}, false, nil
}

func viewerOf(o domain.Org) store.Viewer {
	return store.Viewer{OrgID: o.ID, Oversight: o.Kind.SeesEverything()}
}

// with wraps a handler with authentication and authorisation.
func (s *Server) with(g guard, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, viaKey, err := s.authenticate(r)
		if err != nil {
			fail(w, r, err)
			return
		}
		if p == nil {
			if g.optional {
				h(w, r)
				return
			}
			if viaKey {
				writeProblem(w, r, http.StatusUnauthorized, "Invalid API key", "The key is unknown or revoked.")
				return
			}
			writeProblem(w, r, http.StatusUnauthorized, "Sign in required", "")
			return
		}
		if p.Key != nil {
			if g.scope == "" || !slices.Contains(p.Key.Scopes, g.scope) {
				writeProblem(w, r, http.StatusForbidden, "Not allowed with an API key",
					"This operation needs the "+orDefault(g.scope, "dashboard")+" scope.")
				return
			}
			if ok, rem, reset := s.Cache.Allow(r.Context(), "key:"+p.Key.ID, s.Cfg.APIRatePerMin, time.Minute); !ok {
				w.Header().Set("Retry-After", itoa(int(reset.Seconds())+1))
				w.Header().Set("RateLimit-Remaining", itoa(rem))
				writeProblem(w, r, http.StatusTooManyRequests, "Rate limit", "Slow down; the limit resets within a minute.")
				return
			}
		} else if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("X-Requested-With") != "roadpass" {
			// Cookie-authenticated writes must come from our own scripts (CSRF).
			writeProblem(w, r, http.StatusForbidden, "Missing request header", "Browser writes need X-Requested-With: roadpass.")
			return
		}
		if g.operator && !p.Operator() {
			writeProblem(w, r, http.StatusForbidden, "Operators only", "")
			return
		}
		if g.role != "" && !p.can(g.role) {
			writeProblem(w, r, http.StatusForbidden, "Not allowed", "Your role ("+p.Role()+") cannot do this.")
			return
		}
		if p.Org.Demo && r.Method != http.MethodGet && r.Method != http.MethodHead && r.RemoteAddr != DemoSeedAddr && !s.demoGate(w, r, p) {
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), keyPrincipal, p)))
	}
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func clientIP(r *http.Request) string {
	if xf := r.Header.Get("X-Forwarded-For"); xf != "" {
		return strings.TrimSpace(strings.Split(xf, ",")[0])
	}
	h, _, _ := net.SplitHostPort(r.RemoteAddr)
	return h
}

// ---------------------------------------------------------------- session handlers

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, u store.User) error {
	tok := auth.Token("", 32)
	if err := s.Cache.PutSession(r.Context(), tok, cache.Session{UserID: u.ID, OrgID: u.OrgID, CreatedAt: time.Now().UTC()}, s.Cfg.SessionTTL); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: tok, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: strings.HasPrefix(s.Cfg.PublicURL, "https://"), MaxAge: int(s.Cfg.SessionTTL.Seconds())})
	_ = s.St.TouchLogin(r.Context(), u.ID)
	_ = s.St.Audit(r.Context(), u.OrgID, u.ID, "auth.login", u.Email, nil)
	return nil
}

type meResponse struct {
	Duties    []string    `json:"duties"`
	User      *store.User `json:"user"`
	Org       domain.Org  `json:"org"`
	Demo      bool        `json:"demoMode"`
	Operator  bool        `json:"operator"`
	Oversight bool        `json:"oversight"`
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	writeJSON(w, http.StatusOK, meResponse{Duties: p.Duties(), User: p.User, Org: p.Org, Demo: s.Cfg.DemoMode, Operator: p.Operator(), Oversight: p.Viewer.Oversight})
}

// updateMe changes the caller's own name and time zone.
func (s *Server) updateMe(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	var in struct {
		Name     string `json:"name"`
		Timezone string `json:"timezone"`
	}
	if !decode(w, r, &in, 1024) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	var errs []fieldErr
	if l := len([]rune(in.Name)); l < 1 || l > 80 {
		errs = append(errs, fieldErr{"name", "Enter your name (up to 80 characters)."})
	}
	if in.Timezone != "" {
		if _, err := time.LoadLocation(in.Timezone); err != nil || !strings.Contains(in.Timezone, "/") && in.Timezone != "UTC" {
			errs = append(errs, fieldErr{"timezone", "Pick a time zone from the list."})
		}
	}
	if errs != nil {
		writeProblemErrs(w, r, http.StatusUnprocessableEntity, "Settings not saved", "Fix the highlighted fields.", errs)
		return
	}
	if err := s.St.UpdateProfile(r.Context(), p.User.ID, in.Name, in.Timezone); err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "user.profile", p.User.ID, map[string]any{"timezone": in.Timezone})
	u, err := s.St.UserByID(r.Context(), p.User.ID)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

// changePassword replaces the caller's password after checking the current one.
func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	var in struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if !decode(w, r, &in, 4096) {
		return
	}
	if p.User.Demo {
		writeProblem(w, r, http.StatusForbidden, "Demo account", "Demo accounts sign in without a password; there is nothing to change.")
		return
	}
	if auth.CheckPassword(p.User.PasswordHash, in.Current) != nil {
		writeProblemErrs(w, r, http.StatusUnprocessableEntity, "Password not changed", "", []fieldErr{{"current", "That is not your current password."}})
		return
	}
	if len(in.New) < auth.MinPassword || len(in.New) > 200 {
		writeProblemErrs(w, r, http.StatusUnprocessableEntity, "Password not changed", "", []fieldErr{{"new", fmt.Sprintf("Use at least %d characters.", auth.MinPassword)}})
		return
	}
	hash, err := auth.HashPassword(in.New)
	if err != nil {
		fail(w, r, err)
		return
	}
	if err := s.St.SetPassword(r.Context(), p.User.ID, hash); err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "user.password", p.User.ID, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email     string `json:"email"`
		Password  string `json:"password"`
		Turnstile string `json:"turnstileToken"`
	}
	if !decode(w, r, &in, 4096) {
		return
	}
	if ok, _, _ := s.Cache.Allow(r.Context(), "login:"+clientIP(r), 20, 10*time.Minute); !ok {
		writeProblem(w, r, http.StatusTooManyRequests, "Too many attempts", "Wait a few minutes and try again.")
		return
	}
	if !s.passTurnstile(w, r, in.Turnstile, "Sign-in failed") {
		return
	}
	u, err := s.St.UserByEmail(r.Context(), strings.TrimSpace(in.Email))
	if err != nil || u.DisabledAt != nil || auth.CheckPassword(u.PasswordHash, in.Password) != nil {
		writeProblem(w, r, http.StatusUnauthorized, "Wrong e-mail or password", "")
		return
	}
	if err := s.startSession(w, r, u); err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "signed-in"})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		_ = s.Cache.DeleteSession(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	w.WriteHeader(http.StatusNoContent)
}

// demoUsers lists the seeded users that the demo picker may sign in as.
func (s *Server) demoUsers(w http.ResponseWriter, r *http.Request) {
	if !s.Cfg.DemoMode {
		writeProblem(w, r, http.StatusNotFound, "Not found", "")
		return
	}
	users, err := s.St.Users(r.Context(), "", true)
	if err != nil {
		fail(w, r, err)
		return
	}
	orgs, err := s.St.Orgs(r.Context())
	if err != nil {
		fail(w, r, err)
		return
	}
	byID := map[string]domain.Org{}
	for _, o := range orgs {
		byID[o.ID] = o
	}
	type item struct {
		UserID string         `json:"userId"`
		Name   string         `json:"name"`
		Role   string         `json:"role"`
		Duties []string       `json:"duties"`
		OrgID  string         `json:"orgId"`
		Org    string         `json:"org"`
		Kind   domain.OrgKind `json:"kind"`
		City   string         `json:"city"`
	}
	out := []item{}
	for _, u := range users {
		o := byID[u.OrgID]
		out = append(out, item{u.ID, u.Name, u.Role, u.Duties, o.ID, o.Name, o.Kind, o.City})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) demoLogin(w http.ResponseWriter, r *http.Request) {
	if !s.Cfg.DemoMode {
		writeProblem(w, r, http.StatusNotFound, "Not found", "")
		return
	}
	var in struct {
		UserID string `json:"userId"`
	}
	if !decode(w, r, &in, 1024) {
		return
	}
	u, err := s.St.UserByID(r.Context(), in.UserID)
	if err != nil || !u.Demo {
		writeProblem(w, r, http.StatusNotFound, "Unknown demo user", "")
		return
	}
	if err := s.startSession(w, r, u); err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "signed-in"})
}

func (s *Server) invitationInfo(w http.ResponseWriter, r *http.Request) {
	inv, err := s.St.InvitationByToken(r.Context(), auth.Digest(r.PathValue("token")))
	if err != nil {
		writeProblem(w, r, http.StatusNotFound, "Invitation not found", "It may have expired or been used already.")
		return
	}
	o, err := s.St.Org(r.Context(), inv.OrgID)
	if err != nil {
		fail(w, r, err)
		return
	}
	kinds := append([]domain.OrgKind{o.Kind}, o.Kinds...)
	writeJSON(w, http.StatusOK, map[string]any{"email": inv.Email, "role": inv.Role, "title": domain.RoleTitleIn(kinds, inv.Steps, inv.Role, inv.Duties),
		"org": map[string]string{"name": o.Name, "kind": string(o.Kind)}})
}

func (s *Server) acceptInvitation(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token    string `json:"token"`
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in, 4096) {
		return
	}
	inv, err := s.St.InvitationByToken(r.Context(), auth.Digest(in.Token))
	if err != nil {
		writeProblem(w, r, http.StatusNotFound, "Invitation not found", "It may have expired or been used already.")
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil || strings.TrimSpace(in.Name) == "" {
		writeProblemErrs(w, r, http.StatusUnprocessableEntity, "Please check the form", "", []domain.FieldError{{Field: "password", Message: fmt.Sprintf("at least %d characters, and enter your name", auth.MinPassword)}})
		return
	}
	u := store.User{ID: auth.ID("usr_"), OrgID: inv.OrgID, Email: inv.Email, Name: strings.TrimSpace(in.Name), Role: inv.Role, PasswordHash: hash}
	if err := s.St.AcceptInvitation(r.Context(), inv, u); err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), inv.OrgID, u.ID, "team.join", u.Email, nil)
	if err := s.startSession(w, r, u); err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "signed-in"})
}

func itoa(n int) string { return cache.Itoa(int64(n)) }
