package api

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/sensifai/roadpass/internal/domain"
	"github.com/sensifai/roadpass/internal/store"
)

func TestDashboardCheck(t *testing.T) {
	ok := dashboardIn{Name: " Intake ", Audience: []string{"duty:receive", "role:viewer", "duty:receive"}, Widgets: json.RawMessage(`[
		{"id":"a","type":"kpi","x":0,"y":0,"w":3,"h":2,"title":"Received","query":{"source":"records","types":["shipment"],"measure":"mass_kg","agg":"sum","compare":true}},
		{"id":"b","type":"timeseries","x":3,"y":0,"w":9,"h":4,"title":"Melt","range":"30d","query":{"source":"extruder_readings","measure":"melt_c","agg":"avg","interval":"day","groupBy":"line"},"options":{"style":"line"}},
		{"id":"c","type":"text","x":0,"y":2,"w":3,"h":2,"title":"Note","options":{"text":"Hello"}}
	]`)}
	w, errs := ok.check()
	if errs != nil {
		t.Fatalf("valid dashboard rejected: %+v", errs)
	}
	if ok.Name != "Intake" || len(ok.Audience) != 2 {
		t.Fatalf("not normalised: %q %v", ok.Name, ok.Audience)
	}
	if !strings.Contains(string(w), `"measure":"melt_c"`) {
		t.Fatalf("widgets lost: %s", w)
	}

	bad := map[string]dashboardIn{
		"empty name":      {Name: " ", Widgets: json.RawMessage(`[]`)},
		"audience":        {Name: "x", Audience: []string{"role:root"}},
		"unknown duty":    {Name: "x", Audience: []string{"duty:delete"}},
		"overflow grid":   {Name: "x", Widgets: json.RawMessage(`[{"id":"a","type":"text","x":10,"y":0,"w":4,"h":2}]`)},
		"duplicate ids":   {Name: "x", Widgets: json.RawMessage(`[{"id":"a","type":"text","x":0,"y":0,"w":1,"h":1},{"id":"a","type":"text","x":1,"y":0,"w":1,"h":1}]`)},
		"unknown type":    {Name: "x", Widgets: json.RawMessage(`[{"id":"a","type":"iframe","x":0,"y":0,"w":1,"h":1}]`)},
		"unknown field":   {Name: "x", Widgets: json.RawMessage(`[{"id":"a","type":"text","x":0,"y":0,"w":1,"h":1,"html":"<script>"}]`)},
		"no data":         {Name: "x", Widgets: json.RawMessage(`[{"id":"a","type":"kpi","x":0,"y":0,"w":1,"h":1}]`)},
		"bad measure":     {Name: "x", Widgets: json.RawMessage(`[{"id":"a","type":"kpi","x":0,"y":0,"w":1,"h":1,"query":{"source":"records","measure":"password"}}]`)},
		"bad range":       {Name: "x", Widgets: json.RawMessage(`[{"id":"a","type":"text","x":0,"y":0,"w":1,"h":1,"range":"forever"}]`)},
		"text with query": {Name: "x", Widgets: json.RawMessage(`[{"id":"a","type":"text","x":0,"y":0,"w":1,"h":1,"query":{"source":"records","measure":"mass_kg"}}]`)},
	}
	for name, in := range bad {
		if _, errs := in.check(); errs == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestTeamTemplateForRole(t *testing.T) {
	kinds := []domain.OrgKind{domain.KindRecycler}
	intake, ok := jobRoleByKey(kinds, "intake")
	if !ok || intake.Access != "editor" || !slices.Equal(intake.Duties, []string{"receive"}) {
		t.Fatalf("intake role: %+v %v", intake, ok)
	}
	if _, ok := jobRoleByKey(kinds, "site"); ok {
		t.Fatal("a recycler does not hand out the contractor's site manager role")
	}
	all := []store.Dashboard{
		{ID: "everyone", Audience: []string{"all"}},
		{ID: "receivers", Audience: []string{"duty:receive"}},
		{ID: "intake", Audience: []string{"jobrole:intake"}},
	}
	if d := teamTemplate(all, intake.Access, intake.Key, intake.Duties); d == nil || d.ID != "intake" {
		t.Fatalf("intake got %v", d)
	}
	q, _ := jobRoleByKey(kinds, "quality")
	if d := teamTemplate(all, q.Access, q.Key, q.Duties); d == nil || d.ID != "everyone" {
		t.Fatalf("quality got %v", d)
	}
	if d := teamTemplate(all[1:2], q.Access, q.Key, q.Duties); d != nil {
		t.Fatalf("quality should fall back to the standard layout, got %v", d.ID)
	}
}
