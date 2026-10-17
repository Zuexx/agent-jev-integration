// Command jevcc puts TypeSafe's Jev at Claude Code's decision points: model and
// skill routing per prompt, bulk classification, semantic lint and checklist review.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/zuexx/agent-jev-integration/internal/audit"
	"github.com/zuexx/agent-jev-integration/internal/bench"
	"github.com/zuexx/agent-jev-integration/internal/blendermcp"
	"github.com/zuexx/agent-jev-integration/internal/cite"
	"github.com/zuexx/agent-jev-integration/internal/classify"
	"github.com/zuexx/agent-jev-integration/internal/config"
	"github.com/zuexx/agent-jev-integration/internal/eval"
	"github.com/zuexx/agent-jev-integration/internal/find"
	"github.com/zuexx/agent-jev-integration/internal/fsutil"
	"github.com/zuexx/agent-jev-integration/internal/guard"
	"github.com/zuexx/agent-jev-integration/internal/hook"
	"github.com/zuexx/agent-jev-integration/internal/inject"
	"github.com/zuexx/agent-jev-integration/internal/install"
	"github.com/zuexx/agent-jev-integration/internal/jev"
	"github.com/zuexx/agent-jev-integration/internal/lint"
	"github.com/zuexx/agent-jev-integration/internal/logx"
	"github.com/zuexx/agent-jev-integration/internal/paths"
	"github.com/zuexx/agent-jev-integration/internal/review"
	"github.com/zuexx/agent-jev-integration/internal/roster"
	"github.com/zuexx/agent-jev-integration/internal/router"
	"github.com/zuexx/agent-jev-integration/internal/stats"
	"github.com/zuexx/agent-jev-integration/internal/statusline"
)

const usage = `usage: jevcc <command>

  install [--scope user|project|local] [--dir D] [--default-model haiku] [--on-by-default]
                                                   add the hooks, slash commands and subagents
  uninstall [--scope ...] [--dir D]                undo install for that scope
  doctor [--dir D]                                 check key, claude and every install scope
  key                                              store your TypeSafe API key (read from stdin)

  route [--json] <prompt>          show what Jev decides for a prompt (tier, skill, cost, latency)
  ask <prompt> [-- claude args]    pick the model with Jev, then start claude on it
  roster [--json]                  list the skills Jev chooses from here
  stats                            routing decisions so far (same as /jev stats)
  audit-log [-days N] [-limit N]   routing decisions that look wrong, for a person to check
  warm [--n N --gap D]             tiny Jev call(s); with --n, a latency probe over time

  skills hide|show [--scope S] [--dir D] [--mode name-only]   shrink the skill listing; Jev names the skill
  find "<question>" [--in FILE] [-k N] [--dir D]                Jev picks the files (or lines of one file) to read
  cite --claims F.jsonl [--min-confidence P]                    check quoted claims against their source files
  classify --in F --questions Q [--field X] [--out O] [--limit N] [--rank Q --top N]
  lint [--rules R] [--targets comments,logs,functions] [--fail-on error|warn] [paths...]
  review [--base REF | --staged] [--checklist C] [--threshold P] [--top N]

  eval --set S [--roster R] [--baseline MODEL] [--out DIR]       offline router accuracy
  skilleval [--set S] [--model haiku] [--limit N]               tokens/turn and skill picks: listing vs names vs names+jev
  bench [--tasks DIR] [--arms off,hook,route] [--repeat N] [--model M] [--out DIR]   end-to-end A/B
`

func main() { os.Exit(realMain(os.Args[1:])) }

// realMain is separate from main for testing.
func realMain(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	cmds := map[string]func([]string) int{
		"hook":         func([]string) int { return hook.Main(os.Stdin, os.Stdout) },
		"statusline":   func([]string) int { return statusline.Main(os.Stdin, os.Stdout) },
		"install":      func(a []string) int { return cmdInstall("install", a) },
		"uninstall":    func(a []string) int { return cmdInstall("uninstall", a) },
		"doctor":       func(a []string) int { return cmdInstall("doctor", a) },
		"key":          cmdKey,
		"route":        cmdRoute,
		"ask":          cmdAsk,
		"roster":       cmdRoster,
		"stats":        cmdStats,
		"audit-log":    cmdAuditLog,
		"classify":     cmdClassify,
		"lint":         cmdLint,
		"review":       cmdReview,
		"eval":         cmdEval,
		"bench":        cmdBench,
		"plugin-files": cmdPluginFiles,
		"warm":         cmdWarm,
		"skills":       cmdSkills,
		"skilleval":    cmdSkillEval,
		"escalateval":  cmdEscalateEval,
		"guardeval":    cmdGuardEval,
		"find":         cmdFind,
		"findeval":     cmdFindEval,
		"memeval":      cmdMemEval,
		"injeval":      cmdInjEval,
		"cite":         cmdCite,
		"blender-mcp":  cmdBlenderMCP,
		"judge":        cmdJudge,
		"rate":         cmdRate,
		"report":       cmdReport,
	}
	f, ok := cmds[args[0]]
	if !ok {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	return f(args[1:])
}

func fail(err error) int {
	fmt.Fprintln(os.Stderr, "jevcc:", err)
	return 1
}

// client builds a Jev client for batch commands (retries on 429/529).
func client(cfg *config.Config) *jev.Client {
	return &jev.Client{Endpoint: cfg.Endpoint, Key: cfg.APIKey, Model: cfg.Model, HTTP: &http.Client{Timeout: 60 * time.Second}, Attempts: 5}
}

func loadConfig() (*config.Config, error) {
	cfg, err := config.Load()
	if err == nil && cfg.APIKey == "" {
		err = jev.ErrNoKey
	}
	return cfg, err
}

func exe() (string, error) {
	p, err := os.Executable()
	if err == nil {
		p, err = filepath.EvalSymlinks(p)
	}
	if err == nil && strings.Contains(p, "go-build") {
		err = errors.New("run install from an installed binary (go install ./cmd/jevcc), not `go run`")
	}
	return p, err
}

func cmdInstall(which string, args []string) int {
	fset := flag.NewFlagSet(which, flag.ContinueOnError)
	scope := fset.String("scope", "user", "user | project | local")
	dir := fset.String("dir", ".", "project directory for project/local scope")
	model := fset.String("default-model", "", "also set Claude Code's default model in that scope (e.g. haiku)")
	onDefault := fset.Bool("on-by-default", false, "turn Jev routing on in every new session")
	sline := fset.Bool("statusline", true, "add a jev line under the existing statusline")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	s, err := install.ParseScope(*scope)
	if err != nil {
		return fail(err)
	}
	abs, _ := filepath.Abs(*dir)
	o := install.Options{Scope: s, Dir: abs, Out: os.Stdout, Now: time.Now(), DefaultModel: *model, OnByDefault: *onDefault, Statusline: *sline}
	switch which {
	case "doctor":
		return install.Doctor(o, os.Stdout)
	case "uninstall":
		err = install.Uninstall(o)
	default:
		if o.Exe, err = exe(); err == nil {
			err = install.Install(o)
		}
	}
	if err != nil {
		return fail(err)
	}
	return 0
}

func cmdKey([]string) int {
	if fi, err := os.Stdin.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
		fmt.Fprint(os.Stderr, "TypeSafe API key (input hidden): ")
		if stty := exec.Command("stty", "-echo"); true {
			stty.Stdin = os.Stdin
			if stty.Run() == nil {
				defer func() {
					on := exec.Command("stty", "echo")
					on.Stdin = os.Stdin
					on.Run()
					fmt.Fprintln(os.Stderr)
				}()
			}
		}
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	key := strings.TrimSpace(line)
	if key == "" {
		if err != nil {
			return fail(err)
		}
		return fail(errors.New("empty key"))
	}
	if err := config.SaveKey(key); err != nil {
		return fail(err)
	}
	fmt.Fprintln(os.Stderr, "saved to", config.Path(), "(mode 600)")
	return 0
}

func decide(ctx context.Context, cfg *config.Config, prompt string, skills []roster.Skill) (*router.Decision, error) {
	return router.Route(ctx, client(cfg), cfg, prompt, skills)
}

func cmdRoute(args []string) int {
	fset := flag.NewFlagSet("route", flag.ContinueOnError)
	asJSON := fset.Bool("json", false, "print the full decision as JSON")
	noSkills := fset.Bool("no-skills", false, "only decide the tier")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	prompt := strings.Join(fset.Args(), " ")
	if prompt == "" {
		return fail(errors.New("usage: jevcc route [--json] <prompt>"))
	}
	cfg, err := loadConfig()
	if err != nil {
		return fail(err)
	}
	var skills []roster.Skill
	if !*noSkills {
		cwd, _ := os.Getwd()
		skills = roster.Discover(paths.ClaudeDir(), cwd)
	}
	d, err := decide(context.Background(), cfg, prompt, skills)
	if err != nil {
		return fail(err)
	}
	if *asJSON {
		b, _ := fsutil.Marshal(d)
		os.Stdout.Write(b)
		return 0
	}
	fmt.Printf("tier   %s (p=%.2f, confidence %.2f) %v\n", d.Tier, d.TierProbs[d.Tier], d.TierConf, d.TierProbs)
	fmt.Printf("skill  %q (fit %.2f, gate %.2f, shortlist %v, from %d skills)\n", d.Skill, d.SkillFit, d.Gate, d.Candidates, len(skills))
	fmt.Printf("bulk   %.2f\n", d.Bulk)
	fmt.Printf("jev    %d calls, %d input tokens, $%.6f, %d ms (%s)\n", d.Calls, d.Tokens, d.CostUSD, d.LatencyMS, d.Model)
	if ctx := router.Context(d, cfg); ctx != "" {
		fmt.Printf("\ncontext added to the prompt:\n%s\n", ctx)
	} else {
		fmt.Println("\n(no context added: top tier or low confidence, and no skill/bulk advice)")
	}
	return 0
}

func cmdAsk(args []string) int {
	var extra []string
	for i, a := range args {
		if a == "--" {
			args, extra = args[:i], args[i+1:]
			break
		}
	}
	prompt := strings.Join(args, " ")
	if prompt == "" {
		return fail(errors.New("usage: jevcc ask <prompt> [-- claude args]"))
	}
	cfg, err := loadConfig()
	if err != nil {
		return fail(err)
	}
	d, err := decide(context.Background(), cfg, prompt, nil)
	names := cfg.TierNames()
	model := names[len(names)-1]
	if err != nil {
		fmt.Fprintln(os.Stderr, "jevcc: routing failed, using", model+":", err)
	} else if d.TierConf >= cfg.MinConfidence {
		model = d.Tier
	}
	if d != nil {
		fmt.Fprintf(os.Stderr, "jevcc: %s (Jev picked %s, confidence %.2f, %d ms, $%.6f)\n", model, d.Tier, d.TierConf, d.LatencyMS, d.CostUSD)
	}
	claude, err := exec.LookPath("claude")
	if err != nil {
		return fail(errors.New("claude not found on PATH"))
	}
	cmd := exec.Command(claude, append(append([]string{"--model", model}, extra...), prompt)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		return fail(err)
	}
	return 0
}

func cmdRoster(args []string) int {
	fset := flag.NewFlagSet("roster", flag.ContinueOnError)
	asJSON := fset.Bool("json", false, "print as JSON (use as a frozen roster for eval)")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	cwd, _ := os.Getwd()
	skills := roster.Discover(paths.ClaudeDir(), cwd)
	if *asJSON {
		for i := range skills {
			skills[i].Path = ""
		}
		b, _ := fsutil.Marshal(skills)
		os.Stdout.Write(b)
		return 0
	}
	for _, s := range skills {
		fmt.Printf("%-40s %s\n", s.Name, oneLine(s.Description, 100))
	}
	fmt.Printf("\n%d skills\n", len(skills))
	return 0
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func cmdStats([]string) int {
	recs, err := stats.LoadDecisions()
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fail(err)
	}
	fmt.Println(stats.Summary(recs, time.Time{}))
	return 0
}

func cmdAuditLog(args []string) int {
	fset := flag.NewFlagSet("audit-log", flag.ContinueOnError)
	days := fset.Int("days", 7, "look back this many days")
	limit := fset.Int("limit", 5, "examples to show per kind")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	cfg, err := config.Load()
	if err != nil {
		return fail(err)
	}
	recs, err := stats.LoadDecisions()
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fail(err)
	}
	now := time.Now()
	since := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -*days)
	fmt.Print(audit.Report(audit.Find(recs, cfg, since), since, *limit))
	return 0
}

