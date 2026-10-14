package ledger

import (
	"fmt"
	"strconv"
	"strings"
)

// Money is an amount in cents.
type Money int64

// FromString parses a decimal amount such as "12.34", "-0.10" or "7".
// At most two decimal places are allowed; anything else is an error.
func FromString(s string) (Money, error) {
	bad := fmt.Errorf("bad amount %q", s)
	neg := strings.HasPrefix(s, "-")
	t := strings.TrimPrefix(s, "-")
	whole, frac, hasDot := strings.Cut(t, ".")
	if whole == "" || (hasDot && (frac == "" || len(frac) > 2)) {
		return 0, bad
	}
	for len(frac) < 2 {
		frac += "0"
	}
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, bad
	}
	f, err := strconv.ParseInt(frac, 10, 64)
	if err != nil || strings.ContainsAny(frac, "+-") {
		return 0, bad
	}
	m := Money(w*100 + f)
	if neg {
		m = -m
	}
	return m, nil
}

func (m Money) String() string {
	sign := ""
	if m < 0 {
		sign, m = "-", -m
	}
	return fmt.Sprintf("%s%d.%02d", sign, m/100, m%100)
}
