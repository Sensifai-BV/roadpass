package demo

import (
	"slices"
	"testing"
	"time"

	"github.com/sensifai/roadpass/internal/config"
	"github.com/sensifai/roadpass/internal/domain"
)

// Every demo member must hold a job role the product offers its organisation, so
// the Team page names each person's role and nobody holds an odd combination.
func TestTeamMatchesJobRoles(t *testing.T) {
	kinds := baseOrg().Kinds
	roles := domain.JobRolesFor(kinds)
	emails := map[string]bool{}
	for _, m := range Team {
		if emails[m.Email] {
			t.Errorf("duplicate e-mail %s", m.Email)
		}
		emails[m.Email] = true
		steps := make([]domain.OrgKind, len(m.Steps))
		for i, s := range m.Steps {
			steps[i] = domain.OrgKind(s)
			if !baseOrg().Is(steps[i]) {
				t.Errorf("%s works in %s, which the company does not do", m.Name, s)
			}
		}
		title := domain.RoleTitleIn(kinds, m.Steps, m.Role, m.Duties)
		if m.Role != "admin" && !slices.ContainsFunc(roles, func(r domain.JobRole) bool { return r.Access == m.Role }) {
			t.Errorf("%s: no job role with access %s", m.Name, m.Role)
		}
		t.Logf("%-14s %-34s %s", m.Name, m.Email, title)
	}
	if Team[0].Email != "admin@capital-demo.com" {
		t.Errorf("the administrator is %s", Team[0].Email)
	}
}

func TestLastReset(t *testing.T) {
	m := &Manager{Cfg: config.Config{DemoResetHour: 3}}
	at := func(s string) time.Time { x, _ := time.Parse(time.RFC3339, s); return x }
	if got := m.lastReset(at("2026-10-20T14:00:00Z")); !got.Equal(at("2026-10-20T03:00:00Z")) {
		t.Errorf("afternoon: %s", got)
	}
	if got := m.lastReset(at("2026-10-20T01:30:00Z")); !got.Equal(at("2026-10-19T03:00:00Z")) {
		t.Errorf("before the reset hour: %s", got)
	}
}

func TestScenarioQuantities(t *testing.T) {
	// The Route 740 sections carry the MoDOT final report's published quantities (US short tons).
	want := map[string]float64{"25PE": 1370.8, "50PE": 835.9, "50PEL": 1370.8}
	for _, s := range projects[0].sections {
		if got := s.kg / shortTon; got < want[s.key]-0.01 || got > want[s.key]+0.01 {
			t.Errorf("%s: %.2f short tons, published %.1f", s.key, got, want[s.key])
		}
	}
	for _, p := range projects {
		for _, s := range p.sections {
			if s.dosage < 0.2 || s.dosage > 1.5 {
				t.Errorf("%s %s: dosage %.2f outside the design window", p.code, s.key, s.dosage)
			}
		}
	}
}