func cmdClassify(args []string) int {
	fset := flag.NewFlagSet("classify", flag.ContinueOnError)
	in := fset.String("in", "", "input .csv, .jsonl or .json (array of objects)")
	qpath := fset.String("questions", "", "questions JSON: {id: {type, instructions, criteria}}")
	field := fset.String("field", "", "send only this column as the state (default: the whole row)")
	out := fset.String("out", "", "output .csv or .jsonl (default: <in>.jev.csv)")
	limit := fset.Int("limit", 0, "random sample of N rows (calibration)")
	seed := fset.Int64("seed", 1, "sample seed")
	workers := fset.Int("workers", 16, "concurrent requests")
	minConf := fset.Float64("min-confidence", 0.6, "choice/score confidence below this marks needs_review")
	rank := fset.String("rank", "", "question id to rank by (score/noul)")
	top := fset.Int("top", 10, "rows to print with --rank")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	if *in == "" || *qpath == "" {
		return fail(errors.New("--in and --questions are required"))
	}
	cfg, err := loadConfig()
	if err != nil {
		return fail(err)
	}
	var qs map[string]jev.Question
	if err := fsutil.ReadJSON(*qpath, &qs); err != nil {
		return fail(fmt.Errorf("%s: %w", *qpath, err))
	}
	rows, err := classify.Load(*in)
	if err != nil {
		return fail(err)
	}
	if *out == "" {
		*out = strings.TrimSuffix(*in, filepath.Ext(*in)) + ".jev.csv"
	}
	results, sum := classify.Run(context.Background(), client(cfg), rows, classify.Options{
		Questions: qs, Field: *field, Workers: *workers, Limit: *limit, Seed: *seed, MinConfidence: *minConf,
	})
	f, err := os.Create(*out)
	if err != nil {
		return fail(err)
	}
	format := "jsonl"
	if strings.EqualFold(filepath.Ext(*out), ".csv") {
		format = "csv"
	}
	if err := classify.Write(f, format, results, qs); err != nil {
		f.Close()
		return fail(err)
	}
	f.Close()
	fmt.Println(sum)
	fmt.Println("wrote", *out)
	if *rank != "" {
		fmt.Printf("\ntop %d by %s:\n", *top, *rank)
		for i, r := range classify.Rank(results, *rank) {
			if i == *top {
				break
			}
			b, _ := json.Marshal(r.Row)
			fmt.Printf("%2d. %s=%s  %s\n", i+1, *rank, classify.Value(r.Answers[*rank]), oneLine(string(b), 140))
		}
	}
	if sum.Failed > 0 {
		return 1
	}
	return 0
}

func cmdLint(args []string) int {
	fset := flag.NewFlagSet("lint", flag.ContinueOnError)
	rulesPath := fset.String("rules", "", "rules JSON (default: .jevlint.json if present, else built-in rules)")
	targets := fset.String("targets", "comments,logs,functions", "candidate kinds to check")
	maxN := fset.Int("max", 5000, "maximum candidates to send (cost guard)")
	failOn := fset.String("fail-on", "", "exit 1 if findings of this severity exist (error|warn)")
	workers := fset.Int("workers", 16, "concurrent requests")
	printRules := fset.Bool("print-rules", false, "print the built-in rules as JSON and exit")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	if *printRules {
		b, _ := fsutil.Marshal(lint.DefaultRules)
		os.Stdout.Write(b)
		return 0
	}
	rules := lint.DefaultRules
	if *rulesPath == "" {
		if _, err := os.Stat(".jevlint.json"); err == nil {
			*rulesPath = ".jevlint.json"
		}
	}
	if *rulesPath != "" {
		if err := fsutil.ReadJSON(*rulesPath, &rules); err != nil {
			return fail(fmt.Errorf("%s: %w", *rulesPath, err))
		}
	}
	cfg, err := loadConfig()
	if err != nil {
		return fail(err)
	}
	kinds := map[string]bool{}
	for _, k := range strings.Split(*targets, ",") {
		kinds[strings.TrimSpace(k)] = true
	}
	roots := fset.Args()
	if len(roots) == 0 {
		roots = []string{"."}
	}
	var cands []lint.Candidate
	for _, f := range lint.Files(roots) {
		b, err := os.ReadFile(f)
		if err != nil || len(b) > 1<<20 {
			continue
		}
		cands = append(cands, lint.Extract(f, string(b), kinds)...)
	}
	if len(cands) > *maxN {
		fmt.Fprintf(os.Stderr, "jevcc: %d candidates, checking the first %d (raise --max)\n", len(cands), *maxN)
		cands = cands[:*maxN]
	}
	rep := lint.Run(context.Background(), client(cfg), cands, rules, *workers)
	fmt.Println(rep)
	switch {
	case *failOn == "error" && rep.Count("error") > 0, *failOn == "warn" && rep.Count("error")+rep.Count("warn") > 0:
		return 1
	}
	return 0
}

