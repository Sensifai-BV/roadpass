// Package cache is everything ROADPASS keeps in Redis: the response cache,
// sessions, rate limits, the ingest stream and live events.
//
// Redis is never the only copy of anything (docs/ARCHITECTURE.md §2): losing it
// signs people out and empties the cache, nothing more. Ingest messages are
// acknowledged only after Postgres has committed them.
package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"
)

// Keys.
const (
	keyGen        = "rp:gen"      // cache generation; INCR invalidates every cached answer
	keyCache      = "rp:c:%d:%s"  // gen, name
	keySession    = "rp:s:%s"     // sha256(token)
	keyRate       = "rp:rl:%s:%d" // subject, window
	StreamIngest  = "rp:ingest"   // ingest jobs
	GroupWorkers  = "workers"
	ChannelEvents = "rp:events" // live events for SSE
)

// Cache wraps a Redis client.
type Cache struct {
	R     redis.UniversalClient
	group singleflight.Group
}

// Open connects to Redis (REDIS_URL, e.g. redis://:pass@host:6379/0).
func Open(ctx context.Context, url string) (*Cache, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parse REDIS_URL: %w", err)
	}
	c := redis.NewClient(opt)
	if err := c.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis: %w", err)
	}
	return &Cache{R: c}, nil
}

// Close closes the client.
func (c *Cache) Close() error { return c.R.Close() }

func (c *Cache) gen(ctx context.Context) int64 {
	g, err := c.R.Get(ctx, keyGen).Int64()
	if err != nil {
		return 0
	}
	return g
}

// Invalidate drops every cached answer by moving to a new generation; old keys
// expire on their own TTL.
func (c *Cache) Invalidate(ctx context.Context) error { return c.R.Incr(ctx, keyGen).Err() }

// Remember returns the cached JSON for name, or computes it once (concurrent misses
// share one computation) and caches it for ttl. A Redis failure degrades to
// computing every time; it never fails the request.
func Remember[T any](ctx context.Context, c *Cache, name string, ttl time.Duration, compute func(context.Context) (T, error)) (T, error) {
	var zero T
	if c == nil {
		return compute(ctx)
	}
	key := fmt.Sprintf(keyCache, c.gen(ctx), name)
	if b, err := c.R.Get(ctx, key).Bytes(); err == nil {
		var v T
		if json.Unmarshal(b, &v) == nil {
			return v, nil
		}
	}
	v, err, _ := c.group.Do(key, func() (any, error) {
		v, err := compute(ctx)
		if err != nil {
			return nil, err
		}
		if b, err := json.Marshal(v); err == nil {
			c.R.Set(context.WithoutCancel(ctx), key, b, ttl)
		}
		return v, nil
	})
	if err != nil {
		return zero, err
	}
	return v.(T), nil
}

// ---------------------------------------------------------------- sessions

// Session is a signed-in browser.
type Session struct {
	UserID    string    `json:"u"`
	OrgID     string    `json:"o"`
	CSRF      string    `json:"x"`
	CreatedAt time.Time `json:"c"`
}

// ErrNoSession is returned for an unknown or expired session.
var ErrNoSession = errors.New("no session")

func sessionKey(token string) string {
	h := sha256.Sum256([]byte(token))
	return fmt.Sprintf(keySession, hex.EncodeToString(h[:]))
}

// PutSession stores a session under the hash of its token (a Redis dump does not
// leak usable cookies).
func (c *Cache) PutSession(ctx context.Context, token string, s Session, ttl time.Duration) error {
	b, _ := json.Marshal(s)
	return c.R.Set(ctx, sessionKey(token), b, ttl).Err()
}

// GetSession returns a session and slides its expiry.
func (c *Cache) GetSession(ctx context.Context, token string, ttl time.Duration) (Session, error) {
	var s Session
	b, err := c.R.GetEx(ctx, sessionKey(token), ttl).Bytes()
	if errors.Is(err, redis.Nil) {
		return s, ErrNoSession
	}
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal(b, &s)
}

// DeleteSession signs a browser out.
func (c *Cache) DeleteSession(ctx context.Context, token string) error {
	return c.R.Del(ctx, sessionKey(token)).Err()
}

// ---------------------------------------------------------------- rate limits

// Allow implements a fixed-window limit of n requests per window for subject. It
// returns the remaining budget and when the window resets. A Redis failure allows
// the request: availability over strictness for a limit that protects capacity,
// not money.
func (c *Cache) Allow(ctx context.Context, subject string, n int, window time.Duration) (bool, int, time.Duration) {
	w := time.Now().UnixNano() / int64(window)
	key := fmt.Sprintf(keyRate, subject, w)
	pipe := c.R.TxPipeline()
	incr := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, window+time.Second)
	if _, err := pipe.Exec(ctx); err != nil {
		return true, n, window
	}
	reset := time.Duration((w+1)*int64(window) - time.Now().UnixNano())
	used := int(incr.Val())
	return used <= n, max(0, n-used), reset
}

