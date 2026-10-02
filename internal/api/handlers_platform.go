package api

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/sensifai/roadpass/internal/alerts"
	"github.com/sensifai/roadpass/internal/auth"
	"github.com/sensifai/roadpass/internal/domain"
	"github.com/sensifai/roadpass/internal/store"
)

// The SaaS layer. A fresh installation has no accounts: the server logs a
// one-time setup link, and whoever opens it creates the platform organisation
// and the first platform manager. The manager then creates organisations,
// invites their first admins, manages users and billing. There is no public
// sign-up.

const setupTokens = "setup:tokens"

// AnnounceSetup logs a setup link when the installation has no platform manager
// yet. Every replica adds its own token, so any link it logged works for 24 h.
func (s *Server) AnnounceSetup(ctx context.Context) {
	needed, err := s.St.SetupNeeded(ctx)
	if err != nil || !needed {
		return
	}
	link, err := s.NewSetupLink(ctx)
	if err != nil {
		slog.Error("first-run setup: cannot create a setup link", "err", err)
		return
	}
	slog.Warn("first-run setup: no platform manager yet; open this link to create one (valid 24 h)", "url", link)
}

// NewSetupLink stores a fresh setup token and returns its link.
func (s *Server) NewSetupLink(ctx context.Context) (string, error) {
	tok := auth.Token("stp_", 24)
	d := hex.EncodeToString(auth.Digest(tok))
	if err := s.Cache.R.SAdd(ctx, setupTokens, d).Err(); err != nil {
		return "", err
	}
	if err := s.Cache.R.Expire(ctx, setupTokens, 24*time.Hour).Err(); err != nil {
		return "", err
	}
	return s.Cfg.PublicURL + "/setup?token=" + tok, nil
}

func (s *Server) setupStatus(w http.ResponseWriter, r *http.Request) {
	needed, err := s.St.SetupNeeded(r.Context())
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"needed": needed})
}

// setup creates the platform organisation and its first manager.
func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
		Org   struct {
			Name    string `json:"name"`
			Country string `json:"country"`
			City    string `json:"city"`
			Website string `json:"website"`
		} `json:"org"`
		User struct {
			Name     string `json:"name"`
			Email    string `json:"email"`
			Password string `json:"password"`
		} `json:"user"`
	}
	if !decode(w, r, &in, 8192) {
		return
	}
	ctx := r.Context()
	ok, err := s.Cache.R.SIsMember(ctx, setupTokens, hex.EncodeToString(auth.Digest(in.Token))).Result()
	if err != nil {
		fail(w, r, err)
		return
	}
	if !ok {
		writeProblem(w, r, http.StatusForbidden, "Setup link not valid", "Use the link the server logged at start-up, or run `roadpass setup-link` for a new one.")
		return
	}
	var errs []fieldErr
	in.Org.Name, in.User.Name = strings.TrimSpace(in.Org.Name), strings.TrimSpace(in.User.Name)
	in.User.Email = strings.ToLower(strings.TrimSpace(in.User.Email))
	in.Org.Country = strings.ToUpper(strings.TrimSpace(in.Org.Country))
	if len(in.Org.Name) < 2 {
		errs = append(errs, fieldErr{"org.name", "Name the platform operator's company."})
	}
	if !validCountry(in.Org.Country) {
		errs = append(errs, fieldErr{"org.country", "Pick the country."})
	}
	if in.User.Name == "" {
		errs = append(errs, fieldErr{"user.name", "Enter your name."})
	}
	if !strings.Contains(in.User.Email, "@") {
		errs = append(errs, fieldErr{"user.email", "Enter your work e-mail."})
	}
	if len(in.User.Password) < auth.MinPassword {
		errs = append(errs, fieldErr{"user.password", "Use at least 12 characters: this account manages the whole platform."})
	}
	if errs != nil {
		writeProblemErrs(w, r, http.StatusUnprocessableEntity, "Check the form", "", errs)
		return
	}
	hash, err := auth.HashPassword(in.User.Password)
	if err != nil {
		fail(w, r, err)
		return
	}
	// One setup only, even when two people open the link at the same time.
	tx, err := s.St.DB.Begin(ctx)
	if err != nil {
		fail(w, r, err)
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7349001)`); err != nil {
		fail(w, r, err)
		return
	}
	var done bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users u JOIN orgs o ON o.id=u.org_id WHERE o.kind='operator' AND u.role='admin' AND u.disabled_at IS NULL)`).Scan(&done); err != nil {
		fail(w, r, err)
		return
	}
	if done {
		writeProblem(w, r, http.StatusConflict, "Already set up", "This installation already has a platform manager. Sign in instead.")
		return
	}
	o := domain.Org{ID: auth.ID("org_"), Name: in.Org.Name, Kind: domain.KindOperator, Country: in.Org.Country, City: in.Org.City,
		Website: in.Org.Website, Provenance: domain.ProvOperational, Status: domain.OrgVerified}
	u := store.User{ID: auth.ID("usr_"), OrgID: o.ID, Email: in.User.Email, Name: in.User.Name, Role: "admin", PasswordHash: hash}
	if _, err := tx.Exec(ctx, `INSERT INTO orgs(id,name,kind,kinds,city,province,region,country,lat,lon,description,website,provenance,status,verified_at)
		VALUES ($1,$2,'operator','{operator}',$3,'','',$4,0,0,'',$5,$6,'verified',now())`, o.ID, o.Name, o.City, o.Country, o.Website, o.Provenance); err != nil {
		fail(w, r, err)
		return
	}
	if _, err := tx.Exec(ctx, `INSERT INTO users(id,org_id,email,name,role,duties,password_hash) VALUES ($1,$2,$3,$4,'admin','{}',$5)`,
		u.ID, u.OrgID, u.Email, u.Name, u.PasswordHash); err != nil {
		fail(w, r, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		fail(w, r, err)
		return
	}
	if err := s.Keyring.EnsureOrgKey(ctx, o.ID); err != nil {
		slog.Error("setup: company key", "err", err) // issued again on first use
	}
	if err := s.Notify.DB.InstallStarterRules(ctx, o.ID, o.Kind, func() string { return auth.ID("rul_") }); err != nil {
		slog.Error("setup: starter alerts", "err", err)
	}
	_ = s.Cache.R.Del(ctx, setupTokens).Err()
	_ = s.St.Audit(ctx, o.ID, u.ID, "platform.setup", o.ID, map[string]any{"manager": u.Email})
	u, _ = s.St.UserByID(ctx, u.ID)
	if err := s.startSession(w, r, u); err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"org": o, "user": u})
}

