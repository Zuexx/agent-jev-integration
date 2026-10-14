package router

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zuexx/agent-jev-integration/internal/config"
	"github.com/zuexx/agent-jev-integration/internal/jev"
	"github.com/zuexx/agent-jev-integration/internal/roster"
)

type fake struct {
	calls [][]string
	fn    func(qs map[string]jev.Question) map[string]jev.Answer
}

func (f *fake) Ask(_ context.Context, _ any, qs map[string]jev.Question) (*jev.Response, error) {
	var ids []string
	for k := range qs {
		ids = append(ids, k)
	}
	f.calls = append(f.calls, ids)
	return &jev.Response{Model: "jev-1.13.0", Answers: f.fn(qs), Usage: jev.Usage{InputTokens: 1000}, Latency: 200 * time.Millisecond}, nil
}

func cfg() *config.Config {
	c, _ := config.Load()
	c.Delegate = true
	return c
}

var skills = []roster.Skill{{Name: "tdd", Description: "test first"}, {Name: "pdf", Description: "pdf files"}, {Name: "xlsx"}, {Name: "docx"}}

func TestRoutesTierAndVerifiesSkill(t *testing.T) {
	t.Setenv("JEVCC_HOME", t.TempDir())
	f := &fake{fn: func(qs map[string]jev.Question) map[string]jev.Answer {
		if _, ok := qs["tier"]; ok {
			return map[string]jev.Answer{
				"tier":  {Choice: "haiku", Confidence: 0.8, Probabilities: map[string]float64{"haiku": 0.9, "sonnet": 0.1}},
				"skill": {Probabilities: map[string]float64{"tdd": 0.5, "none": 0.3, "pdf": 0.1, "xlsx": 0.05, "docx": 0.05}},
				"act":   {Noul: 0.9}, "steps": {Noul: 0.8}, "talk": {Noul: 0.1}, "bulk": {Noul: 0.1},
			}
		}
		if len(qs) != 4 { // pick + 3 fits
			t.Errorf("verify questions: %d", len(qs))
		}
		return map[string]jev.Answer{"pick": {Choice: "tdd"}, "fit0": {Noul: 0.7}, "fit1": {Noul: 0.1}, "fit2": {Noul: 0.1}}
	}}
	d, err := Route(context.Background(), f, cfg(), "add a test for parse()", skills)
	if err != nil {
		t.Fatal(err)
	}
	if d.Tier != "haiku" || d.Skill != "tdd" || d.Calls != 2 || d.Tokens != 2000 || d.LatencyMS != 400 {
		t.Fatalf("%+v", d)
	}
	if strings.Join(d.Candidates, ",") != "tdd,pdf,docx" && strings.Join(d.Candidates, ",") != "tdd,pdf,xlsx" {
		t.Fatalf("%v", d.Candidates)
	}
	ctx := Context(d, cfg())
	for _, want := range []string{"<jev_routing>", `subagent_type: "jev-haiku"`, "Skill: tdd — test first"} {
		if !strings.Contains(ctx, want) {
			t.Fatalf("missing %q in\n%s", want, ctx)
		}
	}
}

func TestGateSkipsSecondCall(t *testing.T) {
	t.Setenv("JEVCC_HOME", t.TempDir())
	f := &fake{fn: func(qs map[string]jev.Question) map[string]jev.Answer {
		return map[string]jev.Answer{
			"tier":  {Choice: "opus", Confidence: 0.9, Probabilities: map[string]float64{"opus": 1}},
			"skill": {Probabilities: map[string]float64{"tdd": 0.6, "none": 0.4}},
			"act":   {Noul: 0.1}, "steps": {Noul: 0.1}, "talk": {Noul: 0.95},
		}
	}}
	d, err := Route(context.Background(), f, cfg(), "what is a monad?", skills)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 || d.Skill != "" {
		t.Fatalf("%d calls, %+v", len(f.calls), d)
	}
	if got := Context(d, cfg()); got != "" {
		t.Fatalf("expected silence for top tier + no skill, got %q", got)
	}
}

func TestLowConfidenceGivesNoTierAdvice(t *testing.T) {
	t.Setenv("JEVCC_HOME", t.TempDir())
	d := &Decision{Tier: "haiku", TierConf: 0.2, Bulk: 0.9}
	got := Context(d, cfg())
	if strings.Contains(got, "Model:") || !strings.Contains(got, "jevcc classify") {
		t.Fatal(got)
	}
}

func TestNoDelegationAdviceByDefault(t *testing.T) {
	t.Setenv("JEVCC_HOME", t.TempDir())
	c, _ := config.Load()
	d := &Decision{Tier: "haiku", TierConf: 0.99, TierProbs: map[string]float64{"haiku": 0.99}}
	if got := Context(d, c); got != "" {
		t.Fatal(got)
	}
}

func TestDelegateViaModel(t *testing.T) {
	t.Setenv("JEVCC_HOME", t.TempDir())
	t.Setenv("JEVCC_DELEGATE", "model")
	c, _ := config.Load()
	d := &Decision{Tier: "haiku", TierConf: 0.99, TierProbs: map[string]float64{"haiku": 0.99}}
	if got := Context(d, c); !strings.Contains(got, `model: "haiku"`) {
		t.Fatal(got)
	}
}