// ---------------------------------------------------------------- ingest stream

// Enqueue appends an ingest job id to the stream.
func (c *Cache) Enqueue(ctx context.Context, ingestID string) error {
	return c.R.XAdd(ctx, &redis.XAddArgs{Stream: StreamIngest, MaxLen: 100000, Approx: true,
		Values: map[string]any{"ingest": ingestID}}).Err()
}

// EnsureGroup creates the consumer group if needed.
func (c *Cache) EnsureGroup(ctx context.Context) error {
	err := c.R.XGroupCreateMkStream(ctx, StreamIngest, GroupWorkers, "0").Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return err
	}
	return nil
}

// Job is one stream message.
type Job struct {
	MsgID    string
	IngestID string
}

// Next blocks up to block for jobs; it first reclaims messages another consumer
// left unacknowledged for longer than stale (a crashed worker).
func (c *Cache) Next(ctx context.Context, consumer string, block, stale time.Duration) ([]Job, error) {
	var out []Job
	claimed, _, err := c.R.XAutoClaim(ctx, &redis.XAutoClaimArgs{Stream: StreamIngest, Group: GroupWorkers,
		Consumer: consumer, MinIdle: stale, Start: "0-0", Count: 10}).Result()
	if err == nil {
		for _, m := range claimed {
			out = append(out, Job{MsgID: m.ID, IngestID: fmt.Sprint(m.Values["ingest"])})
		}
	}
	if len(out) > 0 {
		return out, nil
	}
	res, err := c.R.XReadGroup(ctx, &redis.XReadGroupArgs{Group: GroupWorkers, Consumer: consumer,
		Streams: []string{StreamIngest, ">"}, Count: 10, Block: block}).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	for _, s := range res {
		for _, m := range s.Messages {
			out = append(out, Job{MsgID: m.ID, IngestID: fmt.Sprint(m.Values["ingest"])})
		}
	}
	return out, nil
}

// Ack acknowledges a processed job.
func (c *Cache) Ack(ctx context.Context, msgID string) error {
	return c.R.XAck(ctx, StreamIngest, GroupWorkers, msgID).Err()
}

// Backlog is the number of unprocessed plus pending jobs.
func (c *Cache) Backlog(ctx context.Context) int64 {
	info, err := c.R.XInfoGroups(ctx, StreamIngest).Result()
	if err != nil {
		return 0
	}
	var n int64
	for _, g := range info {
		if g.Name == GroupWorkers {
			n = g.Lag + g.Pending
		}
	}
	return n
}

// ---------------------------------------------------------------- live events

// Event is pushed to browsers over SSE.
type Event struct {
	Kind  string   `json:"kind"` // records | seal | finding | ingest | notification
	Orgs  []string `json:"orgs,omitempty"`
	Users []string `json:"users,omitempty"` // only these people (notifications)
	Count int      `json:"count,omitempty"`
	Ref   string   `json:"ref,omitempty"`
	At    string   `json:"at"`
}

// Announce tells browsers without touching the analytics cache (notifications
// change nothing a chart shows).
func (c *Cache) Announce(ctx context.Context, e Event) {
	if e.At == "" {
		e.At = time.Now().UTC().Format(time.RFC3339)
	}
	b, _ := json.Marshal(e)
	c.R.Publish(ctx, ChannelEvents, b)
}

// AllowN counts one use of a limited resource in a fixed window and reports
// whether it is still within limit.
func (c *Cache) AllowN(ctx context.Context, key string, limit int, window time.Duration) bool {
	k := "rl:" + key + ":" + strconv.FormatInt(time.Now().Unix()/int64(window.Seconds()), 10)
	n, err := c.R.Incr(ctx, k).Result()
	if err != nil {
		return true // fail open: a Redis hiccup must not silence alerts
	}
	if n == 1 {
		c.R.Expire(ctx, k, window)
	}
	return n <= int64(limit)
}

// Publish announces a change and invalidates the cache.
func (c *Cache) Publish(ctx context.Context, e Event) {
	if e.At == "" {
		e.At = time.Now().UTC().Format(time.RFC3339)
	}
	_ = c.Invalidate(ctx)
	b, _ := json.Marshal(e)
	c.R.Publish(ctx, ChannelEvents, b)
}

// Subscribe returns a channel of events until ctx ends.
func (c *Cache) Subscribe(ctx context.Context) <-chan Event {
	out := make(chan Event, 16)
	sub := c.R.Subscribe(ctx, ChannelEvents)
	go func() {
		defer close(out)
		defer sub.Close()
		ch := sub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case m, ok := <-ch:
				if !ok {
					return
				}
				var e Event
				if json.Unmarshal([]byte(m.Payload), &e) == nil {
					select {
					case out <- e:
					default: // a slow browser drops events rather than blocking Redis
					}
				}
			}
		}
	}()
	return out
}

// Itoa is a tiny helper for metric labels.
func Itoa(n int64) string { return strconv.FormatInt(n, 10) }
