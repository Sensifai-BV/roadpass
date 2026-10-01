package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/sensifai/roadpass/internal/analytics"
	"github.com/sensifai/roadpass/internal/cache"
)

// window reads ?from&to (default: the last 12 months).
func window(r *http.Request) analytics.Range {
	q := r.URL.Query()
	to := parseTime(q.Get("to"))
	if to.IsZero() {
		to = time.Now().UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
	}
	from := parseTime(q.Get("from"))
	if from.IsZero() || !from.Before(to) {
		from = to.AddDate(-1, 0, 0)
	}
	if to.Sub(from) > 10*365*24*time.Hour {
		from = to.AddDate(-10, 0, 0)
	}
	return analytics.Range{From: from.UTC(), To: to.UTC()}
}

// scope is the viewer's analytics scope; oversight viewers may look at one
// organisation with ?org=.
func scope(r *http.Request) analytics.Scope {
	p := principal(r.Context())
	if p.Viewer.Oversight {
		return analytics.Scope{OrgID: r.URL.Query().Get("org")}
	}
	return analytics.Scope{OrgID: p.Org.ID}
}

// cached answers from Redis for 60 s; any write invalidates (cache generation).
func cached[T any](s *Server, r *http.Request, name string, f func(context.Context) (T, error)) (T, error) {
	sc := scope(r)
	key := fmt.Sprintf("an:%s:%s:%s", name, sc.OrgID, r.URL.RawQuery)
	return cache.Remember(r.Context(), s.Cache, key, time.Minute, f)
}

func answer[T any](w http.ResponseWriter, r *http.Request, v T, err error) {
	if err != nil {
		fail(w, r, err)
		return
	}
	// The figures depend on who asks: without Vary, a browser that switches accounts
	// serves the previous company's numbers from its cache for up to 30 s.
	w.Header().Set("Cache-Control", "private, max-age=30")
	w.Header().Add("Vary", "Cookie, Authorization")
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) anOverview(w http.ResponseWriter, r *http.Request) {
	v, err := cached(s, r, "overview", func(ctx context.Context) (analytics.Overview, error) {
		return s.CH.Overview(ctx, scope(r), window(r))
	})
	answer(w, r, v, err)
}

type series struct {
	Grain  analytics.Grain   `json:"grain"`
	Points []analytics.Point `json:"points"`
}

func grain(r *http.Request, rg analytics.Range) analytics.Grain {
	switch g := analytics.Grain(r.URL.Query().Get("grain")); g {
	case analytics.Day, analytics.Week, analytics.Month:
		return g
	}
	return analytics.GrainFor(rg)
}

func (s *Server) anMaterial(w http.ResponseWriter, r *http.Request) {
	v, err := cached(s, r, "material", func(ctx context.Context) (series, error) {
		rg := window(r)
		g := grain(r, rg)
		pts, err := s.CH.MaterialFlow(ctx, scope(r), rg, g)
		return series{g, nonNil(pts)}, err
	})
	answer(w, r, v, err)
}

func (s *Server) anImpact(w http.ResponseWriter, r *http.Request) {
	v, err := cached(s, r, "impact", func(ctx context.Context) (series, error) {
		rg := window(r)
		g := grain(r, rg)
		pts, err := s.CH.ImpactSeries(ctx, scope(r), rg, g)
		return series{g, nonNil(pts)}, err
	})
	answer(w, r, v, err)
}

func (s *Server) anFlows(w http.ResponseWriter, r *http.Request) {
	v, err := cached(s, r, "flows", func(ctx context.Context) ([]analytics.FlowLink, error) {
		l, err := s.CH.Flows(ctx, scope(r), window(r))
		return nonNil(l), err
	})
	answer(w, r, v, err)
}

func (s *Server) anFunnel(w http.ResponseWriter, r *http.Request) {
	v, err := cached(s, r, "funnel", func(ctx context.Context) ([]analytics.FunnelStep, error) {
		l, err := s.CH.Funnel(ctx, scope(r), window(r))
		return nonNil(l), err
	})
	answer(w, r, v, err)
}

