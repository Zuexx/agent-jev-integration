package billing

import "testing"

func TestCalc(t *testing.T) {
	if calc([]int{1, 2}) != 3 {
		t.Fatal("calc")
	}
}