func cmdReview(args []string) int {
	fset := flag.NewFlagSet("review", flag.ContinueOnError)
	base := fset.String("base", "", "diff against this ref (default: merge-base with origin/HEAD, main or master)")
	staged := fset.Bool("staged", false, "review only staged changes")
	checklist := fset.String("checklist", "", "checklist JSON: [{id, question, severity}]")
	threshold := fset.Float64("threshold", 0.5, "report items whose yes-probability is at least this")
	top := fset.Int("top", 25, "findings to list (0 = all)")
	workers := fset.Int("workers", 16, "concurrent requests")
	asJSON := fset.Bool("json", false, "print findings as JSON")
	printList := fset.Bool("print-checklist", false, "print the built-in checklist as JSON and exit")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	if *printList {
		b, _ := fsutil.Marshal(review.DefaultChecklist)
		os.Stdout.Write(b)
		return 0
	}
	if fset.NArg() > 0 && *base == "" {
		*base = fset.Arg(0)
	}
	items := review.DefaultChecklist
	if *checklist != "" {
		if err := fsutil.ReadJSON(*checklist, &items); err != nil {
			return fail(fmt.Errorf("%s: %w", *checklist, err))
		}
	}
	cfg, err := loadConfig()
	if err != nil {
		return fail(err)
	}
	diffArgs := []string{"diff", "--unified=5", "--no-color", "--no-ext-diff"}
	switch {
	case *staged:
		diffArgs = append(diffArgs, "--cached")
	case *base != "":
		diffArgs = append(diffArgs, *base)
	default:
		if mb := mergeBase(); mb != "" {
			diffArgs = append(diffArgs, mb)
		} else {
			diffArgs = append(diffArgs, "HEAD")
		}
	}
	out, err := exec.Command("git", diffArgs...).Output()
	if err != nil {
		return fail(fmt.Errorf("git %s: %w", strings.Join(diffArgs, " "), err))
	}
	hunks := review.Hunks(string(out))
	if len(hunks) == 0 {
		fmt.Println("jevcc: no changes to review")
		return 0
	}
	rep := review.Run(context.Background(), client(cfg), hunks, items, *threshold, *workers)
	if *asJSON {
		b, _ := fsutil.Marshal(rep.Findings)
		os.Stdout.Write(b)
		return 0
	}
	fmt.Println(rep.Markdown(*top))
	return 0
}

func mergeBase() string {
	for _, ref := range []string{"origin/HEAD", "origin/main", "main", "origin/master", "master"} {
		if out, err := exec.Command("git", "merge-base", "HEAD", ref).Output(); err == nil {
			return strings.TrimSpace(string(out))
		}
	}
	return ""
}

func cmdEval(args []string) int {
	fset := flag.NewFlagSet("eval", flag.ContinueOnError)
	set := fset.String("set", "bench/router_set.jsonl", "labelled prompts (JSONL: prompt, tier, skill)")
	rosterPath := fset.String("roster", "bench/roster.json", "frozen skill roster (jevcc roster --json)")
	baseline := fset.String("baseline", "", "also route with `claude -p --model MODEL` (e.g. haiku) for comparison")
	workers := fset.Int("workers", 8, "concurrent decisions")
	outDir := fset.String("out", "bench/results", "where to write the report and raw rows")
	minConf := fset.Float64("min-confidence", 0, "override config min_confidence")
	skillFit := fset.Float64("skill-fit", 0, "override config skill_fit")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	cfg, err := loadConfig()
	if err != nil {
		return fail(err)
	}
	if *minConf > 0 {
		cfg.MinConfidence = *minConf
	}
	if *skillFit > 0 {
		cfg.SkillFit = *skillFit
	}
	items, err := eval.LoadItems(*set)
	if err != nil {
		return fail(err)
	}
	var skills []roster.Skill
	if err := fsutil.ReadJSON(*rosterPath, &skills); err != nil {
		return fail(fmt.Errorf("%s: %w", *rosterPath, err))
	}
	ctx := context.Background()
	fmt.Fprintf(os.Stderr, "jevcc: routing %d prompts with Jev…\n", len(items))
	rows := eval.RunJev(ctx, client(cfg), cfg, items, skills, *workers)
	if *baseline != "" {
		claude, err := exec.LookPath("claude")
		if err != nil {
			return fail(errors.New("claude not found on PATH"))
		}
		fmt.Fprintf(os.Stderr, "jevcc: routing %d prompts with claude -p --model %s…\n", len(items), *baseline)
		eval.RunBaseline(ctx, claude, *baseline, eval.BaselinePrompt(cfg, skills), rows, min(*workers, 4))
	}
	md := eval.Report(rows, cfg, len(skills)) + "\n\n" + eval.Sweep(rows, cfg, []float64{0.5, 0.6, 0.7, 0.75, 0.8, 0.9}, []float64{0.3, 0.5, 0.6, 0.7, 0.8, 0.9})
	stamp := time.Now().Format("20060102-150405")
	os.MkdirAll(*outDir, 0o755)
	mdPath := filepath.Join(*outDir, "eval-"+stamp+".md")
	os.WriteFile(mdPath, []byte(md+"\n"), 0o644)
	eval.SaveRows(filepath.Join(*outDir, "eval-"+stamp+".jsonl"), rows)
	fmt.Println(md)
	fmt.Fprintln(os.Stderr, "\nwrote", mdPath)
	return 0
}

