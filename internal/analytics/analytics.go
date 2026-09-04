// Package analytics is the ClickHouse side of ROADPASS: the record fact table
// (a projection of Postgres), the plant control logs (mixer cycles, extruder
// readings), and every query behind a dashboard chart.
//
// Rules: every query is scoped to the viewer in its WHERE clause with bound
// parameters; column names never come from user input; ClickHouse can be dropped
// and rebuilt from Postgres (`roadpass project --rebuild`), except the control
// logs, which are ingested here directly.
package analytics

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// DB wraps a ClickHouse connection.
type DB struct{ C driver.Conn }

// Open connects with the native protocol. url: clickhouse://user:pass@host:9000/db
func Open(ctx context.Context, url string) (*DB, error) {
	opt, err := clickhouse.ParseDSN(url)
	if err != nil {
		return nil, fmt.Errorf("parse CLICKHOUSE_URL: %w", err)
	}
	opt.MaxOpenConns = 20
	opt.Compression = &clickhouse.Compression{Method: clickhouse.CompressionLZ4}
	opt.Settings = clickhouse.Settings{"max_execution_time": 30}
	c, err := clickhouse.Open(opt)
	if err != nil {
		return nil, err
	}
	if err := c.Ping(ctx); err != nil {
		return nil, fmt.Errorf("clickhouse: %w", err)
	}
	return &DB{C: c}, nil
}

// Close closes the connection.
func (d *DB) Close() error { return d.C.Close() }

var schema = []string{
	// One row per record version; ReplacingMergeTree keeps the highest version per
	// (type, id). Unpartitioned on purpose: a version can change occurred_at, and
	// replacement only happens within a partition.
	`CREATE TABLE IF NOT EXISTS records (
		seq          UInt64,
		id           String,
		version      UInt32,
		type         LowCardinality(String),
		org_id       LowCardinality(String),
		org_kind     LowCardinality(String),
		site_id      LowCardinality(String),
		region       LowCardinality(String),
		counterparty LowCardinality(String),
		parent_id    String,
		section_id   String,
		owner_org    LowCardinality(String),
		occurred_at  DateTime('UTC'),
		mass_kg      Float64,
		recycled_kg  Float64,
		reject_kg    Float64,
		avoided_kg   Float64,
		virgin_kg    Float64,
		footprint_kg Float64,
		chain_kg     Float64,
		tkm          Float64,
		mix          LowCardinality(String),
		process      LowCardinality(String),
		layer        LowCardinality(String),
		road         LowCardinality(String),
		test         LowCardinality(String),
		test_value   Float64,
		test_pass    Int8,
		dosage       Float32,
		temp_c       Float32,
		distance_km  Float32,
		lat          Float32,
		lon          Float32,
		length_km    Float32,
		area_m2      Float64,
		provenance   LowCardinality(String),
		route        LowCardinality(String),
		status       LowCardinality(String) DEFAULT 'ok'
	) ENGINE = ReplacingMergeTree(version)
	ORDER BY (type, id)`,
	// Installations created before the status column existed.
	`ALTER TABLE records ADD COLUMN IF NOT EXISTS status LowCardinality(String) DEFAULT 'ok'`,

	`CREATE TABLE IF NOT EXISTS mixer_cycles (
		ts          DateTime64(3, 'UTC'),
		org_id      LowCardinality(String),
		site_id     LowCardinality(String),
		lot_id      String,
		mix         LowCardinality(String),
		cycle       UInt32,
		batch_kg    Float32,
		additive_kg Float32,
		binder_kg   Float32,
		temp_c      Float32
	) ENGINE = MergeTree PARTITION BY toYYYYMM(ts) ORDER BY (site_id, ts)`,

	`CREATE TABLE IF NOT EXISTS extruder_readings (
		ts              DateTime('UTC'),
		org_id          LowCardinality(String),
		site_id         LowCardinality(String),
		line            LowCardinality(String),
		lot_id          String,
		melt_c          Float32,
		pressure_bar    Float32,
		throughput_kg_h Float32,
		rpm             Float32,
		load_pct        Float32
	) ENGINE = MergeTree PARTITION BY toYYYYMM(ts) ORDER BY (site_id, line, ts)`,

	// Lot-level dosing summary, maintained on insert (cycles are append-only).
	`CREATE TABLE IF NOT EXISTS lot_dosing (
		lot_id      String,
		site_id     LowCardinality(String),
		org_id      LowCardinality(String),
		day         Date,
		cycles      UInt64,
		missed      UInt64,
		batch_kg    Float64,
		additive_kg Float64,
		binder_kg   Float64
	) ENGINE = SummingMergeTree ORDER BY (org_id, site_id, day, lot_id)`,
	`CREATE MATERIALIZED VIEW IF NOT EXISTS lot_dosing_mv TO lot_dosing AS
		SELECT c.lot_id AS lot_id, c.site_id AS site_id, c.org_id AS org_id, toDate(c.ts) AS day, count() AS cycles,
			countIf(c.additive_kg = 0) AS missed, sum(c.batch_kg) AS batch_kg,
			sum(c.additive_kg) AS additive_kg, sum(c.binder_kg) AS binder_kg
		FROM mixer_cycles AS c GROUP BY lot_id, site_id, org_id, day`,

	// Hourly extruder rollup for long time windows.
	`CREATE TABLE IF NOT EXISTS extruder_hourly (
		hour       DateTime('UTC'),
		org_id     LowCardinality(String),
		site_id    LowCardinality(String),
		line       LowCardinality(String),
		samples    UInt64,
		stopped    UInt64,
		suspect    UInt64,
		melt_sum   Float64,
		press_sum  Float64,
		thr_sum    Float64,
		load_sum   Float64
	) ENGINE = SummingMergeTree ORDER BY (org_id, site_id, line, hour)`,
	`CREATE MATERIALIZED VIEW IF NOT EXISTS extruder_hourly_mv TO extruder_hourly AS
		SELECT toStartOfHour(ts) AS hour, org_id, site_id, line, count() AS samples,
			countIf(throughput_kg_h = 0) AS stopped,
			countIf(melt_c > 400 OR melt_c < 50 OR pressure_bar < 10) AS suspect,
			sumIf(melt_c, melt_c BETWEEN 50 AND 400) AS melt_sum,
			sumIf(pressure_bar, pressure_bar >= 10) AS press_sum,
			sum(throughput_kg_h) AS thr_sum, sum(load_pct) AS load_sum
		FROM extruder_readings GROUP BY hour, org_id, site_id, line`,
}

