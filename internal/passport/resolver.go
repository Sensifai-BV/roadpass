// Package passport assembles passports: lineage, evidence, signatures, seal
// proofs and carbon figures, with redaction by viewer.
package passport

import (
	"context"
	"sync"

	"github.com/sensifai/roadpass/internal/domain"
	"github.com/sensifai/roadpass/internal/lca"
	"github.com/sensifai/roadpass/internal/store"
)

// Resolver computes per-kg intensities (recycled content, avoided emissions,
// footprint) of records from their upstream inputs, memoised by id and version.
// It uses lca.Node, the same definition the passport graph uses.
type Resolver struct {
	st *store.Store
	sc lca.Scenario

	mu   sync.Mutex
	memo map[string]memoEntry
}

type memoEntry struct {
	version int
	it      lca.Intensity
}

// maxMemo bounds the memo; it is cleared when full (cheap to rebuild, and bounded
// memory matters more than a perfect hit rate).
const maxMemo = 400_000

// NewResolver returns a resolver for a scenario.
func NewResolver(st *store.Store, sc lca.Scenario) *Resolver {
	return &Resolver{st: st, sc: sc, memo: map[string]memoEntry{}}
}

// Forget drops memoised intensities of records whose inputs or shipments changed
// (a new shipment adds transport to the lots it carries).
func (rv *Resolver) Forget(ids ...string) {
	rv.mu.Lock()
	defer rv.mu.Unlock()
	for _, id := range ids {
		delete(rv.memo, id)
	}
}

func (rv *Resolver) get(r *domain.Record) (lca.Intensity, bool) {
	rv.mu.Lock()
	defer rv.mu.Unlock()
	e, ok := rv.memo[r.ID]
	return e.it, ok && e.version == r.Version
}

func (rv *Resolver) put(r *domain.Record, v lca.Intensity) {
	rv.mu.Lock()
	defer rv.mu.Unlock()
	if len(rv.memo) >= maxMemo {
		rv.memo = map[string]memoEntry{}
	}
	rv.memo[r.ID] = memoEntry{r.Version, v}
}

// Of returns the per-kg intensity of r.
func (rv *Resolver) Of(ctx context.Context, r *domain.Record) (lca.Intensity, error) {
	return rv.of(ctx, r, 0)
}

func (rv *Resolver) of(ctx context.Context, r *domain.Record, depth int) (lca.Intensity, error) {
	if v, ok := rv.get(r); ok {
		return v, nil
	}
	if depth > 12 {
		return lca.Intensity{}, nil
	}
	var parents []domain.Input
	switch r.Type {
	case domain.TypeRecycledLot, domain.TypeAdditiveBatch, domain.TypeAsphaltLot, domain.TypeDelivery, domain.TypeShipment:
		parents = r.Inputs
	case domain.TypeRoadSection:
		links, err := rv.st.LaidOn(ctx, r.ID)
		if err != nil {
			return lca.Intensity{}, err
		}
		for _, l := range links {
			parents = append(parents, domain.Input{ID: l.Parent, MassKg: l.MassKg})
		}
	}
	var ins []lca.Weighted
	if len(parents) > 0 {
		ids := make([]string, len(parents))
		for i, p := range parents {
			ids[i] = p.ID
		}
		recs, err := rv.st.RecordsByID(ctx, ids)
		if err != nil {
			return lca.Intensity{}, err
		}
		byID := make(map[string]*domain.Record, len(recs))
		for i := range recs {
			byID[recs[i].ID] = &recs[i]
		}
		for _, p := range parents {
			pr := byID[p.ID]
			if pr == nil || pr.MassKg <= 0 || !pr.CountsMass() { // void, or rejected at the paver
				continue
			}
			it, err := rv.of(ctx, pr, depth+1)
			if err != nil {
				return lca.Intensity{}, err
			}
			ins = append(ins, lca.Weighted{Intensity: it, MassKg: p.MassKg})
		}
	}
	var ship lca.Intensity
	if r.Type == domain.TypeRejectLot || r.Type == domain.TypeRecycledLot || r.Type == domain.TypeAdditiveBatch {
		sh, err := rv.st.ShipmentsOf(ctx, []string{r.ID})
		if err != nil {
			return lca.Intensity{}, err
		}
		for _, x := range sh[r.ID] {
			ship = ship.Add(lca.ShipmentShare(&x.Shipment, x.ItemKg, rv.sc))
		}
	}
	out := lca.Node(r, ins, ship, rv.sc)
	rv.put(r, out)
	return out, nil
}
