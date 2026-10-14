package eval

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/zuexx/agent-jev-integration/internal/jev"
)

// EscalateRun records what a haiku session's first response did with the
// Jev routing advice: which subagent it handed the prompt to, if any.
type EscalateRun struct {
	Prompt  string  `json:"prompt"`
	Label   string  `json:"label"`
	Advice  string  `json:"advice"`  // the jev_routing block the hook added, "" if none
	Handoff string  `json:"handoff"` // subagent_type of the first Agent call, "" if none
	First   string  `json:"first_tool"`
	Cost    float64 `json:"cost_usd"`
	Error   string  `json:"error,omitempty"`
}

type EscalateOptions struct {
	Claude, HookCmd, Agents string
	Workers                 int
	MaxTurns                int
	// AdviceFor returns the advice the hook logged for a session ("tier", …).
	AdviceFor func(sessionID string) (tier string, advice []string)
}

// EscalateEval runs each prompt once on haiku with the routing hook in
// escalation mode and reports whether the first response hands off.
func EscalateEval(ctx context.Context, items []Item, o EscalateOptions) []EscalateRun {
	out := make([]EscalateRun, len(items))
	settings := fmt.Sprintf(`{"hooks":{"UserPromptSubmit":[{"hooks":[{"type":"command","command":%q,"timeout":15}]}]}}`, o.HookCmd)
	jev.Parallel(len(items), o.Workers, func(i int) {
		it := items[i]
		r := EscalateRun{Prompt: it.Prompt, Label: it.Tier}
		dir, err := os.MkdirTemp("", "jevcc-escalateval-")
		if err != nil {
			r.Error = err.Error()
			out[i] = r
			return
		}
		defer os.RemoveAll(dir)
		c, cancel := context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(c, o.Claude, "-p", "--model", "haiku", "--max-turns", fmt.Sprint(max(1, o.MaxTurns)), "--output-format", "stream-json", "--verbose",
			"--permission-mode", "bypassPermissions", "--no-session-persistence", "--max-budget-usd", "0.3", "--settings", settings, "--agents", o.Agents, it.Prompt)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "JEVCC_MODE=on", "JEVCC_DELEGATE=agent", "JEVCC_MAIN=haiku", "JEVCC_VERIFY=0")
		raw, _ := cmd.Output()
		session := ""
		sc := bufio.NewScanner(bytes.NewReader(raw))
		sc.Buffer(make([]byte, 1<<20), 1<<26)
		for sc.Scan() {
			var line struct {
				SessionID string  `json:"session_id"`
				Type      string  `json:"type"`
				Subtype   string  `json:"subtype"`
				Cost      float64 `json:"total_cost_usd"`
				Output    string  `json:"output"`
				Message   struct {
					Content []struct {
						Type  string         `json:"type"`
						Name  string         `json:"name"`
						Input map[string]any `json:"input"`
					} `json:"content"`
				} `json:"message"`
			}
			if json.Unmarshal(sc.Bytes(), &line) != nil {
				continue
			}
			if line.SessionID != "" && session == "" {
				session = line.SessionID
			}
			if line.Type == "assistant" {
				for _, b := range line.Message.Content {
					if b.Type != "tool_use" {
						continue
					}
					if r.First == "" {
						r.First = b.Name
					}
					if (b.Name == "Agent" || b.Name == "Task") && r.Handoff == "" {
						r.Handoff, _ = b.Input["subagent_type"].(string)
						if r.Handoff == "" {
							r.Handoff = "general"
						}
					}
				}
			}
			if line.Type == "result" {
				r.Cost = line.Cost
			}
		}
		if o.AdviceFor != nil && session != "" {
			tier, adv := o.AdviceFor(session)
			for _, a := range adv {
				if a == "tier" {
					r.Advice = tier + "-level"
				}
			}
		}
		out[i] = r
	})
	return out
}

// EscalateReport summarises hand-offs by label.
func EscalateReport(runs []EscalateRun) string {
	var b strings.Builder
	b.WriteString("# Does a haiku session hand off when Jev says escalate?\n\nUp to the first few agent turns (`claude -p --model haiku --max-turns N`), routing hook in escalation mode (main = haiku, delegate via jev-<tier> subagents).\n\n")
	b.WriteString("| label | prompts | Jev advised opus | handed to jev-opus when advised | handed to jev-opus overall | any hand-off | cost / run |\n|---|---|---|---|---|---|---|\n")
	for _, lab := range []string{"opus", "sonnet", "haiku"} {
		n, adv, follow, toOpus, any := 0, 0, 0, 0, 0
		var cost float64
		for _, r := range runs {
			if r.Label != lab || r.Error != "" {
				continue
			}
			n++
			cost += r.Cost
			advisedOpus := strings.Contains(r.Advice, "opus-level")
			if advisedOpus {
				adv++
				if r.Handoff == "jev-opus" {
					follow++
				}
			}
			if r.Handoff == "jev-opus" {
				toOpus++
			}
			if r.Handoff != "" {
				any++
			}
		}
		if n == 0 {
			continue
		}
		fmt.Fprintf(&b, "| %s | %d | %d | %s | %s | %s | $%.4f |\n", lab, n, adv, rate(follow, adv), rate(toOpus, n), rate(any, n), cost/float64(n))
	}
	return b.String()
}