func cmdBench(args []string) int {
	fset := flag.NewFlagSet("bench", flag.ContinueOnError)
	tasksDir := fset.String("tasks", "bench/tasks", "comma-separated task directories")
	armsFlag := fset.String("arms", "off,hook,route", "arms to run")
	repeat := fset.Int("repeat", 3, "runs per task and arm")
	model := fset.String("model", "opus", "main model for the off and hook arms")
	only := fset.String("only", "", "comma-separated task names")
	outDir := fset.String("out", "bench/results", "where to write results")
	maxUSD := fset.Float64("max-usd", 2, "per-run --max-budget-usd for claude (0 = none)")
	seed := fset.Int64("seed", 1, "run order seed")
	dry := fset.Bool("dry-run", false, "list the planned runs and exit")
	isolate := fset.Bool("isolate", true, "run each claude in a throwaway HOME / CLAUDE_CONFIG_DIR / JEVCC_HOME (docs/eval-plan.md 4.1)")
	tokenFile := fset.String("token-file", "", "comma-separated files holding `claude setup-token` tokens, one per account, used in order (else $CLAUDE_CODE_OAUTH_TOKEN)")
	max7d := fset.Float64("max-7d", 0.70, "stop using an account once its 7-day quota reaches this (docs/eval-plan.md 9.2)")
	max5h := fset.Float64("max-5h", 0.95, "wait for the 5-hour reset once an account reaches this")
	resume := fset.String("resume", "", "a previous bench-*.jsonl: skip runs that finished there and append to it")
	allowPath := fset.String("allow", "bench/isolation-allow.json", "agents and skills a clean Claude Code loads by itself")
	probe := fset.Bool("probe-allow", false, "run one clean isolated session and write what it loads to --allow, then exit")
	cacheDir := fset.String("cache", filepath.Join(paths.Home(), ".jevcc-bench", "cache"), "where tasks' offline caches live (e.g. remotion node_modules)")
	claudeBin := fset.String("claude", "", "Claude Code binary (default: `claude` on PATH resolved to its versioned file, so an update mid-run cannot change it)")
	effort := fset.String("effort", "medium", "--effort for every run (low, medium, high, xhigh, max); medium is Claude Code's usual default")
	validate := fset.Bool("validate", false, "check every task's check: fails on the fixture, passes with testdata/solution; no claude runs")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	var onlyList []string
	if *only != "" {
		onlyList = strings.Split(*only, ",")
	}
	var tasks []bench.Task
	for _, d := range strings.Split(*tasksDir, ",") {
		ts, err := bench.LoadTasks(d, onlyList)
		if err != nil {
			return fail(err)
		}
		tasks = append(tasks, ts...)
	}
	if *validate {
		bad := 0
		for _, t := range tasks {
			v := bench.Validate(context.Background(), t)
			status := "ok"
			switch {
			case !v.HasSolution && v.FailsOnFixture:
				status = "ok (no testdata/solution: only the fixture side is checked)"
			case !v.OK():
				status, bad = "BAD: "+v.Output, bad+1
			}
			fmt.Printf("%-24s %s", t.Name, status)
			if len(v.Metrics) > 0 {
				fmt.Printf("  metrics with solution: %v", v.Metrics)
			}
			fmt.Println()
		}
		if bad > 0 {
			return 1
		}
		return 0
	}
	var arms []string
	for _, a := range strings.Split(*armsFlag, ",") {
		if _, _, _, ok := bench.ParseLeadArm(a); ok && !bench.LeadArmApplicable(a) {
			fmt.Fprintf(os.Stderr, "skipping %s: nowhere to delegate (docs/eval-plan.md 2.2)\n", a)
			continue
		}
		arms = append(arms, a)
	}
	jobs := bench.Plan(tasks, arms, *repeat, *seed)
	if *dry {
		for _, j := range jobs {
			fmt.Printf("%s\t%s\t#%d\n", j.Task.Name, j.Arm, j.Rep)
		}
		fmt.Printf("%d runs\n", len(jobs))
		return 0
	}
	cfg, err := loadConfig()
	if err != nil {
		return fail(err)
	}
	claude, err := pinnedClaude(*claudeBin)
	if err != nil {
		return fail(err)
	}
	self, err := os.Executable()
	if err != nil {
		return fail(err)
	}
	fmt.Fprintf(os.Stderr, "claude: %s\njevcc: %s\n", claude, self)
	var passEnv []string
	var allow bench.Allow
	// accounts holds one credential per subscription account, in order of use.
	type account struct{ name, env string }
	var accounts []account
	if *isolate || *probe {
		if *tokenFile != "" {
			for _, f := range strings.Split(*tokenFile, ",") {
				b, err := os.ReadFile(f)
				if err != nil {
					return fail(err)
				}
				accounts = append(accounts, account{strings.TrimSuffix(filepath.Base(f), ".token"), "CLAUDE_CODE_OAUTH_TOKEN=" + strings.TrimSpace(string(b))})
			}
		} else if t := os.Getenv("CLAUDE_CODE_OAUTH_TOKEN"); t != "" {
			accounts = append(accounts, account{"env", "CLAUDE_CODE_OAUTH_TOKEN=" + t})
		}
		if len(accounts) == 0 {
			return fail(errors.New("isolated runs cannot use your login: pass --token-file (see `claude setup-token`) or set CLAUDE_CODE_OAUTH_TOKEN"))
		}
		// The hook inside the sandbox has no config file; give it the key.
		if cfg.APIKey != "" {
			passEnv = append(passEnv, "TYPESAFE_API_KEY="+cfg.APIKey)
		}
		if v := os.Getenv("TYPESAFE_ENDPOINT"); v != "" {
			passEnv = append(passEnv, "TYPESAFE_ENDPOINT="+v)
		}
	}
	if *probe {
		pr := &bench.Runner{Claude: claude, PassEnv: append(passEnv, accounts[0].env)}
		a, violations, err := pr.ProbeAllow(context.Background())
		if err != nil {
			return fail(err)
		}
		if len(violations) > 0 {
			return fail(fmt.Errorf("the clean session is not clean: %s", strings.Join(violations, "; ")))
		}
		b, _ := json.MarshalIndent(a, "", "  ")
		if err := os.WriteFile(*allowPath, append(b, '\n'), 0o644); err != nil {
			return fail(err)
		}
		fmt.Printf("wrote %s: %d agents, %d skills; review it before committing\n", *allowPath, len(a.Agents), len(a.Skills))
		return 0
	}
	if *isolate {
		if allow, err = bench.LoadAllow(*allowPath); err != nil {
			return fail(fmt.Errorf("%w (create it with --probe-allow)", err))
		}
	}
	mcp := map[string]any{}
	if b := os.Getenv("BLENDER"); b != "" {
		mcp["blender"] = map[string]any{"type": "stdio", "command": self, "args": []string{"blender-mcp"}, "env": map[string]string{"BLENDER": b}}
	}
	r := &bench.Runner{
		Isolate: *isolate, Allow: allow, PassEnv: passEnv, MCPServers: mcp, CacheDir: *cacheDir, Effort: *effort,
		Claude: claude, MainModel: *model, HookCmd: install.HookCommand(self), Agents: install.AgentsJSON(), AgentsOpen: install.AgentsJSONOpenTools(), MaxUSD: *maxUSD,
		Pick: func(ctx context.Context, prompt string) (string, float64, error) {
			d, err := decide(ctx, cfg, prompt, nil)
			if err != nil {
				return "", 0, err
			}
			if d.TierConf < cfg.MinConfidence {
				return *model, d.CostUSD, nil
			}
			return d.Tier, d.CostUSD, nil
		},
		JevUse: func(session, jevHome string) bench.JevUse {
			recs, _ := stats.LoadDecisions()
			if jevHome != "" {
				recs, _ = stats.LoadDecisionsIn(jevHome)
			}
			var u bench.JevUse
			for _, rec := range recs {
				if rec.SessionID != session {
					continue
				}
				if rec.Error != "" {
					u.Errors = append(u.Errors, rec.Error)
				}
				if rec.Decision != nil {
					u.Decisions++
					u.USD += rec.Decision.CostUSD
				}
				u.Advice = append(u.Advice, rec.Advice...)
			}
			return u
		},
	}
	stamp := time.Now().Format("20060102-150405")
	os.MkdirAll(*outDir, 0o755)
	rawPath := filepath.Join(*outDir, "bench-"+stamp+".jsonl")
	var runs []bench.Run
	done := map[string]bool{}
	if *resume != "" {
		rawPath = *resume
		prev, err := bench.LoadRuns(rawPath)
		if err != nil {
			return fail(err)
		}
		runs, done = prev, bench.Done(prev)
	}
	if *isolate {
		r.SaveDir = strings.TrimSuffix(rawPath, ".jsonl") + "-streams"
		os.MkdirAll(r.SaveDir, 0o755)
	}
	gate := &bench.QuotaGate{Max7d: *max7d, Max5h: *max5h, Accounts: max(len(accounts), 1)}
	for i, j := range jobs {
		if done[bench.JobKey(j.Task.Name, j.Arm, j.Rep)] {
			continue
		}
		acct := -1
		if len(accounts) > 0 {
			for {
				a, wait, finished := gate.Next(time.Now())
				if finished {
					fmt.Fprintf(os.Stderr, "every account reached %.0f%% of its 7-day quota; continue later with --resume %s\n", *max7d*100, rawPath)
					goto report
				}
				if a >= 0 {
					acct = a
					break
				}
				fmt.Fprintf(os.Stderr, "all accounts near their 5-hour limit; waiting %s\n", wait.Round(time.Minute))
				bench.WallSleep(wait + 30*time.Second)
			}
			r.PassEnv = append(append([]string{}, passEnv...), accounts[acct].env)
		}
		fmt.Fprintf(os.Stderr, "[%d/%d] %s %s #%d … ", i+1, len(jobs), j.Task.Name, j.Arm, j.Rep)
		res := r.Do(context.Background(), j.Task, j.Arm, j.Rep)
		// A network outage fails every run the same way; wait it out instead
		// of burning through the queue (failed runs are redone on --resume).
		for tries := 0; bench.Unreachable(res) && tries < 6; tries++ {
			fmt.Fprintf(os.Stderr, "cannot reach the API; retrying in 5 minutes … ")
			bench.WallSleep(5 * time.Minute)
			res = r.Do(context.Background(), j.Task, j.Arm, j.Rep)
		}
		if bench.Unreachable(res) {
			fmt.Fprintf(os.Stderr, "still unreachable; stopping. Continue later with --resume %s\n", rawPath)
			goto report
		}
		res.Build = filepath.Base(self)
		if acct >= 0 {
			res.Account = accounts[acct].name
			gate.Observe(acct, res.Quota)
		}
		runs = append(runs, res)
		fsutil.AppendJSONL(rawPath, res)
		status := "FAIL"
		if res.Pass {
			status = "pass"
		}
		if res.Error != "" {
			status = "error: " + res.Error
		}
		fmt.Fprintf(os.Stderr, "%s, $%.4f, %.0fs (%s)\n", status, res.TotalUSD(), float64(res.DurMS)/1000, res.Model)
	}
report:
	md := bench.Report(runs, arms)
	mdPath := filepath.Join(*outDir, "bench-"+stamp+".md")
	os.WriteFile(mdPath, []byte(md+"\n"), 0o644)
	fmt.Println(md)
	fmt.Fprintln(os.Stderr, "\nwrote", mdPath, "and", rawPath)
	return 0
}

