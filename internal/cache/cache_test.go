package cache

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newCache(t *testing.T) (*Cache, *miniredis.Miniredis) {
	m := miniredis.RunT(t)
	return &Cache{R: redis.NewClient(&redis.Options{Addr: m.Addr()})}, m
}

func TestRememberAndInvalidate(t *testing.T) {
	c, _ := newCache(t)
	ctx := context.Background()
	var calls atomic.Int32
	f := func(context.Context) (int, error) { return int(calls.Add(1)), nil }
	a, _ := Remember(ctx, c, "x", time.Minute, f)
	b, _ := Remember(ctx, c, "x", time.Minute, f)
	if a != 1 || b != 1 {
		t.Fatalf("second call not cached: %d %d", a, b)
	}
	_ = c.Invalidate(ctx)
	if v, _ := Remember(ctx, c, "x", time.Minute, f); v != 2 {
		t.Fatalf("invalidate did not drop the cache: %d", v)
	}
}

func TestRememberDegradesWithoutRedis(t *testing.T) {
	c, m := newCache(t)
	m.Close()
	v, err := Remember(context.Background(), c, "x", time.Minute, func(context.Context) (string, error) { return "ok", nil })
	if err != nil || v != "ok" {
		t.Fatalf("redis down must not fail the request: %v %q", err, v)
	}
}

func TestSessions(t *testing.T) {
	c, m := newCache(t)
	ctx := context.Background()
	if err := c.PutSession(ctx, "tok", Session{UserID: "u1", OrgID: "o1"}, time.Hour); err != nil {
		t.Fatal(err)
	}
	for _, k := range m.Keys() {
		if k == "rp:s:tok" {
			t.Fatal("session stored under the raw token")
		}
	}
	s, err := c.GetSession(ctx, "tok", time.Hour)
	if err != nil || s.UserID != "u1" {
		t.Fatalf("get: %v %+v", err, s)
	}
	_ = c.DeleteSession(ctx, "tok")
	if _, err := c.GetSession(ctx, "tok", time.Hour); err != ErrNoSession {
		t.Fatalf("deleted session still valid: %v", err)
	}
}

func TestRateLimit(t *testing.T) {
	c, _ := newCache(t)
	ctx := context.Background()
	for i := range 3 {
		if ok, _, _ := c.Allow(ctx, "k", 3, time.Minute); !ok {
			t.Fatalf("request %d refused", i)
		}
	}
	if ok, rem, _ := c.Allow(ctx, "k", 3, time.Minute); ok || rem != 0 {
		t.Fatal("4th request allowed")
	}
}

func TestStreamAckAndReclaim(t *testing.T) {
	c, _ := newCache(t)
	ctx := context.Background()
	if err := c.EnsureGroup(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.EnsureGroup(ctx); err != nil {
		t.Fatalf("EnsureGroup must be idempotent: %v", err)
	}
	_ = c.Enqueue(ctx, "ing_1")
	jobs, err := c.Next(ctx, "w1", 10*time.Millisecond, time.Hour)
	if err != nil || len(jobs) != 1 || jobs[0].IngestID != "ing_1" {
		t.Fatalf("next: %v %+v", err, jobs)
	}
	// w1 "crashes": w2 reclaims the unacknowledged job.
	again, _ := c.Next(ctx, "w2", 10*time.Millisecond, 0)
	if len(again) != 1 || again[0].IngestID != "ing_1" {
		t.Fatalf("stale job not reclaimed: %+v", again)
	}
	_ = c.Ack(ctx, again[0].MsgID)
	if left, _ := c.Next(ctx, "w3", 10*time.Millisecond, 0); len(left) != 0 {
		t.Fatalf("acked job delivered again: %+v", left)
	}
}
