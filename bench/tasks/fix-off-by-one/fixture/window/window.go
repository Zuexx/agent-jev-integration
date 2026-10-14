// Package window computes sliding-window aggregates.
package window

// Sums returns the sum of every window of size k over xs, in order.
// For len(xs) < k it returns nil.
func Sums(xs []int, k int) []int {
	if k <= 0 || len(xs) < k {
		return nil
	}
	var out []int
	for i := 0; i < len(xs)-k; i++ {
		s := 0
		for _, x := range xs[i : i+k] {
			s += x
		}
		out = append(out, s)
	}
	return out
}