// ---------------------------------------------------------------- organisations

func (s *Server) platformOverview(w http.ResponseWriter, r *http.Request) {
	orgs, err := s.St.PlatformOrgs(r.Context())
	if err != nil {
		fail(w, r, err)
		return
	}
	plans, err := s.St.Plans(r.Context())
	if err != nil {
		fail(w, r, err)
		return
	}
	price := map[string]int64{}
	for _, p := range plans {
		price[p.ID] = p.PriceCents
	}
	out := struct {
		Orgs        map[string]int `json:"orgs"`
		Members     int            `json:"members"`
		Records     int            `json:"recordsThisMonth"`
		MRRCents    int64          `json:"mrrCents"`
		Trials      int            `json:"trials"`
		PastDue     int            `json:"pastDue"`
		OverLimit   int            `json:"overLimit"`
		Unsubscribe int            `json:"withoutPlan"`
	}{Orgs: map[string]int{}}
	limits := map[string]store.Plan{}
	for _, p := range plans {
		limits[p.ID] = p
	}
	for _, o := range orgs {
		if o.Kind != domain.KindOperator { // customers only, like the Organisations list
			out.Orgs[o.Status]++
		}
		out.Members += o.Members
		out.Records += o.Usage.Records
		switch {
		case o.Kind == domain.KindOperator:
		case o.Subscription == nil:
			out.Unsubscribe++
		case o.Subscription.Status == "active" || o.Subscription.Status == "past_due":
			out.MRRCents += price[o.Subscription.PlanID]
			if o.Subscription.Status == "past_due" {
				out.PastDue++
			}
		case o.Subscription.Status == "trial":
			out.Trials++
		}
		if o.Subscription != nil {
			p := limits[o.Subscription.PlanID]
			if (p.Seats >= 0 && o.Usage.Seats > p.Seats) || (p.RecordsPerMonth >= 0 && o.Usage.Records > p.RecordsPerMonth) {
				out.OverLimit++
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) platformOrgs(w http.ResponseWriter, r *http.Request) {
	orgs, err := s.St.PlatformOrgs(r.Context())
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, orgs)
}

type orgIn struct {
	Name        string           `json:"name"`
	Kind        domain.OrgKind   `json:"kind"`
	Kinds       []domain.OrgKind `json:"kinds"`
	TaxID       string           `json:"taxId"`
	Country     string           `json:"country"`
	Region      string           `json:"region"`
	City        string           `json:"city"`
	Website     string           `json:"website"`
	Description string           `json:"description"`
	Verified    bool             `json:"verified"`
	PlanID      string           `json:"planId"`
	Trial       bool             `json:"trial"`
	AdminEmail  string           `json:"adminEmail"`
	// Test marks a test or demonstration organisation: its records are labelled
	// Simulated, never Operational. One way only (nil on update = unchanged): a
	// test organisation does not become an operational one; create a new one.
	Test *bool `json:"test,omitempty"`
}

func (in *orgIn) check(creating bool) []fieldErr {
	var errs []fieldErr
	in.Name, in.Country = strings.TrimSpace(in.Name), strings.ToUpper(strings.TrimSpace(in.Country))
	in.AdminEmail = strings.ToLower(strings.TrimSpace(in.AdminEmail))
	if len(in.Name) < 2 {
		errs = append(errs, fieldErr{"name", "Enter the company name."})
	}
	if creating && (!in.Kind.Valid() || in.Kind == domain.KindOperator) {
		errs = append(errs, fieldErr{"kind", "Pick what the company does in the chain."})
	}
	if !validCountry(in.Country) {
		errs = append(errs, fieldErr{"country", "Pick the country."})
	}
	if creating && !strings.Contains(in.AdminEmail, "@") {
		errs = append(errs, fieldErr{"adminEmail", "Enter the e-mail of the company's first admin."})
	}
	return errs
}

// platformCreateOrg creates an organisation, its plan and its company key, and
// invites its first admin.
func (s *Server) platformCreateOrg(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	var in orgIn
	if !decode(w, r, &in, 8192) {
		return
	}
	if errs := in.check(true); errs != nil {
		writeProblemErrs(w, r, http.StatusUnprocessableEntity, "Organisation not created", "Fix the highlighted fields.", errs)
		return
	}
	ctx := r.Context()
	o := domain.Org{ID: auth.ID("org_"), Name: in.Name, Kind: in.Kind, Kinds: in.Kinds, TaxID: in.TaxID, Country: in.Country,
		Region: in.Region, City: in.City, Website: in.Website, Description: in.Description, Provenance: domain.ProvOperational,
		Status: domain.OrgPending}
	if in.Test != nil && *in.Test {
		o.Provenance = domain.ProvSimulated
	}
	if in.Verified {
		now := time.Now().UTC()
		o.Status, o.VerifiedAt = domain.OrgVerified, &now
	}
	if err := s.St.InsertOrg(ctx, o); err != nil {
		fail(w, r, err)
		return
	}
	if err := s.Keyring.EnsureOrgKey(ctx, o.ID); err != nil {
		fail(w, r, err)
		return
	}
	if err := s.Notify.DB.InstallStarterRules(ctx, o.ID, o.Kind, func() string { return auth.ID("rul_") }); err != nil {
		fail(w, r, err)
		return
	}
	if in.PlanID != "" {
		sub := store.Subscription{OrgID: o.ID, PlanID: in.PlanID, Status: "active"}
		if in.Trial {
			t := time.Now().AddDate(0, 0, 30)
			sub.Status, sub.TrialEndsAt = "trial", &t
		}
		if err := s.St.SetSubscription(ctx, sub, p.Actor()); err != nil {
			fail(w, r, err)
			return
		}
	}
	link, inv, err := s.inviteTo(ctx, o.ID, in.AdminEmail, "admin", nil, p.Actor())
	if err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(ctx, o.ID, p.Actor(), "platform.org.create", o.ID, map[string]any{"name": o.Name, "admin": in.AdminEmail, "plan": in.PlanID})
	emailed := s.mailInvitation(ctx, in.AdminEmail, o.Name, inviterName(p), "its administrator", link)
	writeJSON(w, http.StatusCreated, map[string]any{"org": o, "invitation": inv, "link": link, "emailed": emailed})
}

// inviteTo creates a one-time invitation and returns its link.
func (s *Server) inviteTo(ctx context.Context, orgID, email, role string, duties []string, by string) (string, store.Invitation, error) {
	tok := auth.Token("inv_", 24)
	inv := store.Invitation{ID: auth.ID("ivt_"), OrgID: orgID, Email: email, Role: role, Duties: duties, InvitedBy: by,
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour)}
	if err := s.St.CreateInvitation(ctx, inv, auth.Digest(tok)); err != nil {
		return "", inv, err
	}
	return s.Cfg.PublicURL + "/join/" + tok, inv, nil
}

func (s *Server) platformUpdateOrg(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	if s.demoProtected(w, r, r.PathValue("id")) {
		return
	}
	var in orgIn
	if !decode(w, r, &in, 8192) {
		return
	}
	if errs := in.check(false); errs != nil {
		writeProblemErrs(w, r, http.StatusUnprocessableEntity, "Organisation not saved", "Fix the highlighted fields.", errs)
		return
	}
	o, err := s.St.Org(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, r, err)
		return
	}
	if in.Test != nil && !*in.Test && o.Provenance == domain.ProvSimulated {
		writeProblem(w, r, http.StatusConflict, "Still a test organisation",
			"A test organisation's records are Simulated and stay so. Create a new organisation for operational data.")
		return
	}
	o.Name, o.TaxID, o.Country, o.Region, o.City, o.Website, o.Description = in.Name, in.TaxID, in.Country, in.Region, in.City, in.Website, in.Description
	if err := s.St.UpdateOrgProfile(r.Context(), o); err != nil {
		fail(w, r, err)
		return
	}
	if in.Test != nil && *in.Test && o.Provenance != domain.ProvSimulated {
		// Only records filed from now on: signed and sealed ones keep their label.
		if err := s.St.MarkOrgSimulated(r.Context(), o.ID); err != nil {
			fail(w, r, err)
			return
		}
		o.Provenance = domain.ProvSimulated
	}
	_ = s.St.Audit(r.Context(), o.ID, p.Actor(), "platform.org.update", o.ID, nil)
	writeJSON(w, http.StatusOK, o)
}

