// Package bench runs the same coding tasks through Claude Code with routing
// off, with the hook on, and with head-less pre-routing, and compares cost,
// time and a deterministic pass/fail check per run.
package bench

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Task is bench/tasks/<name>/task.json; fixture/ next to it is copied into a
// fresh directory for every run.
type Task struct {
	Name     string `json:"-"`
	Dir      string `json:"-"`
	Prompt   string `json:"prompt"`
	Check    string `json:"check"` // sh -c in the work dir; exit 0 = pass
	Tier     string `json:"tier"`  // the tier the author expects; informational
	TimeoutS int    `json:"timeout_s,omitempty"`
	// FixtureFrom copies these paths (relative to the task, each to its base
	// name in the work dir) instead of fixture/, e.g. parts of this repo for
	// code-navigation tasks.
	FixtureFrom []string `json:"fixture_from,omitempty"`
}

const (
	ArmOff   = "off"   // plain claude, main model
	ArmHook  = "hook"  // main model + jevcc hook (JEVCC_MODE=on)
	ArmRoute = "route" // jevcc picks the model, claude runs on it
	// Hook arms that force delegation advice: to the general-purpose subagent
	// with a model override, or to the light jev-<tier> subagents.
	ArmDelegateModel = "delegate-model"
	ArmDelegateAgent = "delegate-agent"
	// Cheap main session: haiku alone, and haiku that escalates hard prompts
	// to the jev-sonnet / jev-opus subagents.
	ArmHaiku    = "haiku"
	ArmEscalate = "escalate"
	// Haiku whose finished turns Jev reviews (Stop hook), escalating risky
	// ones to jev-opus; and the same plus up-front escalation.
	ArmFiles          = "files" // haiku + hook with only the file hint
	ArmVerify         = "verify"
	ArmEscalateVerify = "escalate+verify"
)

func without(args []string, flag string) []string {
	var out []string
	for _, a := range args {
		if a != flag {
			out = append(out, a)
		}
	}
	return out
}

// armAdds reads back the agents (--agents) and hook events (--settings) an
// arm put on the command line, which an isolated run is allowed to load.
func armAdds(args []string) (agents, events []string) {
	for i := 0; i+1 < len(args); i++ {
		switch args[i] {
		case "--agents":
			var m map[string]any
			json.Unmarshal([]byte(args[i+1]), &m)
			for k := range m {
				agents = append(agents, k)
			}
		case "--settings":
			var s struct {
				Hooks map[string]any `json:"hooks"`
			}
			json.Unmarshal([]byte(args[i+1]), &s)
			for k := range s.Hooks {
				events = append(events, k)
			}
		}
	}
	return agents, events
}

func hookSettings(cmd string, events ...string) string {
	h := map[string]any{}
	for _, ev := range events {
		h[ev] = []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": cmd, "timeout": 15}}}}
	}
	b, _ := json.Marshal(map[string]any{"hooks": h})
	return string(b)
}

type Run struct {
	Task      string             `json:"task"`
	Arm       string             `json:"arm"`
	Rep       int                `json:"rep"`
	Model     string             `json:"model"` // --model passed to claude
	Pass      bool               `json:"pass"`
	CheckOut  string             `json:"check_output,omitempty"`
	ClaudeUSD float64            `json:"claude_cost_usd"`
	JevUSD    float64            `json:"jev_cost_usd"`
	DurMS     int64              `json:"duration_ms"`
	Turns     int                `json:"num_turns"`
	Subagents int                `json:"subagents"`
	ModelUSD  map[string]float64 `json:"model_cost_usd"`
	Tokens    int                `json:"tokens"`
	SessionID string             `json:"session_id"`
	Error     string             `json:"error,omitempty"`
	// Isolated runs only: what Claude Code reported loading, and anything
	// outside the allow-list (such a run is an error, not a result).
	ClaudeVersion string   `json:"claude_version,omitempty"`
	Quota         *Quota   `json:"quota,omitempty"`
	Violations    []string `json:"isolation_violations,omitempty"`
}

