package api

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/sensifai/roadpass/internal/analytics"
	"github.com/sensifai/roadpass/internal/cache"
	"github.com/sensifai/roadpass/internal/domain"
	"github.com/sensifai/roadpass/internal/lca"
	"github.com/sensifai/roadpass/internal/passport"
	"github.com/sensifai/roadpass/internal/store"
)

func (s *Server) buildPassport(ctx context.Context, id string, v *store.Viewer) (*passport.Passport, error) {
	key := "pp:" + id + ":public"
	if v != nil {
		key = fmt.Sprintf("pp:%s:%s:%t", id, v.OrgID, v.All())
	}
	return cache.Remember(ctx, s.Cache, key, 2*time.Minute, func(ctx context.Context) (*passport.Passport, error) {
		return s.Passport.Build(ctx, id, v)
	})
}

func (s *Server) getPassport(w http.ResponseWriter, r *http.Request) {
	var v *store.Viewer
	if p := principal(r.Context()); p != nil {
		v = &p.Viewer
	}
	pp, err := s.buildPassport(r.Context(), r.PathValue("id"), v)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, pp)
}

// publicPassport is the product-passport public layer (QR code target): no
// sign-in, only passport record types, no record bodies.
func (s *Server) publicPassport(w http.ResponseWriter, r *http.Request) {
	if ok, _, _ := s.Cache.Allow(r.Context(), "public:"+clientIP(r), 120, time.Minute); !ok {
		writeProblem(w, r, http.StatusTooManyRequests, "Rate limit", "")
		return
	}
	// The path carries a share token; unknown or revoked tokens look like nothing.
	id, err := s.St.ResolveShare(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, r, err)
		return
	}
	pp, err := s.buildPassport(r.Context(), id, nil)
	if err != nil {
		fail(w, r, err)
		return
	}
	if !domain.Types[pp.Root.Type].Passport {
		fail(w, r, store.ErrNotFound)
		return
	}
	// A link can be stopped at any moment, so no browser or proxy may keep serving it:
	// the server-side passport cache already makes the revalidation cheap.
	w.Header().Set("Cache-Control", "no-cache")
	writeJSON(w, http.StatusOK, pp)
}

type shareView struct {
	Shared bool   `json:"shared"`
	Token  string `json:"token,omitempty"`
	URL    string `json:"url,omitempty"`
}

func (s *Server) shareURL(token string) string {
	return strings.TrimSuffix(s.Cfg.PublicURL, "/") + "/p/" + token
}

// ownedPassport loads the passport for the caller and requires that their
// organisation made it: only the maker publishes a passport.
func (s *Server) ownedPassport(w http.ResponseWriter, r *http.Request) (string, bool) {
	p := principal(r.Context())
	pp, err := s.Passport.Build(r.Context(), r.PathValue("id"), &p.Viewer)
	if err != nil {
		fail(w, r, err)
		return "", false
	}
	if !domain.Types[pp.Root.Type].Passport {
		fail(w, r, store.ErrNotFound)
		return "", false
	}
	if pp.Root.OrgID != p.Org.ID {
		writeProblem(w, r, http.StatusForbidden, "Not your passport", "Only the company that made a passport can publish a link to it.")
		return "", false
	}
	return pp.Root.ID, true
}

func (s *Server) getShare(w http.ResponseWriter, r *http.Request) {
	id, ok := s.ownedPassport(w, r)
	if !ok {
		return
	}
	tok, err := s.St.ShareToken(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusOK, shareView{})
		return
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, shareView{Shared: true, Token: tok, URL: s.shareURL(tok)})
}

// createShare returns the passport's public link, creating it the first time.
func (s *Server) createShare(w http.ResponseWriter, r *http.Request) {
	id, ok := s.ownedPassport(w, r)
	if !ok {
		return
	}
	p := principal(r.Context())
	tok, err := s.St.EnsureShare(r.Context(), id, p.Org.ID, p.Actor())
	if err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "passport.share", id, nil)
	writeJSON(w, http.StatusOK, shareView{Shared: true, Token: tok, URL: s.shareURL(tok)})
}

// rotateShare replaces the link: the old one stops working immediately.
func (s *Server) rotateShare(w http.ResponseWriter, r *http.Request) {
	id, ok := s.ownedPassport(w, r)
	if !ok {
		return
	}
	p := principal(r.Context())
	tok, err := s.St.RotateShare(r.Context(), id, p.Org.ID, p.Actor())
	if err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "passport.share.rotate", id, nil)
	writeJSON(w, http.StatusOK, shareView{Shared: true, Token: tok, URL: s.shareURL(tok)})
}

func (s *Server) stopShare(w http.ResponseWriter, r *http.Request) {
	id, ok := s.ownedPassport(w, r)
	if !ok {
		return
	}
	p := principal(r.Context())
	if err := s.St.RevokeShare(r.Context(), id); err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "passport.share.stop", id, nil)
	w.WriteHeader(http.StatusNoContent)
}

