package store

import (
	"context"

	"github.com/sensifai/roadpass/internal/domain"
)

// Graph is a lineage sub-graph: the records and the edges between them.
type Graph struct {
	Nodes     []domain.Record `json:"-"`
	Links     []domain.Link   `json:"links"`
	Truncated bool            `json:"truncated"`
}

// Upstream walks material links from id towards its origins (sorting plants),
// up to maxNodes records. The root is included. A shipment's carried lots are
// linked as shipped (custody, not consumption), so the walk follows shipped
// edges out of the root only: a shipment's passport shows what it carries, and no
// other passport counts a shipment as material.
func (s *Store) Upstream(ctx context.Context, id string, maxNodes int) (*Graph, error) {
	return s.walk(ctx, id, maxNodes, true)
}

// Downstream walks material links from id towards the road.
func (s *Store) Downstream(ctx context.Context, id string, maxNodes int) (*Graph, error) {
	return s.walk(ctx, id, maxNodes, false)
}

func (s *Store) walk(ctx context.Context, id string, maxNodes int, up bool) (*Graph, error) {
	if maxNodes <= 0 {
		maxNodes = 2000
	}
	// UNION (not UNION ALL) stops on cycles; the depth cap is a second guard.
	from, to := "child_id", "parent_id"
	if !up {
		from, to = "parent_id", "child_id"
	}
	q := `WITH RECURSIVE g(node, depth) AS (
			SELECT $1::text, 0
			UNION
			SELECT l.` + to + `, g.depth + 1 FROM links l JOIN g ON l.` + from + ` = g.node
			WHERE (l.kind = ANY($2) OR (g.depth = 0 AND l.kind = ANY($4))) AND g.depth < 12
		)
		SELECT DISTINCT node FROM g LIMIT $3`
	first := []string{}
	if up {
		first = []string{domain.LinkShipped}
	}
	rows, err := s.DB.Query(ctx, q, id, domain.MaterialLinks, maxNodes+1, first)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, n)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	g := &Graph{}
	if len(ids) > maxNodes {
		ids, g.Truncated = ids[:maxNodes], true
	}
	if g.Nodes, err = s.RecordsByID(ctx, ids); err != nil {
		return nil, err
	}
	lrows, err := s.DB.Query(ctx, `SELECT parent_id, child_id, kind, mass_kg FROM links
		WHERE (kind = ANY($2) OR (child_id = $3 AND kind = ANY($4))) AND parent_id = ANY($1) AND child_id = ANY($1)`,
		ids, domain.MaterialLinks, id, first)
	if err != nil {
		return nil, err
	}
	defer lrows.Close()
	for lrows.Next() {
		var l domain.Link
		if err := lrows.Scan(&l.Parent, &l.Child, &l.Kind, &l.MassKg); err != nil {
			return nil, err
		}
		g.Links = append(g.Links, l)
	}
	return g, lrows.Err()
}

// Evidence returns the shipments, lab tests and acceptances attached to the given
// records.
func (s *Store) Evidence(ctx context.Context, ids []string) ([]domain.Record, []domain.Link, error) {
	rows, err := s.DB.Query(ctx, `SELECT parent_id, child_id, kind, mass_kg FROM links
		WHERE parent_id = ANY($1) AND kind IN ('shipped','tested','accepts','releases')`, ids)
	if err != nil {
		return nil, nil, err
	}
	var links []domain.Link
	var childIDs []string
	for rows.Next() {
		var l domain.Link
		if err := rows.Scan(&l.Parent, &l.Child, &l.Kind, &l.MassKg); err != nil {
			rows.Close()
			return nil, nil, err
		}
		links = append(links, l)
		childIDs = append(childIDs, l.Child)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	recs, err := s.RecordsByID(ctx, childIDs)
	return recs, links, err
}

// Summary is a precomputed passport figure set.
type Summary struct {
	RecordID        string   `json:"recordId"`
	RecycledKg      float64  `json:"recycledKg"`
	OriginOrgs      []string `json:"originOrgs"`
	OriginRegions   []string `json:"originRegions"`
	UpstreamRecords int      `json:"upstreamRecords"`
	CO2eAvoidedKg   float64  `json:"co2eAvoidedKg"`
	CO2eFootprintKg float64  `json:"co2eFootprintKg"`
	TransportTkm    float64  `json:"transportTkm"`
	OpenFindings    int      `json:"openFindings"`
}

// PutSummaries upserts passport summaries.
func (s *Store) PutSummaries(ctx context.Context, sums []Summary) error {
	if len(sums) == 0 {
		return nil
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit
	for _, x := range sums {
		if _, err := tx.Exec(ctx, `INSERT INTO passport_summary(record_id,recycled_kg,origin_orgs,origin_regions,upstream_records,
				co2e_avoided_kg,co2e_footprint_kg,transport_tkm,open_findings,updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,now())
			ON CONFLICT (record_id) DO UPDATE SET recycled_kg=EXCLUDED.recycled_kg, origin_orgs=EXCLUDED.origin_orgs,
				origin_regions=EXCLUDED.origin_regions, upstream_records=EXCLUDED.upstream_records,
				co2e_avoided_kg=EXCLUDED.co2e_avoided_kg, co2e_footprint_kg=EXCLUDED.co2e_footprint_kg,
				transport_tkm=EXCLUDED.transport_tkm, open_findings=EXCLUDED.open_findings, updated_at=now()`,
			x.RecordID, x.RecycledKg, x.OriginOrgs, x.OriginRegions, x.UpstreamRecords, x.CO2eAvoidedKg,
			x.CO2eFootprintKg, x.TransportTkm, x.OpenFindings); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// Summary returns one passport summary.
func (s *Store) Summary(ctx context.Context, id string) (Summary, error) {
	x := Summary{RecordID: id}
	err := s.DB.QueryRow(ctx, `SELECT recycled_kg, origin_orgs, origin_regions, upstream_records, co2e_avoided_kg,
		co2e_footprint_kg, transport_tkm, open_findings FROM passport_summary WHERE record_id=$1`, id).
		Scan(&x.RecycledKg, &x.OriginOrgs, &x.OriginRegions, &x.UpstreamRecords, &x.CO2eAvoidedKg, &x.CO2eFootprintKg, &x.TransportTkm, &x.OpenFindings)
	return x, notFound(err)
}

// ShipItem is one shipment that carried an item, with the mass it carried.
type ShipItem struct {
	Shipment domain.Record
	ItemKg   float64
}

// ShipmentsOf returns, for each item id, the shipments that carried it.
func (s *Store) ShipmentsOf(ctx context.Context, itemIDs []string) (map[string][]ShipItem, error) {
	rows, err := s.DB.Query(ctx, `SELECT parent_id, child_id, mass_kg FROM links WHERE kind='shipped' AND parent_id = ANY($1)`, itemIDs)
	if err != nil {
		return nil, err
	}
	type edge struct {
		item, ship string
		kg         float64
	}
	var edges []edge
	var shipIDs []string
	for rows.Next() {
