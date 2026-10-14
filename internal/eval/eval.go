// Package eval measures the router offline against a hand-labelled prompt set
// and a frozen skill roster, optionally next to an LLM router baseline.
package eval

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/zuexx/agent-jev-integration/internal/config"
	"github.com/zuexx/agent-jev-integration/internal/jev"
	"github.com/zuexx/agent-jev-integration/internal/roster"
	"github.com/zuexx/agent-jev-integration/internal/router"
	"github.com/zuexx/agent-jev-integration/internal/stats"
)

// Item is one labelled prompt. Skill "" means "not labelled for skills";
// "none" means no skill should be suggested; "a|b" accepts either skill
// (rosters often hold overlapping skills).
type Item struct {
	Prompt string `json:"prompt"`
	Tier   string `json:"tier"`
	Skill  string `json:"skill,omitempty"`
}

type Pred struct {
	Tier      string  `json:"tier"`
	Conf      float64 `json:"confidence"`
	Skill     string  `json:"skill"`
	Picked    string  `json:"skill_picked,omitempty"`
	Fit       float64 `json:"skill_fit,omitempty"`
	LatencyMS int64   `json:"latency_ms"`
	CostUSD   float64 `json:"cost_usd"`
	Error     string  `json:"error,omitempty"`
}

type Row struct {
	Item     Item  `json:"item"`
	Jev      Pred  `json:"jev"`
	Baseline *Pred `json:"baseline,omitempty"`
}

// SkillOK reports whether a predicted skill matches the label.
func (it Item) SkillOK(pred string) bool {
	if it.Skill == router.None {
		return pred == ""
	}
	for _, s := range strings.Split(it.Skill, "|") {
		if s == pred {
			return true
		}
	}
	return false
}

func LoadItems(path string) ([]Item, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Item
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		var it Item
		if err := json.Unmarshal([]byte(line), &it); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, n, err)
		}
		out = append(out, it)
	}
	return out, sc.Err()
}

// RunJev routes every item with the real router.
func RunJev(ctx context.Context, c router.Asker, cfg *config.Config, items []Item, skills []roster.Skill, workers int) []Row {
	rows := make([]Row, len(items))
	jev.Parallel(len(items), workers, func(i int) {
		rows[i].Item = items[i]
		start := time.Now()
		d, err := router.Route(ctx, c, cfg, items[i].Prompt, skills)
		p := Pred{LatencyMS: time.Since(start).Milliseconds()}
		if err != nil {
			p.Error = err.Error()
		}
		if d != nil {
			p.Tier, p.Conf, p.Skill, p.CostUSD = d.Tier, d.TierConf, d.Skill, d.CostUSD
			p.Picked, p.Fit = d.Picked, d.SkillFit
			p.LatencyMS = d.LatencyMS
		}
		rows[i].Jev = p
	})
	return rows
}

// BaselinePrompt is the system prompt for the LLM router baseline: the same
// tier descriptions and skill list Jev sees.
func BaselinePrompt(cfg *config.Config, skills []roster.Skill) string {
	var b strings.Builder
	b.WriteString("You route requests for a coding agent. Reply with only a JSON object {\"tier\": \"...\", \"skill\": \"...\"} and nothing else.\n")
	b.WriteString("tier: the cheapest model tier that can complete the request fully and at good quality. Options:\n")
	for _, t := range cfg.Tiers {
		fmt.Fprintf(&b, "- %s: %s\n", t.Model, t.Description)
	}
	b.WriteString("skill: the one skill the agent should load for this request, or \"none\" if no skill is clearly relevant. Skills:\n")
	for _, s := range skills {
		fmt.Fprintf(&b, "- %s: %s\n", s.Name, strings.Join(strings.Fields(s.Description), " "))
	}
	return b.String()
}

// RunBaseline asks `claude -p --model <model>` the same question, with tools,
// MCP and slash commands off so its cost is the routing call itself.
func RunBaseline(ctx context.Context, claude, model, system string, rows []Row, workers int) {
	jev.Parallel(len(rows), workers, func(i int) {
		p := Pred{}
		start := time.Now()
		cmd := exec.CommandContext(ctx, claude, "-p", "--model", model, "--system-prompt", system, "--tools", "",
			"--strict-mcp-config", "--disable-slash-commands", "--no-session-persistence", "--setting-sources", "",
			"--output-format", "json", rows[i].Item.Prompt)
		cmd.Env = append(os.Environ(), "JEVCC_MODE=off")
		out, err := cmd.Output()
		p.LatencyMS = time.Since(start).Milliseconds()
		var res struct {
			Result     string  `json:"result"`
			Cost       float64 `json:"total_cost_usd"`
			DurationMS int64   `json:"duration_ms"`
		}
		if err == nil {
			err = json.Unmarshal(out, &res)
		}
		if err != nil {
			p.Error = err.Error()
		} else {
			p.CostUSD = res.Cost
			p.Tier, p.Skill = parseBaseline(res.Result)
		}
		rows[i].Baseline = &p
	})
}

