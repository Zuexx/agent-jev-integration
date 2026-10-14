package platform

import (
	"context"
	"runtime"
	"strings"
	"testing"
)

func TestShellCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		if got := ShellCommand(`C:\Users\a b\jevcc.exe`, "hook"); got != `"C:/Users/a b/jevcc.exe" hook` {
			t.Fatal(got)
		}
		return
	}
	if got := ShellCommand("/a b/jevcc", "hook"); got != "'/a b/jevcc' hook" {
		t.Fatal(got)
	}
	if got := ShellCommand("/x/jevcc", "statusline"); got != "/x/jevcc statusline" {
		t.Fatal(got)
	}
}

func TestShellCmd(t *testing.T) {
	out, err := ShellCmd(context.Background(), "echo hi").Output()
	if err != nil || strings.TrimSpace(string(out)) != "hi" {
		t.Fatalf("%q %v", out, err)
	}
}