// cmdPluginFiles regenerates the plugin directory's command files.
func cmdPluginFiles(args []string) int {
	if len(args) != 1 {
		return fail(errors.New("usage: jevcc plugin-files <plugin dir>"))
	}
	for name, body := range install.Commands("jevcc") {
		if err := fsutil.WriteFileAtomic(filepath.Join(args[0], "commands", name), body, 0o644); err != nil {
			return fail(err)
		}
	}
	for name, body := range install.Agents() {
		if err := fsutil.WriteFileAtomic(filepath.Join(args[0], "agents", name), body, 0o644); err != nil {
			return fail(err)
		}
	}
	return 0
}

// cmdWarm sends tiny requests. One call warms the API before routing starts
// (/jev on runs it in the background); --n/--gap turns it into a latency probe.
func cmdWarm(args []string) int {
	fset := flag.NewFlagSet("warm", flag.ContinueOnError)
	n := fset.Int("n", 1, "number of calls")
	gap := fset.Duration("gap", 0, "pause between calls")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	cfg, err := loadConfig()
	if err != nil {
		return fail(err)
	}
	c := &jev.Client{Endpoint: cfg.Endpoint, Key: cfg.APIKey, Model: cfg.Model, HTTP: &http.Client{Timeout: 60 * time.Second}}
	var lat []float64
	for i := 0; i < *n; i++ {
		if i > 0 {
			time.Sleep(*gap)
		}
		start := time.Now()
		_, err := c.Ask(context.Background(), "ping", map[string]jev.Question{"q": jev.Noul("This is a greeting.")})
		ms := float64(time.Since(start).Milliseconds())
		if err != nil {
			logx.Printf("warm: %v", err)
			if *n > 1 {
				fmt.Printf("%s call %d: error %v\n", time.Now().Format("15:04:05"), i+1, err)
			}
			continue
		}
		lat = append(lat, ms)
		if *n > 1 {
			fmt.Printf("%s call %d: %.0f ms\n", time.Now().Format("15:04:05"), i+1, ms)
		}
	}
	if *n > 1 && len(lat) > 0 {
		fmt.Printf("ok %d/%d, p50 %.0f ms, p95 %.0f ms, max %.0f ms\n", len(lat), *n, stats.Percentile(lat, 50), stats.Percentile(lat, 95), stats.Percentile(lat, 100))
	}
	return 0
}

func cmdSkills(args []string) int {
	if len(args) == 0 || (args[0] != "hide" && args[0] != "show") {
		return fail(errors.New("usage: jevcc skills hide|show [--scope user|project|local] [--dir D] [--mode name-only]"))
	}
	fset := flag.NewFlagSet("skills", flag.ContinueOnError)
	scope := fset.String("scope", "user", "user | project | local")
	dir := fset.String("dir", ".", "project directory for project/local scope")
	mode := fset.String("mode", "name-only", "skillOverrides value: name-only keeps the name listed, so Claude can still load it")
	if err := fset.Parse(args[1:]); err != nil {
		return 2
	}
	s, err := install.ParseScope(*scope)
	if err != nil {
		return fail(err)
	}
	abs, _ := filepath.Abs(*dir)
	o := install.Options{Scope: s, Dir: abs, Out: os.Stdout}
	if args[0] == "show" {
		err = install.ShowSkills(o)
	} else {
		var names []string
		for _, sk := range roster.Discover(paths.ClaudeDir(), abs) {
			names = append(names, sk.Name)
		}
		err = install.HideSkills(o, names, *mode)
	}
	if err != nil {
		return fail(err)
	}
	return 0
}

func cmdSkillEval(args []string) int {
	fset := flag.NewFlagSet("skilleval", flag.ContinueOnError)
	set := fset.String("set", "bench/router_set.jsonl", "labelled prompts (only those with a skill label are used)")
	model := fset.String("model", "haiku", "agent model under test")
	limit := fset.Int("limit", 0, "use only the first N labelled prompts")
	workers := fset.Int("workers", 4, "parallel claude runs")
	outDir := fset.String("out", "bench/results", "where to write results")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	if _, err := loadConfig(); err != nil {
		return fail(err)
	}
	items, err := eval.LoadItems(*set)
	if err != nil {
		return fail(err)
	}
	var labelled []eval.Item
	for _, it := range items {
		if it.Skill != "" && (*limit == 0 || len(labelled) < *limit) {
			labelled = append(labelled, it)
		}
	}
	claude, err := exec.LookPath("claude")
	if err != nil {
		return fail(errors.New("claude not found on PATH"))
	}
	self, err := os.Executable()
	if err != nil {
		return fail(err)
	}
	cwd, _ := os.Getwd()
	var names []string
	for _, s := range roster.Discover(paths.ClaudeDir(), cwd) {
		names = append(names, s.Name)
	}
	conds := []string{eval.CondListing, eval.CondNames, eval.CondNamesJev}
	fmt.Fprintf(os.Stderr, "jevcc: %d prompts × %d conditions with claude -p --model %s…\n", len(labelled), len(conds), *model)
	runs := eval.SkillEval(context.Background(), labelled, conds, eval.SkillEvalOptions{
		Claude: claude, Model: *model, HookCmd: install.HookCommand(self), Skills: names, Workers: *workers,
	})
	md := eval.SkillReport(runs, conds, *model)
	stamp := time.Now().Format("20060102-150405")
	os.MkdirAll(*outDir, 0o755)
	for _, r := range runs {
		fsutil.AppendJSONL(filepath.Join(*outDir, "skilleval-"+stamp+".jsonl"), r)
	}
	os.WriteFile(filepath.Join(*outDir, "skilleval-"+stamp+".md"), []byte(md), 0o644)
	fmt.Println(md)
	return 0
}

func cmdEscalateEval(args []string) int {
	fset := flag.NewFlagSet("escalateval", flag.ContinueOnError)
	sets := fset.String("sets", "bench/router_set.jsonl,bench/router_holdout.jsonl", "labelled prompt files")
	workers := fset.Int("workers", 4, "parallel claude runs")
	turns := fset.Int("max-turns", 3, "agent turns to observe (a hand-off may follow a skill load)")
	only := fset.String("only", "", "only prompts with this tier label")
	outDir := fset.String("out", "bench/results", "where to write results")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	if _, err := loadConfig(); err != nil {
		return fail(err)
	}
	var items []eval.Item
	for _, s := range strings.Split(*sets, ",") {
		its, err := eval.LoadItems(s)
		if err != nil {
			return fail(err)
		}
		for _, it := range its {
			if *only == "" || it.Tier == *only {
				items = append(items, it)
			}
		}
	}
	claude, err := exec.LookPath("claude")
	if err != nil {
		return fail(errors.New("claude not found on PATH"))
	}
	self, err := os.Executable()
	if err != nil {
		return fail(err)
	}
	fmt.Fprintf(os.Stderr, "jevcc: %d prompts on haiku with escalation advice…\n", len(items))
	adviceFor := func(session string) (string, []string) {
		recs, _ := stats.LoadDecisions()
		for _, rec := range recs {
			if rec.SessionID == session && rec.Decision != nil {
				return rec.Decision.Tier, rec.Advice
			}
		}
		return "", nil
	}
	runs := eval.EscalateEval(context.Background(), items, eval.EscalateOptions{Claude: claude, HookCmd: install.HookCommand(self), Agents: install.AgentsJSON(), Workers: *workers, MaxTurns: *turns, AdviceFor: adviceFor})
	md := eval.EscalateReport(runs)
	stamp := time.Now().Format("20060102-150405")
	os.MkdirAll(*outDir, 0o755)
	for _, r := range runs {
		fsutil.AppendJSONL(filepath.Join(*outDir, "escalateval-"+stamp+".jsonl"), r)
	}
	os.WriteFile(filepath.Join(*outDir, "escalateval-"+stamp+".md"), []byte(md), 0o644)
	fmt.Println(md)
	return 0
}