func (r Run) TotalUSD() float64 { return r.ClaudeUSD + r.JevUSD }

func LoadTasks(dir string, only []string) ([]Task, error) {
	want := map[string]bool{}
	for _, o := range only {
		want[o] = true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []Task
	for _, e := range entries {
		if !e.IsDir() || (len(want) > 0 && !want[e.Name()]) {
			continue
		}
		var t Task
		b, err := os.ReadFile(filepath.Join(dir, e.Name(), "task.json"))
		if err != nil {
			continue
		}
		if err := json.Unmarshal(b, &t); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		t.Name = e.Name()
		// Absolute: checks run in a temp work dir and read $TASK_DIR from there.
		if t.Dir, err = filepath.Abs(filepath.Join(dir, e.Name())); err != nil {
			return nil, err
		}
		if t.TimeoutS == 0 {
			t.TimeoutS = 600
		}
		out = append(out, t)
	}
	return out, nil
}

// CopyTree copies src into dst (created).
func CopyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		info, _ := d.Info()
		return os.WriteFile(target, b, info.Mode().Perm())
	})
}

// Runner holds what one run needs; Pick and JevCost are injected by main.
type Runner struct {
	Claude    string
	MainModel string
	HookCmd   string // shell command for the UserPromptSubmit hook in the hook arm
	Agents    string // --agents JSON for the delegate-agent arm
	MaxUSD    float64
	// Pick returns the model for the route arm and Jev's cost for that decision.
	Pick func(ctx context.Context, prompt string) (model string, jevUSD float64, err error)
	// JevCost sums the hook's Jev cost for a session from the decisions logs
	// in jevHome ("" = the evaluator's own state directory).
	JevCost func(sessionID, jevHome string) float64
	// Isolate runs claude in a throwaway HOME / CLAUDE_CONFIG_DIR / JEVCC_HOME
	// with stream-json output, and checks its init event against Allow.
	Isolate bool
	Allow   Allow
	PassEnv []string // added to an isolated run's environment (credentials)
	SaveDir string   // keeps each isolated run's stream here when set
}

