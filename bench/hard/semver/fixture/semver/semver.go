// Package semver compares version strings.
package semver

// Compare compares two Semantic Versioning 2.0.0 versions by precedence.
// It returns -1 if a < b, 0 if they have equal precedence, and +1 if a > b,
// or an error if either string is not a valid version.
func Compare(a, b string) (int, error) {
	panic("not implemented")
}
