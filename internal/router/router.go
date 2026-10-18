// Package router asks Jev which model tier and which skill fit a prompt.
//
// Skill selection follows TypeSafe's skill-suggestion cookbook: call 1 ranks
// every skill and asks gate questions ("does this need a skill at all?");
// call 2 re-reads the top candidates with their SKILL.md opening and may reject
// all of them.
package router

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/zuexx/agent-jev-integration/internal/config"
	"github.com/zuexx/agent-jev-integration/internal/jev"
	"github.com/zuexx/agent-jev-integration/internal/roster"
)

const (
	None         = "none"
	Shortlist    = 3
	maxOptions   = 255
	maxPromptLen = 8000
	descLen      = 240
	// MemoryQuestion asks whether a message holds something worth keeping
	// beyond this session (TypeSafe "Simple memory system" cookbook's gate).
	MemoryQuestion = "`request` states a lasting preference, rule, convention or fact about the user or this project that should still apply in future sessions, not just an instruction for the current task."
	// BulkThreshold is the "many items, same question" noul above which the
	// hook points Claude at jevcc classify.
	BulkThreshold = 0.6
	// CodeThreshold is the p above which a request counts as software work
	// for delegation (delegate_scope "code").
	CodeThreshold = 0.8
)

// CodeQuestion asks whether a request is software work. In the evaluation,
// delegating down saved money only on coding tasks (docs/eval-report.md).
const CodeQuestion = "`request` is software work: writing, changing, fixing, testing, debugging or reviewing program code, scripts or tests."

type Asker interface {
	Ask(ctx context.Context, state any, qs map[string]jev.Question) (*jev.Response, error)
}

type Decision struct {
	Tier       string             `json:"tier"`
	TierConf   float64            `json:"tier_confidence"`
	TierProbs  map[string]float64 `json:"tier_probs,omitempty"`
	Gate       float64            `json:"skill_gate"`
	Candidates []string           `json:"skill_candidates,omitempty"`
	Skill      string             `json:"skill,omitempty"`
	// SkillDesc is the picked skill's description, repeated in the advice
	// because a skill hidden to "name-only" shows Claude no description.
	SkillDesc string `json:"-"`
	// Picked is the verification winner before the SkillFit threshold.
	Picked   string  `json:"skill_picked,omitempty"`
	SkillFit float64 `json:"skill_fit,omitempty"`
	Bulk     float64 `json:"bulk"`
	Remember float64 `json:"remember"`
	// Code is Jev's p that the request is software work; delegation down is
	// limited to it unless delegate_scope is "all".
	Code      float64 `json:"code"`
	Calls     int     `json:"jev_calls"`
	Tokens    int     `json:"jev_input_tokens"`
	CostUSD   float64 `json:"jev_cost_usd"`
	LatencyMS int64   `json:"jev_latency_ms"`
	Model     string  `json:"jev_model,omitempty"`
}

func (d *Decision) account(r *jev.Response) {
	d.Calls++
	d.Tokens += r.Usage.InputTokens
	d.CostUSD += r.Usage.CostUSD()
	d.LatencyMS += r.Latency.Milliseconds()
	d.Model = r.Model
}

func clip(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}

// Route makes one or two Jev calls. skills may be empty.
func Route(ctx context.Context, c Asker, cfg *config.Config, prompt string, skills []roster.Skill) (*Decision, error) {
	d := &Decision{}
	state := map[string]string{"request": clip(prompt, maxPromptLen)}
	tiers := map[string]any{}
	for _, t := range cfg.Tiers {
		tiers[t.Model] = t.Description
	}
	qs := map[string]jev.Question{
		"tier":     jev.Choice("A coding agent must complete `request`. Which model tier is the cheapest one that can complete it fully and at good quality?", tiers),
		"bulk":     jev.Noul("`request` asks to classify, filter, label, score or triage many items (dozens or more), asking the same question about each item."),
		"remember": jev.Noul(MemoryQuestion),
		"code":     jev.Noul(CodeQuestion),
	}
	if len(skills) > 0 {
		pool := skills
		var pre []*jev.Response
		if len(skills) > maxOptions-1 {
			var err error
			if pool, pre, err = narrowSkills(ctx, c, state, skills); err != nil {
				return nil, err
			}
		}
		opts := map[string]any{None: "No listed skill is relevant to the request."}
		for _, s := range pool {
			opts[s.Name] = clip(s.Description, descLen)
		}
		defer func() {
			for _, r := range pre {
				d.account(r)
			}
		}()
		qs["skill"] = jev.Choice("Which skill is most relevant to `request`?", opts)
		qs["act"] = jev.Noul("`request` asks the agent to act on the user's files, code, tools or accounts, not just to talk.")
		qs["steps"] = jev.Noul("`request` is a task that benefits from following a written procedure, workflow or checklist.")
		qs["talk"] = jev.Noul("`request` is only conversation, an opinion, or a general knowledge question.")
	}
	r1, err := c.Ask(ctx, state, qs)
	if err != nil {
		return nil, err
	}
	d.account(r1)
	t := r1.Answers["tier"]
	d.Tier, d.TierConf, d.TierProbs = t.Choice, t.Confidence, t.Probabilities
	d.Bulk = r1.Answers["bulk"].Noul
	d.Remember = r1.Answers["remember"].Noul
	d.Code = r1.Answers["code"].Noul
	if len(skills) == 0 {
		return d, nil
	}
	d.Gate = (r1.Answers["act"].Noul + r1.Answers["steps"].Noul + (1 - r1.Answers["talk"].Noul)) / 3
	for _, name := range r1.Answers["skill"].Ranked() {
		if name != None && len(d.Candidates) < Shortlist {
			d.Candidates = append(d.Candidates, name)
		}
	}
	if d.Gate < cfg.SkillGate || len(d.Candidates) == 0 {
		return d, nil
	}
	if err := verify(ctx, c, state, skills, d); err != nil {
		return d, err
	}
	if d.SkillFit < cfg.SkillFit {
		d.Skill = ""
	}
	return d, nil
}

