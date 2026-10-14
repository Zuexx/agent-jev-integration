package audit

import (
	"strings"
	"testing"
	"time"

	"github.com/zuexx/agent-jev-integration/internal/config"
	"github.com/zuexx/agent-jev-integration/internal/router"
	"github.com/zuexx/agent-jev-integration/internal/stats"
)

func TestFindFlagsEachKind(t *testing.T) {
	t.Setenv("JEVCC_HOME", t.TempDir())
	cfg, _ := config.Load()
	t0 := time.Now().Add(-time.Hour)
	recs := []stats.Record{
		// Cheap session, high-confidence opus, no escalate advice: missed.
		{Time: t0, SessionID: "s1", PromptHead: "prove the lock is safe", Schema: stats.Schema, MainTier: "haiku",
			Decision: &router.Decision{Tier: "opus", TierConf: 0.9, TierProbs: map[string]float64{"opus": 0.9, "sonnet": 0.05}}},
		// The next prompt in the session says the previous answer failed.
		{Time: t0.Add(time.Minute), SessionID: "s1", PromptHead: "还是不行，fix it", Schema: stats.Schema, MainTier: "haiku",
			Decision: &router.Decision{Tier: "haiku", TierConf: 0.8, TierProbs: map[string]float64{"haiku": 0.8, "sonnet": 0.75}}},
		// Escalate was advised, so it is not missed.
		{Time: t0, SessionID: "s2", PromptHead: "hard one", Schema: stats.Schema, MainTier: "haiku", Advice: []string{"escalate"},
			Decision: &router.Decision{Tier: "opus", TierConf: 0.9, TierProbs: map[string]float64{"opus": 0.9}}},
		// Recorded before the main tier was logged: counted, not judged.
		{Time: t0, SessionID: "s3", PromptHead: "old record",
			Decision: &router.Decision{Tier: "opus", TierConf: 0.9}},
		// Errors carry no decision and are left out of the window.
		{Time: t0, SessionID: "s4", Error: "timeout"},
	}
	res := Find(recs, cfg, t0.Add(-time.Minute))
	if res.Checked != 4 || res.NoMainTier != 1 {
		t.Fatalf("checked %d, no main tier %d", res.Checked, res.NoMainTier)
	}
	for kind, want := range map[string]int{MissedEscalation: 1, CorrectedNext: 1, NearTie: 1, AdvisedNotFollowed: 0} {
		if res.ByKind[kind] != want {
			t.Fatalf("%s = %d, want %d: %+v", kind, res.ByKind[kind], want, res.Cases)
		}
	}
	out := Report(res, t0, 5)
	for _, want := range []string{"missed escalation (1)", "next prompt says it failed (1)", "not checked: 1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in\n%s", want, out)
		}
	}
}

func TestReportWhenNothingIsFlagged(t *testing.T) {
	t.Setenv("JEVCC_HOME", t.TempDir())
	cfg, _ := config.Load()
	res := Find(nil, cfg, time.Now())
	if out := Report(res, time.Now(), 5); !strings.Contains(out, "nothing flagged") {
		t.Fatal(out)
	}
}