func cmdGuardEval(args []string) int {
	fset := flag.NewFlagSet("guardeval", flag.ContinueOnError)
	set := fset.String("set", "bench/guard_set.jsonl", "labelled actions")
	outDir := fset.String("out", "bench/results", "where to write results")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	cfg, err := loadConfig()
	if err != nil {
		return fail(err)
	}
	items, err := guard.LoadSet(*set)
	if err != nil {
		return fail(err)
	}
	rows := guard.Eval(context.Background(), client(cfg), items, 8)
	md := guard.EvalReport(rows)
	stamp := time.Now().Format("20060102-150405")
	os.MkdirAll(*outDir, 0o755)
	for _, r := range rows {
		fsutil.AppendJSONL(filepath.Join(*outDir, "guardeval-"+stamp+".jsonl"), r)
	}
	os.WriteFile(filepath.Join(*outDir, "guardeval-"+stamp+".md"), []byte(md), 0o644)
	fmt.Println(md)
	return 0
}

func cmdFind(args []string) int {
	fset := flag.NewFlagSet("find", flag.ContinueOnError)
	in := fset.String("in", "", "rank the lines of this file instead of the project's files")
	k := fset.Int("k", 5, "results to show")
	dir := fset.String("dir", ".", "project root")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	q := strings.Join(fset.Args(), " ")
	if q == "" {
		return fail(errors.New(`usage: jevcc find "<question>" [--in FILE] [-k N]`))
	}
	cfg, err := loadConfig()
	if err != nil {
		return fail(err)
	}
	var res *find.Result
	if *in != "" {
		b, err := os.ReadFile(*in)
		if err != nil {
			return fail(err)
		}
		res, err = find.Lines(context.Background(), client(cfg), q, string(b), *k)
		if err != nil {
			return fail(err)
		}
		lines := strings.Split(string(b), "\n")
		for _, h := range res.Hits {
			n, _ := strconv.Atoi(h.Path)
			fmt.Printf("%s:%d  (%.2f)  %s\n", *in, n, h.P, strings.TrimSpace(lines[n-1]))
		}
	} else {
		files := find.Index(*dir)
		res, err = find.Files(context.Background(), client(cfg), q, files, *k)
		if err != nil {
			return fail(err)
		}
		for _, h := range res.Hits {
			fmt.Printf("%.2f  %s\n", h.P, h.Path)
		}
		fmt.Fprintf(os.Stderr, "(%d files indexed)\n", len(files))
	}
	fmt.Fprintf(os.Stderr, "answer exists: %.2f · jev %d tokens, $%.5f, %d ms\n", res.Exists, res.Tokens, res.CostUSD, res.MS)
	return 0
}

func cmdFindEval(args []string) int {
	fset := flag.NewFlagSet("findeval", flag.ContinueOnError)
	set := fset.String("set", "bench/find_set.jsonl", "questions with the expected file(s), a|b")
	dir := fset.String("dir", ".", "project root the questions are about")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	cfg, err := loadConfig()
	if err != nil {
		return fail(err)
	}
	f, err := os.Open(*set)
	if err != nil {
		return fail(err)
	}
	defer f.Close()
	type item struct {
		Question string `json:"question"`
		File     string `json:"file"`
	}
	var items []item
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var it item
		if json.Unmarshal(sc.Bytes(), &it) == nil && it.Question != "" {
			items = append(items, it)
		}
	}
	files := find.Index(*dir)
	inTop := func(hits []find.Hit, gold string, k int) bool {
		for i, h := range hits {
			if i == k {
				break
			}
			for _, g := range strings.Split(gold, "|") {
				if h.Path == g {
					return true
				}
			}
		}
		return false
	}
	var j1, j3, k1, k3 int
	var ms []float64
	var cost float64
	for _, it := range items {
		res, err := find.Files(context.Background(), client(cfg), it.Question, files, 3)
		if err != nil {
			return fail(err)
		}
		kw := find.Keyword(it.Question, files, 3)
		ms = append(ms, float64(res.MS))
		cost += res.CostUSD
		if inTop(res.Hits, it.File, 1) {
			j1++
		}
		if inTop(res.Hits, it.File, 3) {
			j3++
		} else {
			fmt.Fprintf(os.Stderr, "miss: %s → %v (want %s)\n", it.Question, res.Hits, it.File)
		}
		if inTop(kw, it.File, 1) {
			k1++
		}
		if inTop(kw, it.File, 3) {
			k3++
		}
	}
	n := len(items)
	fmt.Printf("| method | right file first | right file in top 3 |\n|---|---|---|\n")
	fmt.Printf("| keyword match | %d/%d | %d/%d |\n| Jev | %d/%d | %d/%d |\n", k1, n, k3, n, j1, n, j3, n)
	fmt.Printf("\n%d files indexed; Jev p50 %.0f ms, $%.5f per question\n", len(files), stats.Percentile(ms, 50), cost/float64(max(n, 1)))
	return 0
}

func cmdMemEval(args []string) int {
	fset := flag.NewFlagSet("memeval", flag.ContinueOnError)
	set := fset.String("set", "bench/memory_set.jsonl", "messages labelled remember 1/0")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	cfg, err := loadConfig()
	if err != nil {
		return fail(err)
	}
	f, err := os.Open(*set)
	if err != nil {
		return fail(err)
	}
	defer f.Close()
	type item struct {
		Message  string `json:"message"`
		Remember int    `json:"remember"`
	}
	var items []item
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var it item
		if json.Unmarshal(sc.Bytes(), &it) == nil && it.Message != "" {
			items = append(items, it)
		}
	}
	ps := make([]float64, len(items))
	var ms []float64
	jev.Parallel(len(items), 8, func(i int) {
		r, err := client(cfg).Ask(context.Background(), map[string]string{"request": items[i].Message}, map[string]jev.Question{"remember": jev.Noul(router.MemoryQuestion)})
		if err == nil {
			ps[i] = r.Answers["remember"].Noul
			ms = append(ms, float64(r.Latency.Milliseconds()))
		} else {
			ps[i] = -1
		}
	})
	fmt.Println("| threshold | lasting statements caught | one-off instructions flagged |\n|---|---|---|")
	for _, t := range []float64{0.5, 0.6, 0.7, 0.8, 0.9} {
		ck, cn, fk, fn := 0, 0, 0, 0
		for i, it := range items {
			if ps[i] < 0 {
				continue
			}
			if it.Remember == 1 {
				cn++
				if ps[i] >= t {
					ck++
				}
			} else {
				fn++
				if ps[i] >= t {
					fk++
				}
			}
		}
		fmt.Printf("| %.1f | %d/%d | %d/%d |\n", t, ck, cn, fk, fn)
	}
	fmt.Println("\nMisses at 0.7:")
	for i, it := range items {
		if (ps[i] >= 0.7) != (it.Remember == 1) {
			fmt.Printf("- label %d, p=%.2f: %s\n", it.Remember, ps[i], it.Message)
		}
	}
	return 0
}