// Migrate creates the tables.
func (d *DB) Migrate(ctx context.Context) error {
	for _, q := range schema {
		if err := d.C.Exec(ctx, q); err != nil {
			return fmt.Errorf("clickhouse schema: %w (%s)", err, strings.Fields(q)[5])
		}
	}
	return nil
}

// Truncate empties every table (rebuild, E2E).
func (d *DB) Truncate(ctx context.Context, includeLogs bool) error {
	tables := []string{"records"}
	if includeLogs {
		tables = append(tables, "mixer_cycles", "extruder_readings", "lot_dosing", "extruder_hourly")
	}
	for _, t := range tables {
		if err := d.C.Exec(ctx, "TRUNCATE TABLE IF EXISTS "+t); err != nil {
			return err
		}
	}
	return nil
}

// DeleteOrg removes everything projected or logged for one organisation. The
// mutations finish before it returns.
func (d *DB) DeleteOrg(ctx context.Context, orgID string) error {
	for _, t := range []string{"records", "mixer_cycles", "extruder_readings", "lot_dosing", "extruder_hourly"} {
		if err := d.C.Exec(ctx, "ALTER TABLE "+t+" DELETE WHERE org_id = ? SETTINGS mutations_sync = 2", orgID); err != nil {
			return fmt.Errorf("clickhouse delete %s: %w", t, err)
		}
	}
	return nil
}

// Scope restricts a query to what a viewer may see.
type Scope struct {
	OrgID string // "" = everything (operator, auditor)
}

// where returns the scope predicate on records and its argument.
func (s Scope) where() (string, []any) {
	if s.OrgID == "" {
		return "1", nil
	}
	return "(org_id = ? OR counterparty = ? OR owner_org = ?)", []any{s.OrgID, s.OrgID, s.OrgID}
}

// orgOnly is the scope predicate for control logs (only the plant's own company).
func (s Scope) orgOnly() (string, []any) {
	if s.OrgID == "" {
		return "1", nil
	}
	return "org_id = ?", []any{s.OrgID}
}

// Range is a time window.
type Range struct{ From, To time.Time }

func (r Range) args() []any { return []any{r.From.UTC(), r.To.UTC()} }
