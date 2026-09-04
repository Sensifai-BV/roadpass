package cache

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sensifai/roadpass/internal/domain"
)

// Telemetry (extruder readings, mixer cycles) arrives at high rates from plant
// control systems. The API appends it to a Redis stream and returns at once; workers
// read it in batches and insert into ClickHouse, acknowledging only after the insert.
const (
	StreamTelemetry = "rp:telemetry"
	GroupTelemetry  = "telemetry"
)

// TelemetryMsg is one API submission.
type TelemetryMsg struct {
	Readings []domain.Reading    `json:"r,omitempty"`
	Cycles   []domain.MixerCycle `json:"c,omitempty"`
}

// PushTelemetry appends one submission.
func (c *Cache) PushTelemetry(ctx context.Context, m TelemetryMsg) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return c.R.XAdd(ctx, &redis.XAddArgs{Stream: StreamTelemetry, MaxLen: 200000, Approx: true, Values: map[string]any{"m": b}}).Err()
}

// EnsureTelemetryGroup creates the consumer group.
func (c *Cache) EnsureTelemetryGroup(ctx context.Context) error {
	err := c.R.XGroupCreateMkStream(ctx, StreamTelemetry, GroupTelemetry, "0").Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return err
	}
	return nil
}

// TelemetryBatch is what one read returns.
type TelemetryBatch struct {
	MsgIDs   []string
	Readings []domain.Reading
	Cycles   []domain.MixerCycle
}

// NextTelemetry reads up to count messages, reclaiming stale ones first.
func (c *Cache) NextTelemetry(ctx context.Context, consumer string, block, stale time.Duration, count int64) (TelemetryBatch, error) {
	var b TelemetryBatch
	add := func(ms []redis.XMessage) {
		for _, m := range ms {
			b.MsgIDs = append(b.MsgIDs, m.ID)
			var t TelemetryMsg
			if s, ok := m.Values["m"].(string); ok && json.Unmarshal([]byte(s), &t) == nil {
				b.Readings = append(b.Readings, t.Readings...)
				b.Cycles = append(b.Cycles, t.Cycles...)
			}
		}
	}
	claimed, _, err := c.R.XAutoClaim(ctx, &redis.XAutoClaimArgs{Stream: StreamTelemetry, Group: GroupTelemetry,
		Consumer: consumer, MinIdle: stale, Start: "0-0", Count: count}).Result()
	if err == nil && len(claimed) > 0 {
		add(claimed)
		return b, nil
	}
	res, err := c.R.XReadGroup(ctx, &redis.XReadGroupArgs{Group: GroupTelemetry, Consumer: consumer,
		Streams: []string{StreamTelemetry, ">"}, Count: count, Block: block}).Result()
	if errors.Is(err, redis.Nil) || errors.Is(err, context.Canceled) {
		return b, nil
	}
	if err != nil {
		return b, err
	}
	for _, s := range res {
		add(s.Messages)
	}
	return b, nil
}

// AckTelemetry acknowledges inserted messages.
func (c *Cache) AckTelemetry(ctx context.Context, ids ...string) error {
	return c.R.XAck(ctx, StreamTelemetry, GroupTelemetry, ids...).Err()
}
