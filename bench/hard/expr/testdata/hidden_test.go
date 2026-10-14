package calc

import "testing"

func TestHiddenValues(t *testing.T) {
	for in, want := range map[string]int64{
		"1+2*3": 7, "(1+2)*3": 9, "2^3^2": 512, "-2^2": -4, "(-2)^2": 4, "2^-0": 1,
		"7/-2": -3, "-7/2": -3, "-7%3": -1, "7%-3": 1, "2--3": 5, "2- -3": 5, "--3": 3,
		"+-+3": -3, " 10 - 4 - 3 ": 3, "100/10/5": 2, "2*3%4": 2, "-3^2*2": -18, "0^0": 1,
		"(((4)))": 4, "2^10-1": 1023,
	} {
		got, err := Eval(in)
		if err != nil || got != want {
			t.Errorf("Eval(%q)=%d,%v want %d", in, got, err, want)
		}
	}
}

func TestHiddenErrors(t *testing.T) {
	for _, in := range []string{"", "1+", "()", "(1", "1)", "3/(1-1)", "5%0", "2^-1", "1 2", "a+1", "*3", "2^^3"} {
		if _, err := Eval(in); err == nil {
			t.Errorf("Eval(%q) should fail", in)
		}
	}
}
