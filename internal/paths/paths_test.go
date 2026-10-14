package paths

import (
	"path/filepath"
	"testing"
)

func TestStateDirOverride(t *testing.T) {
	t.Setenv("JEVCC_HOME", "/x/y")
	if got := StateDir(); got != "/x/y" {
		t.Fatal(got)
	}
}

func TestSessionFileCannotEscape(t *testing.T) {
	t.Setenv("JEVCC_HOME", "/s")
	if got := SessionFile("../../etc/passwd"); got != filepath.Join("/s", "sessions", "passwd.json") {
		t.Fatal(got)
	}
}
