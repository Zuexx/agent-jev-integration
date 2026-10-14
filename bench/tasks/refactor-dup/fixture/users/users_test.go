package users

import "testing"

func TestValidation(t *testing.T) {
	if Signup(" A@B.co ", "a") != nil || Signup("nope", "a") == nil || Signup("a@b.co", "") == nil {
		t.Fatal("signup")
	}
	if Invite("x@y.co", "A@B.co") != nil || Invite("x@y.co", "bad@") == nil || Invite("a@b.co", "a@b.co") == nil {
		t.Fatal("invite")
	}
}
