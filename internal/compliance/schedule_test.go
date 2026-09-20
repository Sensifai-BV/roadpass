package compliance

import (
	"testing"
	"time"

	"github.com/sensifai/roadpass/internal/store"
)

func TestScheduleDue(t *testing.T) {
	x := store.Schedule{Day: 3, Hour: 8, Timezone: "Europe/Madrid", Enabled: true}
	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	if _, due := Due(x, at("2026-10-03T05:59:00Z")); due { // 07:59 in Madrid
		t.Error("due before the hour")
	}
	p, due := Due(x, at("2026-10-03T06:00:00Z"))
	if !due || p != "2026-09" {
		t.Errorf("at the hour: %q %v, want 2026-09 due", p, due)
	}
	sept := at("2026-09-01T00:00:00Z")
	x.LastPeriod = &sept
	if _, due := Due(x, at("2026-10-20T10:00:00Z")); due {
		t.Error("a month is sent only once")
	}
	// A failed send is retried after an hour, not at every tick.
	x.LastPeriod = nil
	ran := at("2026-10-03T06:30:00Z")
	x.LastStatus, x.LastRunAt = "failed", &ran
	if _, due := Due(x, at("2026-10-03T07:00:00Z")); due {
		t.Error("retried too soon")
	}
	if _, due := Due(x, at("2026-10-03T07:31:00Z")); !due {
		t.Error("not retried after an hour")
	}
	// January sends December of the previous year.
	if p, _ := Due(store.Schedule{Day: 1, Timezone: "UTC"}, at("2027-01-01T00:00:00Z")); p != "2026-12" {
		t.Errorf("January period = %q", p)
	}
}
