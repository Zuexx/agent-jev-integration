package classify

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zuexx/agent-jev-integration/internal/jev"
)

type fake struct{}

func (fake) Ask(_ context.Context, state any, _ map[string]jev.Question) (*jev.Response, error) {
	s, _ := state.(string)
	cat, conf := "neg", 0.9
	if strings.Contains(s, "love") {
		cat = "pos"
	}
	if strings.Contains(s, "meh") {
		conf = 0.2
	}
	return &jev.Response{
		Answers: map[string]jev.Answer{
			"sentiment": {Type: "choice", Choice: cat, Confidence: conf},
			"urgent":    {Type: "noul", Noul: float64(len(s)) / 100},
		},
		Usage: jev.Usage{InputTokens: 100},
	}, nil
}

var qs = map[string]jev.Question{"sentiment": jev.Choice("tone", map[string]any{"pos": nil, "neg": nil}), "urgent": jev.Noul("urgent")}

func TestLoadCSVAndRun(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.csv")
	os.WriteFile(p, []byte("id,text\n1,I love it\n2,meh\n3,this is broken and I need it fixed now\n"), 0o600)
	rows, err := Load(p)
	if err != nil || len(rows) != 3 {
		t.Fatal(rows, err)
	}
	res, sum := Run(context.Background(), fake{}, rows, Options{Questions: qs, Field: "text", Workers: 2, MinConfidence: 0.5})
	if sum.Rows != 3 || sum.NeedsReview != 1 || sum.Tokens != 300 || sum.Choices["sentiment"]["neg"] != 2 {
		t.Fatalf("%+v", sum)
	}
	if res[0].Answers["sentiment"].Choice != "pos" || !res[1].NeedsReview {
		t.Fatalf("%+v", res)
	}
	if top := Rank(res, "urgent"); top[0].Row["id"] != "3" {
		t.Fatalf("%+v", top[0])
	}
	var buf bytes.Buffer
	if err := Write(&buf, "csv", res, qs); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if lines[0] != "id,text,jev_sentiment,jev_sentiment_confidence,jev_urgent,jev_needs_review,jev_error" || !strings.HasPrefix(lines[1], "1,I love it,pos,0.900,") {
		t.Fatalf("%q", lines)
	}
}

func TestSampleLimit(t *testing.T) {
	rows := make([]map[string]any, 50)
	for i := range rows {
		rows[i] = map[string]any{"text": "x"}
	}
	res, sum := Run(context.Background(), fake{}, rows, Options{Questions: qs, Field: "text", Workers: 4, Limit: 5, Seed: 1})
	if len(res) != 5 || sum.Rows != 5 {
		t.Fatal(len(res))
	}
}

func TestLoadJSONLReportsLine(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.jsonl")
	os.WriteFile(p, []byte("{\"a\":1}\n\nnot json\n"), 0o600)
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), ":3:") {
		t.Fatal(err)
	}
}