// platformOrgStatus verifies, suspends or reactivates an organisation.
// Suspension revokes the company's key and every member's personal key (the
// worker records it on the ledger; past signatures stay valid). Verification
// and reactivation make sure the company has a usable key.
func (s *Server) platformOrgStatus(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	if s.demoProtected(w, r, r.PathValue("id")) {
		return
	}
	var in struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if !decode(w, r, &in, 1024) {
		return
	}
	if !slices.Contains([]string{domain.OrgVerified, domain.OrgSuspended, domain.OrgPending}, in.Status) {
		writeProblem(w, r, http.StatusUnprocessableEntity, "Unknown status", "Use verified, suspended or pending.")
		return
	}
	id := r.PathValue("id")
	if id == p.Org.ID && in.Status != domain.OrgVerified {
		writeProblem(w, r, http.StatusConflict, "That is the platform itself", "The platform organisation cannot be suspended.")
		return
	}
	ctx := r.Context()
	if err := s.St.SetOrgStatus(ctx, id, in.Status, p.Actor()); err != nil {
		fail(w, r, err)
		return
	}
	switch in.Status {
	case domain.OrgSuspended:
		if err := s.Keyring.RevokeOrg(ctx, id, orDefault(in.Reason, "organisation suspended")); err != nil {
			fail(w, r, err)
			return
		}
	case domain.OrgVerified:
		if err := s.Keyring.EnsureOrgKey(ctx, id); err != nil {
			fail(w, r, err)
			return
		}
	}
	_ = s.St.Audit(ctx, id, p.Actor(), "org."+in.Status, id, map[string]string{"reason": in.Reason})
	s.Cache.Publish(ctx, cacheEvent("org", id, in.Status))
	msg := map[string]alerts.Notification{
		domain.OrgVerified:  {Kind: "org", Severity: "info", Title: "Your organisation is verified", Body: "Your records are now sealed on the ledger.", Link: "/ledger"},
		domain.OrgSuspended: {Kind: "org", Severity: "critical", Title: "Your organisation is suspended", Body: orDefault(in.Reason, "Contact the platform operator."), Link: "/settings?tab=organisation"},
	}[in.Status]
	if msg.Title != "" {
		s.Notify.Notify(ctx, id, []string{"role:admin"}, msg)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) platformInvite(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	if s.demoProtected(w, r, r.PathValue("id")) {
		return
	}
	var in struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if !decode(w, r, &in, 1024) {
		return
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if !strings.Contains(in.Email, "@") || roleRank[in.Role] == 0 {
		writeProblem(w, r, http.StatusUnprocessableEntity, "Check the invitation", "A valid e-mail and a role are required.")
		return
	}
	if _, err := s.St.Org(r.Context(), r.PathValue("id")); err != nil {
		fail(w, r, err)
		return
	}
	link, inv, err := s.inviteTo(r.Context(), r.PathValue("id"), in.Email, in.Role, nil, p.Actor())
	if err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), r.PathValue("id"), p.Actor(), "platform.invite", in.Email, map[string]string{"role": in.Role})
	o, _ := s.St.Org(r.Context(), r.PathValue("id"))
	emailed := s.mailInvitation(r.Context(), in.Email, o.Name, inviterName(p), "an administrator", link)
	writeJSON(w, http.StatusCreated, map[string]any{"invitation": inv, "link": link, "emailed": emailed})
}

