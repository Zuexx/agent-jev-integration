package cite

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zuexx/agent-jev-integration/internal/jev"
)

type fake struct{}

func (fake) Ask(_ context.Context, state any, _ map[string]jev.Question) (*jev.Response, error) {
	claim := state.(map[string]string)["claim"]
	c := "supports"
	if strings.Contains(claim, "free") {
		c = "contradicts"
	}
	return &jev.Response{Answers: map[string]jev.Answer{"relation": {Choice: c, Confidence: 0.9}}}, nil
}

func TestCheck(t *testing.T) {
	src := filepath.Join(t.TempDir(), "s.md")
	os.WriteFile(src, []byte("Price: $0.042 per million   input tokens.\nOutput is free."), 0o600)
	res := Check(context.Background(), fake{}, []Claim{
		{"Input costs $0.042 per million tokens", "$0.042 per million input tokens", src},
		{"Input is free", "$0.042 per million input tokens", src},
		{"It runs on GPUs", "runs on 8 GPUs", src},
	}, 0.6, 2)
	got := []string{res[0].Verdict, res[1].Verdict, res[2].Verdict}
	if strings.Join(got, ",") != "verified,contradicted,fabricated" {
		t.Fatal(got)
	}
}
