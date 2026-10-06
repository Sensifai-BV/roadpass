// Package demo builds and resets the public demo workspace: one organisation
// (Capital Paving) that plays every step of the chain, with a team of named job
// roles sharing one password and a complete, fresh set of data. It is meant for
// booking demos for B2B customers: it is created when the server starts and
// rebuilt every day, so whatever visitors enter is gone the next morning.
//
// The data is made through the product's own API (in-process), so every record is
// validated, checked, signed by a member's key and sealed like a customer's. The
// quantities of the three Route 740 sections come from the public MoDOT report and
// carry the PublicReference label; everything else is simulated and labelled so.
package demo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/sensifai/roadpass/internal/alerts"
	"github.com/sensifai/roadpass/internal/analytics"
	"github.com/sensifai/roadpass/internal/auth"
	"github.com/sensifai/roadpass/internal/cache"
	"github.com/sensifai/roadpass/internal/config"
	"github.com/sensifai/roadpass/internal/domain"
	"github.com/sensifai/roadpass/internal/keyring"
	"github.com/sensifai/roadpass/internal/store"
)

// OrgID is the demo organisation's id (fixed, so links and sessions survive rebuilds).
const OrgID = "org_capital-paving-demo"

// Domain is the e-mail domain of the demo users.
const Domain = "capital-demo.com"

const lockKey = 7_424_001 // pg advisory lock: one builder at a time across replicas

// Manager keeps the demo workspace present and fresh.
type Manager struct {
	Cfg     config.Config
	St      *store.Store
	CH      *analytics.DB
	Cache   *cache.Cache
	Keys    *keyring.Keyring
	Alerts  *alerts.DB
	Handler http.Handler // the API, called in-process
}

// Member is one demo user.
type Member struct {
	Key    string // short name used by the scenario
	Name   string
	Email  string
	Role   string // admin | editor | viewer
	Title  string // the job role as people read it
	Duties []string
	Steps  []string
}

// Team is the demo team. Admin is admin@capital-demo.com; the others are
// <first name>.<role>@capital-demo.com.
var Team = []Member{
	{"admin", "Alex Carter", "admin@" + Domain, "admin", "Administrator", domain.AllDuties(), nil},
	{"intake", "Dana Reyes", "dana.intake@" + Domain, "editor", "Intake officer", []string{"receive"}, nil},
	{"sorter", "Jordan Blake", "jordan.sorting@" + Domain, "editor", "Sorting operator", []string{"produce"}, []string{"sorting_plant"}},
	{"recycler", "Riley Chen", "riley.recycling@" + Domain, "editor", "Recycling operator", []string{"produce"}, []string{"recycler"}},
	{"compounder", "Casey Morgan", "casey.additive@" + Domain, "editor", "Compounding operator", []string{"produce"}, []string{"additive_producer"}},
	{"mixer", "Taylor Brooks", "taylor.mixing@" + Domain, "editor", "Mixing operator", []string{"produce"}, []string{"asphalt_plant"}},
	{"site", "Sam Ortiz", "sam.paving@" + Domain, "editor", "Site manager", []string{"produce", "test"}, []string{"contractor", "road_owner"}},
	{"lab", "Quinn Patel", "quinn.lab@" + Domain, "editor", "Lab technician", []string{"test"}, nil},
	{"quality", "Avery Stone", "avery.quality@" + Domain, "editor", "Quality manager", []string{"test", "release"}, nil},
	{"dispatch", "Drew Hayes", "drew.dispatch@" + Domain, "editor", "Dispatcher", []string{"ship"}, nil},
	{"assets", "Jamie Lin", "jamie.assets@" + Domain, "editor", "Road asset manager", []string{"release"}, []string{"road_owner"}},
	{"reporter", "Robin Vega", "robin.reporter@" + Domain, "viewer", "Reporter", nil, nil},
}

