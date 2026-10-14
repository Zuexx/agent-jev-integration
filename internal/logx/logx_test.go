package logx

import (
	"os"
	"strings"
	"testing"
)

func TestPrintf(t *testing.T) {
	t.Setenv("JEVCC_HOME", t.TempDir())
	Printf("hello %d", 7)
	b, err := os.ReadFile(Path())
	if err != nil || !strings.Contains(string(b), "hello 7") {
		t.Fatal(string(b), err)
	}
}
