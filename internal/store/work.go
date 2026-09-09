package store

import (
	"context"
	"slices"
	"time"

	"github.com/sensifai/roadpass/internal/domain"
)

// Work queues: what each duty in an organisation has to do next. Queues look at
// the last WorkWindow only; older open items belong to the history, not the inbox
// (lots sold outside the chain are never shipped on the platform).
const WorkWindow = 90 * 24 * time.Hour

// WorkItem is one record waiting for a duty.
type WorkItem struct {
	Record      domain.Record `json:"record"`
	RemainingKg float64       `json:"remainingKg"`
	Decision    string        `json:"decision,omitempty"`
	Since       time.Time     `json:"since"`
	Assignment  *Assignment   `json:"assignment,omitempty"`
}

// Queue is one duty's list.
type Queue struct {
	Total int        `json:"total"`
	Items []WorkItem `json:"items"`
}

// Work holds every queue of an organisation.
type Work struct {
	Receive Queue `json:"receive"` // shipments to us, not yet confirmed
	Process Queue `json:"process"` // received material not yet used
	Release Queue `json:"release"` // our lots without a quality decision
	Ship    Queue `json:"ship"`    // released lots (and asphalt) with mass left to ship or deliver
}

const consumedSQL = `COALESCE((SELECT sum(l.mass_kg) FROM links l WHERE l.parent_id = r.id AND l.kind IN ('input','delivered')), 0)`

// ownUsedSQL is what left a lot's stock at its holder: shipped, delivered, or
// used as an input by the holder itself (an organisation that does the next step).
const ownUsedSQL = `COALESCE((SELECT sum(l.mass_kg) FROM links l JOIN records c ON c.id = l.child_id AND c.latest
	WHERE l.parent_id = r.id AND (l.kind IN ('shipped','delivered') OR (l.kind = 'input' AND c.org_id = r.org_id))), 0)`

const decisionSQL = `(SELECT x.body->>'decision' FROM links l JOIN records x ON x.id = l.child_id AND x.latest
	WHERE l.parent_id = r.id AND l.kind = 'releases' ORDER BY x.occurred_at DESC LIMIT 1)`

func (s *Store) queue(ctx context.Context, q string, args ...any) (Queue, error) {
	rows, err := s.DB.Query(ctx, q, args...)
	if err != nil {
		return Queue{}, err
	}
	defer rows.Close()
	out := Queue{Items: []WorkItem{}}
	for rows.Next() {
		var it WorkItem
		var dec *string
		x := newRecScan(&it.Record)
		if err := rows.Scan(append(x.dest(), &it.RemainingKg, &dec, &it.Since, &out.Total)...); err != nil {
			return Queue{}, err
		}
		if err := x.finish(); err != nil {
			return Queue{}, err
		}
		if dec != nil {
			it.Decision = *dec
		}
		out.Items = append(out.Items, it)
	}
	return out, rows.Err()
}

