package demo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/sensifai/roadpass/internal/api"
	"github.com/sensifai/roadpass/internal/auth"
	"github.com/sensifai/roadpass/internal/cache"
)

// actor is a demo member with a session, calling the API in-process.
type actor struct {
	Member
	userID string
	cookie string
}

// sessions opens a session for every member of the team.
func (m *Manager) sessions(ctx context.Context) (map[string]*actor, error) {
	out := map[string]*actor{}
	for _, mem := range Team {
		u, err := m.St.UserByEmail(ctx, mem.Email)
		if err != nil {
			return nil, fmt.Errorf("user %s: %w", mem.Email, err)
		}
		tok := auth.Token("", 32)
		if err := m.Cache.PutSession(ctx, tok, cache.Session{UserID: u.ID, OrgID: u.OrgID, CreatedAt: time.Now().UTC()}, 2*time.Hour); err != nil {
			return nil, err
		}
		out[mem.Key] = &actor{Member: mem, userID: u.ID, cookie: tok}
	}
	return out, nil
}

// call runs one API request as a member, decoding a JSON answer into out (if
// given). Any status of 300 or more is an error carrying the server's message.
func (m *Manager) call(ctx context.Context, a *actor, method, path string, body, out any) error {
	_, err := m.callStatus(ctx, a, method, path, body, out)
	return err
}

func (m *Manager) callStatus(ctx context.Context, a *actor, method, path string, body, out any) (int, error) {
	var rd *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rd).WithContext(ctx)
	req.RemoteAddr = api.DemoSeedAddr
	req.Header.Set("X-Requested-With", "roadpass")
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: api.SessionCookie, Value: a.cookie})
	rec := httptest.NewRecorder()
	m.Handler.ServeHTTP(rec, req)
	if rec.Code >= 300 {
		return rec.Code, fmt.Errorf("%s %s as %s: %d %s", method, path, a.Key, rec.Code, bytes.TrimSpace(rec.Body.Bytes()))
	}
	if out != nil && rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			return rec.Code, fmt.Errorf("%s %s: bad answer: %w", method, path, err)
		}
	}
	return rec.Code, nil
}
