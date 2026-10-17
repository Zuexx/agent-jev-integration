package billing

import "testing"

func TestCalc(t *testing.T) {
	if calculateTotal([]int{1, 2}) != 3 {
		t.Fatal("calc")
	}
}
