package users

import "errors"

func Invite(from, to string) error {
	to, ok := validEmail(to)
	if !ok {
		return errors.New("invalid email")
	}
	if from == to {
		return errors.New("cannot invite yourself")
	}
	return nil
}
