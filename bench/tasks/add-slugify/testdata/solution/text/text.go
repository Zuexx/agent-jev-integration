// Package text holds small string helpers.
package text

import "strings"

// Title upper-cases the first letter of every word.
func Title(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

// Slugify keeps ASCII letters (lower-cased) and digits, turns every run of
// anything else into one '-', and trims '-' from both ends.
func Slugify(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + 'a' - 'A')
			dash = false
		default:
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = true
		}
	}
	return strings.TrimRight(b.String(), "-")
}
