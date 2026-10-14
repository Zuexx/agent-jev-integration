// Package platform holds the few OS differences jevcc has: how a command line
// for Claude Code's settings is quoted, and how a shell command is run.
// Claude Code runs hooks and the statusline through Git Bash on Windows.
package platform

import "strings"

// unixQuote single-quotes s when it has characters a shell would interpret.
func unixQuote(s string) string {
	if strings.ContainsAny(s, " '\"$`\\") {
		return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
	}
	return s
}
