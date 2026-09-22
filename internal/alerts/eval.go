package alerts

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/sensifai/roadpass/internal/analytics"
	"github.com/sensifai/roadpass/internal/store"
)

// Sources the evaluators read.
type Sources struct {
	St *store.Store
	CH *analytics.DB
}

// Evaluate returns what rule r currently sees as wrong, at now.
func (src Sources) Evaluate(ctx context.Context, r Rule, now time.Time) ([]Hit, error) {
	switch r.Kind {
	case KindMetric:
		return src.metric(ctx, r, now)
	case KindFinding:
		return src.findings(ctx, r)
	case KindReceiptOverdue:
		return src.overdue(ctx, r, now)
	case KindRejected:
		return src.rejected(ctx, r, now)
	case KindTelemetryGap:
		return src.telemetryGap(ctx, r, now)
	case KindSealBacklog:
		return src.sealBacklog(ctx, r, now)
	case KindPlanLimit:
		return src.planLimit(ctx, r, now)
	}
	return nil, fmt.Errorf("unknown rule kind %q", r.Kind)
}

// Crosses compares a value with a threshold.
func Crosses(v float64, op string, threshold float64) bool {
	switch op {
	case ">":
		return v > threshold
	case ">=":
		return v >= threshold
	case "<":
		return v < threshold
	case "<=":
		return v <= threshold
	}
	return false
}

func (src Sources) metric(ctx context.Context, r Rule, now time.Time) ([]Hit, error) {
	p := r.Params
	if p.Query == nil {
		return nil, fmt.Errorf("metric rule %s has no query", r.ID)
	}
	q := *p.Query
	q.Interval, q.Compare = "", false
	win := time.Duration(max(5, p.WindowMins)) * time.Minute
	res, err := src.CH.Explore(ctx, analytics.Scope{OrgID: r.OrgID}, analytics.Range{From: now.Add(-win), To: now}, q)
	if err != nil {
		return nil, err
	}
	var hits []Hit
	for _, row := range res.Rows {
		if row.Count == 0 || !Crosses(row.Value, p.Op, p.Value) {
			continue
		}
		key, who := "value", ""
		if row.Group != "" {
			key, who = row.Group, " for "+row.Group
		}
		hits = append(hits, Hit{Key: key, Value: row.Value,
			Title: fmt.Sprintf("%s%s: %s %s", r.Name, who, strings.ToLower(res.Field.Label[:1])+res.Field.Label[1:], fmtVal(row.Value, res.Unit)),
			Body:  fmt.Sprintf("%s over the last %s is %s, %s the threshold of %s.", res.Field.Label, humanDur(win), fmtVal(row.Value, res.Unit), opWord(p.Op), fmtVal(p.Value, res.Unit)),
			Link:  "/dashboards", Detail: map[string]any{"group": row.Group, "records": row.Count, "unit": res.Unit}})
	}
	return hits, nil
}

func (src Sources) findings(ctx context.Context, r Rule) ([]Hit, error) {
	sev := r.Params.MinSeverity
	rows, err := src.St.DB.Query(ctx, `SELECT f.id, f.record_id, f.code, f.severity, f.message FROM findings f
		WHERE f.org_id=$1 AND f.status='open' AND f.created_at >= $2 AND ($3 <> 'blocker' OR f.severity='blocker')
		ORDER BY f.created_at DESC LIMIT 200`, r.OrgID, r.UpdatedAt, sev)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hits []Hit
	for rows.Next() {
		var id int64
		var rec, code, severity, msg string
		if err := rows.Scan(&id, &rec, &code, &severity, &msg); err != nil {
			return nil, err
		}
		hits = append(hits, Hit{Key: fmt.Sprint(id), Title: fmt.Sprintf("%s on %s: %s", code, rec, msg),
			Body: fmt.Sprintf("A %s data check failed on %s: %s", severity, rec, msg), Link: "/passports/" + rec,
			Detail: map[string]any{"record": rec, "code": code, "severity": severity}})
	}
	return hits, rows.Err()
}

