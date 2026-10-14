//go:build !windows

package platform

import (
	"context"
	"os/exec"
	"strings"
)

// ShellCommand is the command line for exe and its arguments in settings.json.
func ShellCommand(exe string, args ...string) string {
	return strings.Join(append([]string{unixQuote(exe)}, args...), " ")
}

// ShellCmd runs a settings command line the way Claude Code does.
func ShellCmd(ctx context.Context, cmdline string) *exec.Cmd {
	return exec.CommandContext(ctx, "sh", "-c", cmdline)
}
