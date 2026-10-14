// Package lint is a semantic linter: rules are plain-language questions that
// Jev asks about extracted candidates (comment blocks, log lines, functions).
package lint

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zuexx/agent-jev-integration/internal/jev"
)

type Asker interface {
	Ask(ctx context.Context, state any, qs map[string]jev.Question) (*jev.Response, error)
}

// Rule targets one candidate kind. With Options it is a Choice and Severity
// maps an option to "error"/"warn" (unlisted options are fine). Without
// Options it is a Noul and a yes ≥ Threshold is reported at Severity[""].
type Rule struct {
	ID        string            `json:"id"`
	Target    string            `json:"target"` // comments | logs | functions
	Question  string            `json:"question"`
	Options   map[string]string `json:"options,omitempty"`
	Severity  map[string]string `json:"severity"`
	Threshold float64           `json:"threshold,omitempty"`
	// MinP is the probability a choice verdict needs before it is reported.
	MinP map[string]float64 `json:"min_p,omitempty"`
}

type Candidate struct {
	Kind    string `json:"kind"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	Text    string `json:"text"`
	Context string `json:"context"`
}

type Finding struct {
	Candidate
	Rule     string  `json:"rule"`
	Verdict  string  `json:"verdict"`
	Severity string  `json:"severity"`
	P        float64 `json:"p"`
}

type Report struct {
	Files      int
	Candidates map[string]int
	Findings   []Finding
	Failed     int
	Tokens     int
	CostUSD    float64
	Wall       time.Duration
}

var DefaultRules = []Rule{
	{
		ID: "log-sensitive", Target: "logs",
		Question: "What does this log/print statement output, judging by the variables and text in it?",
		Options: map[string]string{
			"safe":   "Nothing sensitive: messages, counts, ids, status, errors, names of accounts, files or settings.",
			"pii":    "Personal data about a real person: their email address, phone, home address, real name, national/ID numbers, IP address.",
			"secret": "A secret or money: password, token, API key, credential, session cookie, card number, or a payment amount.",
		},
		Severity: map[string]string{"pii": "warn", "secret": "error"},
	},
	{
		ID: "comment-quality", Target: "comments",
		Question: "Judge the comment in `text` against the code in `context`.",
		Options: map[string]string{
			"useful":       "Explains why, a constraint, or something the code alone does not say.",
			"redundant":    "Only restates what the code obviously does; deleting it loses nothing.",
			"misleading":   "Clearly contradicts the code shown in `context` (states something the visible code plainly does not do).",
			"unverifiable": "Refers to behaviour, values or code not shown in `context`, so it cannot be checked here.",
			"license":      "A license header, directive, or tooling annotation (e.g. go:build, eslint, type: ignore).",
		},
		Severity: map[string]string{"redundant": "warn", "misleading": "error"},
		// Measured: planted misleading comments score 1.00; accurate comments
		// that mention code outside the window score 0.42–0.80.
		MinP: map[string]float64{"misleading": 0.9},
	},
	{
		ID: "function-name", Target: "functions",
		Question:  "Is the function's name misleading or too vague to tell a reader what it does, including important side effects (writes files, network calls, deletes data, mutates its arguments)?",
		Severity:  map[string]string{"": "warn"},
		Threshold: 0.7,
	},
}

var (
	codeExt = map[string]string{
		".go": "c", ".js": "c", ".jsx": "c", ".ts": "c", ".tsx": "c", ".java": "c", ".kt": "c", ".swift": "c",
		".rs": "c", ".c": "c", ".h": "c", ".cc": "c", ".cpp": "c", ".cs": "c", ".scala": "c", ".dart": "c", ".php": "c",
		".py": "#", ".rb": "#", ".sh": "#", ".bash": "#", ".zsh": "#", ".pl": "#", ".r": "#",
		".sql": "-", ".lua": "-",
	}
	logLine = regexp.MustCompile(`(?i)\b(log|logger|logging|slog|zap|logrus|console|fmt\.(f?print|errorf)|print|println|printf|puts|eprintln|warn|debug|info)\w*\s*[\.(!]`)
	funcRe  = regexp.MustCompile(`^\s*(func\s+(\([^)]*\)\s*)?\w+|(async\s+)?def\s+\w+|(export\s+)?(async\s+)?function\s+\w+|(pub(\(\w+\))?\s+)?(async\s+)?fn\s+\w+|(export\s+)?const\s+\w+\s*=\s*(async\s*)?\([^)]*\)\s*=>)`)
)

func commentText(line, style string) (string, bool) {
	t := strings.TrimSpace(line)
	switch style {
	case "c":
		for _, p := range []string{"///", "//", "/**", "/*", "*/", "*"} {
			if strings.HasPrefix(t, p) {
				return strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(t, p), "*/")), true
			}
		}
	case "#":
		if strings.HasPrefix(t, "#") && !strings.HasPrefix(t, "#!") {
			return strings.TrimSpace(strings.TrimPrefix(t, "#")), true
		}
	case "-":
		if strings.HasPrefix(t, "--") {
			return strings.TrimSpace(strings.TrimPrefix(t, "--")), true
		}
	}
	return "", false
}

func window(lines []string, from, to int) string {
	from, to = max(0, from), min(len(lines), to)
	return strings.Join(lines[from:to], "\n")
}

// Extract finds candidates in one file's content.
func Extract(file, content string, kinds map[string]bool) []Candidate {
	style, ok := codeExt[strings.ToLower(filepath.Ext(file))]
	if !ok {
		return nil
	}
	lines := strings.Split(content, "\n")
	var out []Candidate
	for i := 0; i < len(lines); i++ {
		if c, ok := commentText(lines[i], style); ok {
			start := i
			block := []string{c}
			for i+1 < len(lines) {
				c2, ok := commentText(lines[i+1], style)
				if !ok {
					break
				}
				i++
				block = append(block, c2)
			}
			text := strings.TrimSpace(strings.Join(block, "\n"))
			if kinds["comments"] && len(text) >= 8 {
				out = append(out, Candidate{Kind: "comments", File: file, Line: start + 1, Text: text, Context: window(lines, i+1, i+9)})
			}
			continue
		}
		if kinds["functions"] && funcRe.MatchString(lines[i]) {
			out = append(out, Candidate{Kind: "functions", File: file, Line: i + 1, Text: strings.TrimSpace(lines[i]), Context: window(lines, i, i+25)})
		}
		if kinds["logs"] && logLine.MatchString(lines[i]) {
			out = append(out, Candidate{Kind: "logs", File: file, Line: i + 1, Text: strings.TrimSpace(lines[i]), Context: window(lines, i-3, i+1)})
		}
	}
	return out
}

var skipDirs = map[string]bool{".git": true, "node_modules": true, "vendor": true, "dist": true, "build": true, ".venv": true, "__pycache__": true, "target": true}

// Files lists source files under paths, using `git ls-files` when possible so
// ignored files are skipped.
func Files(paths []string) []string {
	var out []string
	for _, p := range paths {
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		if !fi.IsDir() {
			out = append(out, p)
			continue
		}
		if b, err := exec.Command("git", "-C", p, "ls-files", "--cached", "--others", "--exclude-standard").Output(); err == nil {
			sc := bufio.NewScanner(strings.NewReader(string(b)))
			for sc.Scan() {
				f := filepath.Join(p, sc.Text())
				if _, ok := codeExt[strings.ToLower(filepath.Ext(f))]; ok {
					out = append(out, f)
				}
			}
			continue
		}
		filepath.WalkDir(p, func(f string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			if _, ok := codeExt[strings.ToLower(filepath.Ext(f))]; ok && !d.IsDir() {
				out = append(out, f)
			}
			return nil
		})
	}
	sort.Strings(out)
	return out
}

func ruleQuestion(r Rule) jev.Question {
	if len(r.Options) > 0 {
		opts := map[string]any{}
		for k, v := range r.Options {
			opts[k] = v
		}
		return jev.Choice(r.Question, opts)
	}
	return jev.Noul(r.Question)
}

// Run asks every rule for a candidate's kind in one call per candidate.
func Run(ctx context.Context, c Asker, cands []Candidate, rules []Rule, workers int) Report {
	byKind := map[string][]Rule{}
	for _, r := range rules {
		byKind[r.Target] = append(byKind[r.Target], r)
	}
	rep := Report{Candidates: map[string]int{}}
	files := map[string]bool{}
	for _, cd := range cands {
		rep.Candidates[cd.Kind]++
		files[cd.File] = true
	}
	rep.Files = len(files)
	var mu sync.Mutex
	start := time.Now()
	jev.Parallel(len(cands), workers, func(i int) {
		cd := cands[i]
		rs := byKind[cd.Kind]
		if len(rs) == 0 {
			return
		}
		qs := map[string]jev.Question{}
		for _, r := range rs {
			qs[r.ID] = ruleQuestion(r)
		}
		res, err := c.Ask(ctx, map[string]any{"file": cd.File, "text": cd.Text, "context": cd.Context}, qs)
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			rep.Failed++
			return
		}
		rep.Tokens += res.Usage.InputTokens
		rep.CostUSD += res.Usage.CostUSD()
		for _, r := range rs {
			a := res.Answers[r.ID]
			if len(r.Options) > 0 {
				if sev := r.Severity[a.Choice]; sev != "" && a.Probabilities[a.Choice] >= r.MinP[a.Choice] {
					rep.Findings = append(rep.Findings, Finding{Candidate: cd, Rule: r.ID, Verdict: a.Choice, Severity: sev, P: a.Probabilities[a.Choice]})
				}
			} else if a.Noul >= max(r.Threshold, 0.5) {
				rep.Findings = append(rep.Findings, Finding{Candidate: cd, Rule: r.ID, Verdict: "yes", Severity: r.Severity[""], P: a.Noul})
			}
		}
	})
	rep.Wall = time.Since(start)
	rank := map[string]int{"error": 0, "warn": 1}
	sort.SliceStable(rep.Findings, func(i, j int) bool {
		a, b := rep.Findings[i], rep.Findings[j]
		if rank[a.Severity] != rank[b.Severity] {
			return rank[a.Severity] < rank[b.Severity]
		}
		if a.P != b.P {
			return a.P > b.P
		}
		return a.File+fmt.Sprint(a.Line) < b.File+fmt.Sprint(b.Line)
	})
	return rep
}

func (r Report) Count(sev string) int {
	n := 0
	for _, f := range r.Findings {
		if f.Severity == sev {
			n++
		}
	}
	return n
}

func (r Report) String() string {
	var b strings.Builder
	for _, f := range r.Findings {
		fmt.Fprintf(&b, "%s:%d: %s [%s=%s p=%.2f] %s\n", f.File, f.Line, f.Severity, f.Rule, f.Verdict, f.P, oneLine(f.Text, 100))
	}
	var kinds []string
	for k, n := range r.Candidates {
		kinds = append(kinds, fmt.Sprintf("%s %d", k, n))
	}
	sort.Strings(kinds)
	fmt.Fprintf(&b, "\n%d errors, %d warnings — %d files, candidates: %s, failed %d\n", r.Count("error"), r.Count("warn"), r.Files, strings.Join(kinds, ", "), r.Failed)
	fmt.Fprintf(&b, "jev: %d input tokens, $%.5f, wall %.1fs", r.Tokens, r.CostUSD, r.Wall.Seconds())
	return b.String()
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
