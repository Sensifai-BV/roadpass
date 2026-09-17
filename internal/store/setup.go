package store

import (
	"context"
	"time"

	"github.com/sensifai/roadpass/internal/domain"
)

// SetupFacts are what an organisation has set up so far; the get-started guide
// derives its steps from them, so progress is never typed by hand.
type SetupFacts struct {
	Sites, Members, PendingInvites, Records, Imports, APIKeys int
	Channels, Rules, Limits, MembersWithPrefs                 int
	Dismissed                                                 *time.Time
}

// SetupFacts gathers an organisation's setup state.
func (s *Store) SetupFacts(ctx context.Context, orgID string) (SetupFacts, error) {
	var f SetupFacts
	err := s.DB.QueryRow(ctx, `SELECT
			(SELECT count(*) FROM sites WHERE org_id=$1),
			(SELECT count(*) FROM users WHERE org_id=$1 AND disabled_at IS NULL),
			(SELECT count(*) FROM invitations WHERE org_id=$1 AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > now()),
			(SELECT count(*) FROM records WHERE org_id=$1 AND latest),
			(SELECT count(*) FROM ingests WHERE org_id=$1 AND channel IN ('csv','api','ngsi-ld') AND accepted > 0),
			(SELECT count(*) FROM api_keys WHERE org_id=$1 AND revoked_at IS NULL),
			(SELECT count(*) FROM alert_channels WHERE org_id=$1),
			(SELECT count(*) FROM alert_rules WHERE org_id=$1 AND enabled),
			(SELECT count(*) FROM compliance_limits WHERE org_id=$1 AND enabled),
			(SELECT count(*) FROM notification_prefs p JOIN users u ON u.id=p.user_id WHERE u.org_id=$1),
			(SELECT setup_dismissed_at FROM orgs WHERE id=$1)`, orgID).
		Scan(&f.Sites, &f.Members, &f.PendingInvites, &f.Records, &f.Imports, &f.APIKeys, &f.Channels, &f.Rules, &f.Limits, &f.MembersWithPrefs, &f.Dismissed)
	return f, err
}

// DismissSetup puts the guide away (dismiss=false brings it back).
func (s *Store) DismissSetup(ctx context.Context, orgID string, dismiss bool) error {
	_, err := s.DB.Exec(ctx, `UPDATE orgs SET setup_dismissed_at = CASE WHEN $2 THEN now() ELSE NULL END WHERE id=$1`, orgID, dismiss)
	return err
}

// SetOrgKinds records every activity an organisation has; the primary kind,
// set by the platform operator, always stays first.
func (s *Store) SetOrgKinds(ctx context.Context, o domain.Org, kinds []domain.OrgKind) error {
	all := []string{string(o.Kind)}
	for _, k := range kinds {
		if k != o.Kind && k.Valid() {
			all = append(all, string(k))
		}
	}
	_, err := s.DB.Exec(ctx, `UPDATE orgs SET kinds=$2 WHERE id=$1`, o.ID, all)
	return err
}