func cmdInjEval(args []string) int {
	fset := flag.NewFlagSet("injeval", flag.ContinueOnError)
	set := fset.String("set", "bench/injection_set.jsonl", "snippets labelled injected 1/0")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	cfg, err := loadConfig()
	if err != nil {
		return fail(err)
	}
	f, err := os.Open(*set)
	if err != nil {
		return fail(err)
	}
	defer f.Close()
	type item struct {
		Content  string `json:"content"`
		Injected int    `json:"injected"`
	}
	var items []item
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var it item
		if json.Unmarshal(sc.Bytes(), &it) == nil && it.Content != "" {
			items = append(items, it)
		}
	}
	ps := make([]float64, len(items))
	jev.Parallel(len(items), 8, func(i int) {
		v, err := inject.Check(context.Background(), client(cfg), "WebFetch https://example.com/page", items[i].Content)
		if err != nil {
			ps[i] = -1
			return
		}
		ps[i] = v.Risk
	})
	fmt.Println("| threshold | injected caught | benign flagged |\n|---|---|---|")
	for _, t := range []float64{0.5, 0.6, 0.7, 0.8, 0.9} {
		ck, cn, fk, fn := 0, 0, 0, 0
		for i, it := range items {
			if ps[i] < 0 {
				continue
			}
			if it.Injected == 1 {
				cn++
				if ps[i] >= t {
					ck++
				}
			} else {
				fn++
				if ps[i] >= t {
					fk++
				}
			}
		}
		fmt.Printf("| %.1f | %d/%d | %d/%d |\n", t, ck, cn, fk, fn)
	}
	fmt.Println("\nMistakes at 0.7:")
	for i, it := range items {
		if (ps[i] >= 0.7) != (it.Injected == 1) {
			fmt.Printf("- label %d, p=%.2f: %.90s\n", it.Injected, ps[i], strings.ReplaceAll(it.Content, "\n", " "))
		}
	}
	return 0
}

func cmdCite(args []string) int {
	fset := flag.NewFlagSet("cite", flag.ContinueOnError)
	path := fset.String("claims", "", "JSONL of {claim, quote, source}; an \"expected\" field turns on scoring")
	minConf := fset.Float64("min-confidence", 0.8, "Jev answers below this go to review")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	if *path == "" {
		return fail(errors.New("--claims is required"))
	}
	cfg, err := loadConfig()
	if err != nil {
		return fail(err)
	}
	f, err := os.Open(*path)
	if err != nil {
		return fail(err)
	}
	defer f.Close()
	var claims []cite.Claim
	var expected []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var line struct {
			cite.Claim
			Expected string `json:"expected"`
		}
		if json.Unmarshal(sc.Bytes(), &line) == nil && line.Claim.Claim != "" {
			claims = append(claims, line.Claim)
			expected = append(expected, line.Expected)
		}
	}
	res := cite.Check(context.Background(), client(cfg), claims, *minConf, 8)
	counts := map[string]int{}
	right, scored := 0, 0
	for i, r := range res {
		counts[r.Verdict]++
		mark := ""
		if expected[i] != "" {
			scored++
			if r.Verdict == expected[i] {
				right++
			} else {
				mark = "  ✗ expected " + expected[i]
			}
		}
		extra := ""
		if r.Jev != "" {
			extra = fmt.Sprintf(" (%s, conf %.2f)", r.Jev, r.Confidence)
		}
		fmt.Printf("%-13s%s %s%s\n", r.Verdict, extra, oneLine(r.Claim.Claim, 80), mark)
	}
	fmt.Printf("\nverified %d · contradicted %d · unsupported %d · fabricated %d · needs review %d\n", counts[cite.Verified], counts[cite.Contradicted], counts[cite.Unsupported], counts[cite.Fabricated], counts[cite.Review])
	if scored > 0 {
		fmt.Printf("matches the expected verdict: %d/%d\n", right, scored)
	}
	return 0
}

// cmdBlenderMCP serves the bench's Blender tools over stdio MCP; the scene is
// scene.blend in the current directory.
func cmdBlenderMCP(args []string) int {
	bin := os.Getenv("BLENDER")
	if bin == "" {
		return fail(errors.New("set BLENDER to the Blender binary"))
	}
	e := blendermcp.Exec{Path: bin, Scene: "scene.blend"}
	if err := blendermcp.Serve(context.Background(), os.Stdin, os.Stdout, e); err != nil {
		return fail(err)
	}
	return 0
}

// cmdJudge runs the blind pairwise judge over a bench results file
// (docs/eval-plan.md 5.2) and appends verdicts to judge-<results>.jsonl.
func cmdJudge(args []string) int {
	fset := flag.NewFlagSet("judge", flag.ContinueOnError)
	runsPath := fset.String("runs", "", "bench-*.jsonl to judge (its -streams directory holds the artifacts)")
	tasksDir := fset.String("tasks", "bench/open", "comma-separated task directories (for rubric.md)")
	baseline := fset.String("baseline", "opus/off", "arm every other arm is compared with")
	compare := fset.String("compare", "", "explicit comparisons X:Y,... (default: each arm vs --baseline, and each /open arm vs its restricted twin)")
	model := fset.String("model", "opus", "judge model")
	tokenFile := fset.String("token-file", "", "a `claude setup-token` token file for the judge's runs")
	claudeBin := fset.String("claude", "", "Claude Code binary (default: claude on PATH resolved to its versioned file)")
	max7d := fset.Float64("max-7d", 0.70, "stop once the judge account's 7-day quota reaches this (docs/eval-plan.md 9.2)")
	max5h := fset.Float64("max-5h", 0.95, "wait for the 5-hour reset once the judge account reaches this")
	if err := fset.Parse(args); err != nil || *runsPath == "" {
		fmt.Fprintln(os.Stderr, "usage: jevcc judge --runs bench-*.jsonl --token-file F [--compare X:Y,...]")
		return 2
	}
	runs, err := bench.LoadRuns(*runsPath)
	if err != nil {
		return fail(err)
	}
	tasks := map[string]bench.Task{}
	for _, d := range strings.Split(*tasksDir, ",") {
		ts, err := bench.LoadTasks(d, nil)
		if err != nil {
			return fail(err)
		}
		for _, t := range ts {
			tasks[t.Name] = t
		}
	}
	var comps [][2]string
	if *compare != "" {
		for _, c := range strings.Split(*compare, ",") {
			x, y, ok := strings.Cut(c, ":")
			if !ok {
				return fail(fmt.Errorf("bad comparison %q", c))
			}
			comps = append(comps, [2]string{x, y})
		}
	} else {
		seen := map[string]bool{}
		for _, r := range runs {
			if seen[r.Arm] || r.Arm == *baseline {
				continue
			}
			seen[r.Arm] = true
			comps = append(comps, [2]string{r.Arm, *baseline})
			if base, ok := strings.CutSuffix(r.Arm, "/open"); ok {
				comps = append(comps, [2]string{r.Arm, base})
			}
		}
	}
	token := os.Getenv("CLAUDE_CODE_OAUTH_TOKEN")
	if *tokenFile != "" {
		b, err := os.ReadFile(*tokenFile)
		if err != nil {
			return fail(err)
		}
		token = strings.TrimSpace(string(b))
	}
	if token == "" {
		return fail(errors.New("the judge runs isolated: pass --token-file"))
	}
	claude, err := pinnedClaude(*claudeBin)
	if err != nil {
		return fail(err)
	}
	fmt.Fprintln(os.Stderr, "claude:", claude)
	r := &bench.Runner{Claude: claude, PassEnv: []string{"CLAUDE_CODE_OAUTH_TOKEN=" + token}}
	gate := &bench.QuotaGate{Max7d: *max7d, Max5h: *max5h, Accounts: 1}
	streams := strings.TrimSuffix(*runsPath, ".jsonl") + "-streams"
	outPath := filepath.Join(filepath.Dir(*runsPath), "judge-"+filepath.Base(*runsPath))
	done := map[string]bool{}
	var prev []bench.Verdict
	if b, err := os.ReadFile(outPath); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			var v bench.Verdict
			if json.Unmarshal([]byte(line), &v) == nil && v.Error == "" {
				done[bench.JobKey(v.Task, v.ArmX+"|"+v.ArmY, v.Rep)] = true
				prev = append(prev, v)
			}
		}
	}
	pairs := bench.Pairs(runs, comps)
	total := 0.0
	for i, p := range pairs {
		t, ok := tasks[p.Task]
		if !ok || done[bench.JobKey(p.Task, p.X.Arm+"|"+p.Y.Arm, p.Rep)] {
			continue
		}
		if _, err := os.Stat(filepath.Join(t.Dir, "rubric.md")); err != nil {
			continue // tasks without a rubric are judged by their check alone
		}
		if !judgeGate(gate) {
			break
		}
		fmt.Fprintf(os.Stderr, "[%d/%d] %s #%d %s vs %s … ", i+1, len(pairs), p.Task, p.Rep, p.X.Arm, p.Y.Arm)
		v := r.Judge(context.Background(), t, p, streams, *model)
		// A usage limit or an outage says nothing about the pair: wait and
		// judge it again rather than record an error (up to 6 hours).
		for tries := 0; v.Error != "" && bench.Transient(v.Error) && tries < 36; tries++ {
			total += v.USD
			gate.Observe(0, v.Quota)
			fmt.Fprintf(os.Stderr, "%s; retrying in 10 minutes … ", v.Error)
			bench.WallSleep(10 * time.Minute)
			if !judgeGate(gate) {
				break
			}
			v = r.Judge(context.Background(), t, p, streams, *model)
		}
		if v.Error != "" && bench.Transient(v.Error) {
			fmt.Fprintln(os.Stderr, "still failing; stopping. Run the same command later to judge the rest.")
			break
		}
		gate.Observe(0, v.Quota)
		total += v.USD
		fsutil.AppendJSONL(outPath, v)
		prev = append(prev, v)
		if v.Error != "" {
			fmt.Fprintln(os.Stderr, "error:", v.Error)
			continue
		}
		fmt.Fprintf(os.Stderr, "%s ($%.3f)\n", v.Winner, v.USD)
	}
	wins := map[string][3]int{} // per comparison: X wins, Y wins, ties
	for _, v := range prev {
		if v.Error != "" {
			continue
		}
		k := v.ArmX + " vs " + v.ArmY
		w := wins[k]
		switch v.Winner {
		case v.ArmX:
			w[0]++
		case v.ArmY:
			w[1]++
		default:
			w[2]++
		}
		wins[k] = w
	}
	for k, w := range wins {
		fmt.Printf("%-40s X wins %d, Y wins %d, ties %d\n", k, w[0], w[1], w[2])
	}
	fmt.Fprintf(os.Stderr, "judge cost this run: $%.3f; verdicts in %s\n", total, outPath)
	return 0
}