// Run keeps the workspace fresh until ctx ends: it builds it when it is missing
// or older than the last daily reset, then waits for the next one.
func (m *Manager) Run(ctx context.Context) {
	for ctx.Err() == nil {
		wait := m.tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// lastReset is the most recent daily reset time not after now.
func (m *Manager) lastReset(now time.Time) time.Time {
	t := time.Date(now.Year(), now.Month(), now.Day(), m.Cfg.DemoResetHour, 0, 0, 0, time.UTC)
	if t.After(now) {
		t = t.AddDate(0, 0, -1)
	}
	return t
}

func (m *Manager) tick(ctx context.Context) time.Duration {
	conn, err := m.St.DB.Acquire(ctx)
	if err != nil {
		slog.Warn("demo: no database connection", "err", err)
		return time.Minute
	}
	defer conn.Release()
	var got bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, lockKey).Scan(&got); err != nil || !got {
		return time.Minute // another replica builds
	}
	defer conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, lockKey) //nolint:errcheck // released with the connection anyway
	now := time.Now().UTC()
	built, err := m.builtAt(ctx)
	if err != nil || built.Before(m.lastReset(now)) {
		if err := m.Rebuild(ctx); err != nil {
			slog.Error("demo: rebuild failed; retrying", "err", err)
			return 5 * time.Minute
		}
	}
	next := m.lastReset(time.Now().UTC()).AddDate(0, 0, 1)
	return min(time.Until(next)+time.Second, 15*time.Minute)
}

// builtAt is when the workspace was last completely built.
func (m *Manager) builtAt(ctx context.Context) (time.Time, error) {
	var at *time.Time
	err := m.St.DB.QueryRow(ctx, `SELECT max(at) FROM audit_log WHERE org_id=$1 AND action='demo.built'`, OrgID).Scan(&at)
	if err != nil {
		return time.Time{}, err
	}
	if at == nil {
		return time.Time{}, errors.New("not built")
	}
	return *at, nil
}

// Rebuild wipes the workspace's data and builds it again.
func (m *Manager) Rebuild(ctx context.Context) error {
	start := time.Now()
	slog.Info("demo: rebuilding the demo workspace")
	if err := m.ensureOrg(ctx); err != nil {
		return fmt.Errorf("organisation: %w", err)
	}
	if err := m.St.WipeOrgData(ctx, OrgID); err != nil {
		return err
	}
	if m.CH != nil {
		if err := m.CH.DeleteOrg(ctx, OrgID); err != nil {
			return err
		}
	}
	_ = m.Cache.Invalidate(ctx)
	if err := m.resetOrg(ctx); err != nil {
		return fmt.Errorf("profile: %w", err)
	}
	if err := m.populate(ctx); err != nil {
		return fmt.Errorf("data: %w", err)
	}
	_ = m.Cache.Invalidate(ctx)
	if err := m.St.Audit(ctx, OrgID, "system", "demo.built", OrgID, map[string]any{"tookMs": time.Since(start).Milliseconds()}); err != nil {
		return err
	}
	slog.Info("demo: workspace ready", "took", time.Since(start).Round(time.Second))
	return nil
}

