package semver

import "testing"

func TestHiddenOrder(t *testing.T) {
	order := []string{"1.0.0-0", "1.0.0-1", "1.0.0-0A", "1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta", "1.0.0-beta.2", "1.0.0-beta.11", "1.0.0-rc.1", "1.0.0", "1.9.0", "1.10.0", "2.0.0"}
	for i := range order {
		for j := range order {
			want := 0
			if i < j {
				want = -1
			} else if i > j {
				want = 1
			}
			got, err := Compare(order[i], order[j])
			if err != nil || got != want {
				t.Errorf("Compare(%q,%q)=%d,%v want %d", order[i], order[j], got, err, want)
			}
		}
	}
}

func TestHiddenBuildIgnored(t *testing.T) {
	for _, p := range [][2]string{{"1.0.0+build.1", "1.0.0"}, {"1.0.0-alpha+001", "1.0.0-alpha"}, {"1.0.0+01", "1.0.0+exp.sha.5114f85"}} {
		if got, err := Compare(p[0], p[1]); err != nil || got != 0 {
			t.Errorf("Compare(%q,%q)=%d,%v want 0", p[0], p[1], got, err)
		}
	}
}

func TestHiddenInvalid(t *testing.T) {
	for _, v := range []string{"", "1", "1.0", "1.0.0.0", "01.0.0", "1.02.0", "v1.0.0", "1.0.0-", "1.0.0-01", "1.0.0-a..b", "1.0.0+", "1.0.0+a..b", "1.0.0-a_b", "-1.0.0", "1.0.0 "} {
		if _, err := Compare(v, "1.0.0"); err == nil {
			t.Errorf("Compare(%q) accepted an invalid version", v)
		}
		if _, err := Compare("1.0.0", v); err == nil {
			t.Errorf("Compare(_, %q) accepted an invalid version", v)
		}
	}
}
