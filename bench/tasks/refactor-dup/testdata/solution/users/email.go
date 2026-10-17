package users

import (
	"regexp"
	"strings"
)

var emailRE = regexp.MustCompile(`^[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}$`)

// validEmail normalises an address and reports whether it is valid.
func validEmail(email string) (string, bool) {
	email = strings.TrimSpace(strings.ToLower(email))
	return email, emailRE.MatchString(email)
}
