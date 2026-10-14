package ledger

// Total parses every amount and adds them up.
func Total(amounts []string) (Money, error) {
	var sum Money
	for _, a := range amounts {
		m, err := FromString(a)
		if err != nil {
			return 0, err
		}
		sum += m
	}
	return sum, nil
}

// Share splits each amount between n people and returns what person i owes in total.
func Share(amounts []string, n, i int) (Money, error) {
	var sum Money
	for _, a := range amounts {
		m, err := FromString(a)
		if err != nil {
			return 0, err
		}
		sum += Split(m, n)[i]
	}
	return sum, nil
}
