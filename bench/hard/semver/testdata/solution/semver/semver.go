// Package semver compares version strings.
package semver

import (
	"errors"
	"strconv"
	"strings"
)

type ver struct {
	nums [3]uint64
	pre  []string
}

func ident(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '-') {
			return false
		}
	}
	return true
}

func numeric(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

func parse(s string) (ver, error) {
	var v ver
	bad := errors.New("invalid version: " + s)
	if i := strings.IndexByte(s, '+'); i >= 0 {
		for _, id := range strings.Split(s[i+1:], ".") {
			if !ident(id) {
				return v, bad
			}
		}
		s = s[:i]
	}
	if i := strings.IndexByte(s, '-'); i >= 0 {
		for _, id := range strings.Split(s[i+1:], ".") {
			if !ident(id) || (numeric(id) && len(id) > 1 && id[0] == '0') {
				return v, bad
			}
			v.pre = append(v.pre, id)
		}
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return v, bad
	}
	for i, p := range parts {
		if !numeric(p) || (len(p) > 1 && p[0] == '0') {
			return v, bad
		}
		n, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return v, bad
		}
		v.nums[i] = n
	}
	return v, nil
}

func cmpInt(a, b uint64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// Compare compares two Semantic Versioning 2.0.0 versions by precedence.
func Compare(a, b string) (int, error) {
	va, err := parse(a)
	if err != nil {
		return 0, err
	}
	vb, err := parse(b)
	if err != nil {
		return 0, err
	}
	for i := 0; i < 3; i++ {
		if c := cmpInt(va.nums[i], vb.nums[i]); c != 0 {
			return c, nil
		}
	}
	switch {
	case len(va.pre) == 0 && len(vb.pre) == 0:
		return 0, nil
	case len(va.pre) == 0:
		return 1, nil
	case len(vb.pre) == 0:
		return -1, nil
	}
	for i := 0; i < len(va.pre) && i < len(vb.pre); i++ {
		x, y := va.pre[i], vb.pre[i]
		nx, ny := numeric(x), numeric(y)
		switch {
		case nx && ny:
			ix, _ := strconv.ParseUint(x, 10, 64)
			iy, _ := strconv.ParseUint(y, 10, 64)
			if c := cmpInt(ix, iy); c != 0 {
				return c, nil
			}
		case nx:
			return -1, nil
		case ny:
			return 1, nil
		default:
			if c := strings.Compare(x, y); c != 0 {
				return c, nil
			}
		}
	}
	return cmpInt(uint64(len(va.pre)), uint64(len(vb.pre))), nil
}