func (r *Runner) Do(ctx context.Context, t Task, arm string, rep int) Run {
	res := Run{Task: t.Name, Arm: arm, Rep: rep, Model: r.MainModel}
	work, err := os.MkdirTemp("", "jevcc-bench-"+t.Name+"-")
	if err != nil {
		res.Error = err.Error()
		return res
	}
	defer os.RemoveAll(work)
	if len(t.FixtureFrom) == 0 {
		if err := CopyTree(filepath.Join(t.Dir, "fixture"), work); err != nil && !errors.Is(err, fs.ErrNotExist) {
			res.Error = err.Error()
			return res
		}
	}
	for _, p := range t.FixtureFrom {
		src := filepath.Join(t.Dir, p)
		if err := CopyTree(src, filepath.Join(work, filepath.Base(src))); err != nil {
			res.Error = err.Error()
			return res
		}
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(t.TimeoutS)*time.Second)
	defer cancel()
	mode, delegate := "off", ""
	args := []string{"-p", "--output-format", "json", "--permission-mode", "bypassPermissions", "--no-session-persistence"}
	if r.MaxUSD > 0 {
		args = append(args, "--max-budget-usd", fmt.Sprint(r.MaxUSD))
	}
	var extraEnv []string
	switch arm {
	case ArmHaiku:
		res.Model = "haiku"
	case ArmFiles:
		res.Model, mode, delegate = "haiku", "on", "off"
		extraEnv = append(extraEnv, "JEVCC_SKILLS=0", "JEVCC_FIND=1", "JEVCC_VERIFY=0")
		args = append(args, "--settings", hookSettings(r.HookCmd, "UserPromptSubmit"))
	case ArmEscalate, ArmVerify, ArmEscalateVerify:
		res.Model, mode = "haiku", "on"
		args = append(args, "--agents", r.Agents)
		events := []string{"UserPromptSubmit"}
		if arm == ArmEscalate || arm == ArmEscalateVerify {
			delegate = "agent"
			extraEnv = append(extraEnv, "JEVCC_MAIN=haiku")
		} else {
			delegate = "off"
		}
		if arm != ArmEscalate {
			events = append(events, "Stop")
			extraEnv = append(extraEnv, "JEVCC_VERIFY=1")
			// The Stop check reads the session transcript, so it must be written.
			args = without(args, "--no-session-persistence")
		}
		args = append(args, "--settings", hookSettings(r.HookCmd, events...))
	case ArmDelegateModel, ArmDelegateAgent:
		delegate = strings.TrimPrefix(arm, "delegate-")
		if arm == ArmDelegateAgent {
			args = append(args, "--agents", r.Agents)
		}
		fallthrough
	case ArmHook:
		mode = "on"
		hooks := map[string]any{"hooks": map[string]any{"UserPromptSubmit": []any{map[string]any{
			"hooks": []any{map[string]any{"type": "command", "command": r.HookCmd, "timeout": 15}},
		}}}}
		b, _ := json.Marshal(hooks)
		args = append(args, "--settings", string(b))
	case ArmRoute:
		m, usd, err := r.Pick(ctx, t.Prompt)
		if err != nil {
			res.Error = "route: " + err.Error()
			return res
		}
		res.Model, res.JevUSD = m, usd
	}
	base := os.Environ()
	var sb sandbox
	if r.Isolate {
		if sb, err = newSandbox(); err != nil {
			res.Error = err.Error()
			return res
		}
		defer os.RemoveAll(sb.Root)
		base = sb.env(base, r.PassEnv)
		for i := range args {
			if args[i] == "--output-format" {
				args[i+1] = "stream-json"
			}
		}
		args = append(args, "--verbose", "--strict-mcp-config")
	}
	agents, events := armAdds(args)
	args = append(args, "--model", res.Model, t.Prompt)
	cmd := exec.CommandContext(ctx, r.Claude, args...)
	cmd.Dir = work
	cmd.Env = append(base, "JEVCC_MODE="+mode)
	if delegate != "" {
		cmd.Env = append(cmd.Env, "JEVCC_DELEGATE="+delegate)
	}
	cmd.Env = append(cmd.Env, extraEnv...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	start := time.Now()
	out, err := cmd.Output()
	res.DurMS = time.Since(start).Milliseconds()
	if r.Isolate {
		if r.SaveDir != "" {
			name := fmt.Sprintf("%s_%s_%d_%d.jsonl", t.Name, arm, rep, time.Now().UnixNano())
			os.WriteFile(filepath.Join(r.SaveDir, name), out, 0o600)
		}
		in, hooks, result := parseStream(out)
		if in != nil {
			res.ClaudeVersion = in.Version
		}
		res.Violations = checkIsolation(in, hooks, sb, r.Allow, agents, events)
		res.Quota = parseQuota(out)
		out = result
	}
	var cr struct {
		Cost       float64 `json:"total_cost_usd"`
		DurationMS int64   `json:"duration_ms"`
		Turns      int     `json:"num_turns"`
		SessionID  string  `json:"session_id"`
		IsError    bool    `json:"is_error"`
		Subtype    string  `json:"subtype"`
		Subagents  struct {
			Spawned int `json:"spawned"`
		} `json:"subagent_stats"`
	}
	if jerr := json.Unmarshal(out, &cr); jerr != nil {
		res.Error = fmt.Sprintf("claude: %v %s", err, strings.TrimSpace(stderr.String()))
		return res
	}
	if cr.IsError {
		res.Error = "claude: " + cr.Subtype
	}
	if len(res.Violations) > 0 {
		res.Error = "isolation: " + strings.Join(res.Violations, "; ")
	}
	res.ClaudeUSD, res.Turns, res.SessionID, res.Subagents = cr.Cost, cr.Turns, cr.SessionID, cr.Subagents.Spawned
	res.ModelUSD = map[string]float64{}
	var raw struct {
		ModelUsage map[string]map[string]any `json:"modelUsage"`
	}
	_ = json.Unmarshal(out, &raw)
	for m, u := range raw.ModelUsage {
		res.ModelUSD[m], _ = u["costUSD"].(float64)
		for _, k := range []string{"inputTokens", "outputTokens", "cacheReadInputTokens", "cacheCreationInputTokens"} {
			v, _ := u[k].(float64)
			res.Tokens += int(v)
		}
	}
	if mode == "on" && r.JevCost != nil {
		res.JevUSD = r.JevCost(cr.SessionID, sb.JevHome)
	}
	check := exec.CommandContext(ctx, "sh", "-c", t.Check)
	check.Dir = work
	// TASK_DIR lets a check copy hidden tests that the agent never saw.
	check.Env = append(os.Environ(), "TASK_DIR="+t.Dir)
	co, cerr := check.CombinedOutput()
	res.Pass = cerr == nil
	if !res.Pass {
		res.CheckOut = tail(string(co), 400)
	}
	return res
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return "…" + s[len(s)-n:]
	}
	return s
}

