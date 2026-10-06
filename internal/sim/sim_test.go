package sim

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sensifai/roadpass/internal/domain"
)

type memSink struct {
	recs     []domain.Record
	findings []Finding
	cycles   int
	readings int
}

func (m *memSink) Cycles(_ context.Context, c []MixerCycle) error { m.cycles += len(c); return nil }
func (m *memSink) Readings(_ context.Context, r []Reading) error  { m.readings += len(r); return nil }
func (m *memSink) Records(_ context.Context, r []domain.Record) error {
	m.recs = append(m.recs, r...)
	return nil
}
func (m *memSink) Findings(_ context.Context, f []Finding) error {
	m.findings = append(m.findings, f...)
	return nil
}

func small(defects Defects) Config {
	c := DefaultConfig()
	c.Start = time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	c.End = time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC)
	c.Now = c.End
	c.Scale = 0.3
	c.Telemetry = false
	c.Defects = defects
	return c
}

// Without injected defects the simulated chain must be consistent: no duplicate,
// date, custody, mass, signature or dosage finding. Only C1 (none injected) and
// C6 (natural tails of lab distributions) may appear.
func TestCleanChainHasNoBlockers(t *testing.T) {
	sink := &memSink{}
	st, err := New(small(Defects{}), sink).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + st.Summary())
	if st.Records[domain.TypeDelivery] == 0 || st.Records[domain.TypeAdditiveBatch] == 0 {
		t.Fatalf("chain did not reach the road: %v", st.Records)
	}
	byCode := map[string][]Finding{}
	for _, f := range sink.findings {
		byCode[f.Code] = append(byCode[f.Code], f)
	}
	for _, code := range []string{"C1", "C2", "C3", "C4", "C5", "C7", "C8"} {
		if fs := byCode[code]; len(fs) > 0 {
			t.Errorf("%s fired %d times without injected defects; first: %s %s", code, len(fs), fs[0].RecordID, fs[0].Message)
		}
	}
	// Simulated sections keep area × thickness in line with their tonnage.
	for _, f := range byCode["C6"] {
		if strings.HasPrefix(f.RecordID, "RSC-") {
			t.Errorf("road section mass check fired without defects: %s %s", f.RecordID, f.Message)
			break
		}
	}
}

func TestDeterministicAndDefectsDetected(t *testing.T) {
	a, b := &memSink{}, &memSink{}
	sa, _ := New(small(DefaultDefects), a).Run(context.Background())
	New(small(DefaultDefects), b).Run(context.Background())
	if len(a.recs) != len(b.recs) || a.recs[len(a.recs)-1].ContentHash != b.recs[len(b.recs)-1].ContentHash {
		t.Fatal("same seed produced different data")
	}
	t.Log("\n" + sa.Summary())
	if sa.Findings["C5"] == 0 || sa.Findings["C1"] == 0 {
		t.Fatalf("injected defects not detected: %v", sa.Findings)
	}
}

func TestC6Breakdown(t *testing.T) {
	sink := &memSink{}
	New(small(Defects{}), sink).Run(context.Background())
	byMsg := map[string]int{}
	for _, f := range sink.findings {
		if f.Code == "C6" || f.Code == "C8" {
			m := f.Message
			if len(m) > 28 {
				m = m[:28]
			}
			byMsg[f.Code+" "+m]++
		}
	}
	t.Log(byMsg)
}

// ROADPASS_SIM_FULL=1 go test ./internal/sim -run Full -v: size of the full dataset.
func TestFullScale(t *testing.T) {
	if os.Getenv("ROADPASS_SIM_FULL") == "" {
		t.Skip("set ROADPASS_SIM_FULL=1")
	}
	sink := &memSink{}
	start := time.Now()
	st, err := New(DefaultConfig(), sink).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s in %s\n%s", "full run", time.Since(start).Round(time.Second), st.Summary())
}

// Records are validated against the wall clock, so nothing may be dated well after
// the simulated period, even a release that caught up with an early dispatch. Lab
// results on the last lots may follow up to about eight days later; the default
// run ends two weeks before its Now.
func TestNoRecordAfterTheSimulatedPeriod(t *testing.T) {
	d := DefaultConfig().Defects
	d.EarlyDispatch = 1
	sink := &memSink{}
	c := small(d)
	if _, err := New(c, sink).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	limit := c.End.AddDate(0, 0, 10)
	for _, r := range sink.recs {
		if r.OccurredAt.After(limit) {
			t.Fatalf("%s is dated %s, after the period ends on %s", r.ID, r.OccurredAt, c.End)
		}
	}
}
