// Package cron computes the next run time of a cron expression.
package cron

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

type set map[int]bool

func field(s string, lo, hi int) (set, bool, error) {
	out := set{}
	bad := errors.New("invalid field " + s)
	if s == "" {
		return nil, false, bad
	}
	for _, part := range strings.Split(s, ",") {
		rng, stepS, hasStep := strings.Cut(part, "/")
		step := 1
		if hasStep {
			n, err := strconv.Atoi(stepS)
			if err != nil || n < 1 {
				return nil, false, bad
			}
			step = n
		}
		a, b := lo, hi
		if rng != "*" {
			x, y, isRange := strings.Cut(rng, "-")
			var err error
			if a, err = strconv.Atoi(x); err != nil {
				return nil, false, bad
			}
			b = a
			if isRange {
				if b, err = strconv.Atoi(y); err != nil {
					return nil, false, bad
				}
			} else if hasStep {
				return nil, false, bad
			}
		}
		if a < lo || b > hi || a > b {
			return nil, false, bad
		}
		for v := a; v <= b; v += step {
			out[v] = true
		}
	}
	return out, s != "*", nil
}

func Next(expr string, after time.Time) (time.Time, error) {
	f := strings.Fields(expr)
	if len(f) != 5 || strings.Count(expr, " ") != 4 {
		return time.Time{}, errors.New("need five fields")
	}
	min, _, err := field(f[0], 0, 59)
	if err != nil {
		return time.Time{}, err
	}
	hour, _, err := field(f[1], 0, 23)
	if err != nil {
		return time.Time{}, err
	}
	dom, domR, err := field(f[2], 1, 31)
	if err != nil {
		return time.Time{}, err
	}
	mon, _, err := field(f[3], 1, 12)
	if err != nil {
		return time.Time{}, err
	}
	dow, dowR, err := field(f[4], 0, 7)
	if err != nil {
		return time.Time{}, err
	}
	if dow[7] {
		dow[0] = true
	}
	dayOK := func(t time.Time) bool {
		d, w := dom[t.Day()], dow[int(t.Weekday())]
		if domR && dowR {
			return d || w
		}
		return d && w
	}
	t := after.Truncate(time.Minute).Add(time.Minute)
	limit := after.AddDate(5, 0, 0)
	for !t.After(limit) {
		if !mon[int(t.Month())] || !dayOK(t) {
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, t.Location())
			continue
		}
		if !hour[t.Hour()] {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, t.Location())
			continue
		}
		if !min[t.Minute()] {
			t = t.Add(time.Minute)
			continue
		}
		return t, nil
	}
	return time.Time{}, errors.New("no match within five years")
}