type mapData struct {
	Sites    []analytics.SiteVolume   `json:"sites"`
	Sections []analytics.SectionPoint `json:"sections"`
}

func (s *Server) anMap(w http.ResponseWriter, r *http.Request) {
	v, err := cached(s, r, "map", func(ctx context.Context) (mapData, error) {
		rg := window(r)
		sites, err := s.CH.SiteVolumes(ctx, scope(r), rg)
		if err != nil {
			return mapData{}, err
		}
		sections, err := s.CH.Sections(ctx, scope(r), rg, 3000)
		return mapData{nonNil(sites), nonNil(sections)}, err
	})
	answer(w, r, v, err)
}

type breakdowns struct {
	Regions []analytics.Breakdown `json:"regions"`
	Mixes   []analytics.Breakdown `json:"mixes"`
}

func (s *Server) anBreakdown(w http.ResponseWriter, r *http.Request) {
	v, err := cached(s, r, "breakdown", func(ctx context.Context) (breakdowns, error) {
		rg := window(r)
		reg, err := s.CH.ByRegion(ctx, scope(r), rg)
		if err != nil {
			return breakdowns{}, err
		}
		mix, err := s.CH.ByMix(ctx, scope(r), rg)
		return breakdowns{nonNil(reg), nonNil(mix)}, err
	})
	answer(w, r, v, err)
}

type labData struct {
	Stats  []analytics.TestStats `json:"stats"`
	Series series                `json:"series"`
}

func (s *Server) anLab(w http.ResponseWriter, r *http.Request) {
	v, err := cached(s, r, "lab", func(ctx context.Context) (labData, error) {
		rg := window(r)
		st, err := s.CH.LabStats(ctx, scope(r), rg)
		if err != nil {
			return labData{}, err
		}
		g := grain(r, rg)
		pts, err := s.CH.LabSeries(ctx, scope(r), rg, g)
		return labData{nonNil(st), series{g, nonNil(pts)}}, err
	})
	answer(w, r, v, err)
}

func (s *Server) anDosing(w http.ResponseWriter, r *http.Request) {
	v, err := cached(s, r, "dosing", func(ctx context.Context) ([]analytics.PlantDosing, error) {
		d, err := s.CH.Dosing(ctx, scope(r), window(r))
		return nonNil(d), err
	})
	answer(w, r, v, err)
}

func (s *Server) anLotCycles(w http.ResponseWriter, r *http.Request) {
	v, err := s.CH.LotCycles(r.Context(), scope(r), r.PathValue("id"))
	answer(w, r, nonNil(v), err)
}

type linesData struct {
	Lines    []analytics.LineHealth `json:"lines"`
	Suspects []analytics.Suspect    `json:"suspects"`
}

func (s *Server) anLines(w http.ResponseWriter, r *http.Request) {
	v, err := cached(s, r, "lines", func(ctx context.Context) (linesData, error) {
		rg := window(r)
		l, err := s.CH.Lines(ctx, scope(r), rg)
		if err != nil {
			return linesData{}, err
		}
		sus, err := s.CH.Suspects(ctx, scope(r), rg, 100)
		return linesData{nonNil(l), nonNil(sus)}, err
	})
	answer(w, r, v, err)
}

func (s *Server) anExtruder(w http.ResponseWriter, r *http.Request) {
	site := r.URL.Query().Get("site")
	if site == "" {
		writeProblem(w, r, http.StatusBadRequest, "site required", "")
		return
	}
	rg := window(r)
	if days, _ := strconv.Atoi(r.URL.Query().Get("days")); days > 0 && days <= 120 {
		rg.From = rg.To.AddDate(0, 0, -days)
	}
	v, err := s.CH.Extruder(r.Context(), scope(r), site, rg)
	answer(w, r, nonNil(v), err)
}