// WorkFor returns the queues of an organisation (only those its duties need;
// the caller filters by duty).
func (s *Store) WorkFor(ctx context.Context, org domain.Org, limit int) (Work, error) {
	since := time.Now().Add(-WorkWindow)
	none := Queue{Items: []WorkItem{}} // a step this kind of organisation does not have
	w := Work{Receive: none, Process: none, Release: none, Ship: none}
	var err error
	w.Receive, err = s.queue(ctx, `SELECT `+recColsFor("r")+`, r.mass_kg, NULL::text, r.occurred_at, count(*) OVER ()
		FROM records r WHERE r.type IN ('shipment','delivery') AND r.counterparty=$1 AND r.latest AND r.received_at IS NULL AND r.body->'void' IS NULL AND r.occurred_at >= $2
		ORDER BY r.occurred_at LIMIT $3`, org.ID, since, limit)
	if err != nil {
		return w, err
	}
	// Every step the organisation does: one that recycles and makes additive
	// processes reject film and recycled plastic.
	var ins []string
	for _, k := range append([]domain.OrgKind{org.Kind}, org.Kinds...) {
		if in, ok := domain.Consumes[k]; ok && !slices.Contains(ins, string(in)) {
			ins = append(ins, string(in))
		}
	}
	if len(ins) > 0 {
		// Stock is what the weighbridge read (each lot's share of its shipment's
		// received mass), never a rejected load, plus the organisation's own lots
		// of an input type that it has neither shipped nor used yet (it does the
		// next step itself); a recycled lot or additive batch only once released.
		w.Process, err = s.queue(ctx, `WITH recv AS (
				SELECT l.parent_id AS id, max(sh.received_at) AS at,
					sum(CASE WHEN sh.mass_kg > 0 AND sh.received_mass_kg > 0 THEN l.mass_kg * sh.received_mass_kg / sh.mass_kg ELSE l.mass_kg END) AS kg
				FROM links l JOIN records sh ON sh.id = l.child_id AND sh.latest
				WHERE l.kind='shipped' AND sh.counterparty=$1 AND sh.received_at >= $2 AND sh.rcpt_decision IS DISTINCT FROM 'rejected'
				GROUP BY l.parent_id)
			SELECT `+recColsFor("r")+`, x.remaining, NULL::text, x.at, count(*) OVER () FROM (
				SELECT r.id, recv.at, recv.kg, recv.kg - `+consumedSQL+` AS remaining
				FROM recv JOIN records r ON r.id = recv.id AND r.latest AND r.type = ANY($3)
				UNION ALL
				SELECT r.id, r.occurred_at, r.mass_kg, r.mass_kg - `+ownUsedSQL+` AS remaining
				FROM records r WHERE r.org_id = $1 AND r.latest AND r.type = ANY($3) AND r.occurred_at >= $2 AND r.body->'void' IS NULL
				AND r.id NOT IN (SELECT id FROM recv) -- shipped to one of its own sites: counted above
				AND (r.type NOT IN ('recycled_lot','additive_batch') OR `+decisionSQL+` = 'released')) x
			JOIN records r ON r.id = x.id AND r.latest
			WHERE x.remaining > x.kg * 0.02 ORDER BY x.at DESC LIMIT $4`, org.ID, since, ins, limit)
		if err != nil {
			return w, err
		}
	}
	w.Release, err = s.queue(ctx, `SELECT `+recColsFor("r")+`, r.mass_kg, NULL::text, r.occurred_at, count(*) OVER ()
		FROM records r WHERE r.org_id=$1 AND r.latest AND r.type IN ('recycled_lot','additive_batch') AND r.occurred_at >= $2
		AND NOT EXISTS (SELECT 1 FROM links l WHERE l.parent_id = r.id AND l.kind='releases')
		ORDER BY r.occurred_at DESC LIMIT $3`, org.ID, since, limit)
	if err != nil {
		return w, err
	}
	w.Ship, err = s.queue(ctx, `SELECT `+recColsFor("r")+`, x.remaining, x.decision, r.occurred_at, count(*) OVER () FROM (
			SELECT r.id, r.mass_kg - `+ownUsedSQL+` AS remaining, `+decisionSQL+` AS decision
			FROM records r WHERE r.org_id=$1 AND r.latest AND r.type IN ('reject_lot','recycled_lot','additive_batch','asphalt_lot')
			AND r.occurred_at >= $2) x
		JOIN records r ON r.id = x.id AND r.latest
		WHERE x.remaining > r.mass_kg * 0.02 AND (r.type IN ('reject_lot','asphalt_lot') OR x.decision = 'released')
		ORDER BY r.occurred_at DESC LIMIT $3`, org.ID, since, limit)
	return w, err
}

// StageStatus is the state of the chain at one organisation, for one of its
// activities: a company that sorts, recycles and mixes asphalt has a row per
// activity, so a blocked asphalt lot shows under Mix asphalt, not under Sort.
type StageStatus struct {
	OrgID            string     `json:"orgId"`
	Activity         string     `json:"activity"` // the org kind the records belong to
	InTransitN       int        `json:"inTransit"`
	InTransitKg      float64    `json:"inTransitKg"`
	AwaitingReleaseN int        `json:"awaitingRelease"`
	QuarantinedN     int        `json:"quarantined"`
	Blocked          int        `json:"blocked"`
	Flagged          int        `json:"flagged"`
	Records          int        `json:"records"`
	Sealed           int        `json:"sealed"`
	LastActivity     *time.Time `json:"lastActivity,omitempty"`
}

// prefixKind is the SQL for the activity that makes a record with the given id
// prefix (PRL → sorting_plant…), ” for an unknown prefix.
func prefixKind(id string) string {
	return `CASE left(` + id + `, 3) WHEN 'PRL' THEN 'sorting_plant' WHEN 'RPL' THEN 'recycler' WHEN 'ADB' THEN 'additive_producer'
		WHEN 'ASL' THEN 'asphalt_plant' WHEN 'DLV' THEN 'asphalt_plant' WHEN 'RSC' THEN 'contractor' ELSE '' END`
}

// activitySQL is the activity a record of alias r belongs to: production by its
// type, a shipment by what it carries, a test, release or acceptance by its
// subject. One the organisation does not have falls back to its main kind.
func activitySQL() string {
	act := `CASE r.type WHEN 'reject_lot' THEN 'sorting_plant' WHEN 'recycled_lot' THEN 'recycler'
		WHEN 'additive_batch' THEN 'additive_producer' WHEN 'asphalt_lot' THEN 'asphalt_plant' WHEN 'delivery' THEN 'asphalt_plant'
		WHEN 'road_section' THEN 'contractor' WHEN 'shipment' THEN ` + prefixKind(`r.inputs->0->>'id'`) + `
		ELSE ` + prefixKind(`r.body->>'subject_id'`) + ` END`
	return `CASE WHEN (` + act + `) = o.kind OR (` + act + `) = ANY(o.kinds) THEN (` + act + `) ELSE o.kind END`
}

