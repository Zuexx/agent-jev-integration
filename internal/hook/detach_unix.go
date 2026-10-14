//go:build !windows

package hook

import (
	"os/exec"
	"syscall"
)

// detach makes cmd outlive the hook process (its own session, so Claude Code
// ending the hook's process group does not stop it).
func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
