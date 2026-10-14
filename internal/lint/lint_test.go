package lint

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zuexx/agent-jev-integration/internal/jev"
)

const src = `package x

// increment i
func doStuff(p string) error {
	log.Printf("login ok for %s with password %s", user, pw)
	i++
	return os.Remove(p)
}

//go:build linux
`

var all = map[string]bool{"comments": true, "logs": true, "functions": true}

func TestExtract(t *testing.T) {
	cs := Extract("x.go", src, all)
	var got []string
	for _, c := range cs {
		got = append(got, fmt.Sprintf("%s:%d", c.Kind, c.Line))
	}
	if strings.Join(got, ",") != "comments:3,functions:4,logs:5,comments:10" {
		t.Fatal(got)
	}
	if len(cs) != 4 || cs[0].Text != "increment i" || !strings.Contains(cs[0].Context, "func doStuff") || cs[2].Kind != "logs" {
		t.Fatalf("%+v", cs)
	}
	if Extract("x.md", src, all) != nil {
		t.Fatal("markdown is not code")
	}
	py := Extract("a.py", "#!/usr/bin/env python\n# real comment here\ndef f():\n    print(x)\n", all)
	if len(py) != 3 || py[0].Line != 2 {
		t.Fatalf("%+v", py)
	}
}

type fake struct{}

func (fake) Ask(_ context.Context, state any, qs map[string]jev.Question) (*jev.Response, error) {
	text := state.(map[string]any)["text"].(string)
	ans := map[string]jev.Answer{}
	if _, ok := qs["log-sensitive"]; ok {
		ans["log-sensitive"] = jev.Answer{Type: "choice", Choice: "secret", Probabilities: map[string]float64{"secret": 0.8}}
	}
	if _, ok := qs["comment-quality"]; ok {
		v := "useful"
		if strings.Contains(text, "increment") {
			v = "misleading"
		}
		ans["comment-quality"] = jev.Answer{Type: "choice", Choice: v, Probabilities: map[string]float64{v: 0.95}}
	}
	if _, ok := qs["function-name"]; ok {
		ans["function-name"] = jev.Answer{Type: "noul", Noul: 0.85}
	}
	return &jev.Response{Answers: ans, Usage: jev.Usage{InputTokens: 300}}, nil
}

func TestRun(t *testing.T) {
	rep := Run(context.Background(), fake{}, Extract("x.go", src, all), DefaultRules, 3)
	if rep.Count("error") != 2 || rep.Count("warn") != 1 || rep.Tokens != 1200 {
		t.Fatalf("%+v", rep)
	}
	if rep.Findings[0].Severity != "error" || rep.Findings[2].Rule != "function-name" {
		t.Fatalf("%+v", rep.Findings)
	}
	if !strings.Contains(rep.String(), "x.go:5: error [log-sensitive=secret") {
		t.Fatal(rep.String())
	}
}

func TestFilesWalksWithoutGit(t *testing.T) {
	d := t.TempDir()
	os.MkdirAll(filepath.Join(d, "node_modules", "m"), 0o755)
	os.WriteFile(filepath.Join(d, "a.go"), nil, 0o644)
	os.WriteFile(filepath.Join(d, "README.md"), nil, 0o644)
	os.WriteFile(filepath.Join(d, "node_modules", "m", "b.js"), nil, 0o644)
	t.Setenv("GIT_CEILING_DIRECTORIES", d)
	fs := Files([]string{d})
	if len(fs) != 1 || filepath.Base(fs[0]) != "a.go" {
		t.Fatal(fs)
	}
}

func TestMinPDropsUnsureVerdicts(t *testing.T) {
	rules := []Rule{{ID: "c", Target: "comments", Options: map[string]string{"bad": ""}, Severity: map[string]string{"bad": "error"}, MinP: map[string]float64{"bad": 0.9}}}
	rep := Run(context.Background(), unsure{}, []Candidate{{Kind: "comments", Text: "x"}}, rules, 1)
	if len(rep.Findings) != 0 {
		t.Fatal(rep.Findings)
	}
}

type unsure struct{}

func (unsure) Ask(context.Context, any, map[string]jev.Question) (*jev.Response, error) {
	return &jev.Response{Answers: map[string]jev.Answer{"c": {Type: "choice", Choice: "bad", Probabilities: map[string]float64{"bad": 0.8}}}}, nil
}
