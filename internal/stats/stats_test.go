package stats

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zuexx/agent-jev-integration/internal/router"
)

const transcript = `{"type":"user","message":{"role":"user","content":"find the config loader please"}}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Agent","input":{"model":"haiku","prompt":"x"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","content":"ok"}]}}
{"type":"assistant","message":{"content":[{"type":"text","text":"done"}]}}
{"type":"user","message":{"role":"user","content":[{"type":"text","text":"now write tests"}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Skill","input":{"skill":"tdd"}}]}}
`

func TestFollowed(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	os.WriteFile(p, []byte(transcript), 0o600)
	if d, l, f := Followed(p, "find the config loader please", "haiku", ""); !d || l || !f {
		t.Fatal(d, l, f)
	}
	if d, l, f := Followed(p, "now write tests", "haiku", "superpowers:tdd"); d || !l || !f {
		t.Fatal(d, l, f)
	}
	if _, _, f := Followed(p, "never said", "haiku", ""); f {
		t.Fatal("found")
	}
}

// The default delegate_via "agent" sends subagent_type, not model.
const agentTranscript = `{"type":"user","message":{"role":"user","content":"prove the lock is safe"}}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Agent","input":{"subagent_type":"jev-opus","prompt":"x"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","content":"ok"}]}}
{"type":"user","message":{"role":"user","content":"thanks"}}
`

func TestFollowedSubagentType(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	os.WriteFile(p, []byte(agentTranscript), 0o600)
	if d, _, f := Followed(p, "prove the lock is safe", "opus", ""); !d || !f {
		t.Fatal(d, f)
	}
	if d, _, _ := Followed(p, "prove the lock is safe", "haiku", ""); d {
		t.Fatal("wrong tier counted as delegated")
	}
}

func TestSummaryCountsEscalate(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	os.WriteFile(p, []byte(agentTranscript), 0o600)
	recs := []Record{
		{Time: time.Now(), SessionID: "a", Transcript: p, PromptHead: "prove the lock is safe", Advice: []string{"escalate"},
			Decision: &router.Decision{Tier: "opus", LatencyMS: 100}},
	}
	s := Summary(recs, time.Time{})
	for _, want := range []string{"delegate 1", "delegation 100% (1/1)"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in\n%s", want, s)
		}
	}
}

func TestSummary(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	os.WriteFile(p, []byte(transcript), 0o600)
	now := time.Now()
	recs := []Record{
		{Time: now, SessionID: "a", Transcript: p, PromptHead: "find the config loader please", Advice: []string{"tier"}, ContextChars: 400,
			Decision: &router.Decision{Tier: "haiku", LatencyMS: 300, CostUSD: 0.00004}},
		{Time: now, SessionID: "a", Transcript: p, PromptHead: "now write tests", Advice: []string{"tier", "skill"},
			Decision: &router.Decision{Tier: "sonnet", Skill: "tdd", LatencyMS: 500, CostUSD: 0.00006}},
		{Time: now, SessionID: "b", Error: "timeout"},
	}
	s := Summary(recs, time.Time{})
	for _, want := range []string{"3 prompts in 2 sessions", "haiku 50% (1/2)", "delegation 50% (1/2)", "skill 100% (1/1)", "errors/timeouts:  1", "p95 500 ms"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in\n%s", want, s)
		}
	}
}
