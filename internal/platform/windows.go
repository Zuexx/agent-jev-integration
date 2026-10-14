//go:build windows

package platform

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// ShellCommand quotes exe for Git Bash: forward slashes, and $ and ` escaped
// inside double quotes, so C:\Users\me\go\bin\jevcc.exe survives the shell.
func ShellCommand(exe string, args ...string) string {
	p := strings.NewReplacer(`\`, "/", `$`, `\$`, "`", "\\`").Replace(exe)
	return strings.Join(append([]string{`"` + p + `"`}, args...), " ")
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// GitBash finds the Git Bash Claude Code uses ("" if none). It never returns
// System32\bash.exe, which is WSL.
func GitBash() string {
	if p := os.Getenv("CLAUDE_CODE_GIT_BASH_PATH"); fileExists(p) {
		return p
	}
	if git, err := exec.LookPath("git"); err == nil {
		dir := filepath.Dir(git)
		for _, rel := range []string{`..\bin\bash.exe`, `..\..\bin\bash.exe`, `bash.exe`} {
			if p := filepath.Join(dir, rel); fileExists(p) {
				return p
			}
		}
	}
	for _, env := range []string{"ProgramFiles", "ProgramW6432", "LOCALAPPDATA"} {
		base := os.Getenv(env)
		if env == "LOCALAPPDATA" {
			base = filepath.Join(base, "Programs")
		}
		if p := filepath.Join(base, "Git", "bin", "bash.exe"); base != "" && fileExists(p) {
			return p
		}
	}
	return ""
}

// ShellCmd runs a settings command line through Git Bash (cmd as a fallback),
// in a hidden console so no window flashes up.
func ShellCmd(ctx context.Context, cmdline string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "cmd", "/C", cmdline)
	if bash := GitBash(); bash != "" {
		cmd = exec.CommandContext(ctx, bash, "-c", cmdline)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	return cmd
}
