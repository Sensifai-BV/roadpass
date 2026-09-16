package store

import (
	"context"
	"fmt"
)

// WipeOrgData deletes everything an organisation recorded or configured (records
// and their links, findings, seals, imports, plans, alerts, compliance data,
// dashboards, keys, sites, audit trail) and keeps the organisation, its members
// and their signing keys. The public demo workspace uses it for its daily reset.
func (s *Store) WipeOrgData(ctx context.Context, orgID string) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit
	mine := `(SELECT id FROM records WHERE org_id = $1)`
	steps := []string{
		`DELETE FROM links WHERE parent_id IN ` + mine + ` OR child_id IN ` + mine,
		`DELETE FROM passport_summary WHERE record_id IN ` + mine,
		`DELETE FROM findings WHERE org_id = $1`,
		`DELETE FROM passport_shares WHERE org_id = $1`,
		`DELETE FROM work_assignments WHERE org_id = $1`,
		`DELETE FROM work_plans WHERE org_id = $1`,
		`DELETE FROM records WHERE org_id = $1`,
		`DELETE FROM seals WHERE org_id = $1`,
		`DELETE FROM ingests WHERE org_id = $1`,
		`DELETE FROM audit_log WHERE org_id = $1`,
		`DELETE FROM alert_events WHERE org_id = $1`,
		`DELETE FROM alert_rules WHERE org_id = $1`,
		`DELETE FROM alert_channels WHERE org_id = $1`,
		`DELETE FROM alert_maintenance WHERE org_id = $1`,
		`DELETE FROM notifications WHERE org_id = $1`,
		`DELETE FROM push_subscriptions WHERE user_id IN (SELECT id FROM users WHERE org_id = $1)`,
		`DELETE FROM notification_prefs WHERE user_id IN (SELECT id FROM users WHERE org_id = $1)`,
		`DELETE FROM user_layouts WHERE user_id IN (SELECT id FROM users WHERE org_id = $1)`,
		`DELETE FROM dashboards WHERE org_id = $1`,
		`DELETE FROM compliance_limits WHERE org_id = $1`,
		`DELETE FROM compliance_calibrations WHERE org_id = $1`,
		`DELETE FROM compliance_schedules WHERE org_id = $1`,
		`DELETE FROM compliance_attestations WHERE org_id = $1`,
		`DELETE FROM privacy_requests WHERE org_id = $1`,
		`DELETE FROM api_keys WHERE org_id = $1`,
		`DELETE FROM invitations WHERE org_id = $1`,
		`DELETE FROM org_logos WHERE org_id = $1`,
		`DELETE FROM sites WHERE org_id = $1`,
	}
	for _, q := range steps {
		if _, err := tx.Exec(ctx, q, orgID); err != nil {
			return fmt.Errorf("wipe %s: %w", q[:min(len(q), 48)], err)
		}
	}
	return tx.Commit(ctx)
}
