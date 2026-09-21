package alerts

import (
	"testing"
	"time"
)

func states(es []Emit) []string {
	var out []string
	for _, e := range es {
		out = append(out, e.State)
	}
	return out
}

func TestLifecycle(t *testing.T) {
	t0 := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	at := func(m int) time.Time { return t0.Add(time.Duration(m) * time.Minute) }
	r := Rule{ID: "r", ForMinutes: 10, RepeatMinutes: 60, NotifyResolved: true,
		Escalation: []Step{{AfterMinutes: 90, ChannelIDs: []string{"sms"}}, {AfterMinutes: 30, ChannelIDs: []string{"email"}}}}

	s, e := Decide(r, nil, true, at(0), false)
	if s.State != "pending" || len(e) != 0 {
		t.Fatalf("new condition must hold first: %+v %v", s, e)
	}
	if s, e = Decide(r, s, true, at(5), false); s.State != "pending" || len(e) != 0 {
		t.Fatal("fired before the hold time")
	}
	if _, e = Decide(r, s, true, at(10), true); len(e) != 0 {
		t.Fatal("maintenance must suppress new firings")
	}
	s, e = Decide(r, s, true, at(10), false)
	if s.State != "firing" || len(e) != 1 || e[0].State != "firing" {
		t.Fatalf("should fire at the hold time: %v", states(e))
	}
	// Escalations run in order of their delay, whatever order they were written in.
	s, e = Decide(r, s, true, at(40), false)
	if len(e) != 1 || e[0].State != "escalated" || e[0].Step.ChannelIDs[0] != "email" {
		t.Fatalf("first escalation after 30 min: %+v", e)
	}
	s, e = Decide(r, s, true, at(100), false)
	if len(e) != 1 || e[0].Step == nil || e[0].Step.ChannelIDs[0] != "sms" {
		t.Fatalf("second escalation after 90 min: %v", states(e))
	}
	if s, e = Decide(r, s, true, at(160), false); len(e) != 1 || e[0].State != "reminder" {
		t.Fatalf("reminder after an hour: %v", states(e))
	}
	ack := at(161)
	s.AckAt = &ack
	if _, e = Decide(r, s, true, at(400), false); len(e) != 0 {
		t.Fatal("acknowledged alerts do not remind")
	}
	if n, e := Decide(r, s, false, at(401), false); n != nil || len(e) != 1 || e[0].State != "resolved" {
		t.Fatalf("clearing a firing alert sends the all-clear: %v", states(e))
	}
	// A condition that clears while still pending says nothing.
	p, _ := Decide(r, nil, true, at(0), false)
	if n, e := Decide(r, p, false, at(3), false); n != nil || len(e) != 0 {
		t.Fatal("a pending condition clears silently")
	}
	// Snooze holds reminders until it ends.
	f := &State{State: "firing", Since: t0, FiredAt: &t0, LastNotified: &t0, EscLevel: 2}
	until := at(300)
	f.SnoozeUntil = &until
	if _, e = Decide(r, f, true, at(200), false); len(e) != 0 {
		t.Fatal("snoozed")
	}
	if _, e = Decide(r, f, true, at(301), false); len(e) != 1 || e[0].State != "reminder" {
		t.Fatalf("reminders resume after the snooze: %v", states(e))
	}
}

func TestAudience(t *testing.T) {
	intake := Recipient{ID: "u1", Role: "editor", Duties: []string{"receive"}}
	admin := Recipient{ID: "u2", Role: "admin"}
	viewer := Recipient{ID: "u3", Role: "viewer"}
	for _, c := range []struct {
		r    Recipient
		aud  []string
		want bool
	}{
		{intake, []string{"duty:receive"}, true}, {intake, []string{"duty:ship"}, false}, {admin, []string{"duty:ship"}, true},
		{viewer, nil, false}, {admin, nil, true}, {viewer, []string{"user:u3"}, true}, {viewer, []string{"all"}, true},
	} {
		if c.r.Matches(c.aud) != c.want {
			t.Errorf("%s in %v: want %v", c.r.ID, c.aud, c.want)
		}
	}
}

func TestCrosses(t *testing.T) {
	if !Crosses(251, ">", 250) || Crosses(250, ">", 250) || !Crosses(250, ">=", 250) || !Crosses(1, "<", 2) || Crosses(1, "?", 0) {
		t.Fatal("threshold comparison")
	}
}
