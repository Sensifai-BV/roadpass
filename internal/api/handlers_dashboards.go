package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/sensifai/roadpass/internal/analytics"
	"github.com/sensifai/roadpass/internal/auth"
	"github.com/sensifai/roadpass/internal/cache"
	"github.com/sensifai/roadpass/internal/domain"
	"github.com/sensifai/roadpass/internal/store"
)

// Widget types a dashboard may hold: data widgets driven by a query, and built-in
// views (the chain journey, the site map, data health, recent activity, text).
var widgetTypes = []string{"kpi", "timeseries", "bars", "donut", "gauge", "table", "map", "text", "journey", "sites_map", "data_health", "activity"}

// builtin widgets show their own data and take no query.
var builtinWidgets = []string{"text", "journey", "sites_map", "data_health", "activity"}

const (
	gridCols   = 12
	maxWidgets = 48
)

// widget is the validated shape of one dashboard tile.
type widget struct {
	ID      string           `json:"id"`
	Type    string           `json:"type"`
	X       int              `json:"x"`
	Y       int              `json:"y"`
	W       int              `json:"w"`
	H       int              `json:"h"`
	Title   string           `json:"title"`
	Query   *analytics.Query `json:"query,omitempty"`
	Range   string           `json:"range,omitempty"` // "" = the page's window; 7d, 30d, 90d, 12m, all
	Options json.RawMessage  `json:"options,omitempty"`
}

type dashboardIn struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Audience    []string        `json:"audience"`
	Position    *int            `json:"position"`
	Widgets     json.RawMessage `json:"widgets"`
	Version     int             `json:"version"`
}

type fieldErr struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// check validates a dashboard and returns its normalised widget document.
func (in *dashboardIn) check() (json.RawMessage, []fieldErr) {
	var errs []fieldErr
	add := func(f, format string, a ...any) { errs = append(errs, fieldErr{f, fmt.Sprintf(format, a...)}) }
	in.Name = strings.TrimSpace(in.Name)
	if l := len([]rune(in.Name)); l < 1 || l > 80 {
		add("name", "Give the dashboard a name of up to 80 characters.")
	}
	if len([]rune(in.Description)) > 280 {
		add("description", "Keep the description under 280 characters.")
	}
	for _, a := range in.Audience {
		role, isRole := strings.CutPrefix(a, "role:")
		duty, isDuty := strings.CutPrefix(a, "duty:")
		job, isJob := strings.CutPrefix(a, "jobrole:")
		ok := a == "all" || (isRole && roleRank[role] > 0) || (isDuty && slices.Contains(domain.AllDuties(), duty)) ||
			(isJob && job != "" && len(job) <= 64)
		if !ok {
			add("audience", "Unknown audience %q.", a)
		}
	}
	slices.Sort(in.Audience)
	in.Audience = slices.Compact(in.Audience)

	var ws []widget
	if len(in.Widgets) == 0 {
		in.Widgets = json.RawMessage("[]")
	}
	dec := json.NewDecoder(bytes.NewReader(in.Widgets))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&ws); err != nil {
		add("widgets", "The widget layout is not valid: %v", err)
		return nil, errs
	}
	if len(ws) > maxWidgets {
		add("widgets", "A dashboard holds at most %d widgets.", maxWidgets)
	}
	ids := map[string]bool{}
	for i, x := range ws {
		f := fmt.Sprintf("widgets[%d]", i)
		if x.ID == "" || len(x.ID) > 40 || ids[x.ID] {
			add(f, "Each widget needs a unique id.")
		}
		ids[x.ID] = true
		if !slices.Contains(widgetTypes, x.Type) {
			add(f, "Unknown widget type %q.", x.Type)
		}
		if x.X < 0 || x.W < 1 || x.X+x.W > gridCols || x.Y < 0 || x.Y > 500 || x.H < 1 || x.H > 24 {
			add(f, "Widget %q does not fit the %d-column grid.", x.Title, gridCols)
		}
		if len([]rune(x.Title)) > 120 {
			add(f, "Keep widget titles under 120 characters.")
		}
		if len(x.Options) > 4096 {
			add(f, "Widget options are too large.")
		}
		if !slices.Contains([]string{"", "7d", "30d", "90d", "12m", "all"}, x.Range) {
			add(f, "Unknown time range %q.", x.Range)
		}
		switch {
		case slices.Contains(builtinWidgets, x.Type):
			if x.Query != nil {
				add(f, "This widget shows its own data; it takes no query.")
			}
		case x.Query == nil:
			add(f, "Widget %q needs data: pick a source and a measure.", x.Title)
		default:
			if err := x.Query.Validate(); err != nil {
				add(f, "%s", strings.TrimPrefix(err.Error(), "query: "))
			}
		}
	}
	if len(errs) > 0 {
		return nil, errs
	}
	out, _ := json.Marshal(ws)
	return out, nil
}

