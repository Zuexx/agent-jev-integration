package bench

import (
	"strings"
	"testing"
)

func TestFullReport(t *testing.T) {
	tasks := map[string]Task{"p": {Name: "p", Category: "planning"}, "v": {Name: "v", Category: "planning", Variant: "vague"}}
	var runs []Run
	for rep := 1; rep <= 3; rep++ {
		runs = append(runs,
			Run{Task: "p", Arm: "opus/off", Rep: rep, Pass: true, ClaudeUSD: 0.40, Account: "claude-2", ClaudeVersion: "2.1.300", ModelUSD: map[string]float64{"claude-opus-5-5": 0.4}},
			Run{Task: "p", Arm: "haiku/off", Rep: rep, Pass: rep != 2, ClaudeUSD: 0.02, Account: "claude-2"},
			Run{Task: "p", Arm: "haiku/up", Rep: rep, Pass: true, ClaudeUSD: 0.10, Spawns: map[string]int{"jev-review": 1, "jev-opus": 1}, SubagentTools: map[string]int{"jev-review/Edit": 2}})
	}
	runs = append(runs, Run{Task: "v", Arm: "opus/off", Rep: 1, Error: "isolation: plugin x", Violations: []string{"plugin x"}})
	vs := []Verdict{{Task: "p", Rep: 1, ArmX: "haiku/off", ArmY: "opus/off", Winner: "opus/off"}, {Task: "p", Rep: 2, ArmX: "haiku/off", ArmY: "opus/off", Winner: "tie"}}
	rs := []Rating{{Task: "p", Arm: "haiku/off", Rep: 1, Score: 2}, {Task: "p", Arm: "opus/off", Rep: 1, Score: 5}, {Task: "p", Arm: "haiku/off", Rep: 2, Score: 3}, {Task: "p", Arm: "opus/off", Rep: 2, Score: 3}}
	md := FullReport(ReportInput{Runs: runs, Tasks: tasks, Verdicts: vs, Ratings: rs})
	for _, want := range []string{
		"errors (excluded from results): 1; isolation violations: 1",
		"| planning | haiku/off | 3 | 67% (2/3) |",
		"### haiku/off vs opus/off",
		"| planning | 3 | -0.380 (",
		"| 0 / 1 | 0 / 1 / 1 |",
		"2.5 (n=2) vs 4.0 (n=2)",
		"On 2 pairs rated by both: agreement 100%",
		"### opus/down vs opus/off",
		"| planning | haiku/up | 3 | 100% (3/3) | $0.100 | $0.100 | 0s | 3 / 0 / 3 (6) | 0 |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in\n%s", want, md)
		}
	}
}

func TestSpawnKinds(t *testing.T) {
	k := SpawnKinds(Run{Arm: "sonnet/both", Spawns: map[string]int{"jev-opus": 1, "jev-haiku": 2, "jev-review": 1, "Explore": 1}, SubagentTools: map[string]int{"jev-review/Write": 1}})
	if k != (Spawned{Up: 1, Down: 2, Review: 1, ReviewEdits: 1, Other: 1}) {
		t.Fatalf("%+v", k)
	}
}