// Plan lists (task, arm, rep) in a shuffled order so time-of-day effects and
// cache warmth do not line up with one arm.
type Job struct {
	Task Task
	Arm  string
	Rep  int
}

func Plan(tasks []Task, arms []string, reps int, seed int64) []Job {
	var jobs []Job
	for _, t := range tasks {
		for rep := 1; rep <= reps; rep++ {
			for _, a := range arms {
				jobs = append(jobs, Job{t, a, rep})
			}
		}
	}
	rand.New(rand.NewSource(seed)).Shuffle(len(jobs), func(i, j int) { jobs[i], jobs[j] = jobs[j], jobs[i] })
	return jobs
}

// BootstrapMeanCI returns the 95% percentile-bootstrap interval of the mean.
func BootstrapMeanCI(xs []float64, iters int, seed int64) (lo, hi float64) {
	if len(xs) == 0 {
		return math.NaN(), math.NaN()
	}
	rng := rand.New(rand.NewSource(seed))
	means := make([]float64, iters)
	for i := range means {
		s := 0.0
		for range xs {
			s += xs[rng.Intn(len(xs))]
		}
		means[i] = s / float64(len(xs))
	}
	sort.Float64s(means)
	return means[int(0.025*float64(iters))], means[int(0.975*float64(iters))-1]
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return math.NaN()
	}
	s := 0.0
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return math.NaN()
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}

