package users

import "errors"

func Signup(email, name string) error {
	if _, ok := validEmail(email); !ok {
		return errors.New("invalid email")
	}
	if name == "" {
		return errors.New("name required")
	}
	return nil
}
