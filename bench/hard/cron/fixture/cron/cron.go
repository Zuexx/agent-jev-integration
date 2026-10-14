// Package cron computes the next run time of a cron expression.
package cron

import "time"

// Next returns the first minute strictly after `after` (seconds dropped) that
// matches expr, in after's location.
//
// expr has five space-separated fields: minute (0-59), hour (0-23),
// day of month (1-31), month (1-12), day of week (0-7, where 0 and 7 are Sunday).
// Each field is "*", a number, a range "a-b", a step "*/n" or "a-b/n", or a
// comma-separated list of those. A range must have a <= b and a step n >= 1.
//
// Day matching follows classic cron: if both day of month and day of week are
// restricted (neither is "*"), a day matches when EITHER field matches; if one
// of them is "*", only the other one restricts the day.
//
// It returns an error for an invalid expression, or when no time matches within
// five years of `after` (for example "0 0 30 2 *").
func Next(expr string, after time.Time) (time.Time, error) {
	panic("not implemented")
}
