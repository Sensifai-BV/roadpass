package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/sensifai/roadpass/internal/alerts"
	"github.com/sensifai/roadpass/internal/analytics"
	"github.com/sensifai/roadpass/internal/auth"
	"github.com/sensifai/roadpass/internal/config"
	"github.com/sensifai/roadpass/internal/domain"
	"github.com/sensifai/roadpass/internal/keyring"
	"github.com/sensifai/roadpass/internal/lca"
	"github.com/sensifai/roadpass/internal/passport"
	"github.com/sensifai/roadpass/internal/signing"
	"github.com/sensifai/roadpass/internal/sim"
	"github.com/sensifai/roadpass/internal/store"
)

func openAll(ctx context.Context, cfg config.Config) (*store.Store, *analytics.DB, error) {
	st, err := store.Open(ctx, cfg.DatabaseURL, 20)
	if err != nil {
		return nil, nil, err
	}
	ch, err := analytics.Open(ctx, cfg.ClickHouseURL)
	if err != nil {
		st.Close()
		return nil, nil, err
	}
	return st, ch, nil
}

func cmdMigrate(ctx context.Context, cfg config.Config, _ []string) error {
	st, ch, err := openAll(ctx, cfg)
	if err != nil {
		return err
	}
	defer st.Close()
	defer ch.Close()
	if err := st.Migrate(ctx); err != nil {
		return err
	}
	return ch.Migrate(ctx)
}

func cmdProject(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("project", flag.ExitOnError)
	rebuild := fs.Bool("rebuild", false, "truncate the ClickHouse fact table and project everything again")
	_ = fs.Parse(args)
	st, ch, err := openAll(ctx, cfg)
	if err != nil {
		return err
	}
	defer st.Close()
	defer ch.Close()
	if *rebuild {
		if err := ch.Truncate(ctx, false); err != nil {
			return err
		}
		if _, err := st.DB.Exec(ctx, `DELETE FROM cursors WHERE name=$1`, analytics.CursorName); err != nil {
			return err
		}
	}
	return project(ctx, st, ch)
}

func project(ctx context.Context, st *store.Store, ch *analytics.DB) error {
	p := analytics.NewProjector(st, ch, passport.NewResolver(st, lca.DefaultScenario))
	start := time.Now()
	last := time.Now()
	err := p.CatchUp(ctx, 5000, func(n int) {
		if time.Since(last) > 5*time.Second {
			slog.Info("projecting", "records", n, "per_s", int(float64(n)/time.Since(start).Seconds()))
			last = time.Now()
		}
	})
	slog.Info("projection done", "took", time.Since(start).Round(time.Second))
	return err
}

// seedSink writes simulation output to Postgres and ClickHouse.
type seedSink struct {
	st       *store.Store
	ch       *analytics.DB
	ingestID string
	records  int
	started  time.Time
	// who attributes simulated work to the demo member holding the duty
	// (organisation → duty → user id), so team views have something to show.
	who map[string]map[string]string
}

// attribute names the simulated submitter and receiver, and dates the
// submission shortly after the event, as an operator would have entered it.
func (s *seedSink) attribute(r *domain.Record) {
	if r.CreatedBy == "" {
		if u := s.who[r.OrgID][string(domain.DutyFor(r.Type))]; u != "" {
			r.CreatedBy = u
		} else {
			r.CreatedBy = s.who[r.OrgID]["admin"]
		}
	}
	if r.Receipt != nil && r.Receipt.ReceivedBy == "" {
		r.Receipt.ReceivedBy = s.who[r.Counterparty]["receive"]
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = r.OccurredAt.Add(time.Duration(10+len(r.ID)%50) * time.Minute)
	}
}

