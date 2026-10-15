package bench

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// An isolated run gets its own HOME, CLAUDE_CONFIG_DIR and JEVCC_HOME, so the
// evaluator's plugins, hooks, skills, MCP servers, memory and CLAUDE.md cannot
// reach it, and it cannot write into the evaluator's setup. The init event of
// the stream says what Claude Code actually loaded; checkIsolation turns
// anything outside the allow-list into a violation and the run is discarded.

// passEnv are the evaluator's variables an isolated run may see.
var passEnv = []string{"PATH", "LANG", "LC_ALL", "LC_CTYPE", "TERM", "TMPDIR", "USER", "LOGNAME", "SHELL", "BLENDER"}

type sandbox struct{ Root, Home, ConfigDir, JevHome string }

func newSandbox() (sandbox, error) {
	root, err := os.MkdirTemp("", "jevcc-iso-")
	if err != nil {
		return sandbox{}, err
	}
	sb := sandbox{Root: root, Home: filepath.Join(root, "home"), JevHome: filepath.Join(root, "jevcc")}
	sb.ConfigDir = filepath.Join(sb.Home, ".claude")
	for _, d := range []string{sb.ConfigDir, sb.JevHome} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			os.RemoveAll(root)
			return sandbox{}, err
		}
	}
	return sb, nil
}

// env keeps only passEnv from base, points the setup directories into the
// sandbox, and adds extra (credentials).
func (s sandbox) env(base, extra []string) []string {
	var out []string
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		for _, p := range passEnv {
			if k == p {
				out = append(out, kv)
			}
		}
	}
	out = append(out, "HOME="+s.Home, "CLAUDE_CONFIG_DIR="+s.ConfigDir, "JEVCC_HOME="+s.JevHome)
	return append(out, extra...)
}

// Allow is what a clean Claude Code loads by itself (bench/isolation-allow.json):
// its built-in plugins, agents and bundled skills. Everything a run adds on purpose
// (the arm's agents, hook events, a task's MCP server) is passed separately.
type Allow struct {
	Plugins []string `json:"plugins"` // only ones Claude Code reports as path "builtin"
	Agents  []string `json:"agents"`
	Skills  []string `json:"skills"`
	MCP     []string `json:"mcp_servers,omitempty"`
}

func LoadAllow(path string) (Allow, error) {
	var a Allow
	b, err := os.ReadFile(path)
	if err != nil {
		return a, err
	}
	return a, json.Unmarshal(b, &a)
}

// named reads either "x" or {"name":"x",...}; the init event uses both forms.
type named string

func (n *named) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*n = named(s)
		return nil
	}
	var o struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(b, &o); err != nil {
		return err
	}
	*n = named(o.Name)
	return nil
}

type plugin struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// Init is the part of the stream's init event the isolation check reads.
type Init struct {
	Version     string            `json:"claude_code_version"`
	Model       string            `json:"model"`
	Plugins     []plugin          `json:"plugins"`
	MCPServers  []named           `json:"mcp_servers"`
	Agents      []named           `json:"agents"`
	Skills      []named           `json:"skills"`
	MemoryPaths map[string]string `json:"memory_paths"`
}

// Quota is the subscription usage Claude Code reported during a run (the
// last rate_limit_event): utilization of the 5-hour and 7-day windows and
// when each resets (unix seconds).
type Quota struct {
	FiveHour      float64 `json:"five_hour"`
	SevenDay      float64 `json:"seven_day"`
	FiveHourReset int64   `json:"five_hour_reset"`
	SevenDayReset int64   `json:"seven_day_reset"`
}

func parseQuota(out []byte) *Quota {
	var q *Quota
	for _, line := range bytes.Split(out, []byte("\n")) {
		var e struct {
			Type string `json:"type"`
			Info struct {
				Windows map[string]struct {
					Utilization float64 `json:"utilization"`
					ResetsAt    int64   `json:"resetsAt"`
				} `json:"unifiedWindows"`
			} `json:"rate_limit_info"`
		}
		if json.Unmarshal(line, &e) != nil || e.Type != "rate_limit_event" || len(e.Info.Windows) == 0 {
			continue
		}
		five, seven := e.Info.Windows["five_hour"], e.Info.Windows["seven_day"]
		q = &Quota{five.Utilization, seven.Utilization, five.ResetsAt, seven.ResetsAt}
	}
	return q
}

