package api

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/sensifai/roadpass/internal/cache"
	"github.com/sensifai/roadpass/internal/domain"
	"github.com/sensifai/roadpass/internal/store"
)

// Calendar and planning. Plans are notes between the companies involved, never
// signed or sealed; the calendar shows them beside the records that really happened.

// plannable record types: production runs, shipments, deliveries, road-section paving.
var plannable = []domain.RecordType{domain.TypeRejectLot, domain.TypeRecycledLot, domain.TypeAdditiveBatch,
	domain.TypeAsphaltLot, domain.TypeShipment, domain.TypeDelivery, domain.TypeRoadSection}

func (s *Server) planRoutes(mux *http.ServeMux) {
	viewer := guard{scope: "records:read"}
	editor := guard{role: "editor", scope: "records:write"}
	mux.HandleFunc("GET /api/v1/calendar", s.with(viewer, s.calendar))
	mux.HandleFunc("GET /api/v1/plans", s.with(viewer, s.listPlans))
	mux.HandleFunc("POST /api/v1/plans", s.with(editor, s.createPlan))
	mux.HandleFunc("GET /api/v1/plans/{id}", s.with(viewer, s.getPlan))
	mux.HandleFunc("PUT /api/v1/plans/{id}", s.with(editor, s.updatePlan))
	mux.HandleFunc("POST /api/v1/plans/{id}/status", s.with(editor, s.planStatus))
	mux.HandleFunc("GET /api/v1/plans/{id}/candidates", s.with(viewer, s.planCandidates))
}

// span reads ?from=&to= (RFC 3339), at most max apart.
func span(r *http.Request, max time.Duration) (time.Time, time.Time, bool) {
	from, err1 := time.Parse(time.RFC3339, r.URL.Query().Get("from"))
	to, err2 := time.Parse(time.RFC3339, r.URL.Query().Get("to"))
	if err1 != nil || err2 != nil || !to.After(from) || to.Sub(from) > max {
		return time.Time{}, time.Time{}, false
	}
	return from, to, true
}

// calendar returns the records and plans of a time span the caller may see.
func (s *Server) calendar(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	from, to, ok := span(r, 45*24*time.Hour)
	if !ok {
		writeProblem(w, r, http.StatusBadRequest, "Check the time span", "Give from and to as RFC 3339 times, at most 45 days apart.")
		return
	}
	events, more, err := s.St.CalendarEvents(r.Context(), p.Viewer, from, to, 3000)
	if err != nil {
		fail(w, r, err)
		return
	}
	plans, err := s.St.WorkPlans(r.Context(), p.Viewer, store.WorkPlanFilter{From: from, To: to, Limit: 2000})
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events, "plans": plans, "truncated": more})
}

func (s *Server) listPlans(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	f := store.WorkPlanFilter{Status: r.URL.Query().Get("status")}
	if f.Status != "" && !slices.Contains(store.WorkPlanStatuses, f.Status) {
		writeProblem(w, r, http.StatusBadRequest, "Unknown status", "Use planned, done or cancelled.")
		return
	}
	if r.URL.Query().Has("from") {
		var ok bool
		if f.From, f.To, ok = span(r, 400*24*time.Hour); !ok {
			writeProblem(w, r, http.StatusBadRequest, "Check the time span", "Give from and to as RFC 3339 times, at most 400 days apart.")
			return
		}
	}
	plans, err := s.St.WorkPlans(r.Context(), p.Viewer, f)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, plans)
}

func (s *Server) getPlan(w http.ResponseWriter, r *http.Request) {
	pl, err := s.St.WorkPlan(r.Context(), principal(r.Context()).Viewer, r.PathValue("id"))
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, pl)
}

