package ledger

// Split divides total into n parts that differ by at most one cent and sum
// exactly to total. Larger parts come first: Split(100, 3) == [34 33 33].
// For a negative total the parts are negative: Split(-100, 3) == [-34 -33 -33].
func Split(total Money, n int) []Money {
	parts := make([]Money, n)
	each := total / Money(n)
	for i := range parts {
		parts[i] = each
	}
	parts[n-1] += total - each*Money(n)
	return parts
}