func parseBaseline(s string) (tier, skill string) {
	i, j := strings.Index(s, "{"), strings.LastIndex(s, "}")
	if i < 0 || j < i {
		return "", ""
	}
	var v struct{ Tier, Skill string }
	_ = json.Unmarshal([]byte(s[i:j+1]), &v)
	if v.Skill == router.None {
		v.Skill = ""
	}
	return v.Tier, v.Skill
}

// Wilson returns the 95% Wilson score interval for k successes out of n.
func Wilson(k, n int) (lo, hi float64) {
	if n == 0 {
		return 0, 0
	}
	const z = 1.96
	p := float64(k) / float64(n)
	nn := float64(n)
	den := 1 + z*z/nn
	c := (p + z*z/(2*nn)) / den
	h := z * math.Sqrt(p*(1-p)/nn+z*z/(4*nn*nn)) / den
	return math.Max(0, c-h), math.Min(1, c+h)
}

func rate(k, n int) string {
	if n == 0 {
		return "n/a"
	}
	lo, hi := Wilson(k, n)
	return fmt.Sprintf("%.1f%% (%d/%d, 95%% CI %.0f–%.0f%%)", 100*float64(k)/float64(n), k, n, 100*lo, 100*hi)
}

type side struct {
	name string
	get  func(Row) *Pred
}