type planInput struct {
	RecordType   domain.RecordType `json:"recordType"`
	Title        string            `json:"title"`
	SiteID       string            `json:"siteId"`
	Counterparty string            `json:"counterpartyOrgId"`
	ToSiteID     string            `json:"toSiteId"`
	OwnerOrgID   string            `json:"ownerOrgId"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       *time.Time        `json:"endsAt"`
	MassKg       float64           `json:"massKg"`
	Note         string            `json:"note"`
}

// mayPlan reports why the caller may not plan records of type t ("" = may).
func mayPlan(p *Principal, t domain.RecordType) string {
	if p.Org.Kind.ReadOnly() {
		return "Your organisation has read-only access."
	}
	if p.Org.Status == domain.OrgSuspended {
		return "Your organisation is suspended."
	}
	if !slices.Contains(plannable, t) {
		return "Plan a production run, a shipment, a delivery or a road section."
	}
	allowed := false
	for _, k := range append([]domain.OrgKind{p.Org.Kind}, p.Org.Kinds...) {
		if k == domain.KindOperator || slices.Contains(domain.Types[t].CreatedBy, k) {
			allowed = true
		}
	}
	if !allowed {
		return fmt.Sprintf("%s records are made by another step of the chain.", domain.Types[t].Name)
	}
	if d := domain.DutyFor(t); !domain.HasDuty(p.Duties(), d) {
		return fmt.Sprintf("Planning %s needs the %s duty, which you do not hold.", strings.ToLower(domain.Types[t].Plural), domain.DutyInfo[d].Name)
	}
	if p.User != nil && p.User.Role != "admin" && !domain.InSteps(t, p.User.Steps) {
		return fmt.Sprintf("%s belong to another section of the chain than the ones your role covers.", domain.Types[t].Plural)
	}
	return ""
}

// checkPlan validates an input and turns it into a plan of the caller's organisation.
func (s *Server) checkPlan(w http.ResponseWriter, r *http.Request, in planInput) (store.WorkPlan, bool) {
	p := principal(r.Context())
	if why := mayPlan(p, in.RecordType); why != "" {
		writeProblem(w, r, http.StatusForbidden, "You cannot plan this", why)
		return store.WorkPlan{}, false
	}
	in.Title, in.Note = strings.TrimSpace(in.Title), strings.TrimSpace(in.Note)
	if in.RecordType == domain.TypeShipment {
		in.Counterparty = "" // derived from the receiving site below
	}
	bad := func(detail string) (store.WorkPlan, bool) {
		writeProblem(w, r, http.StatusUnprocessableEntity, "Check the plan", detail)
		return store.WorkPlan{}, false
	}
	switch {
	case in.StartsAt.IsZero():
		return bad("Give the date and time the work starts.")
	case in.EndsAt != nil && in.EndsAt.Before(in.StartsAt):
		return bad("The end must be after the start.")
	case in.EndsAt != nil && in.EndsAt.Sub(in.StartsAt) > 31*24*time.Hour:
		return bad("A plan spans at most 31 days; split longer work into several plans.")
	case in.MassKg < 0 || in.MassKg > 1e8:
		return bad("Expected mass must be between 0 and 100 000 t.")
	case len([]rune(in.Title)) > 160 || len([]rune(in.Note)) > 1000:
		return bad("Keep the title under 160 and the note under 1000 characters.")
	case in.RecordType == domain.TypeShipment && in.ToSiteID == "":
		return bad("Pick the site that receives the shipment.")
	case in.RecordType == domain.TypeRoadSection && in.OwnerOrgID == "":
		return bad("Name the road owner.")
	case in.Counterparty == p.Org.ID:
		return bad("The counterparty is another company than yours.")
	}
	if in.RecordType != domain.TypeRoadSection {
		in.OwnerOrgID = ""
	}
	if in.SiteID != "" {
		site, err := s.St.Site(r.Context(), in.SiteID)
		if err != nil || site.OrgID != p.Org.ID || !site.Own() {
			return bad("Pick one of your own sites.")
		}
	}
	if in.RecordType == domain.TypeShipment {
		// Receivers come from the planner's own address book; an own site is
		// received by the planner's own team, a partner site is external.
		to, err := s.St.Site(r.Context(), in.ToSiteID)
		if err != nil || to.OrgID != p.Org.ID {
			return bad("Pick one of your own or partner sites as the receiver.")
		}
		if to.ID == in.SiteID {
			return bad("The receiving site is the site the shipment leaves from.")
		}
		in.Counterparty = ""
		if to.Own() {
			in.Counterparty = p.Org.ID
		}
	} else {
		in.ToSiteID = ""
	}
	vis, err := s.visibleOrgs(r)
	if err != nil {
		fail(w, r, err)
		return store.WorkPlan{}, false
	}
	for _, id := range []string{in.Counterparty, in.OwnerOrgID} {
		if id == "" || id == p.Org.ID {
			continue
		}
		if _, err := s.St.Org(r.Context(), id); err != nil || (vis != nil && !vis[id]) {
			return bad("Pick a company you already work with.")
		}
	}
	return store.WorkPlan{OrgID: p.Org.ID, RecordType: in.RecordType, Title: in.Title, SiteID: in.SiteID,
		Counterparty: in.Counterparty, ToSiteID: in.ToSiteID, OwnerOrgID: in.OwnerOrgID, StartsAt: in.StartsAt.UTC(), EndsAt: utcPtr(in.EndsAt),
		MassKg: in.MassKg, Note: in.Note}, true
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func (s *Server) createPlan(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	var in planInput
	if !decode(w, r, &in, 8192) {
		return
	}
	pl, ok := s.checkPlan(w, r, in)
	if !ok {
		return
	}
	pl.CreatedBy = p.Actor()
	pl, err := s.St.CreateWorkPlan(r.Context(), pl)
	if err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "plan.create", pl.ID, map[string]string{"type": string(pl.RecordType)})
	s.publishPlan(r, pl)
	writeJSON(w, http.StatusCreated, pl)
}

// ownPlan loads a plan the caller's organisation made, for changing it.
func (s *Server) ownPlan(w http.ResponseWriter, r *http.Request) (store.WorkPlan, bool) {
	p := principal(r.Context())
	pl, err := s.St.WorkPlan(r.Context(), p.Viewer, r.PathValue("id"))
	if err != nil {
		fail(w, r, err)
		return pl, false
	}
	if pl.OrgID != p.Org.ID {
		writeProblem(w, r, http.StatusForbidden, "Not your plan", "Only the company that made a plan can change it.")
		return pl, false
	}
	if why := mayPlan(p, pl.RecordType); why != "" {
		writeProblem(w, r, http.StatusForbidden, "You cannot change this plan", why)
		return pl, false
	}
	return pl, true
}

func (s *Server) updatePlan(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	if _, ok := s.ownPlan(w, r); !ok {
		return
	}
	var in planInput
	if !decode(w, r, &in, 8192) {
		return
	}
	pl, ok := s.checkPlan(w, r, in)
	if !ok {
		return
	}
	pl.ID, pl.UpdatedBy = r.PathValue("id"), p.Actor()
	pl, err := s.St.UpdateWorkPlan(r.Context(), p.Org.ID, pl)
	if err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "plan.update", pl.ID, nil)
	s.publishPlan(r, pl)
	writeJSON(w, http.StatusOK, pl)
}

// planStatus marks a plan done (linked to the record that did the work, when
// there is one), cancelled, or open again.
func (s *Server) planStatus(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	pl, ok := s.ownPlan(w, r)
	if !ok {
		return
	}
	var in struct {
		Status   string `json:"status"`
		RecordID string `json:"recordId"`
	}
	if !decode(w, r, &in, 1024) {
		return
	}
	if !slices.Contains(store.WorkPlanStatuses, in.Status) {
		writeProblem(w, r, http.StatusUnprocessableEntity, "Unknown status", "Use planned, done or cancelled.")
		return
	}
	if in.Status == "done" && in.RecordID != "" {
		rec, err := s.St.Record(r.Context(), p.Viewer, in.RecordID)
		if err != nil {
			fail(w, r, err)
			return
		}
		if rec.OrgID != pl.OrgID || rec.Type != pl.RecordType {
			writeProblem(w, r, http.StatusUnprocessableEntity, "Pick a matching record",
				fmt.Sprintf("Link a %s recorded by your company.", strings.ToLower(domain.Types[pl.RecordType].Name)))
			return
		}
	}
	pl, err := s.St.SetWorkPlanStatus(r.Context(), p.Org.ID, pl.ID, in.Status, in.RecordID, p.Actor())
	if err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "plan."+in.Status, pl.ID, map[string]string{"record": in.RecordID})
	s.publishPlan(r, pl)
	writeJSON(w, http.StatusOK, pl)
}

// planCandidates lists records that may be the real work behind a plan.
func (s *Server) planCandidates(w http.ResponseWriter, r *http.Request) {
	pl, err := s.St.WorkPlan(r.Context(), principal(r.Context()).Viewer, r.PathValue("id"))
	if err != nil {
		fail(w, r, err)
		return
	}
	recs, err := s.St.WorkPlanCandidates(r.Context(), pl, 14*24*time.Hour)
	if err != nil {
		fail(w, r, err)
		return
	}
	if recs == nil {
		recs = []domain.Record{}
	}
	writeJSON(w, http.StatusOK, recs)
}

// publishPlan tells open dashboards of every company involved to refresh.
func (s *Server) publishPlan(r *http.Request, pl store.WorkPlan) {
	orgs := []string{pl.OrgID}
	for _, o := range []string{pl.Counterparty, pl.OwnerOrgID} {
		if o != "" {
			orgs = append(orgs, o)
		}
	}
	s.Cache.Publish(r.Context(), cache.Event{Kind: "plans", Orgs: orgs, Ref: pl.ID})
}