func (s *seedSink) Cycles(ctx context.Context, cs []domain.MixerCycle) error {
	return s.ch.InsertCycles(ctx, cs)
}
func (s *seedSink) Readings(ctx context.Context, rs []domain.Reading) error {
	return s.ch.InsertReadings(ctx, rs)
}
func (s *seedSink) Records(ctx context.Context, rs []domain.Record) error {
	for i := range rs {
		s.attribute(&rs[i])
	}
	if err := s.st.CopyRecords(ctx, rs, s.ingestID); err != nil {
		return err
	}
	s.records += len(rs)
	if s.records%50000 < len(rs) {
		slog.Info("records loaded", "n", s.records, "per_s", int(float64(s.records)/time.Since(s.started).Seconds()))
	}
	return nil
}
func (s *seedSink) Findings(ctx context.Context, fs []sim.Finding) error {
	// One finding per record and rule: a later version's finding replaces the earlier one.
	last := map[string]int{}
	for i, f := range fs {
		last[f.RecordID+"|"+f.Code] = i
	}
	out := make([]store.Finding, 0, len(last))
	for i, f := range fs {
		if last[f.RecordID+"|"+f.Code] != i {
			continue
		}
		out = append(out, store.Finding{RecordID: f.RecordID, Code: f.Code, Severity: f.Severity, Message: f.Message, Fix: f.Fix,
			Detail: f.Detail, Status: f.Status, OrgID: f.OrgID, CreatedAt: f.CreatedAt, ClosedAt: f.ClosedAt,
			ClosedBy: f.ClosedBy, CloseReason: f.Reason})
	}
	for len(out) > 0 {
		n := min(len(out), 20000)
		if err := s.st.CopyFindings(ctx, out[:n]); err != nil {
			return err
		}
		out = out[n:]
	}
	return nil
}

var firstNames = []string{"Lucía", "Javier", "Carmen", "Daniel", "Marta", "Pablo", "Elena", "Sergio", "Laura", "Andrés",
	"Nerea", "Iñaki", "Montserrat", "Jordi", "Ainhoa", "Rubén", "Paula", "Álvaro", "Cristina", "Hugo", "Irene", "Óscar"}
var lastNames = []string{"Martín", "Ruiz", "Navarro", "Iglesias", "Ortega", "Delgado", "Castro", "Romero", "Vidal", "Serrano",
	"Molina", "Blanco", "Etxeberria", "Puig", "Ferrer", "Gil", "Rubio", "Prieto", "Lozano", "Cabrera"}

func cmdSeed(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("seed", flag.ExitOnError)
	reset := fs.Bool("reset", false, "drop all data first")
	scale := fs.Float64("scale", 1, "dataset scale (1 ≈ 2.9M rows)")
	telemetry := fs.Bool("telemetry", true, "generate mixer cycles and extruder readings")
	seed := fs.Uint64("seed", sim.DefaultConfig().Seed, "random seed")
	_ = fs.Parse(args)
	if cfg.Production() && os.Getenv("ROADPASS_ALLOW_SEED") != "1" {
		return errors.New("refusing to seed a production database (set ROADPASS_ALLOW_SEED=1 for a demo deployment)")
	}
	st, ch, err := openAll(ctx, cfg)
	if err != nil {
		return err
	}
	defer st.Close()
	defer ch.Close()
	if *reset {
		slog.Info("resetting databases")
		if err := st.Reset(ctx); err != nil {
			return err
		}
		if err := ch.Truncate(ctx, true); err != nil {
			return err
		}
	}
	if err := st.Migrate(ctx); err != nil {
		return err
	}
	if err := ch.Migrate(ctx); err != nil {
		return err
	}
	var n int64
	if err := st.DB.QueryRow(ctx, `SELECT count(*) FROM orgs`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return errors.New("the database already has organisations; use -reset to start over")
	}
	vault, err := signing.NewVault(cfg.SigningKey)
	if err != nil {
		return err
	}

	sc := sim.DefaultConfig()
	sc.Seed, sc.Scale, sc.Telemetry = *seed, *scale, *telemetry
	sink := &seedSink{st: st, ch: ch, ingestID: "ing_seed", started: time.Now()}
	eng := sim.New(sc, sink)
	world := eng.World()
	keys, err := newKeyring(cfg, st)
	if err != nil {
		return err
	}
	if sink.who, err = seedWorld(ctx, st, vault, keys, world); err != nil {
		return err
	}
	if _, err := st.CreateIngest(ctx, store.Ingest{ID: "ing_seed", OrgID: "org_sensifai", Channel: "seed", Status: "running"}); err != nil {
		return err
	}
	slog.Info("simulating", "from", sc.Start.Format("2006-01-02"), "to", sc.End.Format("2006-01-02"), "scale", sc.Scale)
	stats, err := eng.Run(ctx)
	if err != nil {
		return err
	}
	if err := st.FinalizeLatest(ctx); err != nil {
		return err
	}
	if err := st.SyncCounters(ctx); err != nil {
		return err
	}
	total := 0
	for _, c := range stats.Records {
		total += c
	}
	if err := st.FinishIngest(ctx, store.Ingest{ID: "ing_seed", Status: "done", Total: total, Accepted: total}); err != nil {
		return err
	}
	if _, err := st.DB.Exec(ctx, `ANALYZE`); err != nil {
		return err
	}
	fmt.Print(stats.Summary())
	// Starter alerts last: they only watch what happens after they exist.
	adb := &alerts.DB{P: st.DB}
	for _, o := range world.Orgs {
		if err := adb.InstallStarterRules(ctx, o.ID, o.Kind, func() string { return auth.ID("rul_") }); err != nil {
			return err
		}
	}
	return project(ctx, st, ch)
}