// Report renders the comparison as Markdown.
func Report(rows []Row, cfg *config.Config, rosterSize int) string {
	order := map[string]int{}
	for i, n := range cfg.TierNames() {
		order[n] = i
	}
	sides := []side{{"jev", func(r Row) *Pred { return &r.Jev }}}
	if len(rows) > 0 && rows[0].Baseline != nil {
		sides = append(sides, side{"llm baseline", func(r Row) *Pred { return r.Baseline }})
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Router eval\n\n%d labelled prompts, %d skills in the frozen roster, tiers %s.\n", len(rows), rosterSize, strings.Join(cfg.TierNames(), " < "))
	b.WriteString("Labels are hand-written judgements, not ground truth from runs; read the tier numbers as agreement with that judgement.\n\n")
	b.WriteString("| metric | " + joinNames(sides) + " |\n|---|" + strings.Repeat("---|", len(sides)) + "\n")
	line := func(name string, f func(side) string) {
		cells := []string{}
		for _, s := range sides {
			cells = append(cells, f(s))
		}
		fmt.Fprintf(&b, "| %s | %s |\n", name, strings.Join(cells, " | "))
	}
	count := func(s side, keep func(Row, *Pred) bool, hit func(Row, *Pred) bool) string {
		k, n := 0, 0
		for _, r := range rows {
			p := s.get(r)
			if p.Error != "" || !keep(r, p) {
				continue
			}
			n++
			if hit(r, p) {
				k++
			}
		}
		return rate(k, n)
	}
	all := func(Row, *Pred) bool { return true }
	line("tier exact match", func(s side) string {
		return count(s, all, func(r Row, p *Pred) bool { return p.Tier == r.Item.Tier })
	})
	line("under-routed (cheaper than label: quality risk)", func(s side) string {
		return count(s, all, func(r Row, p *Pred) bool { return order[p.Tier] < order[r.Item.Tier] })
	})
	line("over-routed (pricier than label: wasted money)", func(s side) string {
		return count(s, all, func(r Row, p *Pred) bool { return order[p.Tier] > order[r.Item.Tier] })
	})
	confident := func(r Row, p *Pred) bool { return p.Conf >= cfg.MinConfidence }
	line(fmt.Sprintf("advice coverage (confidence ≥ %.2f)", cfg.MinConfidence), func(s side) string {
		if s.name != "jev" {
			return "n/a"
		}
		return count(s, all, confident)
	})
	line("tier exact match when confident", func(s side) string {
		if s.name != "jev" {
			return "n/a"
		}
		return count(s, confident, func(r Row, p *Pred) bool { return p.Tier == r.Item.Tier })
	})
	line("skill: wrong pick on prompts a skill covers", func(s side) string {
		return count(s, func(r Row, _ *Pred) bool { return r.Item.Skill != "" && r.Item.Skill != router.None },
			func(r Row, p *Pred) bool { return !r.Item.SkillOK(p.Skill) })
	})
	line("skill: needless pick when none fits", func(s side) string {
		return count(s, func(r Row, _ *Pred) bool { return r.Item.Skill == router.None },
			func(r Row, p *Pred) bool { return p.Skill != "" })
	})
	line("latency p50 / p95", func(s side) string {
		var xs []float64
		for _, r := range rows {
			if p := s.get(r); p.Error == "" {
				xs = append(xs, float64(p.LatencyMS))
			}
		}
		return fmt.Sprintf("%.0f / %.0f ms", stats.Percentile(xs, 50), stats.Percentile(xs, 95))
	})
	line("cost per decision (mean)", func(s side) string {
		var sum float64
		n := 0
		for _, r := range rows {
			if p := s.get(r); p.Error == "" {
				sum += p.CostUSD
				n++
			}
		}
		return fmt.Sprintf("$%.6f", sum/float64(max(n, 1)))
	})
	line("errors", func(s side) string {
		n := 0
		for _, r := range rows {
			if s.get(r).Error != "" {
				n++
			}
		}
		return fmt.Sprint(n)
	})
	for _, s := range sides {
		fmt.Fprintf(&b, "\n### Confusion matrix: %s (rows = label, columns = predicted)\n\n", s.name)
		names := cfg.TierNames()
		b.WriteString("| label \\ pred | " + strings.Join(names, " | ") + " | other |\n|---|" + strings.Repeat("---|", len(names)+1) + "\n")
		for _, l := range names {
			cells := []string{}
			other := 0
			counts := map[string]int{}
			for _, r := range rows {
				if r.Item.Tier != l {
					continue
				}
				p := s.get(r)
				if _, ok := order[p.Tier]; ok {
					counts[p.Tier]++
				} else {
					other++
				}
			}
			for _, n := range names {
				cells = append(cells, fmt.Sprint(counts[n]))
			}
			fmt.Fprintf(&b, "| %s | %s | %d |\n", l, strings.Join(cells, " | "), other)
		}
	}
	b.WriteString("\n### Misses (jev)\n\n")
	var misses []string
	for _, r := range rows {
		if r.Jev.Tier != r.Item.Tier || (r.Item.Skill != "" && !r.Item.SkillOK(r.Jev.Skill)) {
			skillLabel := r.Item.Skill
			if skillLabel == "" {
				skillLabel = "-"
			}
			misses = append(misses, fmt.Sprintf("- tier %s→%s (conf %.2f), skill %s→%q: %s", r.Item.Tier, r.Jev.Tier, r.Jev.Conf, skillLabel, r.Jev.Skill, oneLine(r.Item.Prompt, 90)))
		}
	}
	sort.Strings(misses)
	if len(misses) == 0 {
		b.WriteString("none\n")
	}
	b.WriteString(strings.Join(misses, "\n"))
	return b.String()
}

func joinNames(ss []side) string {
	var n []string
	for _, s := range ss {
		n = append(n, s.name)
	}
	return strings.Join(n, " | ")
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// SaveRows writes the raw per-item results next to the report.
func SaveRows(path string, rows []Row) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	for _, r := range rows {
		if err := enc.Encode(r); err != nil {
			return err
		}
	}
	return nil
}

// Sweep re-scores Jev's stored answers at other thresholds, without new calls:
// the tier advice gate (min confidence) and the skill fit threshold.
func Sweep(rows []Row, cfg *config.Config, confs, fits []float64) string {
	order := map[string]int{}
	for i, n := range cfg.TierNames() {
		order[n] = i
	}
	top := cfg.TierNames()[len(cfg.TierNames())-1]
	var b strings.Builder
	b.WriteString("### Threshold sweep (same Jev answers, re-scored)\n\n")
	b.WriteString("Tier advice is only given below the top tier and at confidence ≥ threshold. \"Bad advice\" = advised a tier cheaper than the label.\n\n")
	b.WriteString("| min confidence | prompts advised | advice correct | bad advice (too cheap) | missed savings (label cheaper than top, no advice) |\n|---|---|---|---|---|\n")
	for _, c := range confs {
		adv, ok, bad, missed, cheapLabels := 0, 0, 0, 0, 0
		for _, r := range rows {
			p := r.Jev
			if p.Error != "" {
				continue
			}
			advised := p.Tier != top && p.Conf >= c
			if r.Item.Tier != top {
				cheapLabels++
				if !advised {
					missed++
				}
			}
			if !advised {
				continue
			}
			adv++
			if p.Tier == r.Item.Tier {
				ok++
			}
			if order[p.Tier] < order[r.Item.Tier] {
				bad++
			}
		}
		fmt.Fprintf(&b, "| %.2f | %d/%d | %s | %s | %d/%d |\n", c, adv, len(rows), rate(ok, adv), rate(bad, adv), missed, cheapLabels)
	}
	b.WriteString("\n| skill fit ≥ | wrong pick (covered) | needless pick (none fits) |\n|---|---|---|\n")
	for _, f := range fits {
		wk, wn, nk, nn := 0, 0, 0, 0
		for _, r := range rows {
			p := r.Jev
			if p.Error != "" || r.Item.Skill == "" {
				continue
			}
			pred := ""
			if p.Picked != "" && p.Fit >= f {
				pred = p.Picked
			}
			if r.Item.Skill == router.None {
				nn++
				if pred != "" {
					nk++
				}
			} else {
				wn++
				if !r.Item.SkillOK(pred) {
					wk++
				}
			}
		}
		fmt.Fprintf(&b, "| %.2f | %s | %s |\n", f, rate(wk, wn), rate(nk, nn))
	}
	return b.String()
}
