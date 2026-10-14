// Package verify is the Stop-hook check: when a (cheap) session says it is
// done, Jev reads the turn and decides whether to escalate it to a stronger
// subagent. This is the "extract cheap, verify with Jev, escalate on a signal"
// cascade from TypeSafe's SDE cookbook, applied to coding turns.
package verify

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/zuexx/agent-jev-integration/internal/jev"
)

type Asker interface {
	Ask(ctx context.Context, state any, qs map[string]jev.Question) (*jev.Response, error)
}

// Turn is what the agent did since the last typed prompt.
type Turn struct {
	Request  string   `json:"request"`
	Final    string   `json:"agent_final_message"`
	Commands []string `json:"commands_run"`
	Outputs  []string `json:"last_tool_outputs"`
}

const (
	maxOut      = 1500
	keepOutputs = 4
	keepCmds    = 12
)

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n/2] + "\n…\n" + s[len(s)-n/2:]
}

// ReadTurn extracts the last turn from a Claude Code transcript.
func ReadTurn(path string) (Turn, bool) {
	f, err := os.Open(path)
	if err != nil {
		return Turn{}, false
	}
	defer f.Close()
	var t Turn
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<26)
	for sc.Scan() {
		var line struct {
			Type    string `json:"type"`
			IsMeta  bool   `json:"isMeta"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &line) != nil || line.IsMeta {
			continue
		}
		var s string
		if json.Unmarshal(line.Message.Content, &s) == nil {
			if line.Type == "user" && !systemMessage(s) {
				t = Turn{Request: s}
			} else if line.Type == "assistant" {
				t.Final = s
			}
			continue
		}
		var blocks []struct {
			Type    string          `json:"type"`
			Text    string          `json:"text"`
			Name    string          `json:"name"`
			Input   map[string]any  `json:"input"`
			Content json.RawMessage `json:"content"`
		}
		if json.Unmarshal(line.Message.Content, &blocks) != nil {
			continue
		}
		for _, b := range blocks {
			switch {
			case line.Type == "user" && b.Type == "text" && !systemMessage(b.Text):
				t = Turn{Request: b.Text}
			case line.Type == "user" && b.Type == "tool_result":
				t.Outputs = append(t.Outputs, clip(resultText(b.Content), maxOut))
			case line.Type == "assistant" && b.Type == "text" && strings.TrimSpace(b.Text) != "":
				t.Final = b.Text
			case line.Type == "assistant" && b.Type == "tool_use":
				t.Commands = append(t.Commands, describe(b.Name, b.Input))
			}
		}
	}
	if len(t.Outputs) > keepOutputs {
		t.Outputs = t.Outputs[len(t.Outputs)-keepOutputs:]
	}
	if len(t.Commands) > keepCmds {
		t.Commands = t.Commands[len(t.Commands)-keepCmds:]
	}
	t.Request = clip(t.Request, 6000)
	t.Final = clip(t.Final, 4000)
	return t, t.Request != "" && t.Final != ""
}

var systemTag = regexp.MustCompile(`^<[a-z][a-z0-9-]*[\s>]`)

// systemMessage: injected text (subagent hand-backs, notifications) that
// continues the current turn instead of starting a new one.
func systemMessage(s string) bool { return systemTag.MatchString(strings.TrimSpace(s)) }

func resultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(raw, &parts)
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p.Text)
	}
	return b.String()
}

func describe(name string, in map[string]any) string {
	for _, k := range []string{"command", "file_path", "pattern", "skill", "description"} {
		if v, ok := in[k].(string); ok {
			return name + ": " + clip(v, 200)
		}
	}
	return name
}

// EditedCode reports whether the turn wrote or edited files.
func (t Turn) EditedCode() bool {
	for _, c := range t.Commands {
		for _, p := range []string{"Write:", "Edit:", "MultiEdit:", "NotebookEdit:"} {
			if strings.HasPrefix(c, p) {
				return true
			}
		}
	}
	return false
}

// HandedOff reports whether the turn already gave work to a subagent.
func (t Turn) HandedOff() bool {
	for _, c := range t.Commands {
		if strings.HasPrefix(c, "Agent") || strings.HasPrefix(c, "Task") {
			return true
		}
	}
	return false
}

// Signal is one question; Bad says which side of the noul is the problem.
type Signal struct {
	ID       string
	Question string
	BadIfYes bool
	Reason   string
	// Recorded only: never escalates on its own (an agent honestly saying
	// "still investigating" is not a failure).
	InfoOnly bool
}

var Signals = []Signal{
	{"complete", "The agent did everything `request` asks: every stated rule, edge case and error case is handled, judging by `agent_final_message`, `commands_run` and `last_tool_outputs`.", false, "not every requirement looks handled", false},
	{"unverified", "The agent wrote or edited source code in this turn (see `commands_run`), and `last_tool_outputs` shows no passing test run after its last edit.", true, "code changed without a passing test run after the last edit", false},
	{"failing", "`last_tool_outputs` shows failing tests, compile errors or panics that the final message does not resolve.", true, "unresolved failures in the tool output", false},
	{"hedging", "`agent_final_message` admits uncertainty, skipped parts, assumptions about unclear rules, or leaves TODOs.", true, "the agent hedges or skipped parts", true},
	{"untested_rules", "The agent wrote or edited source code in this turn, and `request` (or the doc comment it points to) lists specific rules or edge cases that the agent's own tests, as far as `commands_run` shows, do not clearly exercise.", true, "specified rules that its tests may not cover", false},
}

type Verdict struct {
	Risk    float64            `json:"risk"`
	Scores  map[string]float64 `json:"scores"`
	Reasons []string           `json:"reasons,omitempty"`
	Tokens  int                `json:"jev_input_tokens"`
	CostUSD float64            `json:"jev_cost_usd"`
	MS      int64              `json:"jev_latency_ms"`
}

// Check asks every signal in one call. Risk is the strongest problem signal.
func Check(ctx context.Context, c Asker, t Turn, threshold float64) (*Verdict, error) {
	qs := map[string]jev.Question{}
	for _, s := range Signals {
		qs[s.ID] = jev.Noul(s.Question)
	}
	r, err := c.Ask(ctx, t, qs)
	if err != nil {
		return nil, err
	}
	v := &Verdict{Scores: map[string]float64{}, Tokens: r.Usage.InputTokens, CostUSD: r.Usage.CostUSD(), MS: r.Latency.Milliseconds()}
	type hit struct {
		p float64
		r string
	}
	var hits []hit
	for _, s := range Signals {
		p := r.Answers[s.ID].Noul
		v.Scores[s.ID] = p
		bad := p
		if !s.BadIfYes {
			bad = 1 - p
		}
		if s.InfoOnly {
			continue
		}
		if bad > v.Risk {
			v.Risk = bad
		}
		if bad >= threshold {
			hits = append(hits, hit{bad, s.Reason})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].p > hits[j].p })
	for _, h := range hits {
		v.Reasons = append(v.Reasons, fmt.Sprintf("%s (%.2f)", h.r, h.p))
	}
	return v, nil
}

// Escalation is the Stop-hook reason that makes Claude continue.
func Escalation(v *Verdict, agent string) string {
	return fmt.Sprintf("Jev review of this turn flagged: %s. The user asks you to escalate before finishing: spawn the %s subagent (Agent tool, subagent_type: %q) with the original request word for word and a summary of what you changed; ask it to check every requirement against the code, fix what is wrong, and run the tests. Then report its result. This standing request counts as the user asking for a subagent.", strings.Join(v.Reasons, "; "), agent, agent)
}
