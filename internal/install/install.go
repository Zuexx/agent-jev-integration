// Package install wires jevcc into Claude Code (hook + slash commands) at the
// scope the user picks, and undoes it.
package install

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/zuexx/agent-jev-integration/internal/config"
	"github.com/zuexx/agent-jev-integration/internal/fsutil"
	"github.com/zuexx/agent-jev-integration/internal/paths"
	"github.com/zuexx/agent-jev-integration/internal/platform"
	"github.com/zuexx/agent-jev-integration/internal/roster"
)

//go:embed commands/*.md agents/*.md
var commandFS embed.FS

const marker = "<!-- jevcc -->"

// Scope is where the hook and commands go.
//
//	user    ~/.claude/settings.json            every project
//	project <dir>/.claude/settings.json        shared with the repo
//	local   <dir>/.claude/settings.local.json  this checkout only (git-ignored)
type Scope string

const (
	User    Scope = "user"
	Project Scope = "project"
	Local   Scope = "local"
)

// Events are the hook events `jevcc hook` handles; each feature is inert until
// turned on (routing: /jev on, verify: "verify", guard: "guard").
var Events = []string{"UserPromptSubmit", "Stop", "PreToolUse", "PostToolUse"}

func ParseScope(s string) (Scope, error) {
	switch Scope(s) {
	case User, Project, Local:
		return Scope(s), nil
	}
	return "", fmt.Errorf("unknown scope %q (user, project, local)", s)
}

type Options struct {
	// DefaultModel, if set, becomes Claude Code's "model" in the scope's
	// settings (the previous value is recorded and restored on uninstall).
	DefaultModel string
	// OnByDefault sets default_on in ~/.jevcc/config.json.
	OnByDefault bool
	// AgentMCP installs the worker subagents without a tools list (agent_mcp).
	AgentMCP bool
	// Guard sets guard: true in ~/.jevcc/config.json.
	Guard bool
	// Statusline wraps the scope's statusLine with `jevcc statusline`.
	Statusline bool
	Exe        string
	Scope      Scope
	Dir        string // project dir for project/local
	Out        io.Writer
	Now        time.Time
}

func (o Options) settingsPath() string {
	switch o.Scope {
	case Project:
		return filepath.Join(o.Dir, ".claude", "settings.json")
	case Local:
		return filepath.Join(o.Dir, ".claude", "settings.local.json")
	}
	return filepath.Join(paths.ClaudeDir(), "settings.json")
}

func (o Options) commandsDir() string { return filepath.Join(o.claudeDir(), "commands") }
func (o Options) agentsDir() string   { return filepath.Join(o.claudeDir(), "agents") }

func (o Options) claudeDir() string {
	if o.Scope == User {
		return paths.ClaudeDir()
	}
	return filepath.Join(o.Dir, ".claude")
}

func quote(s string) string { return platform.ShellCommand(s) }

// HookCommand is the command line written into settings.json.
func HookCommand(exe string) string { return platform.ShellCommand(exe, "hook") }

func isOurs(cmd string) bool {
	return strings.Contains(cmd, "jevcc") && strings.HasSuffix(cmd, " hook")
}

func parseSettings(raw []byte) (map[string]any, error) {
	doc := map[string]any{}
	if len(bytes.TrimSpace(raw)) == 0 {
		return doc, nil
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("settings file is not valid JSON: %w", err)
	}
	return doc, nil
}

func withoutOurHooks(groups []any) []any {
	var out []any
	for _, g := range groups {
		gm, ok := g.(map[string]any)
		if !ok {
			out = append(out, g)
			continue
		}
		hs, _ := gm["hooks"].([]any)
		var kept []any
		for _, h := range hs {
			if hm, ok := h.(map[string]any); ok {
				if c, _ := hm["command"].(string); isOurs(c) {
					continue
				}
			}
			kept = append(kept, h)
		}
		if len(kept) == 0 {
			continue
		}
		gm["hooks"] = kept
		out = append(out, gm)
	}
	return out
}

