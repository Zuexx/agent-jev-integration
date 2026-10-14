package ledger

import (
	"reflect"
	"testing"
)

func TestHiddenFromString(t *testing.T) {
	for in, want := range map[string]Money{"0.29": 29, "0.57": 57, "1.15": 115, "19.99": 1999, "-0.10": -10, "7": 700, "7.5": 750, "1234567.89": 123456789, "-4.35": -435} {
		if got, err := FromString(in); err != nil || got != want {
			t.Errorf("FromString(%q)=%d,%v want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"1.005", "0.001", "abc", "", "1.2.3"} {
		if _, err := FromString(in); err == nil {
			t.Errorf("FromString(%q) must fail: more than two decimals or not a number", in)
		}
	}
}

func TestHiddenSplit(t *testing.T) {
	cases := []struct {
		total Money
		n     int
		want  []Money
	}{
		{100, 3, []Money{34, 33, 33}},
		{-100, 3, []Money{-34, -33, -33}},
		{101, 4, []Money{26, 25, 25, 25}},
		{5, 7, []Money{1, 1, 1, 1, 1, 0, 0}},
		{-5, 7, []Money{-1, -1, -1, -1, -1, 0, 0}},
		{99, 1, []Money{99}},
	}
	for _, c := range cases {
		if got := Split(c.total, c.n); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Split(%d,%d)=%v want %v", c.total, c.n, got, c.want)
		}
	}
}
