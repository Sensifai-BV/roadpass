package analytics

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var year = Range{From: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}

func build(t *testing.T, q Query, s Scope) (string, []any) {
	t.Helper()
	q.scope = s
	p, err := q.plan(year)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	return p.sql()
}

func TestExploreRejectsAnythingOutsideTheCatalogue(t *testing.T) {
	bad := []Query{
		{Source: "users", Measure: "mass_kg"},
		{Source: "records", Measure: "mass_kg; DROP TABLE records"},
		{Source: "records", Measure: "region"},                      // a dimension is not a measure
		{Source: "records", Measure: "mass_kg", Agg: "sum) FROM x"}, // unknown aggregation
		{Source: "records", Measure: "mass_kg", GroupBy: "mass_kg"}, // a measure is not a dimension
		{Source: "records", Measure: "mass_kg", GroupBy: "password"},
		{Source: "records", Measure: "mass_kg", Interval: "second"},
		{Source: "records", Measure: "mass_kg", Interval: "hour"}, // 8760 buckets
		{Source: "records", Measure: "mass_kg", Types: []string{"users"}},
		{Source: "mixer_cycles", Measure: "batch_kg", Types: []string{"asphalt_lot"}},
		{Source: "records", Measure: "mass_kg", Filters: []Filter{{Field: "1=1 OR region", Op: "eq", Value: "x"}}},
		{Source: "records", Measure: "mass_kg", Filters: []Filter{{Field: "region", Op: "LIKE", Value: "%"}}},
		{Source: "records", Measure: "mass_kg", Filters: []Filter{{Field: "region", Op: "gt", Value: "a"}}},
		{Source: "records", Measure: "mass_kg", Filters: []Filter{{Field: "mass_kg", Op: "gt", Value: "lots"}}},
		{Source: "records", Measure: "mass_kg", Filters: []Filter{{Field: "region", Op: "in", Value: []any{}}}},
	}
	for _, q := range bad {
		if _, err := q.plan(year); !errors.Is(err, ErrQuery) {
			t.Errorf("%+v: want ErrQuery, got %v", q, err)
		}
	}
}

func TestExploreBindsValuesAndScopes(t *testing.T) {
	evil := "x' OR 1=1 --"
	sql, args := build(t, Query{Source: "records", Measure: "recycled_kg", Agg: "sum", GroupBy: "region",
		Types:   []string{"road_section"},
		Filters: []Filter{{Field: "region", Op: "eq", Value: evil}, {Field: "mass_kg", Op: "gte", Value: 1000.0}},
	}, Scope{OrgID: "org_a"})
	if strings.Contains(sql, evil) || strings.Contains(sql, "org_a") {
		t.Fatalf("values must be bound, not inlined: %s", sql)
	}
	if !strings.Contains(sql, "(org_id = ? OR counterparty = ? OR owner_org = ?)") {
		t.Fatalf("records must carry the viewer scope: %s", sql)
	}
	if want := strings.Count(sql, "?"); want != len(args) {
		t.Fatalf("%d placeholders, %d args", want, len(args))
	}
	if !strings.Contains(sql, "GROUP BY g ORDER BY v DESC LIMIT 50") {
		t.Fatalf("grouped value: %s", sql)
	}
}

func TestExploreControlLogsAreOwnOrgOnly(t *testing.T) {
	sql, args := build(t, Query{Source: "extruder_readings", Measure: "melt_c", Agg: "avg", Interval: "week"}, Scope{OrgID: "org_r"})
	if !strings.Contains(sql, "org_id = ?") || strings.Contains(sql, "counterparty") || args[0] != "org_r" {
		t.Fatalf("control logs are visible to the plant's own company only: %s %v", sql, args)
	}
	if !strings.Contains(sql, "toMonday(ts)") {
		t.Fatalf("bucket: %s", sql)
	}
}

func TestExploreSeriesPerTopGroup(t *testing.T) {
	sql, args := build(t, Query{Source: "records", Measure: "mass_kg", GroupBy: "org_kind", Interval: "month", Limit: 5}, Scope{})
	if !strings.Contains(sql, "IN (SELECT toString(org_kind)") || !strings.Contains(sql, "LIMIT 5)") {
		t.Fatalf("series must be limited to the top groups: %s", sql)
	}
	if strings.Count(sql, "?") != len(args) {
		t.Fatalf("%d placeholders, %d args", strings.Count(sql, "?"), len(args))
	}
}

func TestExploreCountIgnoresAggregation(t *testing.T) {
	q := Query{Source: "records", Measure: "records", Agg: "avg", scope: Scope{}}
	p, err := q.plan(year)
	if err != nil || p.agg != "count" {
		t.Fatalf("a record count is always count(): %v %s", err, p.agg)
	}
}

func TestCatalogueIsWellFormed(t *testing.T) {
	for _, s := range Catalogue {
		seen := map[string]bool{}
		measures := 0
		for _, f := range s.Fields {
			if seen[f.Key] || f.col == "" || f.Label == "" {
				t.Errorf("%s.%s: duplicate or incomplete", s.Key, f.Key)
			}
			seen[f.Key] = true
			if f.Kind == Measure {
				measures++
			}
		}
		if measures == 0 || s.timeCol == "" || s.scoped == nil {
			t.Errorf("%s: incomplete source", s.Key)
		}
	}
}
