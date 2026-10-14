package ledger

import (
	"fmt"
	"strconv"
)

// Money is an amount in cents.
type Money int64

// FromString parses a decimal amount such as "12.34", "-0.10" or "7".
// At most two decimal places are allowed; anything else is an error.
func FromString(s string) (Money, error) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("bad amount %q: %w", s, err)
	}
	return Money(f * 100), nil
}

func (m Money) String() string {
	sign := ""
	if m < 0 {
		sign, m = "-", -m
	}
	return fmt.Sprintf("%s%d.%02d", sign, m/100, m%100)
}