type member struct {
	handle, role string
	duties       []string
}

// teamFor is the demo team of an organisation: plants have one person per duty,
// so the workflow (intake → production → quality → dispatch) can be walked through.
func teamFor(k domain.OrgKind) []member {
	admin := member{"admin", "admin", domain.AllDuties()}
	switch k {
	case domain.KindRecycler, domain.KindAdditiveProducer, domain.KindAsphaltPlant:
		return []member{admin, {"intake", "editor", []string{"receive"}}, {"production", "editor", []string{"produce"}},
			{"quality", "editor", []string{"test", "release"}}, {"dispatch", "editor", []string{"ship"}}, {"viewer", "viewer", nil}}
	case domain.KindSortingPlant:
		return []member{admin, {"production", "editor", []string{"produce", "test"}}, {"dispatch", "editor", []string{"ship"}}}
	case domain.KindContractor:
		return []member{admin, {"site", "editor", []string{"produce", "test"}}, {"viewer", "viewer", nil}}
	case domain.KindRoadOwner, domain.KindAuditor:
		return []member{admin, {"viewer", "viewer", nil}}
	case domain.KindReporter:
		return []member{{"reporter", "viewer", nil}}
	}
	return []member{admin}
}

// seedWorld stores the simulated organisations, sites and demo members. Company
// keys are sealed locally: the simulation signs millions of historical records
// with them in-process. Members' personal keys are issued by the keyring, so with
// a vault configured they live in OpenBao from the start.
func seedWorld(ctx context.Context, st *store.Store, vault *signing.Vault, keys *keyring.Keyring, w sim.World) (map[string]map[string]string, error) {
	who := map[string]map[string]string{}
	pw := os.Getenv("SEED_PASSWORD")
	if pw == "" {
		// No shared password: demo users sign in through the DEMO_MODE picker.
		pw = auth.Token("", 24)
	}
	hash, err := auth.HashPassword(pw)
	if err != nil {
		return nil, err
	}
	for _, o := range w.Orgs {
		if err := st.InsertOrg(ctx, o); err != nil {
			return nil, fmt.Errorf("org %s: %w", o.ID, err)
		}
	}
	for _, s := range w.Sites {
		if err := st.InsertSite(ctx, s); err != nil {
			return nil, fmt.Errorf("site %s: %w", s.ID, err)
		}
	}
	i := 0
	for _, o := range w.Orgs {
		k := w.Keys[o.ID]
		sealed, err := vault.Seal(k)
		if err != nil {
			return nil, err
		}
		if err := st.InsertSigningKey(ctx, store.SigningKey{ID: k.ID, OrgID: o.ID, Public: k.Public, SealedSeed: sealed}); err != nil {
			return nil, err
		}
		slug := strings.TrimPrefix(o.ID, "org_")
		for _, m := range teamFor(o.Kind) {
			name := firstNames[i%len(firstNames)] + " " + lastNames[(i*7)%len(lastNames)]
			i++
			email := fmt.Sprintf("%s@%s.example", m.handle, slug)
			uid := auth.ID("usr_")
			if err := st.InsertUser(ctx, store.User{ID: uid, OrgID: o.ID, Email: email, Name: name, Role: m.role,
				Duties: m.duties, PasswordHash: hash, Demo: true}); err != nil {
				return nil, fmt.Errorf("user %s: %w", email, err)
			}
			if who[o.ID] == nil {
				who[o.ID] = map[string]string{}
			}
			if m.role == "admin" {
				who[o.ID]["admin"] = uid
			}
			for _, d := range m.duties {
				if m.role != "admin" || who[o.ID][d] == "" {
					who[o.ID][d] = uid // the specialist, else the admin
				}
			}
			if _, err := keys.Create(ctx, o.ID, uid); err != nil {
				return nil, fmt.Errorf("personal key for %s: %w", email, err)
			}
		}
	}
	slog.Info("world created", "orgs", len(w.Orgs), "sites", len(w.Sites))
	return who, nil
}
