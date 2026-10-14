// Package find narrows what Claude has to read: Jev ranks a project's files
// (or one file's lines) against a plain-language question, after TypeSafe's
// "Line-by-line search" and "Re-ranking" cookbooks.
package find

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/zuexx/agent-jev-integration/internal/jev"
	"github.com/zuexx/agent-jev-integration/internal/lint"
)

type Asker interface {
	Ask(ctx context.Context, state any, qs map[string]jev.Question) (*jev.Response, error)
}

const (
	maxChoice  = 254 // TypeSafe allows 255 options; one is "none"
	summaryLen = 220
	None       = "none"
)

var symbolRe = regexp.MustCompile(`(?m)^\s*(?:func\s+(?:\([^)]*\)\s*)?(\w+)|type\s+(\w+)|def\s+(\w+)|class\s+(\w+)|(?:export\s+)?(?:async\s+)?function\s+(\w+)|(?:pub\s+)?fn\s+(\w+))`)

// Summary is the one line Jev sees per file: its first comment and its symbols.
func Summary(path, content string) string {
	var doc string
	for _, l := range strings.Split(content, "\n") {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "//") || strings.HasPrefix(t, "#") && !strings.HasPrefix(t, "#!") {
			doc = strings.TrimSpace(strings.TrimLeft(t, "/# "))
			break
		}
		if t != "" && !strings.HasPrefix(t, "package") && !strings.HasPrefix(t, "#!") {
			break
		}
	}
	var syms []string
	for _, m := range symbolRe.FindAllStringSubmatch(content, -1) {
		for _, g := range m[1:] {
			if g != "" {
				syms = append(syms, g)
				break
			}
		}
	}
	s := doc
	if len(syms) > 0 {
		s += " | " + strings.Join(syms, ", ")
	}
	if r := []rune(s); len(r) > summaryLen {
		s = string(r[:summaryLen]) + "…"
	}
	return s
}

type File struct {
	Path    string
	Summary string
}

var markers = []string{".git", "go.mod", "package.json", "pyproject.toml", "Cargo.toml", "pom.xml", "build.gradle"}

// IsProject reports whether dir looks like a code project root, so the hook
// never indexes a home directory or other large unrelated tree.
func IsProject(dir string) bool {
	for _, m := range markers {
		if _, err := os.Stat(filepath.Join(dir, m)); err == nil {
			return true
		}
	}
	return false
}

// Index summarises the source files under root (tests and testdata skipped).
func Index(root string) []File {
	var out []File
	for _, p := range lint.Files([]string{root}) {
		rel, _ := filepath.Rel(root, p)
		if strings.HasSuffix(rel, "_test.go") || strings.Contains(rel, "testdata"+string(filepath.Separator)) {
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil || len(b) > 1<<20 {
			continue
		}
		out = append(out, File{rel, Summary(rel, string(b))})
	}
	return out
}

type Hit struct {
	Path string  `json:"path"`
	P    float64 `json:"p"`
}

type Result struct {
	Hits    []Hit   `json:"hits"`
	Exists  float64 `json:"exists"`
	Tokens  int     `json:"jev_input_tokens"`
	CostUSD float64 `json:"jev_cost_usd"`
	MS      int64   `json:"jev_latency_ms"`
}

func (r *Result) add(res *jev.Response) {
	r.Tokens += res.Usage.InputTokens
	r.CostUSD += res.Usage.CostUSD()
	r.MS += res.Latency.Milliseconds()
}

func choice(files []File) map[string]any {
	opts := map[string]any{None: "No listed file is relevant."}
	for _, f := range files {
		opts[f.Path] = f.Summary
	}
	return opts
}

// Files ranks files for the question. Above 254 files it runs one call per
// chunk and a final call over the chunk winners (a two-level beam).
func Files(ctx context.Context, c Asker, question string, files []File, k int) (*Result, error) {
	res := &Result{}
	state := map[string]string{"question": question}
	q := "Which file of this code base would a developer open first to answer or carry out `question`?"
	pool := files
	if len(files) > maxChoice {
		var winners []File
		by := map[string]File{}
		for _, f := range files {
			by[f.Path] = f
		}
		for i := 0; i < len(files); i += maxChoice {
			chunk := files[i:min(i+maxChoice, len(files))]
			r, err := c.Ask(ctx, state, map[string]jev.Question{"file": jev.Choice(q, choice(chunk))})
			if err != nil {
				return nil, err
			}
			res.add(r)
			for j, p := range r.Answers["file"].Ranked() {
				if j == 8 {
					break
				}
				if p != None {
					winners = append(winners, by[p])
				}
			}
		}
		pool = winners
	}
	r, err := c.Ask(ctx, state, map[string]jev.Question{
		"file":   jev.Choice(q, choice(pool)),
		"exists": jev.Noul("`question` is about code that lives in this code base (not general knowledge or an external service)."),
	})
	if err != nil {
		return nil, err
	}
	res.add(r)
	res.Exists = r.Answers["exists"].Noul
	a := r.Answers["file"]
	for _, p := range a.Ranked() {
		if p == None {
			continue
		}
		res.Hits = append(res.Hits, Hit{p, a.Probabilities[p]})
		if len(res.Hits) == k {
			break
		}
	}
	return res, nil
}

// Lines ranks the lines of one file for the question (ids are 1-based line numbers).
func Lines(ctx context.Context, c Asker, question, content string, k int) (*Result, error) {
	lines := strings.Split(content, "\n")
	if len(lines) > maxChoice {
		lines = lines[:maxChoice] // single call; long files: rank files first, then read nearby
	}
	opts := map[string]any{None: "No line answers the question."}
	for i, l := range lines {
		if strings.TrimSpace(l) != "" {
			opts[fmt.Sprint(i+1)] = strings.TrimSpace(l)
		}
	}
	r, err := c.Ask(ctx, map[string]string{"question": question}, map[string]jev.Question{
		"line":   jev.Choice("Which line best answers `question`?", opts),
		"exists": jev.Noul("The listed lines contain an answer to `question`."),
	})
	if err != nil {
		return nil, err
	}
	res := &Result{Exists: r.Answers["exists"].Noul}
	res.add(r)
	a := r.Answers["line"]
	for _, p := range a.Ranked() {
		if p != None {
			res.Hits = append(res.Hits, Hit{p, a.Probabilities[p]})
		}
		if len(res.Hits) == k {
			break
		}
	}
	return res, nil
}

// Keyword is the comparison baseline: files scored by how many words of the
// question appear in their path and summary.
func Keyword(question string, files []File, k int) []Hit {
	words := strings.FieldsFunc(strings.ToLower(question), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	})
	var hits []Hit
	for _, f := range files {
		hay := strings.ToLower(f.Path + " " + f.Summary)
		n := 0
		for _, w := range words {
			if len(w) > 3 && strings.Contains(hay, w) {
				n++
			}
		}
		if n > 0 {
			hits = append(hits, Hit{f.Path, float64(n)})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].P > hits[j].P })
	if len(hits) > k {
		hits = hits[:k]
	}
	return hits
}