// ChainStatus is the state of every organisation in the window (reporters,
// auditors and the operator), per activity.
func (s *Store) ChainStatus(ctx context.Context) ([]StageStatus, error) {
	since := time.Now().Add(-WorkWindow)
	// A shipment is inbound for the activity that uses what it carries.
	inbound := `CASE left(r.inputs->0->>'id', 3) WHEN 'PRL' THEN 'recycler' WHEN 'RPL' THEN 'additive_producer' WHEN 'ADB' THEN 'asphalt_plant' ELSE '' END`
	rows, err := s.DB.Query(ctx, `
		WITH transit AS (
			SELECT r.counterparty AS org_id, CASE WHEN (`+inbound+`) = o.kind OR (`+inbound+`) = ANY(o.kinds) THEN (`+inbound+`) ELSE o.kind END AS act,
				count(*) n, sum(r.mass_kg) kg
			FROM records r JOIN orgs o ON o.id = r.counterparty
			WHERE r.type='shipment' AND r.latest AND r.received_at IS NULL AND r.body->'void' IS NULL AND r.occurred_at >= $1 GROUP BY 1, 2),
		rel AS (
			SELECT r.org_id, `+activitySQL()+` AS act,
				count(*) FILTER (WHERE NOT EXISTS (SELECT 1 FROM links l WHERE l.parent_id = r.id AND l.kind='releases')) AS waiting,
				count(*) FILTER (WHERE EXISTS (SELECT 1 FROM links l JOIN records x ON x.id = l.child_id AND x.latest
					WHERE l.parent_id = r.id AND l.kind='releases' AND x.body->>'decision' = 'quarantined')) AS held
			FROM records r JOIN orgs o ON o.id = r.org_id
			WHERE r.latest AND r.type IN ('recycled_lot','additive_batch') AND r.occurred_at >= $1 GROUP BY 1, 2),
		st AS (
			SELECT r.org_id, `+activitySQL()+` AS act, count(*) FILTER (WHERE r.status='blocked') blocked, count(*) FILTER (WHERE r.status='flagged') flagged,
				count(*) n, count(*) FILTER (WHERE s.status='sealed') sealed, max(r.created_at) last
			FROM records r JOIN orgs o ON o.id = r.org_id LEFT JOIN seals s ON s.id = r.seal_id WHERE r.latest GROUP BY 1, 2),
		base AS (
			SELECT o.id AS org_id, k AS act FROM orgs o, unnest(array_append(o.kinds, o.kind)) AS k
			UNION SELECT org_id, act FROM transit UNION SELECT org_id, act FROM rel UNION SELECT org_id, act FROM st)
		SELECT b.org_id, b.act, COALESCE(t.n,0), COALESCE(t.kg,0), COALESCE(rel.waiting,0), COALESCE(rel.held,0),
			COALESCE(st.blocked,0), COALESCE(st.flagged,0), COALESCE(st.n,0), COALESCE(st.sealed,0), st.last
		FROM base b LEFT JOIN transit t ON t.org_id=b.org_id AND t.act=b.act LEFT JOIN rel ON rel.org_id=b.org_id AND rel.act=b.act
			LEFT JOIN st ON st.org_id=b.org_id AND st.act=b.act
		ORDER BY b.org_id, b.act`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StageStatus
	for rows.Next() {
		var x StageStatus
		if err := rows.Scan(&x.OrgID, &x.Activity, &x.InTransitN, &x.InTransitKg, &x.AwaitingReleaseN, &x.QuarantinedN, &x.Blocked, &x.Flagged,
			&x.Records, &x.Sealed, &x.LastActivity); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// OpenSection is a road section still being laid: what an asphalt plant needs
// to name it on a delivery ticket.
type OpenSection struct {
	ID           string    `json:"id"`
	Ref          string    `json:"ref"`
	ContractorID string    `json:"contractorId"`
	Road         string    `json:"road"`
	KmFrom       float64   `json:"kmFrom"`
	KmTo         float64   `json:"kmTo"`
	Mix          string    `json:"mix"`
	OccurredAt   time.Time `json:"occurredAt"`
}

// OpenSections lists road sections without a completion date, newest first.
func (s *Store) OpenSections(ctx context.Context, limit int) ([]OpenSection, error) {
	rows, err := s.DB.Query(ctx, `SELECT id, ref, org_id, COALESCE(body->>'road',''), COALESCE((body->>'km_from')::float8,0),
			COALESCE((body->>'km_to')::float8,0), COALESCE(body->>'mix',''), occurred_at
		FROM records WHERE type='road_section' AND latest AND COALESCE(body->>'completed_at','') = ''
		ORDER BY occurred_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OpenSection{}
	for rows.Next() {
		var o OpenSection
		if err := rows.Scan(&o.ID, &o.Ref, &o.ContractorID, &o.Road, &o.KmFrom, &o.KmTo, &o.Mix, &o.OccurredAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