// ---------------------------------------------------------------- users

func (s *Server) platformUsers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	users, err := s.St.PlatformUsers(r.Context(), strings.TrimSpace(q.Get("q")), q.Get("org"), q.Get("disabled") == "1", limit)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, nonNil(users))
}

func (s *Server) platformUserStatus(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	var in struct {
		Disabled bool `json:"disabled"`
	}
	if !decode(w, r, &in, 256) {
		return
	}
	id := r.PathValue("id")
	if id == p.User.ID {
		writeProblem(w, r, http.StatusConflict, "That is you", "Ask another platform manager.")
		return
	}
	u, err := s.St.UserByID(r.Context(), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	if s.demoProtected(w, r, u.OrgID) {
		return
	}
	if err := s.St.SetUserDisabled(r.Context(), id, in.Disabled); err != nil {
		fail(w, r, err)
		return
	}
	if in.Disabled {
		if err := s.Keyring.Revoke(r.Context(), u.OrgID, id, "account disabled by the platform"); err != nil {
			fail(w, r, err)
			return
		}
	}
	action := "platform.user.enable"
	if in.Disabled {
		action = "platform.user.disable"
	}
	_ = s.St.Audit(r.Context(), u.OrgID, p.Actor(), action, id, nil)
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------- billing

func (s *Server) platformPlans(w http.ResponseWriter, r *http.Request) {
	plans, err := s.St.Plans(r.Context())
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, plans)
}

func (s *Server) platformUpdatePlan(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	var in store.Plan
	if !decode(w, r, &in, 4096) {
		return
	}
	in.ID = r.PathValue("id")
	if strings.TrimSpace(in.Name) == "" || in.PriceCents < 0 || in.Seats < -1 || in.RecordsPerMonth < -1 || len(in.Currency) != 3 {
		writeProblem(w, r, http.StatusUnprocessableEntity, "Check the plan", "A name, a price of 0 or more, a three-letter currency, and limits of -1 (unlimited) or more.")
		return
	}
	if err := s.St.UpdatePlan(r.Context(), in); err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "platform.plan.update", in.ID, in)
	writeJSON(w, http.StatusOK, in)
}

