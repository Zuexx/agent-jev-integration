package billing

func calc(items []int) int {
	t := 0
	for _, i := range items {
		t += i
	}
	return t
}
