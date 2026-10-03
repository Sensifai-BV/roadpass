package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/sensifai/roadpass/internal/cache"
	"github.com/sensifai/roadpass/internal/domain"
)

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte("ok"))
}

// readyz checks every dependency; the ledger is reported but never fails
// readiness (the platform works while Fabric is down; records wait to be sealed).
func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	out := map[string]any{"version": s.Version, "uptimeSeconds": int(time.Since(s.Started).Seconds())}
	ok := true
	check := func(name string, err error) {
		if err != nil {
			out[name] = err.Error()
			ok = false
			return
		}
		out[name] = "ok"
	}
	check("postgres", s.St.DB.Ping(ctx))
	check("clickhouse", s.CH.C.Ping(ctx))
	check("redis", s.Cache.R.Ping(ctx).Err())
	if s.Keyring != nil {
		check("vault", s.Keyring.Health(ctx))
		out["keyCustody"] = s.Keyring.Custody()
	}
	if _, err := s.Ledger.ChainInfo(); err != nil {
		out["ledger"] = err.Error()
	} else {
		out["ledger"] = "ok"
	}
	if st, err := s.St.SealStatistics(ctx); err == nil {
		out["unsealed"] = st.Pending
	}
	code := http.StatusOK
	if !ok {
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, out)
}

// events streams changes relevant to the viewer (Server-Sent Events). The browser
// refetches what it shows; events carry no data beyond what changed.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	fl, ok := w.(http.Flusher)
	if !ok {
		writeProblem(w, r, http.StatusInternalServerError, "Streaming unsupported", "")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	ch := s.Cache.Subscribe(ctx)
	fmt.Fprint(w, "retry: 5000\n\n")
	fl.Flush()
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		case e, ok := <-ch:
			if !ok {
				return
			}
			if len(e.Users) > 0 && (p.User == nil || !slices.Contains(e.Users, p.User.ID)) {
				continue // someone else's notification
			}
			if len(e.Users) == 0 && !p.Viewer.Oversight && len(e.Orgs) > 0 && !slices.Contains(e.Orgs, p.Org.ID) && e.Kind != "seal" {
				continue
			}
			b, _ := json.Marshal(cache.Event{Kind: e.Kind, Count: e.Count, At: e.At})
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Kind, b)
			fl.Flush()
		}
	}
}

// pushTelemetry accepts plant control-log rows and queues them (202): extruder
// readings and mixer cycles go to ClickHouse through the Redis stream.
func (s *Server) pushTelemetry(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	var body struct {
		Readings []domain.Reading    `json:"readings"`
		Cycles   []domain.MixerCycle `json:"cycles"`
	}
	if !decode(w, r, &body, 4<<20) {
		return
	}
	in := cache.TelemetryMsg{Readings: body.Readings, Cycles: body.Cycles}
	n := len(in.Readings) + len(in.Cycles)
	if n == 0 || n > 5000 {
		writeProblem(w, r, http.StatusUnprocessableEntity, "Batch size", "Send 1–5,000 rows per request.")
		return
	}
	sites, err := s.St.Sites(r.Context(), p.Org.ID)
	if err != nil {
		fail(w, r, err)
		return
	}
	own := map[string]bool{}
	for _, x := range sites {
		own[x.ID] = true
	}
	now := time.Now().Add(time.Hour)
	for i := range in.Readings {
		x := &in.Readings[i]
		if !own[x.SiteID] || x.TS.IsZero() || x.TS.After(now) || x.Line == "" {
			writeProblem(w, r, http.StatusUnprocessableEntity, "Invalid reading", fmt.Sprintf("row %d: unknown site, missing line or bad timestamp", i+1))
			return
		}
		x.OrgID = p.Org.ID
	}
	for i := range in.Cycles {
		x := &in.Cycles[i]
		if !own[x.SiteID] || x.TS.IsZero() || x.TS.After(now) || x.LotID == "" || x.BatchKg <= 0 {
			writeProblem(w, r, http.StatusUnprocessableEntity, "Invalid mixer cycle", fmt.Sprintf("row %d: unknown site, lot or bad values", i+1))
			return
		}
		x.OrgID = p.Org.ID
	}
	if p.Org.Kind != domain.KindOperator && len(in.Cycles) > 0 && !p.Org.Is(domain.KindAsphaltPlant) {
		writeProblem(w, r, http.StatusForbidden, "Mixer cycles come from asphalt plants", "")
		return
	}
	if err := s.Cache.PushTelemetry(r.Context(), in); err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]int{"queued": n})
}