// narrowSkills handles rosters over the 255-option limit with a two-level
// beam (TypeSafe's hierarchical classification cookbook): one Choice per chunk,
// then the top few of every chunk go on to the main call.
func narrowSkills(ctx context.Context, c Asker, state any, skills []roster.Skill) ([]roster.Skill, []*jev.Response, error) {
	const perChunk = 6
	by := map[string]roster.Skill{}
	for _, s := range skills {
		by[s.Name] = s
	}
	var keep []roster.Skill
	var resps []*jev.Response
	for i := 0; i < len(skills); i += maxOptions - 1 {
		chunk := skills[i:min(i+maxOptions-1, len(skills))]
		opts := map[string]any{None: "No listed skill is relevant to the request."}
		for _, s := range chunk {
			opts[s.Name] = clip(s.Description, descLen)
		}
		r, err := c.Ask(ctx, state, map[string]jev.Question{"skill": jev.Choice("Which skill is most relevant to `request`?", opts)})
		if err != nil {
			return nil, resps, err
		}
		resps = append(resps, r)
		n := 0
		for _, name := range r.Answers["skill"].Ranked() {
			if name == None {
				continue
			}
			keep = append(keep, by[name])
			if n++; n == perChunk {
				break
			}
		}
	}
	if len(keep) > maxOptions-1 {
		keep = keep[:maxOptions-1]
	}
	return keep, resps, nil
}

func verify(ctx context.Context, c Asker, state map[string]string, skills []roster.Skill, d *Decision) error {
	by := map[string]roster.Skill{}
	for _, s := range skills {
		by[s.Name] = s
	}
	opts := map[string]any{None: "None of these skills fits what the request actually asks for."}
	qs := map[string]jev.Question{}
	for i, name := range d.Candidates {
		s := by[name]
		detail := map[string]string{"name": s.Name, "description": s.Description, "instructions_opening": s.Excerpt}
		opts[name] = detail
		qs[fmt.Sprintf("fit%d", i)] = jev.Noul(map[string]any{
			"skill":    detail,
			"question": "Would loading `skill` directly help the agent do what `request` asks?",
		})
	}
	qs["pick"] = jev.Choice("Which skill should the agent load for `request`?", opts)
	r2, err := c.Ask(ctx, state, qs)
	if err != nil {
		return err
	}
	d.account(r2)
	pick := r2.Answers["pick"].Choice
	for i, name := range d.Candidates {
		if name == pick {
			d.Skill, d.Picked, d.SkillDesc = pick, pick, by[name].Description
			d.SkillFit = r2.Answers[fmt.Sprintf("fit%d", i)].Noul
		}
	}
	return nil
}

// IsSystemMessage reports whether a "prompt" is text Claude Code injected
// (a subagent hand-back, a task notification, a reminder), not something the
// user typed. These must not be routed or reviewed as user requests.
func IsSystemMessage(p string) bool {
	return systemTag.MatchString(strings.TrimSpace(p))
}

var systemTag = regexp.MustCompile(`^<[a-z][a-z0-9-]*[\s>]`)

// TierOf maps a model id or alias ("claude-haiku-5-5", "sonnet") to a tier
// name, or "" when it matches none.
func TierOf(model string, cfg *config.Config) string {
	m := strings.ToLower(model)
	for _, n := range cfg.TierNames() {
		if strings.Contains(m, n) {
			return n
		}
	}
	return ""
}