func TestEscalatesFromCheapMain(t *testing.T) {
	t.Setenv("JEVCC_HOME", t.TempDir())
	t.Setenv("JEVCC_DELEGATE", "agent")
	t.Setenv("JEVCC_MAIN", "haiku")
	c, _ := config.Load()
	hard := &Decision{Tier: "opus", TierConf: 0.9, TierProbs: map[string]float64{"opus": 0.9}, Skill: "tdd", SkillFit: 0.9}
	if got := Context(hard, c); !strings.Contains(got, "FIRST") || !strings.Contains(got, `"jev-opus"`) || strings.Contains(got, "- Skill:") || !strings.Contains(got, "tdd skill looks relevant") {
		t.Fatal(got)
	}
	easy := &Decision{Tier: "haiku", TierConf: 0.9, TierProbs: map[string]float64{"haiku": 0.9}}
	if got := Context(easy, c); got != "" {
		t.Fatal(got)
	}
}

func TestEscalatesByDefaultFromDetectedHaiku(t *testing.T) {
	t.Setenv("JEVCC_HOME", t.TempDir())
	c, _ := config.Load()
	c.MainModel = "haiku"
	hard := &Decision{Tier: "opus", TierConf: 0.9, TierProbs: map[string]float64{"opus": 0.9}}
	if got := Context(hard, c); !strings.Contains(got, `"jev-opus"`) {
		t.Fatal("escalation should be on by default", got)
	}
	c.MainModel = "opus"
	easy := &Decision{Tier: "haiku", TierConf: 0.99, TierProbs: map[string]float64{"haiku": 0.99}}
	if got := Context(easy, c); got != "" {
		t.Fatal("delegating down must stay off by default", got)
	}
	if TierOf("claude-haiku-5-5", c) != "haiku" || TierOf("x", c) != "" {
		t.Fatal("TierOf")
	}
}

func TestLargeRosterUsesChunks(t *testing.T) {
	t.Setenv("JEVCC_HOME", t.TempDir())
	var big []roster.Skill
	for i := 0; i < 600; i++ {
		big = append(big, roster.Skill{Name: fmt.Sprintf("s%03d", i)})
	}
	f := &fake{fn: func(qs map[string]jev.Question) map[string]jev.Answer {
		opts := qs["skill"].Criteria.(map[string]any)
		if len(opts) > 255 {
			t.Errorf("a Choice got %d options", len(opts))
		}
		probs := map[string]float64{}
		for k := range opts {
			probs[k] = 0.001
		}
		if _, ok := opts["s512"]; ok {
			probs["s512"] = 0.9
		}
		return map[string]jev.Answer{"skill": {Probabilities: probs}, "tier": {Choice: "opus"}, "act": {Noul: 0.1}, "steps": {Noul: 0.1}, "talk": {Noul: 0.9}}
	}}
	d, err := Route(context.Background(), f, cfg(), "x", big)
	if err != nil || d.Candidates[0] != "s512" || d.Calls != 4 {
		t.Fatalf("%+v %v calls=%d", d, err, len(f.calls))
	}
}

func TestEscalationCap(t *testing.T) {
	t.Setenv("JEVCC_HOME", t.TempDir())
	c, _ := config.Load()
	c.MainModel = "haiku"
	c.EscalationsUsed = c.EscalateMaxPerSession
	hard := &Decision{Tier: "opus", TierConf: 0.9, TierProbs: map[string]float64{"opus": 0.9}}
	got := Context(hard, c)
	if strings.Contains(got, `"jev-opus"`) || !strings.Contains(got, "already escalated 3 times") || !strings.Contains(got, "/model opus") {
		t.Fatal(got)
	}
}

func TestBuildReportsAdviceKinds(t *testing.T) {
	t.Setenv("JEVCC_HOME", t.TempDir())
	c, _ := config.Load()
	c.MainModel = "haiku"
	hard := &Decision{Tier: "opus", TierConf: 0.9, TierProbs: map[string]float64{"opus": 0.9}, Skill: "tdd", SkillFit: 0.9}
	text, kinds := Build(hard, c)
	if text != Context(hard, c) {
		t.Fatal("Build and Context disagree")
	}
	if len(kinds) != 1 || kinds[0] != "escalate" {
		t.Fatal(kinds)
	}
	c.EscalationsUsed = c.EscalateMaxPerSession
	// Past the cap the skill line is not suppressed, so both kinds appear.
	if _, kinds := Build(hard, c); len(kinds) != 2 || kinds[0] != "escalate_capped" || kinds[1] != "skill" {
		t.Fatal(kinds)
	}
	c.EscalationsUsed = 0
	c.MainModel = "opus"
	easy := &Decision{Tier: "haiku", TierConf: 0.99, TierProbs: map[string]float64{"haiku": 0.99}, Skill: "pdf", SkillFit: 0.9}
	if text, kinds := Build(easy, c); !strings.Contains(text, "- Skill: pdf") || len(kinds) != 1 || kinds[0] != "skill" {
		t.Fatal(text, kinds)
	}
	if text, kinds := Build(&Decision{}, c); text != "" || kinds != nil {
		t.Fatal(text, kinds)
	}
}
