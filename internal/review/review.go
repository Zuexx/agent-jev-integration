// Package review asks a fixed checklist of yes/no questions about every hunk
// of a diff in one Jev call per hunk, so only the risky items reach Claude.
package review

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zuexx/agent-jev-integration/internal/jev"
)

type Asker interface {
	Ask(ctx context.Context, state any, qs map[string]jev.Question) (*jev.Response, error)
}

// Item is one checklist question. A "yes" means a problem.
type Item struct {
	ID       string `json:"id"`
	Question string `json:"question"`
	Severity string `json:"severity"` // high | medium | low
}

type Hunk struct {
	File string `json:"file"`
	Line int    `json:"line"` // first line in the new file
	Text string `json:"text"`
}

type Finding struct {
	Hunk     Hunk    `json:"hunk"`
	Item     Item    `json:"item"`
	P        float64 `json:"p"`
	Priority float64 `json:"priority"`
}

type Report struct {
	Hunks     int
	Questions int
	Findings  []Finding
	Failed    []string
	Tokens    int
	CostUSD   float64
	Wall      time.Duration
}

const maxHunk = 12000

var hunkHeader = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// Hunks splits unified diff output into hunks, skipping deleted files.
func Hunks(diff string) []Hunk {
	var out []Hunk
	var cur *Hunk
	file := ""
	flush := func() {
		if cur != nil && file != "" && file != "/dev/null" {
			if len(cur.Text) > maxHunk {
				cur.Text = cur.Text[:maxHunk] + "\n…(hunk truncated)"
			}
			out = append(out, *cur)
		}
		cur = nil
	}
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			flush()
			file = ""
		case strings.HasPrefix(line, "+++ "):
			file = strings.TrimPrefix(strings.TrimPrefix(line, "+++ "), "b/")
		case strings.HasPrefix(line, "--- "):
		case strings.HasPrefix(line, "@@"):
			flush()
			n := 0
			if m := hunkHeader.FindStringSubmatch(line); m != nil {
				n, _ = strconv.Atoi(m[1])
			}
			cur = &Hunk{File: file, Line: n, Text: line + "\n"}
		default:
			if cur != nil {
				cur.Text += line + "\n"
			}
		}
	}
	flush()
	return out
}

var weight = map[string]float64{"high": 3, "medium": 2, "low": 1}

// Run asks every checklist item about every hunk. Findings are items whose
// "yes" probability is at least threshold, highest priority first.
func Run(ctx context.Context, c Asker, hunks []Hunk, items []Item, threshold float64, workers int) Report {
	qs := map[string]jev.Question{}
	for _, it := range items {
		qs[it.ID] = jev.Noul(map[string]any{
			"question": it.Question,
			"scope":    "Answer only about the change shown in `hunk` (lines starting with + were added, - removed). The rest of the change is not shown; `other_files_in_this_change` lists its files. Answer no unless the hunk itself shows the problem.",
		})
	}
	rep := Report{Hunks: len(hunks), Questions: len(hunks) * len(items)}
	seen := map[string]bool{}
	var files []string
	for _, h := range hunks {
		if !seen[h.File] {
			seen[h.File] = true
			files = append(files, h.File)
		}
	}
	var mu sync.Mutex
	start := time.Now()
	jev.Parallel(len(hunks), workers, func(i int) {
		h := hunks[i]
		r, err := c.Ask(ctx, map[string]any{"file": h.File, "hunk": h.Text, "other_files_in_this_change": files}, qs)
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			rep.Failed = append(rep.Failed, fmt.Sprintf("%s:%d: %v", h.File, h.Line, err))
			return
		}
		rep.Tokens += r.Usage.InputTokens
		rep.CostUSD += r.Usage.CostUSD()
		for _, it := range items {
			if p := r.Answers[it.ID].Noul; p >= threshold {
				rep.Findings = append(rep.Findings, Finding{Hunk: h, Item: it, P: p, Priority: p * weight[it.Severity]})
			}
		}
	})
	rep.Wall = time.Since(start)
	sort.SliceStable(rep.Findings, func(i, j int) bool {
		a, b := rep.Findings[i], rep.Findings[j]
		if a.Priority != b.Priority {
			return a.Priority > b.Priority
		}
		if a.Hunk.File != b.Hunk.File {
			return a.Hunk.File < b.Hunk.File
		}
		return a.Hunk.Line < b.Hunk.Line
	})
	return rep
}

// Markdown renders the report; top limits the findings listed (0 = all).
func (r Report) Markdown(top int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## jev review: %d findings over %d hunks\n\n", len(r.Findings), r.Hunks)
	fmt.Fprintf(&b, "%d checklist questions asked; jev %d input tokens, $%.5f, %.1fs.\n\n", r.Questions, r.Tokens, r.CostUSD, r.Wall.Seconds())
	fs := r.Findings
	if top > 0 && len(fs) > top {
		fs = fs[:top]
	}
	for i, f := range fs {
		fmt.Fprintf(&b, "%d. **%s** `%s:%d` [%s, p=%.2f] %s\n", i+1, f.Item.ID, f.Hunk.File, f.Hunk.Line, f.Item.Severity, f.P, f.Item.Question)
	}
	if len(fs) < len(r.Findings) {
		fmt.Fprintf(&b, "\n…and %d lower-priority findings (use --top 0 to list all).\n", len(r.Findings)-len(fs))
	}
	for _, f := range r.Failed {
		fmt.Fprintf(&b, "\n⚠ not reviewed: %s", f)
	}
	return strings.TrimRight(b.String(), "\n")
}