// exportGRETA writes the GRETA-ready LCA input: one row per finished road
// section, with the inputs needed to recompute any scenario and the default
// scenario's results. Factor sources are listed in the header comment.
func (s *Server) exportGRETA(w http.ResponseWriter, r *http.Request) {
	rg := window(r)
	scKey := r.URL.Query().Get("scenario")
	var sc lca.Scenario
	for _, x := range lca.Scenarios {
		if x.Key == scKey {
			sc = x
		}
	}
	if sc.Key == "" {
		sc = lca.DefaultScenario
	}
	rows, err := s.CH.GRETARows(r.Context(), scope(r), rg)
	if err != nil {
		fail(w, r, err)
		return
	}
	// Factors follow each organisation's country.
	orgs, err := s.St.Orgs(r.Context())
	if err != nil {
		fail(w, r, err)
		return
	}
	cs := make(map[string]string, len(orgs))
	for _, o := range orgs {
		cs[o.ID] = o.Country
	}
	lca.SetOrgCountries(cs)
	// The sensitivity is recomputed over each section's lineage with its own
	// resolver, so every lot gets the residual mix and landfill factor of its
	// own country (the projection holds the default scenario only).
	var sens map[string]lca.Result
	if sc.Key == "sensitivity" {
		if sens, err = s.sensitivity(r.Context(), rows, sc); err != nil {
			fail(w, r, err)
			return
		}
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="roadpass-greta-%s-%s.csv"`, sc.Key, rg.To.Format("20060102")))
	fmt.Fprintf(w, "# ROADPASS GRETA-ready LCA input. Scenario: %s — %s\n", sc.Name, sc.About)
	fmt.Fprintf(w, "# Factors apply by the country of the organisation that recorded each lot; national values follow each fallback.\n")
	for _, k := range []string{"incineration", "landfill", "virgin_ldpe", "grid", "grid_residual", "natural_gas", "gas_kwh_per_m3", "truck", "hma_plant"} {
		f := lca.Factors[k]
		fmt.Fprintf(w, "# factor %s = %g %s (%s %d, %s)\n", f.Key, f.Value, f.Unit, f.Geo, f.Year, f.Source)
		for _, c := range slices.Sorted(maps.Keys(lca.Regional[k])) {
			n := lca.Regional[k][c]
			fmt.Fprintf(w, "#   %s %s = %g %s (%d, %s)\n", f.Key, c, n.Value, n.Unit, n.Year, n.Source)
		}
	}
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"section_id", "road", "region", "layer", "completed", "asphalt_t", "recycled_plastic_kg", "reject_diverted_kg",
		"transport_tkm", "chain_footprint_kgco2e", "avoided_disposal_kgco2e", "avoided_virgin_kgco2e", "net_benefit_kgco2e",
		"scenario", "provenance"})
	for _, x := range rows {
		disposal, virgin, chain := x.AvoidedKg-x.VirginKg, x.VirginKg, x.ChainKg
		switch sc.Key {
		case "baseline":
			// Without the additive nothing is avoided and the plastic chain does not run.
			disposal, virgin, chain = 0, 0, 0
		case "sensitivity":
			res := sens[x.ID]
			disposal, virgin, chain = res.DisposalKg, res.VirginKg, res.ChainFootKg
		}
		_ = cw.Write([]string{x.ID, x.Road, x.Region, x.Layer, x.At.Format("2006-01-02"), ff(x.MassKg/1000, 1),
			ff(x.RecycledKg, 1), ff(x.RejectKg, 1), ff(x.Tkm, 1), ff(chain, 1), ff(disposal, 1), ff(virgin, 1),
			ff(disposal+virgin-chain, 1), sc.Key, x.Provenance})
	}
	cw.Flush()
}

// sensitivity computes a scenario's results for road sections from their lineage.
func (s *Server) sensitivity(ctx context.Context, rows []analytics.GRETARow, sc lca.Scenario) (map[string]lca.Result, error) {
	rv := passport.NewResolver(s.St, sc)
	out := make(map[string]lca.Result, len(rows))
	for ids := range slices.Chunk(rows, 500) {
		keys := make([]string, len(ids))
		for i, x := range ids {
			keys[i] = x.ID
		}
		recs, err := s.St.RecordsByID(ctx, keys)
		if err != nil {
			return nil, err
		}
		for i := range recs {
			it, err := rv.Of(ctx, &recs[i])
			if err != nil {
				return nil, err
			}
			out[recs[i].ID] = lca.Summarise(&recs[i], map[string]lca.Intensity{recs[i].ID: it}, sc)
		}
	}
	return out, nil
}

func ff(x float64, d int) string { return strconv.FormatFloat(x, 'f', d, 64) }

var errNoLedger = errors.New("ledger not configured")
