package bench

import (
	"fmt"
	"sort"
	"strings"
)

// FullReport is the evaluation report of docs/eval-plan.md 6: everything is
// recomputed from the raw runs, judge verdicts and human ratings, so anyone
// with the same files gets the same numbers.

// Rating is one human score joined with its run (from ratings.json + rate-key.json).
type Rating struct {
	Task, Arm string
	Rep       int
	Score     int
}

// PrimaryComparisons are the plan's 2.4 comparisons; everything else in the
// report is exploratory.
var PrimaryComparisons = [][2]string{
	{"haiku/off", "opus/off"}, {"sonnet/off", "opus/off"},
	{"haiku/up", "opus/off"}, {"opus/down", "opus/off"},
}

// ReportInput holds what the report is computed from.
type ReportInput struct {
	Runs     []Run
	Tasks    map[string]Task
	Verdicts []Verdict
	Ratings  []Rating
}

func FullReport(in ReportInput) string {
	var b strings.Builder
	b.WriteString("# Evaluation report: lead model × Jev × delegation × tools\n\n")
	b.WriteString("Plan: docs/eval-plan.md. Cost is Claude's `total_cost_usd` (list price; a subscription spends quota, not dollars) plus Jev's own cost. ")
	b.WriteString("Only the primary comparisons (plan 2.4) support conclusions; every other number is exploratory. A confidence interval that includes 0 means no clear difference.\n\n")

	// Environment and validity.
	versions, models, accounts := map[string]int{}, map[string]bool{}, map[string]int{}
	errs, violations := 0, 0
	for _, r := range in.Runs {
		if r.ClaudeVersion != "" {
			versions[r.ClaudeVersion]++
		}
		for m := range r.ModelUSD {
			models[m] = true
		}
		if r.Account != "" {
			accounts[r.Account]++
		}
		if r.Error != "" {
			errs++
		}
		if len(r.Violations) > 0 {
			violations++
		}
	}
	fmt.Fprintf(&b, "## Validity\n\n- Runs: %d; errors (excluded from results): %d; isolation violations: %d.\n", len(in.Runs), errs, violations)
	fmt.Fprintf(&b, "- Claude Code versions: %s. Models seen: %s.\n- Accounts (runs): %s.\n\n", keysCount(versions), strings.Join(sortedKeys(models), ", "), keysCount(accounts))

	// Per category and arm.
	type cell struct {
		runs, pass               int
		cost                     []float64
		dur                      []float64
		jevErr                   int
		up, down, review, rEdits int
	}
	cells := map[string]map[string]*cell{}
	var cats, arms []string
	seenCat, seenArm := map[string]bool{}, map[string]bool{}
	for _, r := range in.Runs {
		if r.Error != "" {
			continue
		}
		c := category(in.Tasks[r.Task])
		if !seenCat[c] {
			seenCat[c], cats = true, append(cats, c)
		}
		if !seenArm[r.Arm] {
			seenArm[r.Arm], arms = true, append(arms, r.Arm)
		}
		if cells[c] == nil {
			cells[c] = map[string]*cell{}
		}
		x := cells[c][r.Arm]
		if x == nil {
			x = &cell{}
			cells[c][r.Arm] = x
		}
		x.runs++
		if r.Pass {
			x.pass++
		}
		x.cost = append(x.cost, r.TotalUSD())
		x.dur = append(x.dur, float64(r.DurMS)/1000)
		x.jevErr += len(r.JevErrors)
		k := SpawnKinds(r)
		x.up, x.down, x.review, x.rEdits = x.up+k.Up, x.down+k.Down, x.review+k.Review, x.rEdits+k.ReviewEdits
	}
	sort.Strings(cats)
	sort.Strings(arms)
	b.WriteString("## Results by category and arm\n\nSubagents started: up = a stronger jev-* agent, down = a cheaper one, review = jev-review after the turn (edits = files it changed, i.e. how much of the output the reviewer rewrote).\n\n")
	b.WriteString("| category | arm | runs | pass | mean cost | cost per pass | mean time | up / down / review (edits) | Jev errors |\n|---|---|---|---|---|---|---|---|---|\n")
	for _, c := range cats {
		for _, a := range arms {
			x := cells[c][a]
			if x == nil {
				continue
			}
			perPass := "—"
			if x.pass > 0 {
				perPass = fmt.Sprintf("$%.3f", sum(x.cost)/float64(x.pass))
			}
			fmt.Fprintf(&b, "| %s | %s | %d | %s | $%.3f | %s | %.0fs | %d / %d / %d (%d) | %d |\n", c, a, x.runs, passRate(x.pass, x.runs), mean(x.cost), perPass, mean(x.dur), x.up, x.down, x.review, x.rEdits, x.jevErr)
		}
	}

	// Primary comparisons.
	b.WriteString("\n## Primary comparisons\n\n")
	byKey := map[string]Run{}
	for _, r := range in.Runs {
		if r.Error == "" {
			byKey[JobKey(r.Task, r.Arm, r.Rep)] = r
		}
	}
	for _, pc := range PrimaryComparisons {
		x, y := pc[0], pc[1]
		fmt.Fprintf(&b, "### %s vs %s\n\n| category | pairs | cost difference per run (95%% CI) | pass: %s only / %s only | judge: %s wins / %s wins / ties | human mean score %s vs %s |\n|---|---|---|---|---|---|\n",
			x, y, x, y, x, y, x, y)
		any := false
		for _, c := range cats {
			var diffs []float64
			xOnly, yOnly := 0, 0
			for _, r := range in.Runs {
				if r.Arm != x || r.Error != "" || category(in.Tasks[r.Task]) != c {
					continue
				}
				o, ok := byKey[JobKey(r.Task, y, r.Rep)]
				if !ok {
					continue
				}
				diffs = append(diffs, r.TotalUSD()-o.TotalUSD())
				if r.Pass && !o.Pass {
					xOnly++
				}
				if o.Pass && !r.Pass {
					yOnly++
				}
			}
			if len(diffs) == 0 {
				continue
			}
			any = true
			ci := "too few pairs"
			if len(diffs) >= 3 {
				lo, hi := BootstrapMeanCI(diffs, 4000, 1)
				ci = fmt.Sprintf("%+.3f (%+.3f to %+.3f)", mean(diffs), lo, hi)
				if lo <= 0 && hi >= 0 {
					ci += ", no clear difference"
				}
			}
			wx, wy, ties := 0, 0, 0
			for _, v := range in.Verdicts {
				if v.Error != "" || category(in.Tasks[v.Task]) != c || !((v.ArmX == x && v.ArmY == y) || (v.ArmX == y && v.ArmY == x)) {
					continue
				}
				switch v.Winner {
				case x:
					wx++
				case y:
					wy++
				default:
					ties++
				}
			}
			hx, hy := humanMean(in.Ratings, in.Tasks, c, x), humanMean(in.Ratings, in.Tasks, c, y)
			fmt.Fprintf(&b, "| %s | %d | %s | %d / %d | %d / %d / %d | %s vs %s |\n", c, len(diffs), ci, xOnly, yOnly, wx, wy, ties, hx, hy)
		}
		if !any {
			b.WriteString("| — | no paired runs yet | | | | |\n")
		}
		b.WriteString("\n")
	}

	// Per task graded measures.
	b.WriteString("## Graded measures per task (mean of METRIC lines, exploratory)\n\n")
	type agg struct {
		n   int
		sum map[string]float64
	}
	per := map[string]map[string]*agg{}
	for _, r := range in.Runs {
		if r.Error != "" || len(r.Metrics) == 0 {
			continue
		}
		if per[r.Task] == nil {
			per[r.Task] = map[string]*agg{}
		}
		a := per[r.Task][r.Arm]
		if a == nil {
			a = &agg{sum: map[string]float64{}}
			per[r.Task][r.Arm] = a
		}
		a.n++
		for k, v := range r.Metrics {
			a.sum[k] += v
		}
	}
	tasks := make([]string, 0, len(per))
	for t := range per {
		tasks = append(tasks, t)
	}
	sort.Strings(tasks)
	for _, t := range tasks {
		fmt.Fprintf(&b, "- **%s**: ", t)
		var parts []string
		for _, a := range arms {
			x := per[t][a]
			if x == nil {
				continue
			}
			var ms []string
			for _, k := range sortedKeys(x.sum) {
				ms = append(ms, fmt.Sprintf("%s %.2g", k, x.sum[k]/float64(x.n)))
			}
			parts = append(parts, fmt.Sprintf("%s (%s)", a, strings.Join(ms, ", ")))
		}
		b.WriteString(strings.Join(parts, "; ") + "\n")
	}

	// Judge vs human (plan 5.3): on pairs where both outputs were rated by
	// the human, turn the two scores into a preference and compare it with
	// the judge's verdict.
	agree, n, kappa := JudgeHumanAgreement(in.Verdicts, in.Ratings, in.Tasks)
	b.WriteString("\n## Judge vs human\n\n")
	if n == 0 {
		b.WriteString("No judged pair has both outputs rated by the human yet.\n")
	} else {
		fmt.Fprintf(&b, "On %d pairs rated by both: agreement %.0f%%, Cohen's kappa %.2f.", n, agree*100, kappa)
		if kappa < 0.6 {
			b.WriteString(" Below 0.6: the judge's verdicts are reported as unreliable here; rely on the human scores.")
		}
		b.WriteString("\n")
	}
	b.WriteString("\n## Known limits\n\nSee docs/eval-plan.md 11: single-prompt runs without user feedback; subagents cannot ask the user; tasks written by this project; ")
	b.WriteString("timing measured on one Intel Mac; Jev's decisions vary between runs; small samples per category.\n")
	return b.String()
}

