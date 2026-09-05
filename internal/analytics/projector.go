package analytics

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sensifai/roadpass/internal/domain"
	"github.com/sensifai/roadpass/internal/lca"
	"github.com/sensifai/roadpass/internal/passport"
	"github.com/sensifai/roadpass/internal/store"
)

// CursorName is the projector's position in Postgres. Its suffix is the fact
// row's layout: bump it when a column is added that rows already projected lack
// (v2: status, without which blocked records counted as ok). A new name starts at
// 0, so every version is projected again; on equal versions ReplacingMergeTree
// keeps the newer row, so the replay overwrites the stale ones.
const CursorName = "clickhouse_projector_v2"

// Projector copies record versions from Postgres into the ClickHouse fact table,
// in seq order, at least once (ReplacingMergeTree makes replays harmless).
type Projector struct {
	st *store.Store
	ch *DB
	rv *passport.Resolver
	sc lca.Scenario

	mu     sync.Mutex
	orgs   map[string]domain.Org
	sites  map[string]domain.Site
	owners map[string]string // road section id -> owner org
	loaded time.Time
}

// NewProjector returns a projector using the default scenario.
func NewProjector(st *store.Store, ch *DB, rv *passport.Resolver) *Projector {
	return &Projector{st: st, ch: ch, rv: rv, sc: lca.DefaultScenario, owners: map[string]string{}}
}

func (p *Projector) refresh(ctx context.Context) error {
	if time.Since(p.loaded) < time.Minute && p.orgs != nil {
		return nil
	}
	orgs, err := p.st.Orgs(ctx)
	if err != nil {
		return err
	}
	sites, err := p.st.Sites(ctx, "")
	if err != nil {
		return err
	}
	p.orgs, p.sites = map[string]domain.Org{}, map[string]domain.Site{}
	for _, o := range orgs {
		p.orgs[o.ID] = o
	}
	for _, s := range sites {
		p.sites[s.ID] = s
	}
	cs := make(map[string]string, len(orgs))
	for _, o := range orgs {
		cs[o.ID] = o.Country
	}
	lca.SetOrgCountries(cs)
	p.loaded = time.Now()
	return nil
}

func (p *Projector) owner(ctx context.Context, sectionID string) string {
	if o, ok := p.owners[sectionID]; ok {
		return o
	}
	r, err := p.st.RecordAny(ctx, sectionID)
	if err != nil {
		return ""
	}
	o := domain.Str(r.Body, "owner_org_id")
	if len(p.owners) > 200_000 {
		p.owners = map[string]string{}
	}
	p.owners[sectionID] = o
	return o
}

// Step projects up to batch versions; it returns how many it projected.
func (p *Projector) Step(ctx context.Context, batch int) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.refresh(ctx); err != nil {
		return 0, err
	}
	pos, err := p.st.CursorPos(ctx, CursorName)
	if err != nil {
		return 0, err
	}
	recs, err := p.st.RecordsAfter(ctx, pos, batch)
	if err != nil {
		return 0, err
	}
	if len(recs) == 0 {
		return p.reproject(ctx, batch)
	}
	if err := p.insert(ctx, recs); err != nil {
		return 0, err
	}
	return len(recs), p.st.SetCursor(ctx, CursorName, recs[len(recs)-1].Seq)
}

// reproject rebuilds the rows of records whose receipt changed what they count
// for (store.SetReceipt queues them). The row keeps its version: on equal
// versions ReplacingMergeTree keeps the last inserted row, and this runs only
// once the seq-ordered feed is drained, so the re-projection is the newest. A
// replay of the feed reads the same Postgres state, so it cannot undo it.
func (p *Projector) reproject(ctx context.Context, batch int) (int, error) {
	ids, recs, upTo, err := p.st.Reprojections(ctx, batch)
	if err != nil || len(ids) == 0 {
		return 0, err
	}
	recs, err = p.withSections(ctx, recs)
	if err != nil {
		return 0, err
	}
	if len(recs) > 0 {
		if err := p.insert(ctx, recs); err != nil {
			return 0, err
		}
	}
	return len(ids), p.st.DoneReprojections(ctx, ids, upTo)
}

// withSections adds, for each re-projected delivery, the road section it was
// laid on: its recycled content and avoided CO₂e come from the deliveries that
// count, and a receipt changes neither record's version, so the resolver's memo
// of both is dropped first.
func (p *Projector) withSections(ctx context.Context, recs []domain.Record) ([]domain.Record, error) {
	seen := map[string]bool{}
	for _, r := range recs {
		seen[r.ID] = true
	}
	var ids []string
	for _, r := range recs {
		if r.Type != domain.TypeDelivery {
			continue
		}
		p.rv.Forget(r.ID)
		if sec := domain.Str(r.Body, "section_id"); sec != "" && !seen[sec] {
			seen[sec] = true
			ids = append(ids, sec)
		}
	}
	if len(ids) == 0 {
		return recs, nil
	}
	p.rv.Forget(ids...)
	secs, err := p.st.RecordsByID(ctx, ids)
	if err != nil {
		return nil, err
	}
	return append(recs, secs...), nil
}

func (p *Projector) insert(ctx context.Context, recs []domain.Record) error {
	for i := range recs {
		if recs[i].Type == domain.TypeShipment {
			for _, in := range recs[i].Inputs {
				p.rv.Forget(in.ID)
			}
		}
	}
	b, err := p.ch.C.PrepareBatch(ctx, "INSERT INTO records")
	if err != nil {
		return err
	}
	for i := range recs {
		row, err := p.fact(ctx, &recs[i])
		if err != nil {
			_ = b.Abort()
			return fmt.Errorf("project %s: %w", recs[i].ID, err)
		}
		if err := b.Append(row...); err != nil {
			_ = b.Abort()
			return err
		}
	}
	return b.Send()
}

// CatchUp projects until nothing is left.
func (p *Projector) CatchUp(ctx context.Context, batch int, progress func(n int)) error {
	total := 0
	for {
		n, err := p.Step(ctx, batch)
		if err != nil {
			return err
		}
		if n == 0 {
			return nil
		}
		total += n
		if progress != nil {
			progress(total)
		}
	}
}

func (p *Projector) fact(ctx context.Context, r *domain.Record) ([]any, error) {
	o := p.orgs[r.OrgID]
	region := o.Region
	if s, ok := p.sites[r.SiteID]; ok && s.Region != "" {
		region = s.Region
	}
	var parent, section, owner, mix, process, layer, road, test, route string
	var testValue, area float64
	var testPass int8
	var dosage, temp, dist, lat, lon, length float32
	if len(r.Inputs) > 0 {
		parent = r.Inputs[0].ID
	}
	b := r.Body
	switch r.Type {