// dashboards lists the caller's dashboards: every one for admins, otherwise
// those whose audience includes the caller's role or one of their duties.
func (s *Server) dashboards(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	all, err := s.St.Dashboards(r.Context(), p.Org.ID)
	if err != nil {
		fail(w, r, err)
		return
	}
	if !p.can("admin") {
		all = slices.DeleteFunc(all, func(d store.Dashboard) bool { return !d.Sees(p.Role(), p.Duties()) })
	}
	writeJSON(w, http.StatusOK, all)
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	d, err := s.St.Dashboard(r.Context(), p.Org.ID, r.PathValue("id"))
	if err == nil && !p.can("admin") && !d.Sees(p.Role(), p.Duties()) {
		err = store.ErrNotFound
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) createDashboard(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	var in dashboardIn
	if !decode(w, r, &in, 512<<10) {
		return
	}
	widgets, errs := in.check()
	if errs != nil {
		writeProblemErrs(w, r, http.StatusUnprocessableEntity, "Dashboard not saved", "Fix the highlighted fields.", errs)
		return
	}
	d, err := s.St.CreateDashboard(r.Context(), store.Dashboard{ID: auth.ID("dsh_"), OrgID: p.Org.ID, Name: in.Name,
		Description: in.Description, Audience: in.Audience, Widgets: widgets, CreatedBy: p.Actor()})
	if err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "dashboard.create", d.ID, map[string]any{"name": d.Name})
	writeJSON(w, http.StatusCreated, d)
}

func (s *Server) updateDashboard(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	var in dashboardIn
	if !decode(w, r, &in, 512<<10) {
		return
	}
	widgets, errs := in.check()
	if errs != nil {
		writeProblemErrs(w, r, http.StatusUnprocessableEntity, "Dashboard not saved", "Fix the highlighted fields.", errs)
		return
	}
	cur, err := s.St.Dashboard(r.Context(), p.Org.ID, r.PathValue("id"))
	if err != nil {
		fail(w, r, err)
		return
	}
	pos := cur.Position
	if in.Position != nil {
		pos = *in.Position
	}
	d, err := s.St.UpdateDashboard(r.Context(), store.Dashboard{ID: cur.ID, OrgID: p.Org.ID, Name: in.Name, Description: in.Description,
		Audience: in.Audience, Position: pos, Widgets: widgets, UpdatedBy: p.Actor()}, in.Version)
	if errors.Is(err, store.ErrConflict) {
		writeProblem(w, r, http.StatusConflict, "Changed by someone else", "Another admin saved this dashboard after you opened it. Reload to see their version.")
		return
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "dashboard.update", d.ID, map[string]any{"name": d.Name, "version": d.Version})
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) deleteDashboard(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	if err := s.St.DeleteDashboard(r.Context(), p.Org.ID, r.PathValue("id")); err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "dashboard.delete", r.PathValue("id"), nil)
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------- personal layouts

const layoutPage = "overview"

// myLayout returns what the caller's Overview shows: their own layout, else
// their team's default (the first dashboard published to their role or duty),
// else nothing (the built-in layout). Admins pass ?as=<job role key> to see the
// Overview a role starts from (never anyone's personal layout).
func (s *Server) myLayout(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	kinds := append([]domain.OrgKind{p.Org.Kind}, p.Org.Kinds...)
	access, jobRole, duties := p.Role(), "", p.Duties()
	if as := r.URL.Query().Get("as"); as != "" {
		if p.Role() != "admin" {
			writeProblem(w, r, http.StatusForbidden, "Admins only", "Only administrators can see the Overview of another role.")
			return
		}
		role, ok := jobRoleByKey(kinds, as)
		if !ok {
			writeProblem(w, r, http.StatusNotFound, "No such role", "Your organisation does not hand out this role.")
			return
		}
		access, jobRole, duties = role.Access, role.Key, role.Duties
	} else {
		if l, err := s.St.UserLayout(r.Context(), p.User.ID, layoutPage); err == nil {
			writeJSON(w, http.StatusOK, map[string]any{"source": "personal", "widgets": l.Widgets, "version": l.Version, "updatedAt": l.UpdatedAt})
			return
		} else if !errors.Is(err, store.ErrNotFound) {
			fail(w, r, err)
			return
		}
		jobRole = domain.RoleKeyFor(kinds, access, duties)
	}
	all, err := s.St.Dashboards(r.Context(), p.Org.ID)
	if err != nil {
		fail(w, r, err)
		return
	}
	if pick := teamTemplate(all, access, jobRole, duties); pick != nil {
		writeJSON(w, http.StatusOK, map[string]any{"source": "team", "widgets": pick.Widgets, "version": 0, "template": map[string]string{"id": pick.ID, "name": pick.Name}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"source": "default", "widgets": nil, "version": 0})
}

// teamTemplate is the template aimed most precisely at a member: their job
// role, then their access or duties, then everyone (nil when none applies).
func teamTemplate(all []store.Dashboard, access, jobRole string, duties []string) *store.Dashboard {
	var pick *store.Dashboard
	bestN := 0
	for i := range all {
		if n := all[i].Match(access, jobRole, duties); n > bestN {
			pick, bestN = &all[i], n
		}
	}
	return pick
}

// jobRoleByKey finds a role the organisation hands out.
func jobRoleByKey(kinds []domain.OrgKind, key string) (domain.JobRole, bool) {
	for _, r := range domain.JobRolesFor(kinds) {
		if r.Key == key {
			return r, true
		}
	}
	return domain.JobRole{}, false
}

func (s *Server) saveMyLayout(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	in := dashboardIn{Name: "overview"}
	if !decode(w, r, &in, 512<<10) {
		return
	}
	widgets, errs := in.check()
	if errs != nil {
		writeProblemErrs(w, r, http.StatusUnprocessableEntity, "Layout not saved", "Fix the highlighted widgets.", errs)
		return
	}
	l, err := s.St.SaveUserLayout(r.Context(), p.User.ID, layoutPage, widgets, in.Version)
	if errors.Is(err, store.ErrConflict) {
		writeProblem(w, r, http.StatusConflict, "Saved elsewhere", "You changed this layout in another tab or on another device. Reload to continue from there.")
		return
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"source": "personal", "widgets": l.Widgets, "version": l.Version, "updatedAt": l.UpdatedAt})
}