func category(t Task) string {
	switch {
	case t.Category == "" && strings.Contains(t.Dir, "/hard/"):
		return "code-hard"
	case t.Category == "":
		return "code-easy"
	case t.Variant != "":
		return t.Category + " (" + t.Variant + ")"
	}
	return t.Category
}

func humanMean(rs []Rating, tasks map[string]Task, cat, arm string) string {
	var xs []float64
	for _, r := range rs {
		if r.Arm == arm && category(tasks[r.Task]) == cat && r.Score > 0 {
			xs = append(xs, float64(r.Score))
		}
	}
	if len(xs) == 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f (n=%d)", mean(xs), len(xs))
}

func sum(xs []float64) float64 {
	s := 0.0
	for _, x := range xs {
		s += x
	}
	return s
}

func sortedKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func keysCount(m map[string]int) string {
	var parts []string
	for _, k := range sortedKeys(m) {
		parts = append(parts, fmt.Sprintf("%s (%d)", k, m[k]))
	}
	if len(parts) == 0 {
		return "none recorded"
	}
	return strings.Join(parts, ", ")
}

// JudgeHumanAgreement compares judge verdicts with the human's implied
// preference (higher score wins, equal is a tie) on pairs where the human
// rated both outputs: the share that agree, how many pairs, and Cohen's
// kappa over the three outcomes.
func JudgeHumanAgreement(vs []Verdict, rs []Rating, tasks map[string]Task) (agree float64, n int, kappa float64) {
	score := map[string]int{}
	for _, r := range rs {
		score[JobKey(r.Task, r.Arm, r.Rep)] = r.Score
	}
	var conf [3][3]float64 // judge outcome × human outcome: X, Y, tie
	idx := func(winner, x, y string) int {
		switch winner {
		case x:
			return 0
		case y:
			return 1
		}
		return 2
	}
	for _, v := range vs {
		sx, okx := score[JobKey(v.Task, v.ArmX, v.Rep)]
		sy, oky := score[JobKey(v.Task, v.ArmY, v.Rep)]
		if v.Error != "" || !okx || !oky {
			continue
		}
		h := "tie"
		if sx > sy {
			h = v.ArmX
		} else if sy > sx {
			h = v.ArmY
		}
		conf[idx(v.Winner, v.ArmX, v.ArmY)][idx(h, v.ArmX, v.ArmY)]++
		n++
	}
	if n == 0 {
		return 0, 0, 0
	}
	var po, pe float64
	for i := 0; i < 3; i++ {
		po += conf[i][i]
		var row, col float64
		for j := 0; j < 3; j++ {
			row += conf[i][j]
			col += conf[j][i]
		}
		pe += row * col
	}
	po /= float64(n)
	pe /= float64(n * n)
	if pe == 1 {
		return po, n, 1
	}
	return po, n, (po - pe) / (1 - pe)
}

// Spawned is what kinds of subagents a run started, relative to its lead.
type Spawned struct{ Up, Down, Review, ReviewEdits, Other int }

var tierRank = map[string]int{"haiku": 0, "sonnet": 1, "opus": 2}

// SpawnKinds classifies a run's subagents: jev-<tier> above the lead's tier
// is up, below is down; jev-review is the post-turn review, and its Edit and
// Write calls are counted as the files it changed.
func SpawnKinds(r Run) Spawned {
	var k Spawned
	lead, _, _, _ := ParseLeadArm(r.Arm)
	if lead == "" {
		lead = r.Model
	}
	for t, n := range r.Spawns {
		tier := strings.TrimPrefix(t, "jev-")
		switch {
		case t == "jev-review":
			k.Review += n
		case strings.HasPrefix(t, "jev-") && tierRank[tier] > tierRank[lead]:
			k.Up += n
		case strings.HasPrefix(t, "jev-") && tierRank[tier] < tierRank[lead]:
			k.Down += n
		default:
			k.Other += n
		}
	}
	k.ReviewEdits = r.SubagentTools["jev-review/Edit"] + r.SubagentTools["jev-review/Write"] + r.SubagentTools["jev-review/MultiEdit"]
	return k
}