func HasOurHook(raw []byte) bool {
	doc, err := parseSettings(raw)
	if err != nil {
		return false
	}
	hooks, _ := doc["hooks"].(map[string]any)
	ups, _ := hooks["UserPromptSubmit"].([]any)
	return countOurs(ups) > 0
}

func countOurs(groups []any) int {
	n := 0
	for _, g := range groups {
		gm, _ := g.(map[string]any)
		hs, _ := gm["hooks"].([]any)
		for _, h := range hs {
			hm, _ := h.(map[string]any)
			if c, _ := hm["command"].(string); isOurs(c) {
				n++
			}
		}
	}
	return n
}

// ApplySettings adds (or replaces) the jevcc UserPromptSubmit hook.
func ApplySettings(raw []byte, exe string) ([]byte, error) {
	doc, err := parseSettings(raw)
	if err != nil {
		return nil, err
	}
	hooks, _ := doc["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	// The same command handles both events: routing on UserPromptSubmit,
	// the (opt-in) verify check on Stop.
	for _, ev := range Events {
		groups, _ := hooks[ev].([]any)
		g := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": HookCommand(exe), "timeout": 15}}}
		switch ev {
		case "PreToolUse":
			g["matcher"] = "Bash|Write|Edit|MultiEdit|NotebookEdit"
		case "PostToolUse":
			g["matcher"] = "WebFetch|WebSearch|Read|mcp__.*"
		}
		hooks[ev] = append(withoutOurHooks(groups), g)
	}
	doc["hooks"] = hooks
	return fsutil.Marshal(doc)
}

func RevertSettings(raw []byte) ([]byte, error) {
	doc, err := parseSettings(raw)
	if err != nil {
		return nil, err
	}
	if hooks, ok := doc["hooks"].(map[string]any); ok {
		for _, ev := range Events {
			if countOurs(asSlice(hooks[ev])) == 0 {
				continue // leave events we never touched exactly as they are
			}
			if gs := withoutOurHooks(asSlice(hooks[ev])); len(gs) == 0 {
				delete(hooks, ev)
			} else {
				hooks[ev] = gs
			}
		}
		if len(hooks) == 0 {
			delete(doc, "hooks")
		}
	}
	return fsutil.Marshal(doc)
}

func asSlice(v any) []any { s, _ := v.([]any); return s }

// Commands renders the slash command files with the jevcc invocation.
func Commands(jevcc string) map[string][]byte {
	out := map[string][]byte{}
	entries, _ := fs.ReadDir(commandFS, "commands")
	for _, e := range entries {
		b, _ := commandFS.ReadFile("commands/" + e.Name())
		out[e.Name()] = bytes.ReplaceAll(b, []byte("{{JEVCC}}"), []byte(jevcc))
	}
	return out
}

// Agents returns the light subagents the delegation advice names.
func Agents() map[string][]byte { return AgentsFor(false) }

// AgentsFor returns the subagents; with mcp the workers (not jev-review)
// drop their tools line and so inherit every tool of the session, MCP
// servers and Skill included.
func AgentsFor(mcp bool) map[string][]byte {
	out := map[string][]byte{}
	entries, _ := fs.ReadDir(commandFS, "agents")
	for _, e := range entries {
		b, _ := commandFS.ReadFile("agents/" + e.Name())
		if mcp && e.Name() != "jev-review.md" {
			b = toolsLine.ReplaceAll(b, nil)
		}
		out[e.Name()] = b
	}
	return out
}

var toolsLine = regexp.MustCompile(`(?m)^tools:.*\n`)

// WriteAgents rewrites the installed subagents in dir for the mcp setting.
// Only files jevcc installed there are touched.
func WriteAgents(dir string, mcp bool) (int, error) {
	n := 0
	for name, body := range AgentsFor(mcp) {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if err := fsutil.WriteFileAtomic(p, body, 0o644); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// UserAgentsDir is where a user-scope install puts the subagents.
func UserAgentsDir() string { return filepath.Join(paths.ClaudeDir(), "agents") }

// AgentsJSON renders the agents for `claude --agents` (used by the benchmark).
func AgentsJSON() string { return agentsJSON(false) }

// AgentsJSONOpenTools is AgentsJSON without the tools lists, so every agent
// inherits the session's tools, MCP and Skill included (the bench's tools
// comparison, docs/eval-plan.md Q4).
func AgentsJSONOpenTools() string { return agentsJSON(true) }

func agentsJSON(openTools bool) string {
	m := map[string]any{}
	for name, b := range Agents() {
		fm, body := roster.Frontmatter(string(b))
		a := map[string]any{"description": fm["description"], "prompt": strings.TrimSpace(body), "model": fm["model"]}
		if t := strings.ReplaceAll(fm["tools"], " ", ""); t != "" && !openTools {
			a["tools"] = strings.Split(t, ",")
		}
		m[strings.TrimSuffix(name, ".md")] = a
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// backupName turns a settings path into a file name that is valid on every
// OS (no separators, and no ':' as in Windows drive letters).
func backupName(p string) string {
	return strings.Trim(strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-':
			return r
		}
		return '_'
	}, p), "_")
}

// writeFollowingLinks writes through a symlink (dotfile managers) instead of replacing it.
func writeFollowingLinks(path string, data []byte, perm os.FileMode) error {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	return fsutil.WriteFileAtomic(path, data, perm)
}

func Install(o Options) error {
	sp := o.settingsPath()
	raw, err := os.ReadFile(sp)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err == nil {
		backup := filepath.Join(paths.StateDir(), "backup", backupName(sp)+"."+o.Now.Format("20060102-150405"))
		if err := fsutil.WriteFileAtomic(backup, raw, 0o600); err != nil {
			return err
		}
	}
	out, err := ApplySettings(raw, o.Exe)
	if err != nil {
		return fmt.Errorf("%s: %w", sp, err)
	}
	if err := writeFollowingLinks(sp, out, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(o.Out, "✓ hooks (%s) added to %s\n", strings.Join(Events, ", "), sp)
	for name, body := range Commands(quote(o.Exe)) {
		if err := fsutil.WriteFileAtomic(filepath.Join(o.commandsDir(), name), body, 0o644); err != nil {
			return err
		}
	}
	fmt.Fprintf(o.Out, "✓ /jev, /jev-classify, /jev-lint, /jev-review, /jev-audit added to %s\n", o.commandsDir())
	for name, body := range AgentsFor(o.AgentMCP) {
		if err := fsutil.WriteFileAtomic(filepath.Join(o.agentsDir(), name), body, 0o644); err != nil {
			return err
		}
	}
	var names []string
	for name := range Agents() {
		names = append(names, strings.TrimSuffix(name, ".md"))
	}
	sort.Strings(names)
	fmt.Fprintf(o.Out, "✓ subagents %s added to %s\n", strings.Join(names, ", "), o.agentsDir())
	if o.DefaultModel != "" {
		if err := setModel(o, o.DefaultModel); err != nil {
			return err
		}
		fmt.Fprintf(o.Out, "✓ Claude Code default model set to %s in %s (previous value kept for uninstall)\n", o.DefaultModel, sp)
	}
	if o.Statusline {
		if err := wrapStatusline(o); err != nil {
			return err
		}
	}
	if o.OnByDefault {
		if err := config.SetField("default_on", true); err != nil {
			return err
		}
		fmt.Fprintln(o.Out, "✓ Jev routing is on in every new session (/jev off turns it off per session)")
	}
	if o.AgentMCP {
		if err := config.SetField("agent_mcp", true); err != nil {
			return err
		}
		fmt.Fprintln(o.Out, "✓ jev-haiku/sonnet/opus get every session tool, MCP and Skill included (/jev mcp off reverts)")
	}
	if o.Guard {
		if err := config.SetField("guard", true); err != nil {
			return err
		}
		fmt.Fprintln(o.Out, "✓ guard on: risky commands and writes ask you first (/jev guard off turns it off)")
	}
	cfg, err := config.Load()
	if err == nil && cfg.APIKey == "" {
		fmt.Fprintln(o.Out, "  next: store your TypeSafe key with `jevcc key` (or export TYPESAFE_API_KEY)")
	}
	if o.OnByDefault {
		fmt.Fprintln(o.Out, "Done. New Claude Code sessions start with Jev on; /jev stats shows what it did.")
	} else {
		fmt.Fprintln(o.Out, "Done. In Claude Code type /jev on to start routing this session; /jev stats shows what it did.")
	}
	return nil
}

func isOurStatusline(cmd string) bool {
	return strings.Contains(cmd, "jevcc") && strings.HasSuffix(cmd, " statusline")
}

// WrapStatusline points statusLine at `jevcc statusline` and returns the
// command it replaced ("" if none, or if it was already ours).
func WrapStatusline(raw []byte, exe string) ([]byte, string, error) {
	doc, err := parseSettings(raw)
	if err != nil {
		return nil, "", err
	}
	sl, _ := doc["statusLine"].(map[string]any)
	if sl == nil {
		sl = map[string]any{}
	}
	inner := ""
	if c, _ := sl["command"].(string); c != "" && !isOurStatusline(c) {
		inner = c
	}
	sl["type"] = "command"
	sl["command"] = platform.ShellCommand(exe, "statusline")
	doc["statusLine"] = sl
	out, err := fsutil.Marshal(doc)
	return out, inner, err
}

// UnwrapStatusline puts the wrapped command back (or removes statusLine).
func UnwrapStatusline(raw []byte, inner string) ([]byte, error) {
	doc, err := parseSettings(raw)
	if err != nil {
		return nil, err
	}
	if sl, ok := doc["statusLine"].(map[string]any); ok {
		if c, _ := sl["command"].(string); isOurStatusline(c) {
			if inner == "" {
				delete(doc, "statusLine")
			} else {
				sl["command"] = inner
			}
		}
	}
	return fsutil.Marshal(doc)
}

func wrapStatusline(o Options) error {
	sp := o.settingsPath()
	raw, err := os.ReadFile(sp)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	out, inner, err := WrapStatusline(raw, o.Exe)
	if err != nil {
		return err
	}
	// Record the wrapped command before settings point at us: the running
	// claude re-renders the statusline at once.
	if inner != "" {
		if err := config.SetField("statusline_inner", inner); err != nil {
			return err
		}
	}
	if err := writeFollowingLinks(sp, out, 0o644); err != nil {
		return err
	}
	fmt.Fprintln(o.Out, "✓ statusline: one jev line added under your existing statusline")
	return nil
}

func Uninstall(o Options) error {
	sp := o.settingsPath()
	if raw, err := os.ReadFile(sp); err == nil {
		out, err := RevertSettings(raw)
		if err != nil {
			return fmt.Errorf("%s: %w", sp, err)
		}
		if err := writeFollowingLinks(sp, out, 0o644); err != nil {
			return err
		}
		fmt.Fprintln(o.Out, "✓ hook removed from", sp)
	}
	for name := range Commands("jevcc") {
		p := filepath.Join(o.commandsDir(), name)
		if b, err := os.ReadFile(p); err == nil && bytes.Contains(b, []byte(marker)) {
			os.Remove(p)
		}
	}
	fmt.Fprintln(o.Out, "✓ slash commands removed from", o.commandsDir())
	if err := restoreModel(o); err != nil {
		return err
	}
	if raw, err := os.ReadFile(o.settingsPath()); err == nil {
		cfg, _ := config.Load()
		inner := ""
		if cfg != nil {
			inner = cfg.StatuslineInner
		}
		if out, err := UnwrapStatusline(raw, inner); err == nil {
			if err := writeFollowingLinks(o.settingsPath(), out, 0o644); err != nil {
				return err
			}
		}
	}
	for name := range Agents() {
		p := filepath.Join(o.agentsDir(), name)
		if b, err := os.ReadFile(p); err == nil && bytes.Contains(b, []byte("name: jev-")) {
			os.Remove(p)
		}
	}
	fmt.Fprintln(o.Out, "Config, key and decision log in", paths.StateDir(), "were kept.")
	return nil
}

// Doctor checks every scope and returns 1 if jevcc is not usable.
func Doctor(o Options, out io.Writer) int {
	fails := 0
	check := func(ok bool, what, fix string) {
		if ok {
			fmt.Fprintln(out, "✓", what)
			return
		}
		fails++
		fmt.Fprintf(out, "✗ %s\n    fix: %s\n", what, fix)
	}
	cfg, err := config.Load()
	check(err == nil, "config readable ("+config.Path()+")", fmt.Sprint(err))
	if cfg != nil {
		check(cfg.APIKey != "", "TypeSafe API key present", "jevcc key   (or export TYPESAFE_API_KEY)")
	}
	_, err = exec.LookPath("claude")
	check(err == nil, "claude on PATH", "install Claude Code")
	installed := 0
	for _, s := range []Scope{User, Project, Local} {
		so := o
		so.Scope = s
		raw, err := os.ReadFile(so.settingsPath())
		if err == nil && HasOurHook(raw) {
			installed++
			_, cerr := os.Stat(filepath.Join(so.commandsDir(), "jev.md"))
			fmt.Fprintf(out, "✓ hook installed at %s scope (%s)\n", s, so.settingsPath())
			check(cerr == nil, "  /jev command file for "+string(s)+" scope", "jevcc install --scope "+string(s))
		}
	}
	plugin := pluginInstalled()
	if plugin {
		installed++
		fmt.Fprintln(out, "✓ jevcc plugin enabled")
	}
	check(installed > 0, "hook installed somewhere", "jevcc install [--scope user|project|local]  or install the plugin (see README)")
	if installed > 1 {
		fmt.Fprintln(out, "  note: installed in more than one place; each prompt is still routed once.")
	}
	if fails > 0 {
		return 1
	}
	return 0
}

func pluginInstalled() bool {
	var inst struct {
		Plugins map[string]any `json:"plugins"`
	}
	if fsutil.ReadJSON(filepath.Join(paths.ClaudeDir(), "plugins", "installed_plugins.json"), &inst) != nil {
		return false
	}
	for k := range inst.Plugins {
		if strings.HasPrefix(k, "jevcc@") {
			return true
		}
	}
	return false
}

func prevModelPath() string { return filepath.Join(paths.StateDir(), "previous-model.json") }

// setModel writes "model" into the scope's settings, remembering what was there.
func setModel(o Options, model string) error {
	sp := o.settingsPath()
	raw, err := os.ReadFile(sp)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	doc, err := parseSettings(raw)
	if err != nil {
		return err
	}
	prev := map[string]any{}
	_ = fsutil.ReadJSON(prevModelPath(), &prev)
	if _, done := prev[sp]; !done {
		old, _ := doc["model"].(string)
		prev[sp] = old
		if err := fsutil.WriteJSON(prevModelPath(), prev); err != nil {
			return err
		}
	}
	doc["model"] = model
	out, err := fsutil.Marshal(doc)
	if err != nil {
		return err
	}
	return writeFollowingLinks(sp, out, 0o644)
}

// restoreModel puts back the "model" setModel replaced for this scope.
func restoreModel(o Options) error {
	sp := o.settingsPath()
	prev := map[string]any{}
	if fsutil.ReadJSON(prevModelPath(), &prev) != nil {
		return nil
	}
	old, ok := prev[sp].(string)
	if !ok {
		return nil
	}
	raw, err := os.ReadFile(sp)
	if err != nil {
		return nil
	}
	doc, err := parseSettings(raw)
	if err != nil {
		return err
	}
	if old == "" {
		delete(doc, "model")
	} else {
		doc["model"] = old
	}
	out, err := fsutil.Marshal(doc)
	if err != nil {
		return err
	}
	if err := writeFollowingLinks(sp, out, 0o644); err != nil {
		return err
	}
	delete(prev, sp)
	fmt.Fprintln(o.Out, "✓ Claude Code default model restored in", sp)
	return fsutil.WriteJSON(prevModelPath(), prev)
}
