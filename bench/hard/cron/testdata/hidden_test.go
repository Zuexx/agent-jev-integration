package cron

import (
	"testing"
	"time"
)

func at(s string) time.Time {
	t, err := time.Parse("2006-01-02 15:04:05", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestHiddenNext(t *testing.T) {
	cases := []struct{ expr, after, want string }{
		{"*/15 * * * *", "2026-10-14 10:07:30", "2026-10-14 10:15:00"},
		{"5,10 * * * *", "2026-10-14 10:10:00", "2026-10-14 11:05:00"},
		{"0 0 1 1 *", "2026-01-01 00:00:00", "2027-01-01 00:00:00"},
		{"0 12 13 * 5", "2026-10-14 00:00:00", "2026-10-16 12:00:00"},
		{"0 12 13 * *", "2026-10-14 00:00:00", "2026-11-13 12:00:00"},
		{"0 0 * * 7", "2026-10-14 00:00:00", "2026-10-18 00:00:00"},
		{"0 0 * * 0", "2026-10-14 00:00:00", "2026-10-18 00:00:00"},
		{"30 9 * * 1-5", "2026-10-16 10:00:00", "2026-10-19 09:30:00"},
		{"0 9-17/4 * * *", "2026-10-14 09:00:00", "2026-10-14 13:00:00"},
		{"0 9-17/4 * * *", "2026-10-14 17:00:00", "2026-10-15 09:00:00"},
		{"0 0 29 2 *", "2026-03-01 00:00:00", "2028-02-29 00:00:00"},
		{"59 23 31 12 *", "2026-12-31 23:59:00", "2027-12-31 23:59:00"},
	}
	for _, c := range cases {
		got, err := Next(c.expr, at(c.after))
		if err != nil || !got.Equal(at(c.want)) {
			t.Errorf("Next(%q, %s) = %v, %v; want %s", c.expr, c.after, got, err, c.want)
		}
	}
}

func TestHiddenLocation(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*3600)
	after := time.Date(2026, 10, 14, 23, 30, 0, 0, loc)
	got, err := Next("0 0 * * *", after)
	want := time.Date(2026, 10, 15, 0, 0, 0, 0, loc)
	if err != nil || !got.Equal(want) || got.Location() != loc {
		t.Errorf("got %v %v want %v in after's location", got, err, want)
	}
}

func TestHiddenErrors(t *testing.T) {
	for _, e := range []string{"0 0 30 2 *", "60 * * * *", "* * * *", "* * * * * *", "*/0 * * * *", "5-1 * * * *", "* * 0 * *", "* * * 13 *", "* * * * 8", "a * * * *", "1,,2 * * * *", ""} {
		if _, err := Next(e, at("2026-10-14 00:00:00")); err == nil {
			t.Errorf("Next(%q) should fail", e)
		}
	}
}
