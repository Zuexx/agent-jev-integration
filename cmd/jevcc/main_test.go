package main

import "testing"

func TestUnknownCommand(t *testing.T) {
	if realMain(nil) != 2 || realMain([]string{"nope"}) != 2 {
		t.Fatal("expected usage exit")
	}
}

func TestOneLine(t *testing.T) {
	if got := oneLine("a\n  b   c", 3); got != "a b…" {
		t.Fatal(got)
	}
}
