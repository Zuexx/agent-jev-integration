// Package paths resolves the directories jevcc and Claude Code use.
package paths

import (
	"os"
	"path/filepath"
)

// Home returns the user's home directory ("." if it cannot be determined).
func Home() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return h
}

// StateDir is where jevcc keeps its own state: $JEVCC_HOME or ~/.jevcc.
func StateDir() string {
	if d := os.Getenv("JEVCC_HOME"); d != "" {
		return d
	}
	return filepath.Join(Home(), ".jevcc")
}

// ClaudeDir is Claude Code's config directory: $CLAUDE_CONFIG_DIR or ~/.claude.
func ClaudeDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	return filepath.Join(Home(), ".claude")
}

func DecisionLog() string { return filepath.Join(StateDir(), "decisions.jsonl") }

func SessionFile(id string) string {
	return filepath.Join(StateDir(), "sessions", filepath.Base(id)+".json")
}
