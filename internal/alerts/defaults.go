package alerts

import (
	"context"

	"github.com/sensifai/roadpass/internal/domain"
)

// StarterRules are the alerts a new organisation begins with, suited to what it
// does in the chain. Admins change or remove them like any other rule.
func StarterRules(orgID string, kind domain.OrgKind, newID func() string) []Rule {
	r := func(name, k, sev string, p Params, audience []string, forMin, repeat int) Rule {
		return Rule{ID: newID(), OrgID: orgID, Name: name, Kind: k, Params: p, Severity: sev, Audience: audience,
			ForMinutes: forMin, RepeatMinutes: repeat, NotifyResolved: true, Escalation: []Step{}, Enabled: true, CreatedBy: "system"}
	}
	var out []Rule
	switch kind {
	case domain.KindOperator:
		out = append(out, r("Records waiting for a seal", KindSealBacklog, "critical", Params{Minutes: 30}, []string{"role:admin"}, 5, 120))
		return out
	case domain.KindAuditor, domain.KindReporter:
		return nil
	}
	out = append(out, r("Blocking data check", KindFinding, "critical", Params{MinSeverity: "blocker"}, []string{"role:admin", "duty:produce"}, 0, 0))
	if kind != domain.KindSortingPlant && kind != domain.KindRoadOwner {
		out = append(out, r("Shipment not received after 24 h", KindReceiptOverdue, "warning", Params{Hours: 24}, []string{"duty:receive"}, 0, 720))
	}
	if kind != domain.KindRoadOwner && kind != domain.KindContractor {
		out = append(out, r("Our load was rejected at intake", KindRejected, "warning", Params{Hours: 48}, []string{"duty:ship", "role:admin"}, 0, 0))
	}
	out = append(out, r("Plan limit at 80 %", KindPlanLimit, "info", Params{Percent: 80}, []string{"role:admin"}, 0, 0))
	return out
}

// InstallStarterRules saves the starter rules of an organisation.
func (d *DB) InstallStarterRules(ctx context.Context, orgID string, kind domain.OrgKind, newID func() string) error {
	for _, r := range StarterRules(orgID, kind, newID) {
		if _, err := d.SaveRule(ctx, r); err != nil {
			return err
		}
	}
	return nil
}