func (s *Server) resetMyLayout(w http.ResponseWriter, r *http.Request) {
	if err := s.St.ResetUserLayout(r.Context(), principal(r.Context()).User.ID, layoutPage); err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// publishLayout makes a layout the default Overview of a role or duty (admins).
// Members who have not customised their own see it.
func (s *Server) publishLayout(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	var in dashboardIn
	if !decode(w, r, &in, 512<<10) {
		return
	}
	if len(in.Audience) == 0 {
		writeProblem(w, r, http.StatusUnprocessableEntity, "Choose who gets this layout", "Pick at least one role or duty.")
		return
	}
	if in.Name == "" {
		in.Name = "Team overview"
	}
	widgets, errs := in.check()
	if errs != nil {
		writeProblemErrs(w, r, http.StatusUnprocessableEntity, "Layout not published", "Fix the highlighted widgets.", errs)
		return
	}
	// One template per audience: replace an existing one with the same audience.
	all, err := s.St.Dashboards(r.Context(), p.Org.ID)
	if err != nil {
		fail(w, r, err)
		return
	}
	for _, d := range all {
		if slices.Equal(d.Audience, in.Audience) {
			out, err := s.St.UpdateDashboard(r.Context(), store.Dashboard{ID: d.ID, OrgID: p.Org.ID, Name: in.Name, Description: in.Description,
				Audience: in.Audience, Position: d.Position, Widgets: widgets, UpdatedBy: p.Actor()}, d.Version)
			if err != nil {
				fail(w, r, err)
				return
			}
			_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "layout.publish", out.ID, map[string]any{"audience": in.Audience})
			writeJSON(w, http.StatusOK, out)
			return
		}
	}
	d, err := s.St.CreateDashboard(r.Context(), store.Dashboard{ID: auth.ID("dsh_"), OrgID: p.Org.ID, Name: in.Name, Description: in.Description,
		Audience: in.Audience, Widgets: widgets, CreatedBy: p.Actor()})
	if err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "layout.publish", d.ID, map[string]any{"audience": in.Audience})
	writeJSON(w, http.StatusCreated, d)
}

// exploreCatalog lists the data sources and fields widgets can use.
func (s *Server) exploreCatalog(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, max-age=300")
	writeJSON(w, http.StatusOK, map[string]any{"sources": analytics.Catalogue, "widgetTypes": widgetTypes})
}

// exploreQuery runs one widget's query in the caller's scope.
func (s *Server) exploreQuery(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Query analytics.Query `json:"query"`
		From  time.Time       `json:"from"`
		To    time.Time       `json:"to"`
	}
	if !decode(w, r, &in, 16<<10) {
		return
	}
	rg := analytics.Range{From: in.From.UTC(), To: in.To.UTC()}
	if rg.To.IsZero() {
		rg.To = time.Now().UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
	}
	if rg.From.IsZero() || !rg.From.Before(rg.To) {
		rg.From = rg.To.AddDate(-1, 0, 0)
	}
	if rg.To.Sub(rg.From) > 10*365*24*time.Hour {
		rg.From = rg.To.AddDate(-10, 0, 0)
	}
	sc := scope(r)
	body, _ := json.Marshal(in.Query)
	sum := sha256.Sum256(fmt.Appendf(body, "|%d|%d", rg.From.Unix(), rg.To.Unix()))
	key := "ex:" + sc.OrgID + ":" + hex.EncodeToString(sum[:12])
	res, err := cache.Remember(r.Context(), s.Cache, key, time.Minute, func(ctx context.Context) (analytics.Result, error) {
		return s.CH.Explore(ctx, sc, rg, in.Query)
	})
	if errors.Is(err, analytics.ErrQuery) {
		writeProblem(w, r, http.StatusUnprocessableEntity, "Widget query not valid", strings.TrimPrefix(err.Error(), "query: "))
		return
	}
	answer(w, r, res, err)
}
