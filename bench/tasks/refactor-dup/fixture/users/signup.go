package users

import (
	"errors"
	"regexp"
	"strings"
)

func Signup(email, name string) error {
	email = strings.TrimSpace(strings.ToLower(email))
	re := regexp.MustCompile(`^[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}$`)
	if !re.MatchString(email) {
		return errors.New("invalid email")
	}
	if name == "" {
		return errors.New("name required")
	}
	return nil
}