// Context renders the advice block for Claude ("" when there is nothing to say).
// Only actionable lines are emitted, so a no-op decision costs no main-model tokens.
// Context is the routing block the hook adds to the prompt ("" for none).
func Context(d *Decision, cfg *config.Config) string {
	text, _ := Build(d, cfg)
	return text
}

// Build is Context plus the kinds of advice it holds: "tier", "escalate",
// "escalate_capped", "skill", "memory" or "bulk". Each kind is recorded at the
// place its line is added, so the log cannot drift from the wording.
func Build(d *Decision, cfg *config.Config) (string, []string) {
	var lines, kinds []string
	add := func(kind, line string) {
		lines = append(lines, line)
		kinds = append(kinds, kind)
	}
	names := cfg.TierNames()
	top := names[len(names)-1]
	escalated := false
	delegate := cfg.DelegateOn() && (cfg.DelegateAll() || d.Code >= CodeThreshold)
	if (delegate || cfg.EscalateOn()) && d.Tier != "" && d.TierConf >= min(cfg.MinConfidence, cfg.EscalateConfidence) {
		rank := map[string]int{}
		for i, n := range names {
			rank[n] = i
		}
		main := cfg.MainModel
		if _, ok := rank[main]; !ok {
			main = top
		}
		how := fmt.Sprintf("spawn the jev-%s subagent (Agent tool, subagent_type: %q)", d.Tier, "jev-"+d.Tier)
		if cfg.DelegateVia == "model" {
			how = fmt.Sprintf("spawn a subagent with the Agent tool, model: %q", d.Tier)
		}
		switch {
		case delegate && rank[d.Tier] < rank[main] && d.TierConf >= cfg.MinConfidence:
			add("tier", fmt.Sprintf("- Model: this is %s-level work (Jev p=%.2f). The user asks you to delegate it: unless you already run on %s or cheaper, %s, give it a self-contained brief, and relay its result. This standing request counts as the user asking for a subagent. Do it yourself only if it needs conversation context that would be costly to hand over.", d.Tier, d.TierProbs[d.Tier], d.Tier, how))
		case cfg.EscalateOn() && rank[d.Tier] > rank[main] && d.TierConf >= cfg.EscalateConfidence && cfg.EscalationsUsed >= cfg.EscalateMaxPerSession:
			add("escalate_capped", fmt.Sprintf("- Model: Jev rates this %s-level (p=%.2f), but this session already escalated %d times, the limit the user set. Do not spawn a subagent for it; tell the user in one line that it looks like %s-level work so they can switch with /model %s if they want, then do your best.", d.Tier, d.TierProbs[d.Tier], cfg.EscalationsUsed, d.Tier, d.Tier))
		case cfg.EscalateOn() && rank[d.Tier] > rank[main] && d.TierConf >= cfg.EscalateConfidence:
			skill := ""
			if d.Skill != "" {
				skill = fmt.Sprintf(" Tell it that the %s skill looks relevant so it can load it.", d.Skill)
			}
			add("escalate", fmt.Sprintf("- Model: this needs %s-level reasoning (Jev p=%.2f) and this session runs on %s. Do this FIRST, before loading skills, reading files or running commands: %s (exactly one; never several in parallel), give it the user's request word for word plus anything you already know, wait for its result and relay it.%s This standing request counts as the user asking for a subagent.", d.Tier, d.TierProbs[d.Tier], main, how, skill))
			escalated = true
		}
	}
	if d.Skill != "" && !escalated && cfg.SkillAdviceOn() {
		desc := ""
		if d.SkillDesc != "" {
			desc = " — " + clip(d.SkillDesc, 200)
		}
		add("skill", fmt.Sprintf("- Skill: %s%s. Jev rates it relevant (fit %.2f); load it with the Skill tool if it fits what the user asked, otherwise ignore this.", d.Skill, desc, d.SkillFit))
	}
	if cfg.MemoryThreshold > 0 && d.Remember >= cfg.MemoryThreshold {
		add("memory", fmt.Sprintf("- Memory: this message states something lasting (Jev p=%.2f). If it is not saved yet, save it to memory in one line (your memory directory, or CLAUDE.md for project rules), then carry on with the request.", d.Remember))
	}
	if d.Bulk >= BulkThreshold {
		add("bulk", "- Bulk judgment: for steps that ask the same question about many items, run `jevcc classify` (see /jev-classify) instead of judging items yourself, then review only low-confidence or top-ranked rows.")
	}
	if len(lines) == 0 {
		return "", nil
	}
	return "<jev_routing>\nThe user turned on Jev routing (jevcc); these are its suggestions for this request.\n" + strings.Join(lines, "\n") + "\n</jev_routing>", kinds
}
