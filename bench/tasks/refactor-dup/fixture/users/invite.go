package users

import (
	"errors"
	"regexp"
	"strings"
)

func Invite(from, to string) error {
	to = strings.TrimSpace(strings.ToLower(to))
	re := regexp.MustCompile(`^[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}$`)
	if !re.MatchString(to) {
		return errors.New("invalid email")
	}
	if from == to {
		return errors.New("cannot invite yourself")
	}
	return nil
}
