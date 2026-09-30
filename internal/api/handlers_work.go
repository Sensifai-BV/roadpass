package api

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/sensifai/roadpass/internal/alerts"
	"github.com/sensifai/roadpass/internal/cache"
	"github.com/sensifai/roadpass/internal/domain"
	"github.com/sensifai/roadpass/internal/store"
)

// work returns the caller's queues: only those its duties cover are filled.
func (s *Server) work(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	ds := p.Duties()
	org := p.Org
	if p.User != nil && p.User.Role != "admin" {
		// A member assigned to some sections only sees those sections' inputs: a
		// mixing operator is not offered the reject lots the sorting side made.
		org = withinSteps(org, p.User.Steps)
	}
	all, err := s.St.WorkFor(r.Context(), org, 100)
	if err != nil {
		fail(w, r, err)
		return
	}
	empty := store.Queue{Items: []store.WorkItem{}}
	if !domain.HasDuty(ds, domain.DutyReceive) {
		all.Receive = empty
	}
	if !domain.HasDuty(ds, domain.DutyProduce) {
		all.Process = empty
	}
	if !domain.HasDuty(ds, domain.DutyRelease) && !domain.HasDuty(ds, domain.DutyTest) {
		all.Release = empty
	}
	if !domain.HasDuty(ds, domain.DutyShip) {
		all.Ship = empty
	}
	as, err := s.St.Assignments(r.Context(), p.Org.ID)
	if err != nil {
		fail(w, r, err)
		return
	}
	attach := func(q *store.Queue, duty string) {
		for i := range q.Items {
			if a, ok := as[q.Items[i].Record.ID+"|"+duty]; ok {
				q.Items[i].Assignment = &a
			}
		}
	}
	attach(&all.Receive, "receive")
	attach(&all.Process, "produce")
	attach(&all.Release, "release")
	attach(&all.Ship, "ship")
	writeJSON(w, http.StatusOK, map[string]any{"duties": ds, "queues": all, "windowDays": int(store.WorkWindow.Hours() / 24)})
}

// withinSteps narrows an organisation's activities to a member's sections of the
// chain (none = every section; sections the company no longer does are ignored).
func withinSteps(o domain.Org, steps []string) domain.Org {
	var ks []domain.OrgKind
	for _, k := range append([]domain.OrgKind{o.Kind}, o.Kinds...) {
		if slices.Contains(steps, string(k)) && !slices.Contains(ks, k) {
			ks = append(ks, k)
		}
	}
	if len(ks) == 0 {
		return o
	}
	o.Kind, o.Kinds = ks[0], ks
	return o
}

// assign hands a waiting item to a member (admins), or back to the team.
func (s *Server) assign(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	var in struct {
		RecordID string `json:"recordId"`
		Duty     string `json:"duty"`
		UserID   string `json:"userId"` // empty: unassign
		Note     string `json:"note"`
	}
	if !decode(w, r, &in, 2048) {
		return
	}
	if !slices.Contains(domain.AllDuties(), in.Duty) || in.RecordID == "" || len([]rune(in.Note)) > 500 {
		writeProblem(w, r, http.StatusUnprocessableEntity, "Check the assignment", "Name the record, the duty and the member (note up to 500 characters).")
		return
	}
	if _, err := s.St.Record(r.Context(), p.Viewer, in.RecordID); err != nil {
		fail(w, r, err)
		return
	}
	var err error
	if in.UserID == "" {
		err = s.St.Unassign(r.Context(), p.Org.ID, in.RecordID, in.Duty)
	} else {
		err = s.St.Assign(r.Context(), p.Org.ID, store.Assignment{RecordID: in.RecordID, Duty: in.Duty, UserID: in.UserID, AssignedBy: p.Actor(), Note: in.Note})
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "work.assign", in.RecordID, map[string]string{"duty": in.Duty, "to": in.UserID})
	if in.UserID != "" && in.UserID != p.User.ID {
		rs, _ := s.Notify.Recipients(r.Context(), p.Org.ID, []string{"user:" + in.UserID})
		body := fmt.Sprintf("%s assigned %s to you (%s).", p.User.Name, in.RecordID, domain.DutyInfo[domain.Duty(in.Duty)].Name)
		if in.Note != "" {
			body += " “" + in.Note + "”"
		}
		s.Notify.NotifyPeople(r.Context(), p.Org.ID, rs, alerts.Notification{Kind: "assignment", Severity: "info", Title: "New work for you", Body: body, Link: "/work"})
	}
	s.Cache.Publish(r.Context(), cacheEvent("work", p.Org.ID, in.UserID))
	w.WriteHeader(http.StatusNoContent)
}

// team is the admin's view of who does what: members, their keys, their recent
// work, and what waits in each queue.
func (s *Server) team(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	ctx := r.Context()
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days <= 0 || days > 365 {
		days = 30
	}
	members, err := s.St.Users(ctx, p.Org.ID, false)
	if err != nil {
		fail(w, r, err)
		return
	}
	act, err := s.St.TeamActivity(ctx, p.Org.ID, days)
	if err != nil {
		fail(w, r, err)
		return
	}
	keys, err := s.St.APIKeys(ctx, p.Org.ID)
	if err != nil {
		fail(w, r, err)
		return
	}
	queues, err := s.St.WorkFor(ctx, p.Org, 500)
	if err != nil {
		fail(w, r, err)
		return
	}
	as, err := s.St.Assignments(ctx, p.Org.ID)
	if err != nil {
		fail(w, r, err)
		return
	}
	type queueSummary struct {
		Duty       string `json:"duty"`
		Waiting    int    `json:"waiting"`
		Unassigned int    `json:"unassigned"`
		OldestDays int    `json:"oldestDays"`
	}
	sum := func(duty string, q store.Queue) queueSummary {
		out := queueSummary{Duty: duty, Waiting: q.Total}
		assigned := 0
		for _, it := range q.Items {
			if _, ok := as[it.Record.ID+"|"+duty]; ok {
				assigned++
			}
			if d := int(time.Since(it.Since).Hours() / 24); d > out.OldestDays {
				out.OldestDays = d
			}
		}
		out.Unassigned = q.Total - assigned
		return out
	}
	type keyRow struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	ks := make([]keyRow, 0, len(keys))
	for _, k := range keys {
		ks = append(ks, keyRow{k.ID, k.Name})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"days": days, "members": nonNil(members), "activity": act, "apiKeys": ks,
		"queues": []queueSummary{sum("receive", queues.Receive), sum("produce", queues.Process), sum("release", queues.Release), sum("ship", queues.Ship)},
	})
}

// chainStatus is the reporter's board: the state of every organisation.
func (s *Server) chainStatus(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	v, err := cache.Remember(r.Context(), s.Cache, "chain:status:v2", time.Minute, func(ctx context.Context) ([]store.StageStatus, error) {
		return s.St.ChainStatus(ctx)
	})
	// Oversight bodies see every company; a company sees its own state.
	if err == nil && !p.Viewer.Oversight {
		v = slices.DeleteFunc(slices.Clone(v), func(x store.StageStatus) bool { return x.OrgID != p.Org.ID })
	}
	answer(w, r, nonNil(v), err)
}

// openSections lists road sections still being laid, so an asphalt plant can
// name the section on a delivery ticket (it cannot see contractors' records).
func (s *Server) openSections(w http.ResponseWriter, r *http.Request) {
	out, err := s.St.OpenSections(r.Context(), 200)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