// judgeGate waits while the judge account is at its 5-hour limit and reports
// false once it has reached its weekly cap.
func judgeGate(g *bench.QuotaGate) bool {
	for {
		a, wait, finished := g.Next(time.Now())
		if finished {
			fmt.Fprintf(os.Stderr, "the judge account reached %.0f%% of its 7-day quota; run the same command later to judge the rest\n", g.Max7d*100)
			return false
		}
		if a >= 0 {
			return true
		}
		fmt.Fprintf(os.Stderr, "judge account near its 5-hour limit; waiting %s\n", wait.Round(time.Minute))
		bench.WallSleep(wait + 30*time.Second)
	}
}

// cmdRate writes the blind human rating page for a bench results file
// (docs/eval-plan.md 5.3): rate.html, rate-media/ and rate-key.json next to it.
func cmdRate(args []string) int {
	fset := flag.NewFlagSet("rate", flag.ContinueOnError)
	runsPath := fset.String("runs", "", "bench-*.jsonl to sample from")
	tasksDir := fset.String("tasks", "bench/open", "comma-separated task directories (for human.json)")
	perTask := fset.Int("per-task", 6, "outputs to rate per task (whole repetitions: every arm's run of a rep)")
	seed := fset.Int64("seed", 1, "sampling and order seed")
	only := fset.String("only", "", "comma-separated task names to rate (default: every task with human.json)")
	minimal := fset.Bool("minimal", false, "show only images and videos with plain 1–5 buttons (no task text or option descriptions)")
	exclude := fset.String("exclude", "", "a previous rate-key.json: leave out the runs it already showed")
	if err := fset.Parse(args); err != nil || *runsPath == "" {
		fmt.Fprintln(os.Stderr, "usage: jevcc rate --runs bench-*.jsonl [--per-task N]")
		return 2
	}
	runs, err := bench.LoadRuns(*runsPath)
	if err != nil {
		return fail(err)
	}
	tasks := map[string]bench.Task{}
	for _, d := range strings.Split(*tasksDir, ",") {
		ts, err := bench.LoadTasks(d, nil)
		if err != nil {
			return fail(err)
		}
		for _, t := range ts {
			tasks[t.Name] = t
		}
	}
	if *only != "" {
		keep := map[string]bool{}
		for _, n := range strings.Split(*only, ",") {
			keep[strings.TrimSpace(n)] = true
		}
		for n := range tasks {
			if !keep[n] {
				delete(tasks, n)
			}
		}
	}
	if *exclude != "" {
		b, err := os.ReadFile(*exclude)
		if err != nil {
			return fail(err)
		}
		var seen []bench.RateKeyEntry
		if err := json.Unmarshal(b, &seen); err != nil {
			return fail(err)
		}
		skip := map[string]bool{}
		for _, k := range seen {
			skip[bench.JobKey(k.Task, k.Arm, k.Rep)] = true
		}
		kept := runs[:0]
		for _, r := range runs {
			if !skip[bench.JobKey(r.Task, r.Arm, r.Rep)] {
				kept = append(kept, r)
			}
		}
		runs = kept
	}
	dir := filepath.Dir(*runsPath)
	sample := bench.SampleForRating(runs, tasks, *perTask, *seed)
	if err := bench.WriteRatingPage(dir, strings.TrimSuffix(*runsPath, ".jsonl")+"-streams", sample, tasks, *minimal); err != nil {
		return fail(err)
	}
	fmt.Printf("%d outputs; open %s (the key is in rate-key.json; do not open it before rating)\n", len(sample), filepath.Join(dir, "rate.html"))
	return 0
}

// cmdReport writes the evaluation report (docs/eval-plan.md 6) from a bench
// results file, its judge verdicts and, when given, the human ratings.
func cmdReport(args []string) int {
	fset := flag.NewFlagSet("report", flag.ContinueOnError)
	runsPath := fset.String("runs", "", "bench-*.jsonl")
	tasksDir := fset.String("tasks", "bench/tasks,bench/hard,bench/open", "comma-separated task directories")
	ratingsPath := fset.String("ratings", "", "ratings.json exported from rate.html (its key is rate-key.json next to the runs)")
	if err := fset.Parse(args); err != nil || *runsPath == "" {
		fmt.Fprintln(os.Stderr, "usage: jevcc report --runs bench-*.jsonl [--ratings ratings.json]")
		return 2
	}
	runs, err := bench.LoadRuns(*runsPath)
	if err != nil {
		return fail(err)
	}
	tasks := map[string]bench.Task{}
	for _, d := range strings.Split(*tasksDir, ",") {
		ts, _ := bench.LoadTasks(d, nil)
		for _, t := range ts {
			tasks[t.Name] = t
		}
	}
	dir := filepath.Dir(*runsPath)
	var verdicts []bench.Verdict
	if b, err := os.ReadFile(filepath.Join(dir, "judge-"+filepath.Base(*runsPath))); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			var v bench.Verdict
			if json.Unmarshal([]byte(line), &v) == nil && v.Task != "" {
				verdicts = append(verdicts, v)
			}
		}
	}
	var ratings []bench.Rating
	if *ratingsPath != "" {
		var key []bench.RateKeyEntry
		kb, err := os.ReadFile(filepath.Join(dir, "rate-key.json"))
		if err != nil {
			return fail(err)
		}
		if err := json.Unmarshal(kb, &key); err != nil {
			return fail(err)
		}
		var scores map[string]struct {
			Score int `json:"score"`
		}
		rb, err := os.ReadFile(*ratingsPath)
		if err != nil {
			return fail(err)
		}
		if err := json.Unmarshal(rb, &scores); err != nil {
			return fail(err)
		}
		for _, k := range key {
			if s, ok := scores[k.ID]; ok && s.Score > 0 {
				ratings = append(ratings, bench.Rating{Task: k.Task, Arm: k.Arm, Rep: k.Rep, Score: s.Score})
			}
		}
	}
	md := bench.FullReport(bench.ReportInput{Runs: runs, Tasks: tasks, Verdicts: verdicts, Ratings: ratings})
	out := filepath.Join(dir, "report-"+strings.TrimSuffix(filepath.Base(*runsPath), ".jsonl")+".md")
	if err := os.WriteFile(out, []byte(md), 0o644); err != nil {
		return fail(err)
	}
	fmt.Println(md)
	fmt.Fprintln(os.Stderr, "wrote", out)
	return 0
}

// pinnedClaude resolves the Claude Code binary to its real, versioned file
// (~/.local/bin/claude is a symlink an update re-points).
func pinnedClaude(flagValue string) (string, error) {
	p := flagValue
	if p == "" {
		var err error
		if p, err = exec.LookPath("claude"); err != nil {
			return "", errors.New("claude not found on PATH")
		}
	}
	return filepath.EvalSymlinks(p)
}
