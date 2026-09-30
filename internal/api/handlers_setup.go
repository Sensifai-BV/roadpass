package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/sensifai/roadpass/internal/alerts"
	"github.com/sensifai/roadpass/internal/domain"
)

// Onboarding: the steps a new organisation's admin goes through, derived from
// what is actually set up (never ticked by hand), and the invitation e-mail.

type setupStep struct {
	Key      string `json:"key"`
	Done     bool   `json:"done"`
	Optional bool   `json:"optional"`
	Detail   string `json:"detail"`
}

var producers = []domain.OrgKind{domain.KindSortingPlant, domain.KindRecycler, domain.KindAdditiveProducer, domain.KindAsphaltPlant, domain.KindContractor}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

func (s *Server) orgSetup(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	o := p.Org
	f, err := s.St.SetupFacts(r.Context(), o.ID)
	if err != nil {
		fail(w, r, err)
		return
	}
	kinds := append([]domain.OrgKind{o.Kind}, o.Kinds...)
	makes := slices.ContainsFunc(kinds, func(k domain.OrgKind) bool { return slices.Contains(producers, k) })
	plant := slices.ContainsFunc(kinds, func(k domain.OrgKind) bool { return slices.Contains(producers[:4], k) })
	profile := o.Country != "" && o.City != ""
	steps := []setupStep{
		{Key: "profile", Done: profile, Detail: map[bool]string{true: "Company details and activities confirmed.", false: "Add your country and city, and confirm what you do in the chain."}[profile]},
		{Key: "sites", Done: f.Sites > 0, Optional: !makes, Detail: plural(f.Sites, "site", "sites")},
		{Key: "team", Done: f.Members > 1 || f.PendingInvites > 0, Detail: fmt.Sprintf("%s, %s pending", plural(f.Members, "member", "members"), plural(f.PendingInvites, "invitation", "invitations"))},
		{Key: "data", Done: f.Records > 0, Optional: !makes, Detail: plural(f.Records, "record", "records")},
		{Key: "connect", Done: f.APIKeys > 0, Optional: true, Detail: plural(f.APIKeys, "API key", "API keys")},
		{Key: "notify", Done: f.Channels > 0 || f.MembersWithPrefs > 0, Optional: true, Detail: fmt.Sprintf("%s watching your data", plural(f.Rules, "alert rule", "alert rules"))},
	}
	if plant {
		steps = append(steps, setupStep{Key: "limits", Done: f.Limits > 0, Optional: true, Detail: plural(f.Limits, "limit", "limits")})
	}
	steps = append(steps, setupStep{Key: "verified", Done: o.Status == domain.OrgVerified, Detail: o.Status})
	done, required := 0, 0
	for _, st := range steps {
		if !st.Optional {
			required++
			if st.Done {
				done++
			}
		}
	}
	// Seats are a soft limit (flagged to the operator, never blocking), so the
	// admin sees them before inviting past the plan.
	var seats map[string]any
	if limit, plan, ok, err := s.St.SeatLimit(r.Context(), o.ID); err != nil {
		fail(w, r, err)
		return
	} else if ok {
		seats = map[string]any{"used": f.Members + f.PendingInvites, "members": f.Members, "pending": f.PendingInvites, "limit": limit, "plan": plan}
	}
	writeJSON(w, http.StatusOK, map[string]any{"steps": steps, "done": done, "required": required, "dismissed": f.Dismissed != nil,
		"mail": s.mailReady(), "kinds": kinds, "seats": seats})
}

func (s *Server) dismissSetup(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	var in struct {
		Dismissed bool `json:"dismissed"`
	}
	if !decode(w, r, &in, 256) {
		return
	}
	if err := s.St.DismissSetup(r.Context(), p.Org.ID, in.Dismissed); err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// chainKinds are the activities an organisation may add to its own profile;
// oversight kinds (operator, auditor, reporter) are set by the platform only.
var chainKinds = []domain.OrgKind{domain.KindSortingPlant, domain.KindRecycler, domain.KindAdditiveProducer, domain.KindAsphaltPlant,
	domain.KindContractor, domain.KindRoadOwner, domain.KindLab}

func (s *Server) setActivities(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	var in struct {
		Kinds []domain.OrgKind `json:"kinds"`
	}
	if !decode(w, r, &in, 1024) {
		return
	}
	if !slices.Contains(chainKinds, p.Org.Kind) {
		writeProblem(w, r, http.StatusForbidden, "Fixed by the platform", "Oversight organisations have one activity.")
		return
	}
	for _, k := range in.Kinds {
		if !slices.Contains(chainKinds, k) {
			writeProblem(w, r, http.StatusUnprocessableEntity, "Unknown activity", fmt.Sprintf("%q is not a step of the chain.", k))
			return
		}
	}
	if err := s.St.SetOrgKinds(r.Context(), p.Org, in.Kinds); err != nil {
		fail(w, r, err)
		return
	}
	s.Cache.Invalidate(r.Context())
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "org.activities", p.Org.ID, map[string]any{"kinds": in.Kinds})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) mailReady() bool {
	return s.Notify != nil && s.Notify.Senders != nil && s.Notify.Senders.Mail(context.Background()).Configured()
}

// mailInvitation e-mails an invitation link when the platform has a mail
// server; the admin always gets the link to share by other means as well.
func (s *Server) mailInvitation(ctx context.Context, to, org, from, role, link string) bool {
	if !s.mailReady() {
		return false
	}
	text := strings.Join([]string{
		fmt.Sprintf("%s invited you to join %s on ROADPASS as %s.", from, org, role),
		"ROADPASS keeps the digital passports of recycled plastic in roads: every record you enter is signed with your own key and checked before it is shared.",
		"Open the link to choose your password. It works once and expires in 7 days.",
	}, "\n")
	err := alerts.SendMail(ctx, s.Notify.Senders.Mail(ctx), to, alerts.Message{Subject: fmt.Sprintf("Join %s on ROADPASS", org), Text: text, Severity: "info", State: "invite", Link: link})
	if err != nil {
		slog.Warn("invitation e-mail failed", "err", err)
		return false
	}
	return true
}

func inviterName(p *Principal) string {
	if p.User != nil {
		return p.User.Name
	}
	return "Your administrator"
}
