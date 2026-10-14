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
	"github.com/zuexx/agent-jev-integration/internal/router"
)

// Skill-listing conditions compared by SkillEval.
const (
	CondListing  = "listing"   // Claude Code as is: names and descriptions
	CondNames    = "names"     // skillOverrides name-only for every skill
	CondNamesJev = "names+jev" // name-only + the jevcc hook naming the skill
)

type SkillRun struct {
	Prompt string  `json:"prompt"`
	Label  string  `json:"label"`
	Cond   string  `json:"cond"`
	Loaded string  `json:"loaded"` // first Skill tool call, "" if none
	Tokens int     `json:"first_request_input_tokens"`
	Cost   float64 `json:"cost_usd"`
	MS     int64   `json:"ms"`
	Error  string  `json:"error,omitempty"`
}

type SkillEvalOptions struct {
	Claude, Model, HookCmd string
	Skills                 []string
	Workers                int
}

func (o SkillEvalOptions) settings(cond string) (string, []string) {
	doc := map[string]any{}
	env := []string{"JEVCC_MODE=off"}
	if cond != CondListing {
		ov := map[string]string{}
		for _, s := range o.Skills {
			ov[s] = "name-only"
		}
		doc["skillOverrides"] = ov
	}
	if cond == CondNamesJev {
		doc["hooks"] = map[string]any{"UserPromptSubmit": []any{map[string]any{
			"hooks": []any{map[string]any{"type": "command", "command": o.HookCmd, "timeout": 15}},
		}}}
		env = []string{"JEVCC_MODE=on", "JEVCC_DELEGATE=off"}
	}
	b, _ := json.Marshal(doc)
	return string(b), env
}

// SkillEval sends each labelled prompt to `claude -p --max-turns 1` under each
// condition and records which skill the first response loads and how many
// input tokens the first request carried.
func SkillEval(ctx context.Context, items []Item, conds []string, o SkillEvalOptions) []SkillRun {
	type job struct {
		it   Item
		cond string
	}
	var jobs []job
	for _, it := range items {
		if it.Skill == "" {
			continue
		}
		for _, c := range conds {
			jobs = append(jobs, job{it, c})
		}
	}
	out := make([]SkillRun, len(jobs))
	jev.Parallel(len(jobs), o.Workers, func(i int) {
		j := jobs[i]
		out[i] = o.one(ctx, j.it, j.cond)
	})
	return out
}

func (o SkillEvalOptions) one(ctx context.Context, it Item, cond string) SkillRun {
	r := SkillRun{Prompt: it.Prompt, Label: it.Skill, Cond: cond}
	dir, err := os.MkdirTemp("", "jevcc-skilleval-")
	if err != nil {
		r.Error = err.Error()
		return r
	}
	defer os.RemoveAll(dir)
	settings, env := o.settings(cond)
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, o.Claude, "-p", "--model", o.Model, "--max-turns", "1", "--output-format", "stream-json", "--verbose",
		"--permission-mode", "bypassPermissions", "--no-session-persistence", "--settings", settings, it.Prompt)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	start := time.Now()
	raw, _ := cmd.Output()
	r.MS = time.Since(start).Milliseconds()
	first := true
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 1<<20), 1<<26)
	for sc.Scan() {
		var line struct {
			Type    string  `json:"type"`
			Cost    float64 `json:"total_cost_usd"`
			Message struct {
				Content []struct {
					Type  string         `json:"type"`
					Name  string         `json:"name"`
					Input map[string]any `json:"input"`
				} `json:"content"`
				Usage struct {
					In, CacheW, CacheR int
					Input              int `json:"input_tokens"`
					CacheCreation      int `json:"cache_creation_input_tokens"`
					CacheRead          int `json:"cache_read_input_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &line) != nil {
			continue
		}
		switch line.Type {
		case "assistant":
			if first {
				u := line.Message.Usage
				r.Tokens = u.Input + u.CacheCreation + u.CacheRead
				first = false
			}
			for _, b := range line.Message.Content {
				if b.Type == "tool_use" && b.Name == "Skill" && r.Loaded == "" {
					r.Loaded, _ = b.Input["skill"].(string)
				}
			}
		case "result":
			r.Cost = line.Cost
		}
	}
	if first {
		r.Error = "no assistant message"
	}
	return r
}

// skillMatch accepts "superpowers:tdd" for a label "tdd" and the reverse.
func skillMatch(it Item, loaded string) bool {
	if it.SkillOK(loaded) {
		return true
	}
	if loaded == "" || it.Skill == router.None {
		return false
	}
	for _, s := range strings.Split(it.Skill, "|") {
		if strings.HasSuffix(s, ":"+loaded) || strings.HasSuffix(loaded, ":"+s) {
			return true
		}
	}
	return false
}

// SkillReport renders the comparison.
func SkillReport(runs []SkillRun, conds []string, model string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Skill listing: hide descriptions, let Jev name the skill\n\nAgent: `claude -p --model %s --max-turns 1`, first response only, in an empty directory. Tokens are the first request's input (system prompt + skill listing + prompt).\n\n", model)
	b.WriteString("| condition | runs | input tokens / turn (mean) | vs listing | wrong or missed load (a skill fits) | needless load (none fits) | cost / run |\n|---|---|---|---|---|---|---|\n")
	base := 0.0
	for _, c := range conds {
		var tok, cost float64
		n, wk, wn, nk, nn, errs := 0, 0, 0, 0, 0, 0
		for _, r := range runs {
			if r.Cond != c {
				continue
			}
			if r.Error != "" {
				errs++
				continue
			}
			n++
			tok += float64(r.Tokens)
			cost += r.Cost
			it := Item{Skill: r.Label}
			if r.Label == router.None {
				nn++
				if r.Loaded != "" {
					nk++
				}
			} else {
				wn++
				if !skillMatch(it, r.Loaded) {
					wk++
				}
			}
		}
		if n == 0 {
			continue
		}
		mean := tok / float64(n)
		delta := "—"
		if c == CondListing {
			base = mean
		} else if base > 0 {
			delta = fmt.Sprintf("%+.0f (%+.1f%%)", mean-base, 100*(mean-base)/base)
		}
		fmt.Fprintf(&b, "| %s | %d (+%d errors) | %.0f | %s | %s | %s | $%.4f |\n", c, n, errs, mean, delta, rate(wk, wn), rate(nk, nn), cost/float64(n))
	}
	return b.String()
}
