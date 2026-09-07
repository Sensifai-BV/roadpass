package analytics

import (
	"context"

	"github.com/sensifai/roadpass/internal/domain"
)

// InsertCycles appends mixer cycles.
func (d *DB) InsertCycles(ctx context.Context, cs []domain.MixerCycle) error {
	b, err := d.C.PrepareBatch(ctx, "INSERT INTO mixer_cycles")
	if err != nil {
		return err
	}
	for _, c := range cs {
		if err := b.Append(c.TS, c.OrgID, c.SiteID, c.LotID, c.Mix, c.Cycle, c.BatchKg, c.AdditiveKg, c.BinderKg, c.TC); err != nil {
			_ = b.Abort()
			return err
		}
	}
	return b.Send()
}

// InsertReadings appends extruder readings.
func (d *DB) InsertReadings(ctx context.Context, rs []domain.Reading) error {
	b, err := d.C.PrepareBatch(ctx, "INSERT INTO extruder_readings")
	if err != nil {
		return err
	}
	for _, r := range rs {
		if err := b.Append(r.TS, r.OrgID, r.SiteID, r.Line, r.LotID, r.MeltC, r.PressureBar, r.ThroughputKgH, r.RPM, r.LoadPct); err != nil {
			_ = b.Abort()
			return err
		}
	}
	return b.Send()
}