func (s *Server) platformSetSubscription(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	if s.demoProtected(w, r, r.PathValue("id")) {
		return
	}
	var in store.Subscription
	if !decode(w, r, &in, 1024) {
		return
	}
	in.OrgID = r.PathValue("id")
	if !slices.Contains([]string{"trial", "active", "past_due", "cancelled"}, in.Status) {
		writeProblem(w, r, http.StatusUnprocessableEntity, "Unknown status", "Use trial, active, past_due or cancelled.")
		return
	}
	if err := s.St.SetSubscription(r.Context(), in, p.Actor()); err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), in.OrgID, p.Actor(), "platform.subscription", in.OrgID, map[string]string{"plan": in.PlanID, "status": in.Status})
	w.WriteHeader(http.StatusNoContent)
}

func monthParam(v string) (*time.Time, error) {
	if v == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01", v)
	if err != nil {
		return nil, errors.New("month must look like 2026-10")
	}
	return &t, nil
}

func (s *Server) platformInvoices(w http.ResponseWriter, r *http.Request) {
	m, err := monthParam(r.URL.Query().Get("month"))
	if err != nil {
		writeProblem(w, r, http.StatusUnprocessableEntity, "Check the month", err.Error())
		return
	}
	inv, err := s.St.Invoices(r.Context(), r.URL.Query().Get("org"), m)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, inv)
}

