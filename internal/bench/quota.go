package bench

import (
	"fmt"
	"strings"
	"time"
)

// QuotaGate picks which subscription account runs next (docs/eval-plan.md
// 4.4). Each run reports its account's quota; an account whose 7-day use has
// reached Max7d is done for this session, and one whose 5-hour use has
// reached Max5h waits for its reset. Accounts without a report yet are tried.
type QuotaGate struct {
	Max7d, Max5h float64
	Accounts     int
	last         map[int]*Quota
}

// Observe records the quota a run on account i reported.
func (g *QuotaGate) Observe(i int, q *Quota) {
	if q == nil {
		return
	}
	if g.last == nil {
		g.last = map[int]*Quota{}
	}
	g.last[i] = q
}

// Next returns the account to use now; or, when every usable account is
// waiting for its 5-hour reset, how long to wait; or done when every account
// has reached its weekly cap.
func (g *QuotaGate) Next(now time.Time) (account int, wait time.Duration, done bool) {
	var soonest time.Duration = -1
	for i := 0; i < g.Accounts; i++ {
		q := g.last[i]
		if q == nil {
			return i, 0, false
		}
		if q.SevenDay >= g.Max7d && time.Unix(q.SevenDayReset, 0).After(now) {
			continue
		}
		reset := time.Unix(q.FiveHourReset, 0)
		if q.FiveHour < g.Max5h || !reset.After(now) {
			return i, 0, false
		}
		if w := reset.Sub(now); soonest < 0 || w < soonest {
			soonest = w
		}
	}
	if soonest < 0 {
		return 0, 0, true
	}
	return -1, soonest, false
}

// Done reports which (task, arm, rep) runs in rs finished without error, so
// a resumed bench skips them.
func Done(rs []Run) map[string]bool {
	m := map[string]bool{}
	for _, r := range rs {
		if r.Error == "" {
			m[JobKey(r.Task, r.Arm, r.Rep)] = true
		}
	}
	return m
}

func JobKey(task, arm string, rep int) string {
	return fmt.Sprintf("%s\x00%s\x00%d", task, arm, rep)
}

// Unreachable reports a run that failed because the API could not be reached
// (DNS or network down), as opposed to a result about the task.
func Unreachable(r Run) bool {
	return r.Error != "" && r.ClaudeUSD == 0 && (strings.Contains(r.Error, "reach the API") || strings.Contains(r.Error, "ENOTFOUND"))
}

// WallSleep waits until d of wall-clock time has passed. time.Sleep counts
// only time the machine is awake (on macOS its clock stops during system
// sleep), so a long wait for a quota reset could overshoot by hours.
func WallSleep(d time.Duration) {
	end := time.Now().Add(d).Round(0) // Round(0) drops the monotonic reading
	for {
		left := time.Until(end)
		if left <= 0 {
			return
		}
		time.Sleep(min(left, time.Minute))
	}
}
