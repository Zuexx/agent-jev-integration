package eval

import (
	"math"
	"strings"
	"testing"

	"github.com/zuexx/agent-jev-integration/internal/config"
)

func TestWilson(t *testing.T) {
	lo, hi := Wilson(8, 10)
	if math.Abs(lo-0.49) > 0.01 || math.Abs(hi-0.943) > 0.01 {
		t.Fatal(lo, hi)
	}
	if lo, hi := Wilson(0, 0); lo != 0 || hi != 0 {
		t.Fatal(lo, hi)
	}
}

func TestParseBaseline(t *testing.T) {
	if tier, skill := parseBaseline("```json\n{\"tier\":\"haiku\",\"skill\":\"none\"}\n```"); tier != "haiku" || skill != "" {
		t.Fatal(tier, skill)
	}
}

func TestReport(t *testing.T) {
	t.Setenv("JEVCC_HOME", t.TempDir())
	cfg, _ := config.Load()
	rows := []Row{
		{Item: Item{Prompt: "find x", Tier: "haiku", Skill: "none"}, Jev: Pred{Tier: "haiku", Conf: 0.9, LatencyMS: 300, CostUSD: 0.0001}},
		{Item: Item{Prompt: "design y", Tier: "opus", Skill: "brainstorming"}, Jev: Pred{Tier: "sonnet", Conf: 0.4, Skill: "brainstorming", LatencyMS: 600}},
		{Item: Item{Prompt: "add test", Tier: "sonnet", Skill: "tdd|superpowers:tdd"}, Jev: Pred{Tier: "opus", Conf: 0.7, Skill: "pdf", LatencyMS: 500}},
	}
	md := Report(rows, cfg, 10)
	for _, want := range []string{
		"| tier exact match | 33.3% (1/3",
		"| under-routed (cheaper than label: quality risk) | 33.3% (1/3",
		"| tier exact match when confident | 100.0% (1/1",
		"| skill: wrong pick on prompts a skill covers | 50.0% (1/2",
		"| skill: needless pick when none fits | 0.0% (0/1",
		"| opus | 0 | 1 | 0 | 0 |",
		"tier sonnet→opus",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("missing %q in\n%s", want, md)
		}
	}
}

func TestSkillOK(t *testing.T) {
	it := Item{Skill: "a|b"}
	if !it.SkillOK("b") || it.SkillOK("") || it.SkillOK("c") {
		t.Fatal("alternatives")
	}
	if !(Item{Skill: "none"}).SkillOK("") || (Item{Skill: "none"}).SkillOK("a") {
		t.Fatal("none")
	}
}
