package bench

import (
	"testing"
	"time"
)

func TestQuotaGate(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	later := now.Add(2 * time.Hour).Unix()
	week := now.Add(72 * time.Hour).Unix()
	g := &QuotaGate{Max7d: 0.7, Max5h: 0.95, Accounts: 2}
	if a, _, _ := g.Next(now); a != 0 {
		t.Fatal("first account first", a)
	}
	g.Observe(0, &Quota{FiveHour: 0.2, SevenDay: 0.71, FiveHourReset: later, SevenDayReset: week})
	if a, _, _ := g.Next(now); a != 1 {
		t.Fatal("account 0 is at its weekly cap; untried account 1 next", a)
	}
	g.Observe(1, &Quota{FiveHour: 0.96, SevenDay: 0.3, FiveHourReset: later, SevenDayReset: week})
	if a, w, done := g.Next(now); a != -1 || w != 2*time.Hour || done {
		t.Fatal("wait for account 1's 5-hour reset", a, w, done)
	}
	if a, _, _ := g.Next(time.Unix(later+1, 0)); a != 1 {
		t.Fatal("after the reset account 1 runs again", a)
	}
	g.Observe(1, &Quota{FiveHour: 0.1, SevenDay: 0.7, FiveHourReset: later, SevenDayReset: week})
	if _, _, done := g.Next(now); !done {
		t.Fatal("both at the weekly cap: done")
	}
	if a, _, _ := g.Next(time.Unix(week+1, 0)); a != 0 {
		t.Fatal("a new week frees account 0", a)
	}
}

func TestDone(t *testing.T) {
	d := Done([]Run{{Task: "a", Arm: "opus/off", Rep: 1}, {Task: "a", Arm: "opus/off", Rep: 2, Error: "x"}})
	if !d[JobKey("a", "opus/off", 1)] || d[JobKey("a", "opus/off", 2)] {
		t.Fatal(d)
	}
}

func TestUnreachable(t *testing.T) {
	if !Unreachable(Run{Error: "claude: success: API Error: Can't reach the API server — check your internet or DNS (ENOTFOUND)"}) {
		t.Fatal("outage not detected")
	}
	if Unreachable(Run{Error: "claude: success: API Error: Can't reach the API server", ClaudeUSD: 0.2}) || Unreachable(Run{Error: "claude: error_max_turns: x"}) {
		t.Fatal("a run that did work, or another error, is not an outage")
	}
}

func TestWallSleep(t *testing.T) {
	start := time.Now()
	WallSleep(20 * time.Millisecond)
	if d := time.Since(start); d < 20*time.Millisecond || d > time.Second {
		t.Fatal(d)
	}
	WallSleep(-time.Second) // returns at once
}
