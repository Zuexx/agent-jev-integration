package find

import (
	"context"
	"strings"
	"testing"

	"github.com/zuexx/agent-jev-integration/internal/jev"
)

func TestSummary(t *testing.T) {
	s := Summary("a.go", "// Package a does things.\npackage a\n\nfunc Run() {}\ntype T struct{}\nfunc (t T) M() {}\n")
	if s != "Package a does things. | Run, T, M" {
		t.Fatal(s)
	}
}

type fake struct{ calls int }

func (f *fake) Ask(_ context.Context, _ any, qs map[string]jev.Question) (*jev.Response, error) {
	f.calls++
	opts := qs["file"].Criteria.(map[string]any)
	probs := map[string]float64{}
	for k := range opts {
		probs[k] = 0.001
		if strings.HasSuffix(k, "7.go") {
			probs[k] = 0.9
		}
	}
	return &jev.Response{Answers: map[string]jev.Answer{"file": {Probabilities: probs}, "exists": {Noul: 0.9}}}, nil
}

func TestFilesChunksLargeRepos(t *testing.T) {
	var files []File
	for i := 0; i < 600; i++ {
		files = append(files, File{Path: "p/f" + strings.Repeat("x", i%3) + string(rune('a'+i%26)) + "_" + itoa(i) + ".go"})
	}
	f := &fake{}
	r, err := Files(context.Background(), f, "q", files, 3)
	if err != nil || f.calls != 4 || len(r.Hits) != 3 || !strings.HasSuffix(r.Hits[0].Path, "7.go") {
		t.Fatalf("calls=%d %+v %v", f.calls, r, err)
	}
}

func itoa(i int) string {
	return strings.TrimSpace(strings.Repeat(" ", 0) + string(rune('0'+i/100)) + string(rune('0'+i/10%10)) + string(rune('0'+i%10)))
}

func TestKeyword(t *testing.T) {
	h := Keyword("where is the latency probe", []File{{"cmd/warm.go", "latency probe"}, {"x.go", "other"}}, 2)
	if len(h) != 1 || h[0].Path != "cmd/warm.go" {
		t.Fatal(h)
	}
}