// Report renders a Markdown comparison of arms against "off".
func Report(runs []Run, arms []string) string {
	by := map[string][]Run{}
	for _, r := range runs {
		by[r.Arm] = append(by[r.Arm], r)
	}
	var b strings.Builder
	b.WriteString("# End-to-end A/B\n\n")
	b.WriteString("Each run is a fresh copy of the task fixture; pass = the task's check command exits 0. Cost = Claude's `total_cost_usd` (list price, even on a subscription) + Jev's own cost.\n\n")
	b.WriteString("| arm | runs | errors | pass rate | mean cost | median cost | mean time | mean turns | runs with subagents | cost by model |\n|---|---|---|---|---|---|---|---|---|---|\n")
	for _, a := range arms {
		rs := by[a]
		var cost, dur, turns []float64
		pass, errs, sub := 0, 0, 0
		modelCost := map[string]float64{}
		for _, r := range rs {
			if r.Error != "" {
				errs++
			}
			if r.Pass {
				pass++
			}
			if r.Subagents > 0 {
				sub++
			}
			cost = append(cost, r.TotalUSD())
			dur = append(dur, float64(r.DurMS)/1000)
			turns = append(turns, float64(r.Turns))
			for m, c := range r.ModelUSD {
				modelCost[m] += c
			}
		}
		fmt.Fprintf(&b, "| %s | %d | %d | %s | $%.4f | $%.4f | %.0fs | %.1f | %d | %s |\n", a, len(rs), errs, passRate(pass, len(rs)), mean(cost), median(cost), mean(dur), mean(turns), sub, shares(modelCost))
	}
	base := map[string]Run{}
	for _, r := range by[ArmOff] {
		base[fmt.Sprintf("%s#%d", r.Task, r.Rep)] = r
	}
	for _, a := range arms {
		if a == ArmOff || len(base) == 0 {
			continue
		}
		var diffs, rel []float64
		regress, fixes := 0, 0
		for _, r := range by[a] {
			o, ok := base[fmt.Sprintf("%s#%d", r.Task, r.Rep)]
			if !ok || r.Error != "" || o.Error != "" {
				continue
			}
			diffs = append(diffs, r.TotalUSD()-o.TotalUSD())
			if o.TotalUSD() > 0 {
				rel = append(rel, r.TotalUSD()/o.TotalUSD()-1)
			}
			if o.Pass && !r.Pass {
				regress++
			}
			if !o.Pass && r.Pass {
				fixes++
			}
		}
		lo, hi := BootstrapMeanCI(diffs, 5000, 1)
		fmt.Fprintf(&b, "\n**%s vs off** (%d paired runs): cost change per run %+.4f USD (95%% bootstrap CI %+.4f to %+.4f), median relative change %+.0f%%; quality: %d runs failed where off passed, %d passed where off failed.\n",
			a, len(diffs), mean(diffs), lo, hi, 100*median(rel), regress, fixes)
		switch {
		case len(diffs) < 10:
			b.WriteString("Too few paired runs for a firm conclusion; add --repeat.\n")
		case hi < 0:
			b.WriteString("The saving is statistically clear at this sample size.\n")
		case lo > 0:
			b.WriteString("This arm costs MORE than off at this sample size.\n")
		default:
			b.WriteString("The interval includes zero: no clear cost difference at this sample size.\n")
		}
	}
	b.WriteString("\n## Per task\n\n| task | " + strings.Join(arms, " | ") + " |\n|---|" + strings.Repeat("---|", len(arms)) + "\n")
	tasks := map[string]bool{}
	for _, r := range runs {
		tasks[r.Task] = true
	}
	var names []string
	for t := range tasks {
		names = append(names, t)
	}
	sort.Strings(names)
	for _, t := range names {
		var cells []string
		for _, a := range arms {
			var cost []float64
			pass, n := 0, 0
			models := map[string]bool{}
			for _, r := range by[a] {
				if r.Task != t {
					continue
				}
				n++
				cost = append(cost, r.TotalUSD())
				if r.Pass {
					pass++
				}
				models[r.Model] = true
			}
			var ms []string
			for m := range models {
				ms = append(ms, m)
			}
			sort.Strings(ms)
			cells = append(cells, fmt.Sprintf("%d/%d pass, $%.4f (%s)", pass, n, mean(cost), strings.Join(ms, ",")))
		}
		fmt.Fprintf(&b, "| %s | %s |\n", t, strings.Join(cells, " | "))
	}
	return b.String()
}

func passRate(k, n int) string {
	if n == 0 {
		return "-"
	}
	return fmt.Sprintf("%.0f%% (%d/%d)", 100*float64(k)/float64(n), k, n)
}

func shares(m map[string]float64) string {
	total := 0.0
	for _, v := range m {
		total += v
	}
	if total == 0 {
		return "-"
	}
	var names []string
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	var parts []string
	for _, k := range names {
		parts = append(parts, fmt.Sprintf("%s %.0f%%", strings.TrimPrefix(k, "claude-"), 100*m[k]/total))
	}
	return strings.Join(parts, ", ")
}