// parseStream splits stream-json output into the init event, the names of
// the hooks that started, and the final result line.
func parseStream(out []byte) (*Init, []string, []byte) {
	var in *Init
	var hooks []string
	var result []byte
	for _, line := range bytes.Split(out, []byte("\n")) {
		var head struct {
			Type     string `json:"type"`
			Subtype  string `json:"subtype"`
			HookName string `json:"hook_name"`
		}
		if json.Unmarshal(line, &head) != nil {
			continue
		}
		switch {
		case head.Type == "system" && head.Subtype == "init" && in == nil:
			var i Init
			if json.Unmarshal(line, &i) == nil {
				in = &i
			}
		case head.Type == "system" && head.Subtype == "hook_started":
			hooks = append(hooks, head.HookName)
		case head.Type == "result":
			result = line
		}
	}
	return in, hooks, result
}

// checkIsolation lists everything the run loaded that it should not have.
// agents and events are what the arm itself adds (--agents, hook events).
func checkIsolation(in *Init, hooks []string, sb sandbox, allow Allow, agents, events []string) []string {
	if in == nil {
		return []string{"no init event in the stream"}
	}
	var v []string
	has := func(list []string, x string) bool {
		for _, y := range list {
			if y == x {
				return true
			}
		}
		return false
	}
	for _, p := range in.Plugins {
		if p.Path != "builtin" || !has(allow.Plugins, p.Name) {
			v = append(v, fmt.Sprintf("plugin %s", p.Name))
		}
	}
	for _, m := range in.MCPServers {
		if !has(allow.MCP, string(m)) {
			v = append(v, fmt.Sprintf("mcp server %s", m))
		}
	}
	for _, a := range in.Agents {
		if !has(allow.Agents, string(a)) && !has(agents, string(a)) {
			v = append(v, fmt.Sprintf("agent %s", a))
		}
	}
	for _, s := range in.Skills {
		if !has(allow.Skills, string(s)) {
			v = append(v, fmt.Sprintf("skill %s", s))
		}
	}
	for _, h := range hooks {
		ev, _, _ := strings.Cut(h, ":")
		if !has(events, ev) {
			v = append(v, fmt.Sprintf("hook %s", h))
		}
	}
	for _, p := range in.MemoryPaths {
		if !strings.HasPrefix(p, sb.Root) {
			v = append(v, fmt.Sprintf("memory path %s", p))
		}
	}
	return v
}

// ProbeAllow runs one trivial isolated session and returns the agents and
// skills a clean Claude Code loads by itself, to be reviewed and saved as the
// allow-list. Plugins, MCP servers, hooks or memory outside the sandbox are
// never allowed, so they come back as violations instead.
func (r *Runner) ProbeAllow(ctx context.Context) (Allow, []string, error) {
	sb, err := newSandbox()
	if err != nil {
		return Allow{}, nil, err
	}
	defer os.RemoveAll(sb.Root)
	work := filepath.Join(sb.Root, "work")
	os.MkdirAll(work, 0o700)
	cmd := exec.CommandContext(ctx, r.Claude, "-p", "--output-format", "stream-json", "--verbose", "--strict-mcp-config",
		"--no-session-persistence", "--model", "haiku", "Reply with the word ok.")
	cmd.Dir = work
	cmd.Env = append(sb.env(os.Environ(), r.PassEnv), "JEVCC_MODE=off")
	out, err := cmd.Output()
	in, hooks, result := parseStream(out)
	if in == nil {
		return Allow{}, nil, fmt.Errorf("no init event (claude: %v) %s", err, tail(string(result), 300))
	}
	// Signed-out sessions load fewer skills, so a failed login would give a
	// wrong allow-list.
	var res struct {
		IsError bool   `json:"is_error"`
		Result  string `json:"result"`
	}
	if json.Unmarshal(result, &res) != nil || res.IsError {
		return Allow{}, nil, fmt.Errorf("probe session failed: %s", tail(res.Result, 300))
	}
	var a Allow
	for _, p := range in.Plugins {
		if p.Path == "builtin" {
			a.Plugins = append(a.Plugins, p.Name)
		}
	}
	sort.Strings(a.Plugins)
	for _, x := range in.Agents {
		a.Agents = append(a.Agents, string(x))
	}
	for _, x := range in.Skills {
		a.Skills = append(a.Skills, string(x))
	}
	sort.Strings(a.Agents)
	sort.Strings(a.Skills)
	return a, checkIsolation(in, hooks, sb, a, nil, nil), nil
}