func (src Sources) overdue(ctx context.Context, r Rule, now time.Time) ([]Hit, error) {
	h := max(1, r.Params.Hours)
	rows, err := src.St.DB.Query(ctx, `SELECT id, org_id, occurred_at, mass_kg FROM records
		WHERE counterparty=$1 AND latest AND type='shipment' AND received_at IS NULL AND body->'void' IS NULL
		  AND occurred_at < $2 AND occurred_at > $3 ORDER BY occurred_at LIMIT 200`, r.OrgID, now.Add(-time.Duration(h)*time.Hour), now.AddDate(0, 0, -30))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hits []Hit
	for rows.Next() {
		var id, from string
		var at time.Time
		var kg float64
		if err := rows.Scan(&id, &from, &at, &kg); err != nil {
			return nil, err
		}
		late := now.Sub(at)
		hits = append(hits, Hit{Key: id, Value: late.Hours(), Title: fmt.Sprintf("Shipment %s not received after %s", id, humanDur(late)),
			Body: fmt.Sprintf("%.1f t sent %s ago is still waiting for an intake review.", kg/1000, humanDur(late)), Link: "/work",
			Detail: map[string]any{"shipment": id, "from": from, "massKg": kg}})
	}
	return hits, rows.Err()
}

func (src Sources) rejected(ctx context.Context, r Rule, now time.Time) ([]Hit, error) {
	h := r.Params.Hours
	if h <= 0 {
		h = 48
	}
	since := now.Add(-time.Duration(h) * time.Hour)
	if r.UpdatedAt.After(since) {
		since = r.UpdatedAt
	}
	rows, err := src.St.DB.Query(ctx, `SELECT id, counterparty, received_at, rcpt_note FROM records
		WHERE org_id=$1 AND latest AND type='shipment' AND rcpt_decision='rejected' AND received_at >= $2 ORDER BY received_at DESC LIMIT 200`, r.OrgID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hits []Hit
	for rows.Next() {
		var id, to, note string
		var at time.Time
		if err := rows.Scan(&id, &to, &at, &note); err != nil {
			return nil, err
		}
		hits = append(hits, Hit{Key: id, Title: fmt.Sprintf("Shipment %s rejected at intake", id),
			Body: fmt.Sprintf("The receiver rejected it: “%s”", note), Link: "/passports?type=shipment&q=" + id,
			Detail: map[string]any{"shipment": id, "receiver": to, "note": note}})
	}
	return hits, rows.Err()
}

func (src Sources) telemetryGap(ctx context.Context, r Rule, now time.Time) ([]Hit, error) {
	mins := max(5, r.Params.Minutes)
	var last []struct {
		Site string    `ch:"site_id"`
		At   time.Time `ch:"at"`
	}
	err := src.CH.C.Select(ctx, &last, `SELECT site_id, max(at) AS at FROM (
			SELECT site_id, toDateTime(ts) AS at FROM extruder_readings WHERE org_id = ? AND ts > ?
			UNION ALL SELECT site_id, toDateTime(ts) AS at FROM mixer_cycles WHERE org_id = ? AND ts > ?)
		GROUP BY site_id`, r.OrgID, now.AddDate(0, 0, -7), r.OrgID, now.AddDate(0, 0, -7))
	if err != nil {
		return nil, err
	}
	var hits []Hit
	for _, x := range last {
		gap := now.Sub(x.At)
		if gap < time.Duration(mins)*time.Minute {
			continue
		}
		hits = append(hits, Hit{Key: x.Site, Value: gap.Minutes(), Title: fmt.Sprintf("No plant data from %s for %s", x.Site, humanDur(gap)),
			Body: fmt.Sprintf("The plant control system at %s last reported %s ago.", x.Site, humanDur(gap)), Link: "/plants",
			Detail: map[string]any{"site": x.Site, "lastAt": x.At}})
	}
	return hits, nil
}

func (src Sources) sealBacklog(ctx context.Context, r Rule, now time.Time) ([]Hit, error) {
	mins := max(5, r.Params.Minutes)
	var n int
	var oldest *time.Time
	err := src.St.DB.QueryRow(ctx, `SELECT count(*), min(r.created_at) FROM records r JOIN orgs o ON o.id = r.org_id
		WHERE r.seal_id IS NULL AND o.status='verified' AND r.created_at < $1`, now.Add(-time.Duration(mins)*time.Minute)).Scan(&n, &oldest)
	if err != nil || n == 0 {
		return nil, err
	}
	age := now.Sub(*oldest)
	return []Hit{{Key: "backlog", Value: float64(n), Title: fmt.Sprintf("%d records waiting for a ledger seal", n),
		Body: fmt.Sprintf("The oldest has waited %s. Check the ledger and the worker.", humanDur(age)), Link: "/platform?tab=system",
		Detail: map[string]any{"waiting": n, "oldestMinutes": int(age.Minutes())}}}, nil
}

func (src Sources) planLimit(ctx context.Context, r Rule, now time.Time) ([]Hit, error) {
	pct := r.Params.Percent
	if pct <= 0 {
		pct = 80
	}
	var plan store.Plan
	err := src.St.DB.QueryRow(ctx, `SELECT p.id, p.name, p.seats, p.records_per_month FROM subscriptions s JOIN plans p ON p.id = s.plan_id
		WHERE s.org_id=$1 AND s.status <> 'cancelled'`, r.OrgID).Scan(&plan.ID, &plan.Name, &plan.Seats, &plan.RecordsPerMonth)
	if err != nil {
		return nil, nil // no plan, nothing to watch
	}
	usage, err := src.St.UsageByOrg(ctx, store.MonthStart(now))
	if err != nil {
		return nil, err
	}
	u := usage[r.OrgID]
	var hits []Hit
	check := func(key, what string, used, limit int) {
		if limit <= 0 {
			return
		}
		share := 100 * float64(used) / float64(limit)
		if share >= float64(pct) {
			hits = append(hits, Hit{Key: key, Value: share, Title: fmt.Sprintf("%s at %.0f %% of the %s plan", what, share, plan.Name),
				Body: fmt.Sprintf("%d of %d %s used this month.", used, limit, what), Link: "/settings?tab=billing"})
		}
	}
	check("records", "Records", u.Records, plan.RecordsPerMonth)
	check("seats", "Members", u.Seats, plan.Seats)
	return hits, nil
}

func opWord(op string) string {
	return map[string]string{">": "above", ">=": "at or above", "<": "below", "<=": "at or below"}[op]
}

func humanDur(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf("%d days", int(d.Hours()/24))
	case d >= 2*time.Hour:
		return fmt.Sprintf("%d hours", int(d.Hours()))
	case d >= time.Hour:
		return "1 hour"
	}
	return fmt.Sprintf("%d minutes", int(math.Max(1, d.Minutes())))
}

// fmtVal formats a value in its catalogue unit for messages.
func fmtVal(v float64, unit string) string {
	switch unit {
	case "kg":
		if math.Abs(v) >= 1000 {
			return fmt.Sprintf("%.1f t", v/1000)
		}
		return fmt.Sprintf("%.0f kg", v)
	case "kgco2e":
		return fmt.Sprintf("%.1f t CO₂e", v/1000)
	case "pct":
		return fmt.Sprintf("%.1f %%", v)
	case "c":
		return fmt.Sprintf("%.1f °C", v)
	case "bar":
		return fmt.Sprintf("%.1f bar", v)
	case "count":
		return fmt.Sprintf("%.0f", v)
	case "kg_h":
		return fmt.Sprintf("%.0f kg/h", v)
	}
	return fmt.Sprintf("%.2f", v)
}
