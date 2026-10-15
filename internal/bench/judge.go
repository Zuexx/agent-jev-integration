package bench

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// The blind pairwise judge (docs/eval-plan.md 5.2). For a pair of runs of
// the same task it lays their artifacts out as A/ and B/, asks the judge
// model which better satisfies the task's rubric.md, then asks again with
// A and B swapped; only a winner that survives the swap counts, otherwise
// the pair is a tie. The judge sees outputs only: no arm, model or transcript.

// Pair is one comparison: X against Y on the same task and repetition.
type Pair struct {
	Task string
	Rep  int
	X, Y Run
}

// Verdict is the judged outcome of a pair.
type Verdict struct {
	Task     string   `json:"task"`
	Rep      int      `json:"rep"`
	ArmX     string   `json:"arm_x"`
	ArmY     string   `json:"arm_y"`
	Winner   string   `json:"winner"` // ArmX, ArmY or "tie"
	Reasons  []string `json:"reasons"`
	Model    string   `json:"judge_model"`
	USD      float64  `json:"judge_cost_usd"`
	Error    string   `json:"error,omitempty"`
	Duration int64    `json:"duration_ms"`
}

// Pairs matches each comparison "X:Y" by task and repetition among runs that
// finished without error and kept artifacts.
func Pairs(runs []Run, comparisons [][2]string) []Pair {
	by := map[string]Run{}
	for _, r := range runs {
		if r.Error == "" && r.Artifacts != "" {
			by[JobKey(r.Task, r.Arm, r.Rep)] = r
		}
	}
	var out []Pair
	seen := map[string]bool{}
	for _, r := range runs {
		for _, c := range comparisons {
			if r.Arm != c[0] {
				continue
			}
			x, okx := by[JobKey(r.Task, c[0], r.Rep)]
			y, oky := by[JobKey(r.Task, c[1], r.Rep)]
			k := JobKey(r.Task, c[0]+"|"+c[1], r.Rep)
			if okx && oky && !seen[k] {
				seen[k] = true
				out = append(out, Pair{Task: r.Task, Rep: r.Rep, X: x, Y: y})
			}
		}
	}
	return out
}

// Judge runs the comparison of p. streams is the directory artifacts are
// relative to; t gives the task prompt and rubric.md.
func (r *Runner) Judge(ctx context.Context, t Task, p Pair, streams, model string) Verdict {
	v := Verdict{Task: p.Task, Rep: p.Rep, ArmX: p.X.Arm, ArmY: p.Y.Arm, Model: model}
	start := time.Now()
	defer func() { v.Duration = time.Since(start).Milliseconds() }()
	rubric, err := os.ReadFile(filepath.Join(t.Dir, "rubric.md"))
	if err != nil {
		v.Error = "no rubric.md"
		return v
	}
	var picks []string // "X", "Y" or "tie", one per order
	for _, xFirst := range []bool{true, false} {
		a, b := p.X, p.Y
		if !xFirst {
			a, b = b, a
		}
		w, reason, usd, err := r.judgeOnce(ctx, t.Prompt, string(rubric), filepath.Join(streams, a.Artifacts), filepath.Join(streams, b.Artifacts), model)
		v.USD += usd
		if err != nil {
			v.Error = err.Error()
			return v
		}
		v.Reasons = append(v.Reasons, reason)
		switch {
		case w == "tie":
			picks = append(picks, "tie")
		case (w == "A") == xFirst:
			picks = append(picks, "X")
		default:
			picks = append(picks, "Y")
		}
	}
	switch {
	case picks[0] == "X" && picks[1] == "X":
		v.Winner = p.X.Arm
	case picks[0] == "Y" && picks[1] == "Y":
		v.Winner = p.Y.Arm
	default:
		v.Winner = "tie"
	}
	return v
}

var verdictRE = regexp.MustCompile(`(?s)\{[^{}]*"winner"\s*:\s*"(A|B|tie)"[^{}]*\}`)

func (r *Runner) judgeOnce(ctx context.Context, prompt, rubric, dirA, dirB, model string) (winner, reason string, usd float64, err error) {
	sb, err := newSandbox()
	if err != nil {
		return "", "", 0, err
	}
	defer os.RemoveAll(sb.Root)
	ev := filepath.Join(sb.Root, "evidence")
	for name, src := range map[string]string{"A": dirA, "B": dirB} {
		if err := evidence(src, filepath.Join(ev, name)); err != nil {
			return "", "", 0, err
		}
	}
	q := "You are a blind judge comparing two submissions for the same task. Their outputs are in A/ and B/ in the " +
		"current directory (images are PNG; videos were reduced to frames under frames/). Read everything in both before " +
		"deciding. You do not know who made them; judge only the outputs.\n\n## The task the authors were given\n" + prompt +
		"\n\n## Rubric\n" + rubric + "\n\nEnd your answer with one line of JSON: " +
		`{"winner": "A" | "B" | "tie", "reason": "<one or two sentences>"}` + ". Use tie only when neither is better on the rubric."
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.Claude, "-p", "--output-format", "json", "--no-session-persistence", "--strict-mcp-config",
		"--allowedTools", "Read,Glob,Grep", "--disallowedTools", "Bash", "Edit", "Write", "WebSearch", "WebFetch", "Agent",
		"--model", model, q)
	cmd.Dir = ev
	cmd.Env = sb.env(os.Environ(), r.PassEnv)
	out, err := cmd.Output()
	var res struct {
		Result  string  `json:"result"`
		Cost    float64 `json:"total_cost_usd"`
		IsError bool    `json:"is_error"`
	}
	if jerr := json.Unmarshal(out, &res); jerr != nil {
		return "", "", 0, fmt.Errorf("judge: %v %q", err, tail(string(out), 300))
	}
	if res.IsError {
		return "", "", res.Cost, fmt.Errorf("judge: %s", tail(res.Result, 200))
	}
	ms := verdictRE.FindAllString(res.Result, -1)
	if len(ms) == 0 {
		return "", "", res.Cost, fmt.Errorf("judge gave no verdict: %s", tail(res.Result, 200))
	}
	var j struct{ Winner, Reason string }
	json.Unmarshal([]byte(ms[len(ms)-1]), &j)
	return j.Winner, j.Reason, res.Cost, nil
}

// evidence copies a run's artifacts for the judge: videos become six frames,
// Blender files are dropped (their renders are kept), the rest is copied.
func evidence(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		os.MkdirAll(filepath.Dir(target), 0o755)
		switch strings.ToLower(filepath.Ext(p)) {
		case ".blend", ".blend1":
			return nil
		case ".mp4", ".mov", ".webm", ".mkv":
			return frames(p, filepath.Join(dst, "frames", strings.TrimSuffix(rel, filepath.Ext(rel))))
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
}

// frames extracts six evenly spaced frames of a video as PNGs.
func frames(video, dir string) error {
	os.MkdirAll(dir, 0o755)
	probe, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", video).Output()
	if err != nil {
		return fmt.Errorf("ffprobe %s: %w", video, err)
	}
	var dur float64
	fmt.Sscan(strings.TrimSpace(string(probe)), &dur)
	for i := 0; i < 6; i++ {
		at := dur * (float64(i) + 0.5) / 6
		out := filepath.Join(dir, fmt.Sprintf("t%05.1fs.png", at))
		exec.Command("ffmpeg", "-v", "error", "-ss", fmt.Sprintf("%.2f", at), "-i", video, "-frames:v", "1", "-y", out).Run()
	}
	return nil
}