func (s *Server) platformGenerateInvoices(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	var in struct {
		Month string `json:"month"`
	}
	if !decode(w, r, &in, 256) {
		return
	}
	m, err := monthParam(in.Month)
	if err != nil || m == nil {
		writeProblem(w, r, http.StatusUnprocessableEntity, "Check the month", "Send a month such as 2026-10.")
		return
	}
	n, err := s.St.GenerateInvoices(r.Context(), *m, func() string { return auth.ID("inv_") })
	if err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "platform.invoices.generate", in.Month, map[string]int{"drafted": n})
	writeJSON(w, http.StatusOK, map[string]int{"drafted": n})
}

func (s *Server) platformInvoiceStatus(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	var in struct {
		Status string `json:"status"`
	}
	if !decode(w, r, &in, 256) {
		return
	}
	inv, err := s.St.SetInvoiceStatus(r.Context(), r.PathValue("id"), in.Status)
	if errors.Is(err, store.ErrConflict) {
		writeProblem(w, r, http.StatusConflict, "Not allowed", strings.TrimPrefix(err.Error(), "conflict: "))
		return
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), inv.OrgID, p.Actor(), "platform.invoice."+in.Status, inv.ID, map[string]string{"number": inv.Number})
	if in.Status == "issued" {
		s.Notify.Notify(r.Context(), inv.OrgID, []string{"role:admin"}, alerts.Notification{Kind: "billing", Severity: "info",
			Title: "Invoice " + inv.Number + " issued", Body: fmt.Sprintf("%.2f %s for %s.", float64(inv.AmountCents)/100, inv.Currency, inv.Period.Format("January 2006")),
			Link: "/settings?tab=billing"})
		s.mailInvoice(r.Context(), inv)
	}
	writeJSON(w, http.StatusOK, inv)
}

// orgBilling shows an organisation's admins their plan, usage and invoices.
func (s *Server) orgBilling(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	ctx := r.Context()
	orgs, err := s.St.PlatformOrgs(ctx)
	if err != nil {
		fail(w, r, err)
		return
	}
	var me *store.OrgSummary
	for i := range orgs {
		if orgs[i].ID == p.Org.ID {
			me = &orgs[i]
		}
	}
	plans, err := s.St.Plans(ctx)
	if err != nil {
		fail(w, r, err)
		return
	}
	inv, err := s.St.Invoices(ctx, p.Org.ID, nil)
	if err != nil {
		fail(w, r, err)
		return
	}
	inv = slices.DeleteFunc(inv, func(i store.Invoice) bool { return i.Status == "draft" })
	out := map[string]any{"invoices": inv, "plans": plans}
	if me != nil {
		out["subscription"], out["usage"] = me.Subscription, me.Usage
	}
	writeJSON(w, http.StatusOK, out)
}

// mailInvoice e-mails an issued invoice to the organisation's admins (billing
// mail does not depend on personal notification preferences).
func (s *Server) mailInvoice(ctx context.Context, inv store.Invoice) {
	if !s.mailReady() {
		return
	}
	users, err := s.St.Users(ctx, inv.OrgID, false)
	if err != nil {
		slog.Warn("invoice e-mail: members", "err", err)
		return
	}
	text := fmt.Sprintf("Invoice %s for %s: %.2f %s.\nSee the invoice lines and your plan's usage under Settings → Billing.",
		inv.Number, inv.Period.Format("January 2006"), float64(inv.AmountCents)/100, inv.Currency)
	for _, u := range users {
		if u.Role != "admin" || u.DisabledAt != nil {
			continue
		}
		if err := alerts.SendMail(context.WithoutCancel(ctx), s.Notify.Senders.Mail(ctx), u.Email, alerts.Message{Subject: "ROADPASS invoice " + inv.Number,
			Text: text, Severity: "info", State: "billing", Link: strings.TrimRight(s.Cfg.PublicURL, "/") + "/settings?tab=billing"}); err != nil {
			slog.Warn("invoice e-mail failed", "err", err)
		}
	}
}
