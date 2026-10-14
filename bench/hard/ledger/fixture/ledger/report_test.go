package ledger

import "testing"

func TestMonthlyReport(t *testing.T) {
	amounts := []string{"0.29", "19.99", "4.10", "100"}
	total, err := Total(amounts)
	if err != nil || total.String() != "124.38" {
		t.Fatalf("total %v %v", total, err)
	}
	var owed Money
	for i := 0; i < 3; i++ {
		s, _ := Share(amounts, 3, i)
		owed += s
	}
	if owed != total {
		t.Fatalf("shares add up to %v, total is %v", owed, total)
	}
}
