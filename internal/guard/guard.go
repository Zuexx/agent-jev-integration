// Package guard is the PreToolUse check: before Claude runs a command or
// writes a file, Jev answers a few hazard questions about it, and a risky call
// is turned into a confirmation prompt (never a silent block). It follows the
// shape of TypeSafe's "Guardrails for LLMs" cookbook: one request per action,
// one Noul per hazard, thresholds in code.
package guard

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/zuexx/agent-jev-integration/internal/jev"
)

type Asker interface {
	Ask(ctx context.Context, state any, qs map[string]jev.Question) (*jev.Response, error)
}

type Hazard struct {
	ID, Question, Label string
}

var Hazards = []Hazard{
	{"destructive", "The action deletes, truncates or overwrites files or data that are not throwaway build output in the current project, or irreversibly rewrites version-control history (force push, reset --hard, branch -D, clean -fd).", "deletes or overwrites data / rewrites history"},
	{"exfiltration", "The action sends secrets, credentials, environment variables, SSH keys, or local files to a remote host or third-party service.", "sends secrets or local files off the machine"},
	{"system", "The action changes the machine outside the project: sudo, global installs, system settings, shell startup files, credentials stores, cron jobs, or piping a downloaded script into a shell.", "changes the system outside the project"},
	{"secret_written", "The action writes a hard-coded password, API key, token or private key into a file.", "writes a secret into a file"},
}

// Action is the tool call Jev sees.
type Action struct {
	Tool    string `json:"tool"`
	Command string `json:"command,omitempty"`
	File    string `json:"file,omitempty"`
	Content string `json:"content_excerpt,omitempty"`
	Cwd     string `json:"working_directory,omitempty"`
}

// Guarded reports whether a tool can change anything; read-only tools are not checked.
func Guarded(tool string) bool {
	switch tool {
	case "Bash", "Write", "Edit", "MultiEdit", "NotebookEdit":
		return true
	}
	return false
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// FromToolInput builds the Action for a PreToolUse payload.
func FromToolInput(tool string, in map[string]any, cwd string) Action {
	a := Action{Tool: tool, Cwd: cwd}
	str := func(k string) string { s, _ := in[k].(string); return s }
	a.Command = clip(str("command"), 4000)
	a.File = str("file_path")
	if a.File == "" {
		a.File = str("notebook_path")
	}
	for _, k := range []string{"content", "new_string", "new_source"} {
		if v := str(k); v != "" {
			a.Content = clip(v, 3000)
			break
		}
	}
	return a
}

type Verdict struct {
	Risk    float64            `json:"risk"`
	Scores  map[string]float64 `json:"scores"`
	Reasons []string           `json:"reasons,omitempty"`
	MS      int64              `json:"jev_latency_ms"`
	CostUSD float64            `json:"jev_cost_usd"`
}

func Check(ctx context.Context, c Asker, a Action, threshold float64) (*Verdict, error) {
	qs := map[string]jev.Question{}
	for _, h := range Hazards {
		qs[h.ID] = jev.Noul(map[string]any{"question": h.Question, "about": "the agent's pending `tool` call described in the state"})
	}
	r, err := c.Ask(ctx, a, qs)
	if err != nil {
		return nil, err
	}
	v := &Verdict{Scores: map[string]float64{}, MS: r.Latency.Milliseconds(), CostUSD: r.Usage.CostUSD()}
	type hit struct {
		p float64
		l string
	}
	var hits []hit
	for _, h := range Hazards {
		p := r.Answers[h.ID].Noul
		v.Scores[h.ID] = p
		if p > v.Risk {
			v.Risk = p
		}
		if p >= threshold {
			hits = append(hits, hit{p, h.Label})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].p > hits[j].p })
	for _, h := range hits {
		v.Reasons = append(v.Reasons, fmt.Sprintf("%s (%.2f)", h.l, h.p))
	}
	return v, nil
}

func Reason(v *Verdict) string {
	return "Jev guard: this action " + strings.Join(v.Reasons, "; ") + ". Confirm only if you intended it."
}