// ensureOrg creates the organisation, its subscription and its members once.
func (m *Manager) ensureOrg(ctx context.Context) error {
	hash, err := auth.HashPassword(m.Cfg.DemoPassword)
	if err != nil {
		return err
	}
	if _, err := m.St.Org(ctx, OrgID); errors.Is(err, store.ErrNotFound) {
		now := time.Now().UTC()
		o := baseOrg()
		o.VerifiedAt, o.Status = &now, domain.OrgVerified
		if err := m.St.InsertOrg(ctx, o); err != nil {
			return err
		}
		if err := m.Keys.EnsureOrgKey(ctx, OrgID); err != nil {
			return err
		}
		if err := m.St.SetSubscription(ctx, store.Subscription{OrgID: OrgID, PlanID: "business", Status: "active"}, "demo"); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	for _, mem := range Team {
		u, err := m.St.UserByEmail(ctx, mem.Email)
		if errors.Is(err, store.ErrNotFound) {
			uid := auth.ID("usr_")
			if err := m.St.InsertUser(ctx, store.User{ID: uid, OrgID: OrgID, Email: mem.Email, Name: mem.Name, Role: mem.Role,
				Duties: mem.Duties, Steps: mem.Steps, PasswordHash: hash}); err != nil {
				return fmt.Errorf("user %s: %w", mem.Email, err)
			}
			if _, err := m.Keys.Create(ctx, OrgID, uid); err != nil {
				return fmt.Errorf("personal key of %s: %w", mem.Email, err)
			}
			continue
		}
		if err != nil {
			return err
		}
		// Back to the canonical team: visitors may have renamed themselves.
		if mem.Duties == nil {
			mem.Duties = []string{}
		}
		if mem.Steps == nil {
			mem.Steps = []string{}
		}
		if _, err := m.St.DB.Exec(ctx, `UPDATE users SET name=$2, role=$3, duties=$4, steps=$5, password_hash=$6, disabled_at=NULL, timezone='UTC' WHERE id=$1`,
			u.ID, mem.Name, mem.Role, mem.Duties, mem.Steps, hash); err != nil {
			return err
		}
	}
	return nil
}

func baseOrg() domain.Org {
	return domain.Org{ID: OrgID, Name: "Capital Paving (public-data demo)", Kind: domain.KindAsphaltPlant,
		Kinds: []domain.OrgKind{domain.KindSortingPlant, domain.KindRecycler, domain.KindAdditiveProducer, domain.KindAsphaltPlant,
			domain.KindContractor, domain.KindRoadOwner, domain.KindLab},
		City: "Columbia", Province: "Boone County", Region: "Missouri", Country: "US", Lat: 38.9517, Lon: -92.3341,
		Description: "Demo workspace. It plays every step of the chain for the MoDOT Route 740 recycled-polyethylene trial (Columbia, Missouri): " +
			"the section quantities, dosage and asphalt-binder content come from the public MoDOT final report; all other values are simulated. " +
			"Not operated by, or affiliated with, Capital Paving, Avangard Innovative or MoDOT.",
		Website: "", Provenance: domain.ProvSimulated, Demo: true}
}

// resetOrg restores the profile a visitor may have seen change, and the sites.
func (m *Manager) resetOrg(ctx context.Context) error {
	o := baseOrg()
	kinds := make([]string, len(o.Kinds))
	for i, k := range o.Kinds {
		kinds[i] = string(k)
	}
	if _, err := m.St.DB.Exec(ctx, `UPDATE orgs SET name=$2, kind=$3, kinds=$4, city=$5, province=$6, region=$7, country=$8, lat=$9, lon=$10,
		description=$11, provenance=$12, demo=true, status='verified', legal_name=$13, address=$14, contact_email=$15, phone=$16, report_footer=$17,
		website='' WHERE id=$1`,
		OrgID, o.Name, string(o.Kind), kinds, o.City, o.Province, o.Region, o.Country, o.Lat, o.Lon, o.Description, o.Provenance,
		"Capital Paving (public-data demo)", "Columbia, Missouri, USA", "demo@"+Domain, "",
		"Demonstration workspace: Route 740 quantities are public MoDOT figures; all other data is simulated."); err != nil {
		return err
	}
	for _, s := range sites() {
		if err := m.St.InsertSite(ctx, s); err != nil {
			return fmt.Errorf("site %s: %w", s.ID, err)
		}
	}
	return m.Alerts.InstallStarterRules(ctx, OrgID, domain.KindAsphaltPlant, func() string { return auth.ID("rul_") })
}

func sites() []domain.Site {
	mk := func(slug, name, kind string, lat, lon, cap float64, eq string) domain.Site {
		return domain.Site{ID: "site_capdemo_" + slug, OrgID: OrgID, Name: name, Kind: kind, City: "Columbia", Province: "Boone County",
			Region: "Missouri", Country: "US", Lat: lat, Lon: lon, CapacityT: cap, Equipment: eq}
	}
	return []domain.Site{
		mk("sorting", "Sorting yard Columbia", "sorting_plant", 38.9500, -92.3300, 9000, "Baler, ballistic separator, optical sorter"),
		mk("recycler", "Pellet recycling Columbia", "recycler", 38.9600, -92.3000, 6000, "Washing line, 900 kg/h extruder with melt filtration"),
		mk("additive", "Additive handling Columbia", "additive_producer", 38.9700, -92.2800, 4000, "Pellet silo and metering unit"),
		mk("asphalt", "Asphalt plant Columbia", "asphalt_plant", 38.9300, -92.2500, 120000, "200 t/h batch plant, 3 t mixer, additive doser"),
		mk("paving", "Paving crew base Columbia", "contractor", 38.9400, -92.3200, 0, "Pavers, rollers, haul trucks"),
		mk("assets", "Route 740 asset office", "road_owner", 38.9386, -92.3290, 0, ""),
		mk("lab", "Materials laboratory Columbia", "lab", 38.9410, -92.3100, 0, "Marshall, wheel tracking, extraction and DSC equipment"),
		mk("yard", "Pellet storage yard", "warehouse", 38.9350, -92.2600, 800, "Covered bays"),
	}
}
