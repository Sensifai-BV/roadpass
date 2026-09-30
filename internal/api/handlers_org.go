package api

import (
	"encoding/base64"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/sensifai/roadpass/internal/auth"
	"github.com/sensifai/roadpass/internal/cache"
	"github.com/sensifai/roadpass/internal/checks"
	"github.com/sensifai/roadpass/internal/domain"
	"github.com/sensifai/roadpass/internal/lca"
	"github.com/sensifai/roadpass/internal/store"
)

func cacheEvent(kind, org, ref string) cache.Event {
	return cache.Event{Kind: kind, Orgs: []string{org}, Ref: ref}
}

// meta is the public catalogue the UI and integrators build on.
func (s *Server) meta(w http.ResponseWriter, r *http.Request) {
	types := make([]domain.TypeInfo, 0, len(domain.RecordTypes))
	for _, t := range domain.RecordTypes {
		types = append(types, domain.Types[t])
	}
	// The UI labels the data "Simulated" only when such records exist.
	var simulated bool
	_ = s.St.DB.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM records WHERE provenance = $1)`, domain.ProvSimulated).Scan(&simulated)
	w.Header().Set("Cache-Control", "no-cache")
	writeJSON(w, http.StatusOK, map[string]any{
		"simulated":       simulated,
		"version":         s.Version,
		"types":           types,
		"fields":          domain.Fields,
		"labTests":        domain.LabTests,
		"orgKinds":        domain.OrgKinds,
		"siteKinds":       domain.SiteKinds,
		"rules":           checks.Rules,
		"glossary":        domain.Glossary,
		"factors":         lca.Factors,
		"regionalFactors": lca.Regional,
		"scenarios":       lca.Scenarios,
		"provenances":     domain.Provenances,
		"duties":          domain.Duties,
		"dutyInfo":        domain.DutyInfo,
		"jobRoles":        domain.AllJobRoles(),
		"produces":        domain.Produces,
		"consumes":        domain.Consumes,
		"demoMode":        s.Cfg.DemoMode,
		"ledger":          s.Cfg.LedgerEnabled(),
		"channel":         s.Cfg.FabricChannel,
		"map":             map[string]string{"tiles": s.Cfg.TileURL, "attribution": s.Cfg.TileAttribution},
	})
}

// visibleOrgs is the set of organisations the caller may see: all of them for
// oversight, otherwise its own and those it already shares records with.
// Customers are isolated: there is no platform-wide directory.
func (s *Server) visibleOrgs(r *http.Request) (map[string]bool, error) {
	p := principal(r.Context())
	if p.Viewer.Oversight {
		return nil, nil
	}
	ids, err := s.St.LinkedOrgs(r.Context(), p.Org.ID)
	if err != nil {
		return nil, err
	}
	m := make(map[string]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m, nil
}

func (s *Server) listOrgs(w http.ResponseWriter, r *http.Request) {
	orgs, err := s.St.Orgs(r.Context())
	if err != nil {
		fail(w, r, err)
		return
	}
	vis, err := s.visibleOrgs(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	p := principal(r.Context())
	out := make([]domain.Org, 0, len(orgs))
	for _, o := range orgs {
		if vis != nil && !vis[o.ID] {
			continue
		}
		if !p.Viewer.Oversight && o.ID != p.Org.ID {
			o.TaxID = ""
		}
		out = append(out, o)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getOrg(w http.ResponseWriter, r *http.Request) {
	o, err := s.St.Org(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, r, err)
		return
	}
	vis, err := s.visibleOrgs(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	p := principal(r.Context())
	if vis != nil && !vis[o.ID] {
		fail(w, r, store.ErrNotFound)
		return
	}
	sites, err := s.St.Sites(r.Context(), o.ID)
	if err != nil {
		fail(w, r, err)
		return
	}
	if o.ID != p.Org.ID {
		if !p.Viewer.Oversight {
			o.TaxID = ""
		}
		// Partner sites are the organisation's private address book.
		sites = slices.DeleteFunc(sites, func(x domain.Site) bool { return !x.Own() && !p.Viewer.Oversight })
	}
	writeJSON(w, http.StatusOK, map[string]any{"org": o, "sites": nonNil(sites)})
}

func (s *Server) listSites(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	var sites []domain.Site
	var err error
	if p.Viewer.Oversight {
		sites, err = s.St.Sites(r.Context(), r.URL.Query().Get("org"))
	} else {
		var linked []string
		if linked, err = s.St.LinkedOrgs(r.Context(), p.Org.ID); err == nil {
			sites, err = s.St.VisibleSites(r.Context(), p.Org.ID, linked)
		}
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, nonNil(sites))
}

func nonNil[T any](xs []T) []T {
	if xs == nil {
		return []T{}
	}
	return xs
}

func (s *Server) ownOrg(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	sites, err := s.St.Sites(r.Context(), p.Org.ID)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"org": p.Org, "sites": nonNil(sites)})
}

func (s *Server) updateOrg(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	var in domain.Org
	if !decode(w, r, &in, 8192) {
		return
	}
	o := p.Org
	o.Name, o.TaxID, o.City, o.Province, o.Region = strings.TrimSpace(in.Name), in.TaxID, in.City, in.Province, in.Region
	if c := strings.ToUpper(strings.TrimSpace(in.Country)); c != "" {
		if !validCountry(c) {
			writeProblem(w, r, http.StatusUnprocessableEntity, "Check the country", "Use an ISO 3166-1 alpha-2 code, e.g. NL.")
			return
		}
		o.Country = c
	}
	o.Lat, o.Lon, o.Description, o.Website = in.Lat, in.Lon, in.Description, in.Website
	o.LegalName, o.Address, o.Phone = clip(in.LegalName, 200), clip(in.Address, 400), clip(in.Phone, 40)
	o.ContactEmail, o.ReportFooter = clip(strings.ToLower(in.ContactEmail), 200), clip(in.ReportFooter, 300)
	if len(o.Name) < 2 {
		writeProblem(w, r, http.StatusUnprocessableEntity, "Name required", "")
		return
	}
	if o.ContactEmail != "" && !strings.Contains(o.ContactEmail, "@") {
		writeProblemErrs(w, r, http.StatusUnprocessableEntity, "Check the contact e-mail", "", []fieldErr{{"contactEmail", "Enter an e-mail address, or leave it empty."}})
		return
	}
	if err := s.St.UpdateOrgProfile(r.Context(), o); err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), o.ID, p.Actor(), "org.update", o.ID, nil)
	writeJSON(w, http.StatusOK, o)
}

// siteIn validates a site from the request. Own sites need a location; a
// site sent without a type takes defKind (the organisation's own kind). keep
// is the type a site already has: an older site may carry its organisation's
// kind (auditor, reporter…), which stays accepted until someone changes it.
func siteIn(w http.ResponseWriter, r *http.Request, defKind domain.OrgKind, keep string) (domain.Site, bool) {
	var in domain.Site
	if !decode(w, r, &in, 8192) {
		return in, false
	}
	if in.Kind == "" && keep != "" {
		in.Kind = keep
	} else if in.Kind == "" && slices.Contains(domain.SiteKinds, string(defKind)) {
		in.Kind = string(defKind)
	}
	in.Name, in.Partner, in.KindOther = clip(strings.TrimSpace(in.Name), 160), clip(strings.TrimSpace(in.Partner), 160), clip(strings.TrimSpace(in.KindOther), 80)
	in.Address, in.City, in.Province, in.Region = clip(in.Address, 400), clip(in.City, 120), clip(in.Province, 120), clip(in.Region, 120)
	in.Country = strings.ToUpper(strings.TrimSpace(in.Country))
	bad := func(field, msg string) (domain.Site, bool) {
		writeProblemErrs(w, r, http.StatusUnprocessableEntity, "Check the site", "", []fieldErr{{field, msg}})
		return in, false
	}
	switch {
	case in.Name == "":
		return bad("name", "Give the site a name.")
	case !slices.Contains(domain.SiteKinds, in.Kind) && (keep == "" || in.Kind != keep):
		return bad("kind", "Pick a site type.")
	case in.Kind == domain.SiteOther && in.KindOther == "":
		return bad("kindOther", "Say what kind of site it is.")
	case in.Country != "" && !validCountry(in.Country):
		return bad("country", "Use an ISO 3166-1 alpha-2 code, e.g. NL.")
	case in.Lat < -90 || in.Lat > 90 || in.Lon < -180 || in.Lon > 180:
		return bad("lat", "Latitude and longitude are out of range.")
	case in.Partner == "" && in.Lat == 0 && in.Lon == 0:
		return bad("lat", "Your own sites need a location (latitude and longitude).")
	}
	if in.Kind != domain.SiteOther {
		in.KindOther = ""
	}
	return in, true
}

// mayEditSite: admins manage the company's own sites; editors may keep the
// partner address book (dispatchers add the sites they ship to).
func mayEditSite(w http.ResponseWriter, r *http.Request, x domain.Site) bool {
	if x.Own() && !principal(r.Context()).can("admin") {
		writeProblem(w, r, http.StatusForbidden, "Admins only", "Only an admin can add or change your company's own sites. You can add partner sites.")
		return false
	}
	return true
}

func (s *Server) createSite(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	in, ok := siteIn(w, r, p.Org.Kind, "")
	if !ok || !mayEditSite(w, r, in) {
		return
	}
	in.ID, in.OrgID, in.ArchivedAt = auth.ID("site_"), p.Org.ID, nil
	if err := s.St.InsertSite(r.Context(), in); err != nil {
		fail(w, r, err)
		return
	}
	x, err := s.St.Site(r.Context(), in.ID)
	if err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "site.create", in.ID, map[string]any{"kind": in.Kind, "partner": in.Partner})
	writeJSON(w, http.StatusCreated, x)
}

func (s *Server) updateSite(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	cur, err := s.St.Site(r.Context(), r.PathValue("id"))
	if err != nil || cur.OrgID != p.Org.ID {
		fail(w, r, store.ErrNotFound)
		return
	}
	in, ok := siteIn(w, r, p.Org.Kind, cur.Kind)
	if !ok || !mayEditSite(w, r, cur) || !mayEditSite(w, r, in) {
		return
	}
	in.ID, in.OrgID = cur.ID, p.Org.ID
	if err := s.St.UpdateSite(r.Context(), in); err != nil {
		fail(w, r, err)
		return
	}
	x, err := s.St.Site(r.Context(), in.ID)
	if err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "site.update", in.ID, nil)
	writeJSON(w, http.StatusOK, x)
}

func (s *Server) archiveSite(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	var in struct {
		Archived bool `json:"archived"`
	}
	if !decode(w, r, &in, 256) {
		return
	}
	cur, err := s.St.Site(r.Context(), r.PathValue("id"))
	if err != nil || cur.OrgID != p.Org.ID {
		fail(w, r, store.ErrNotFound)
		return
	}
	if !mayEditSite(w, r, cur) {
		return
	}
	if err := s.St.ArchiveSite(r.Context(), p.Org.ID, cur.ID, in.Archived); err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), map[bool]string{true: "site.archive", false: "site.restore"}[in.Archived], cur.ID, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) members(w http.ResponseWriter, r *http.Request) {
	users, err := s.St.Users(r.Context(), principal(r.Context()).Org.ID, false)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, nonNil(users))
}

func (s *Server) updateMember(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	var in struct {
		Role   string   `json:"role"`
		Duties []string `json:"duties"`
		Steps  []string `json:"steps"`
	}
	if !decode(w, r, &in, 1024) {
		return
	}
	if in.Steps != nil {
		if !validSteps(p.Org, in.Steps) {
			writeProblem(w, r, http.StatusUnprocessableEntity, "Unknown section", "Pick sections your company works in.")
			return
		}
		if err := s.St.SetUserSteps(r.Context(), p.Org.ID, r.PathValue("id"), in.Steps); err != nil {
			fail(w, r, err)
			return
		}
	}
	if in.Role != "" {
		if roleRank[in.Role] == 0 {
			writeProblem(w, r, http.StatusUnprocessableEntity, "Unknown role", "Use admin, editor or viewer.")
			return
		}
		if err := s.St.SetUserRole(r.Context(), p.Org.ID, r.PathValue("id"), in.Role); err != nil {
			fail(w, r, err)
			return
		}
	}
	if in.Duties != nil {
		if !validDuties(in.Duties) {
			writeProblem(w, r, http.StatusUnprocessableEntity, "Unknown duty", "Use receive, produce, test, release or ship.")
			return
		}
		if err := s.St.SetUserDuties(r.Context(), p.Org.ID, r.PathValue("id"), in.Duties); err != nil {
			fail(w, r, err)
			return
		}
	}
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "team.update", r.PathValue("id"), map[string]any{"role": in.Role, "duties": in.Duties, "steps": in.Steps})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) removeMember(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	if r.PathValue("id") == p.User.ID {
		writeProblem(w, r, http.StatusConflict, "That is you", "Ask another admin to remove your account.")
		return
	}
	if err := s.St.DisableUser(r.Context(), p.Org.ID, r.PathValue("id")); err != nil {
		fail(w, r, err)
		return
	}
	// Their personal key stops signing now; the worker revokes it on the ledger.
	if err := s.Keyring.Revoke(r.Context(), p.Org.ID, r.PathValue("id"), "member removed"); err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "team.remove", r.PathValue("id"), nil)
	w.WriteHeader(http.StatusNoContent)
}

// myKeys lists the caller's personal signing keys, creating the first one.
func (s *Server) myKeys(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	if _, err := s.Keyring.UserSigner(r.Context(), p.Org.ID, p.User.ID); err != nil {
		fail(w, r, err)
		return
	}
	keys, err := s.St.KeysOf(r.Context(), p.User.ID)
	if err != nil {
		fail(w, r, err)
		return
	}
	type view struct {
		ID         string    `json:"id"`
		PublicKey  string    `json:"publicKey"`
		Status     string    `json:"status"`
		Reason     string    `json:"reason,omitempty"`
		Custody    string    `json:"custody"`
		OnLedger   bool      `json:"onLedger"`
		LedgerTxID string    `json:"ledgerTxId,omitempty"`
		CreatedAt  time.Time `json:"createdAt"`
	}
	out := make([]view, 0, len(keys))
	for _, k := range keys {
		out = append(out, view{k.ID, base64.StdEncoding.EncodeToString(k.Public), k.Status, k.RevokeReason, k.Custody,
			k.LedgerTxID != "", k.LedgerTxID, k.CreatedAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": out, "vault": s.Keyring.Custody()})
}

// rotateMyKey retires the caller's key and issues a new one.
func (s *Server) rotateMyKey(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	k, err := s.Keyring.Rotate(r.Context(), p.Org.ID, p.User.ID)
	if err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "key.rotate", k.ID, nil)
	writeJSON(w, http.StatusOK, map[string]any{"id": k.ID, "custody": k.Custody})
}

func (s *Server) listInvitations(w http.ResponseWriter, r *http.Request) {
	inv, err := s.St.Invitations(r.Context(), principal(r.Context()).Org.ID)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, nonNil(inv))
}

// invite creates an invitation and returns its link once. ROADPASS does not send
// invitation by e-mail when the platform has a mail server; the admin can always
// share the link by other means.
func (s *Server) invite(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	var in struct {
		Email  string   `json:"email"`
		Role   string   `json:"role"`
		Duties []string `json:"duties"`
		Steps  []string `json:"steps"`
	}
	if !decode(w, r, &in, 1024) {
		return
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if !strings.Contains(in.Email, "@") || roleRank[in.Role] == 0 || !validDuties(in.Duties) || !validSteps(p.Org, in.Steps) {
		writeProblem(w, r, http.StatusUnprocessableEntity, "Check the invitation", "A valid e-mail and a role are required.")
		return
	}
	tok := auth.Token("inv_", 24)
	inv := store.Invitation{ID: auth.ID("ivt_"), OrgID: p.Org.ID, Email: in.Email, Role: in.Role, Duties: in.Duties, Steps: in.Steps, InvitedBy: p.User.ID,
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour)}
	if err := s.St.CreateInvitation(r.Context(), inv, auth.Digest(tok)); err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "team.invite", in.Email, map[string]string{"role": in.Role})
	link := s.Cfg.PublicURL + "/join/" + tok
	role := domain.RoleTitleIn(append([]domain.OrgKind{p.Org.Kind}, p.Org.Kinds...), in.Steps, in.Role, in.Duties)
	emailed := s.mailInvitation(r.Context(), in.Email, p.Org.Name, inviterName(p), strings.ToLower(role[:1])+role[1:], link)
	writeJSON(w, http.StatusCreated, map[string]any{"invitation": inv, "link": link, "emailed": emailed})
}

func (s *Server) revokeInvitation(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	if err := s.St.RevokeInvitation(r.Context(), p.Org.ID, r.PathValue("id")); err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listKeys(w http.ResponseWriter, r *http.Request) {
	ks, err := s.St.APIKeys(r.Context(), principal(r.Context()).Org.ID)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, nonNil(ks))
}

var allScopes = []string{"records:read", "records:write"}

func (s *Server) createKey(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	var in struct {
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
		Duties []string `json:"duties"`
	}
	if !decode(w, r, &in, 1024) {
		return
	}
	if strings.TrimSpace(in.Name) == "" || len(in.Scopes) == 0 {
		writeProblem(w, r, http.StatusUnprocessableEntity, "Name and scopes required", "")
		return
	}
	if in.Duties == nil {
		in.Duties = domain.AllDuties()
	}
	if !validDuties(in.Duties) {
		writeProblem(w, r, http.StatusUnprocessableEntity, "Unknown duty", "Use receive, produce, test, release or ship.")
		return
	}
	for _, sc := range in.Scopes {
		if !slices.Contains(allScopes, sc) {
			writeProblem(w, r, http.StatusUnprocessableEntity, "Unknown scope", sc)
			return
		}
	}
	secret := auth.Token("rpk_live_", 30)
	if !s.Cfg.Production() {
		secret = auth.Token("rpk_test_", 30)
	}
	k := store.APIKey{ID: auth.ID("key_"), OrgID: p.Org.ID, Name: strings.TrimSpace(in.Name), Prefix: secret[:14] + "…",
		Hash: auth.Digest(secret), Scopes: in.Scopes, Duties: in.Duties, CreatedBy: p.User.ID, CreatedAt: time.Now().UTC()}
	if err := s.St.InsertAPIKey(r.Context(), k); err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "apikey.create", k.ID, map[string]any{"scopes": k.Scopes})
	// The secret is returned once and never stored.
	writeJSON(w, http.StatusCreated, map[string]any{"key": k, "secret": secret})
}

func validDuties(ds []string) bool {
	for _, d := range ds {
		if !domain.Duty(d).Valid() {
			return false
		}
	}
	return true
}

func (s *Server) revokeKey(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	if err := s.St.RevokeAPIKey(r.Context(), p.Org.ID, r.PathValue("id")); err != nil {
		fail(w, r, err)
		return
	}
	s.keys.mu.Lock()
	s.keys.m = map[string]keyEntry{}
	s.keys.mu.Unlock()
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "apikey.revoke", r.PathValue("id"), nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) signingKeys(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	keys, err := s.St.SigningKeys(r.Context())
	if err != nil {
		fail(w, r, err)
		return
	}
	type view struct {
		ID         string    `json:"id"`
		OrgID      string    `json:"orgId"`
		Algorithm  string    `json:"algorithm"`
		PublicKey  string    `json:"publicKey"`
		Status     string    `json:"status"`
		OnLedger   bool      `json:"onLedger"`
		LedgerTxID string    `json:"ledgerTxId,omitempty"`
		Custody    string    `json:"custody"` // vault | platform | customer
		UserID     string    `json:"userId,omitempty"`
		CreatedAt  time.Time `json:"createdAt"`
	}
	out := []view{}
	for _, k := range keys {
		if k.OrgID != p.Org.ID && !p.Viewer.Oversight {
			continue
		}
		out = append(out, view{k.ID, k.OrgID, "Ed25519", base64.StdEncoding.EncodeToString(k.Public), k.Status,
			k.LedgerTxID != "", k.LedgerTxID, k.Custody, k.UserID, k.CreatedAt})
	}
	writeJSON(w, http.StatusOK, out)
}

// ---------------------------------------------------------------- operator

func (s *Server) auditLog(w http.ResponseWriter, r *http.Request) {
	es, err := s.St.AuditLog(r.Context(), principal(r.Context()).Viewer, 200)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, nonNil(es))
}

// validCountry reports whether c looks like an ISO 3166-1 alpha-2 code.
func validCountry(c string) bool {
	return len(c) == 2 && c[0] >= 'A' && c[0] <= 'Z' && c[1] >= 'A' && c[1] <= 'Z'
}

// validSteps reports whether every section is one the organisation works in.
func validSteps(o domain.Org, steps []string) bool {
	for _, st := range steps {
		if st != string(o.Kind) && !slices.Contains(o.Kinds, domain.OrgKind(st)) {
			return false
		}
	}
	return true
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// maxLogo bounds a logo: it is printed small on reports and passports.
const maxLogo = 300 << 10

// setOrgLogo stores the organisation's logo: PNG or JPEG (sniffed, not trusted
// from the client), at most 300 kB. SVG is refused: it can carry scripts.
func (s *Server) setOrgLogo(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	r.Body = http.MaxBytesReader(w, r.Body, maxLogo+1)
	data, err := io.ReadAll(r.Body)
	if err != nil || len(data) > maxLogo {
		writeProblem(w, r, http.StatusRequestEntityTooLarge, "Logo too large", "Use a PNG or JPEG of at most 300 kB.")
		return
	}
	ct := http.DetectContentType(data)
	if ct != "image/png" && ct != "image/jpeg" {
		writeProblem(w, r, http.StatusUnsupportedMediaType, "Use a PNG or JPEG", "Other formats (SVG included) are not accepted.")
		return
	}
	if err := s.St.SetOrgLogo(r.Context(), p.Org.ID, ct, data); err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "org.logo", p.Org.ID, map[string]any{"bytes": len(data)})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteOrgLogo(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	if err := s.St.SetOrgLogo(r.Context(), p.Org.ID, "", nil); err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "org.logo.remove", p.Org.ID, nil)
	w.WriteHeader(http.StatusNoContent)
}

// orgLogo serves a logo publicly: it appears on public passports.
func (s *Server) orgLogo(w http.ResponseWriter, r *http.Request) {
	ct, data, at, err := s.St.OrgLogo(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("Content-Security-Policy", "default-src 'none'")
	w.Header().Set("Last-Modified", at.UTC().Format(http.TimeFormat))
	_, _ = w.Write(data)
}
