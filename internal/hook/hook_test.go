package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zuexx/agent-jev-integration/internal/config"
	"github.com/zuexx/agent-jev-integration/internal/jev"
	"github.com/zuexx/agent-jev-integration/internal/journal"
	"github.com/zuexx/agent-jev-integration/internal/paths"
	"github.com/zuexx/agent-jev-integration/internal/roster"
	"github.com/zuexx/agent-jev-integration/internal/stats"
)

func setup(t *testing.T) *config.Config {
	startWarm = func() {}
	t.Setenv("JEVCC_HOME", t.TempDir())
	t.Setenv("JEVCC_MODE", "")
	t.Setenv("TYPESAFE_API_KEY", "k")
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestToggleIsPerSession(t *testing.T) {
	cfg := setup(t)
	if out, ok := Command(Input{SessionID: "s1", Prompt: "/jev on"}, cfg); !ok || out.Decision != "block" || !strings.Contains(out.Reason, "ON") {
		t.Fatalf("%+v", out)
	}
	if !Enabled(cfg, "s1") || Enabled(cfg, "s2") {
		t.Fatal("toggle leaked across sessions")
	}
	Command(Input{SessionID: "s1", Prompt: "/jev off"}, cfg)
	if Enabled(cfg, "s1") {
		t.Fatal("still on")
	}
	t.Setenv("JEVCC_MODE", "on")
	if !Enabled(cfg, "s1") {
		t.Fatal("env override ignored")
	}
}

func TestIgnoresOtherPrompts(t *testing.T) {
	cfg := setup(t)
	for _, p := range []string{"hello", "/jevx", "/jev-classify a.csv", "please /jev on", ""} {
		if _, ok := Command(Input{SessionID: "s", Prompt: p}, cfg); ok {
			t.Fatalf("%q handled", p)
		}
	}
}

type errAsker struct{}

func (errAsker) Ask(context.Context, any, map[string]jev.Question) (*jev.Response, error) {
	return nil, errors.New("timeout")
}

func TestRouteErrorIsSilentButLogged(t *testing.T) {
	cfg := setup(t)
	out, rec := Route(context.Background(), Input{SessionID: "s", Prompt: "x"}, Deps{
		Config: cfg, Asker: errAsker{}, Skills: func(string) []roster.Skill { return nil }, Now: time.Now,
	})
	if out != nil || rec.Error != "timeout" {
		t.Fatalf("%+v %+v", out, rec)
	}
}

func TestMainEndToEnd(t *testing.T) {
	cfg := setup(t)
	_ = cfg
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"model":"jev-1.13.0","answers":{"tier":{"type":"choice","choice":"haiku","confidence":0.9,"probabilities":{"haiku":0.95,"sonnet":0.05}},"bulk":{"type":"noul","noul":0.02}},"usage":{"input_tokens":500}}`))
	}))
	defer srv.Close()
	t.Setenv("TYPESAFE_ENDPOINT", srv.URL)
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir()) // no skills
	t.Setenv("JEVCC_MODE", "on")
	os.WriteFile(config.Path(), []byte(`{"delegate":true}`), 0o600)
	in, _ := json.Marshal(Input{SessionID: "s", Prompt: "where is main.go?", Cwd: t.TempDir()})
	var out bytes.Buffer
	if code := Main(bytes.NewReader(in), &out); code != 0 {
		t.Fatal(code)
	}
	var o Output
	if err := json.Unmarshal(out.Bytes(), &o); err != nil || o.Specific == nil || !strings.Contains(o.Specific.AdditionalContext, `"jev-haiku"`) {
		t.Fatalf("%s %v", out.String(), err)
	}
	recs, err := stats.LoadDecisions()
	if err != nil || len(recs) != 1 || recs[0].Decision.Tier != "haiku" || recs[0].Advice[0] != "tier" {
		t.Fatalf("%+v %v", recs, err)
	}
}

func TestMainNeverFails(t *testing.T) {
	setup(t)
	var out bytes.Buffer
	if Main(strings.NewReader("not json"), &out) != 0 || out.Len() != 0 {
		t.Fatal("bad input must be silent")
	}
	// routing off: nothing printed, nothing logged
	in, _ := json.Marshal(Input{SessionID: "s", Prompt: "hello"})
	Main(bytes.NewReader(in), &out)
	if out.Len() != 0 {
		t.Fatal(out.String())
	}
	if _, err := os.Stat(filepath.Join(paths.StateDir(), "decisions.jsonl")); err == nil {
		t.Fatal("logged while off")
	}
}

func TestFirstDedupesAcrossScopes(t *testing.T) {
	setup(t)
	in := Input{SessionID: "s", Prompt: "same"}
	if !First(in) || First(in) {
		t.Fatal("second run of the same prompt must be skipped")
	}
	if !First(Input{SessionID: "s", Prompt: "other"}) {
		t.Fatal("different prompt skipped")
	}
}

func TestSessionTier(t *testing.T) {
	cfg := setup(t)
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("ANTHROPIC_MODEL", "")
	dir := t.TempDir()
	tr := filepath.Join(dir, "t.jsonl")
	os.WriteFile(tr, []byte(`{"type":"assistant","message":{"model":"claude-opus-5-5"}}
{"type":"assistant","message":{"model":"claude-haiku-5-5"}}
`), 0o600)
	if got := SessionTier(Input{TranscriptPath: tr}, cfg); got != "haiku" {
		t.Fatal("transcript", got)
	}
	os.MkdirAll(filepath.Join(dir, ".claude"), 0o755)
	os.WriteFile(filepath.Join(dir, ".claude", "settings.json"), []byte(`{"model":"sonnet"}`), 0o600)
	if got := SessionTier(Input{Cwd: dir}, cfg); got != "sonnet" {
		t.Fatal("settings", got)
	}
	if got := SessionTier(Input{}, cfg); got != "" {
		t.Fatal("unknown", got)
	}
}

func TestStatuslineCommandAndStatus(t *testing.T) {
	cfg := setup(t)
	out, ok := Command(Input{SessionID: "s", Prompt: "/jev statusline compact"}, cfg)
	if !ok || !strings.Contains(out.Reason, "compact") {
		t.Fatalf("%+v", out)
	}
	c2, _ := config.Load()
	if c2.Statusline != "compact" {
		t.Fatal(c2.Statusline)
	}
	out, _ = Command(Input{SessionID: "s", Prompt: "/jev statusline nope"}, cfg)
	if !strings.Contains(out.Reason, "usage") {
		t.Fatal(out.Reason)
	}
	out, _ = Command(Input{SessionID: "s", Prompt: "/jev status"}, cfg)
	for _, want := range []string{"session model", "guard risky commands", "screen fetched content", "statusline"} {
		if !strings.Contains(out.Reason, want) {
			t.Fatalf("missing %q in\n%s", want, out.Reason)
		}
	}
}

func TestSystemMessagesAreNotRouted(t *testing.T) {
	setup(t)
	t.Setenv("JEVCC_MODE", "on")
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer srv.Close()
	t.Setenv("TYPESAFE_ENDPOINT", srv.URL)
	for _, p := range []string{"<agent-message from=\"a1\">\n[Subagent hand-back] done", "<task-notification>\n<task-id>x</task-id>"} {
		in, _ := json.Marshal(Input{HookEventName: "UserPromptSubmit", SessionID: "s", Prompt: p})
		var out bytes.Buffer
		Main(bytes.NewReader(in), &out)
		if out.Len() != 0 {
			t.Fatalf("routed a system message: %s", out.String())
		}
	}
	if calls != 0 {
		t.Fatalf("Jev called %d times for system messages", calls)
	}
}

func TestStopSkipsTurnsWithoutEditsOrWithHandOff(t *testing.T) {
	cfg := setup(t)
	t.Setenv("JEVCC_MODE", "on")
	cfg.MainModel = "haiku"
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte(`{"answers":{"complete":{"type":"noul","noul":0.1},"unverified":{"type":"noul","noul":0.9},"failing":{"type":"noul","noul":0.1},"hedging":{"type":"noul","noul":0.9},"untested_rules":{"type":"noul","noul":0.1}}}`))
	}))
	defer srv.Close()
	cfg.Endpoint = srv.URL
	dir := t.TempDir()
	write := func(lines string) string {
		p := filepath.Join(dir, fmt.Sprintf("t%d.jsonl", calls+len(lines)))
		os.WriteFile(p, []byte(lines), 0o600)
		return p
	}
	investigate := write(`{"type":"user","message":{"content":"why does the window flash?"}}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"grep -r spawn ."}}]}}
{"type":"assistant","message":{"content":[{"type":"text","text":"Still investigating; not sure yet."}]}}
`)
	if out := stop(Input{SessionID: "s", TranscriptPath: investigate}, cfg); out != nil || calls != 0 {
		t.Fatalf("reviewed an investigation turn: %+v calls=%d", out, calls)
	}
	handed := write(`{"type":"user","message":{"content":"fix the bug"}}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Edit","input":{"file_path":"a.go"}}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Agent","input":{"subagent_type":"jev-opus","description":"x"}}]}}
{"type":"user","message":{"content":"<agent-message from=\"a1\">done</agent-message>"}}
{"type":"assistant","message":{"content":[{"type":"text","text":"Done."}]}}
`)
	if out := stop(Input{SessionID: "s", TranscriptPath: handed}, cfg); out != nil || calls != 0 {
		t.Fatalf("reviewed a handed-off turn: %+v", out)
	}
	edited := write(`{"type":"user","message":{"content":"fix the bug"}}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Edit","input":{"file_path":"a.go"}}]}}
{"type":"assistant","message":{"content":[{"type":"text","text":"Fixed."}]}}
`)
	for i := 0; i < 4; i++ {
		os.WriteFile(edited, []byte(fmt.Sprintf(`{"type":"user","message":{"content":"fix bug %d"}}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Edit","input":{"file_path":"a.go"}}]}}
{"type":"assistant","message":{"content":[{"type":"text","text":"Fixed."}]}}
`, i)), 0o600)
		stop(Input{SessionID: "s", TranscriptPath: edited}, cfg)
	}
	if n := reviewEscalations("s"); n != cfg.VerifyMaxPerSession {
		t.Fatalf("escalations %d, cap %d", n, cfg.VerifyMaxPerSession)
	}
}

func TestStopSkipReasonsAreTraced(t *testing.T) {
	cfg := setup(t)
	t.Setenv("JEVCC_MODE", "on")
	cfg.MainModel = "haiku"
	cfg.Endpoint = "http://127.0.0.1:1" // never reached: nothing here needs a review
	investigate := filepath.Join(t.TempDir(), "t.jsonl")
	os.WriteFile(investigate, []byte(`{"type":"user","message":{"content":"why does the window flash?"}}
{"type":"assistant","message":{"content":[{"type":"text","text":"Not sure yet."}]}}
`), 0o600)
	logPath := journal.Path("verify", time.Now())

	stop(Input{SessionID: "s", TranscriptPath: investigate}, cfg)
	if _, err := os.Stat(logPath); err == nil {
		t.Fatal("skip reasons were written without trace")
	}

	trace := true
	cfg.Trace = &trace
	stop(Input{SessionID: "s", TranscriptPath: investigate}, cfg)
	stop(Input{SessionID: "s", TranscriptPath: investigate}, cfg) // second scope, same event
	b, err := os.ReadFile(logPath)
	if err != nil || !strings.Contains(string(b), `"skipped":"not_code"`) || !strings.Contains(string(b), `"request_head":"why does the window flash?"`) {
		t.Fatal(err, string(b))
	}
	if n := strings.Count(string(b), `"skipped"`); n != 1 {
		t.Fatalf("logged the same event %d times:\n%s", n, b)
	}
}

type tierAsker struct{ tier string }

func (a tierAsker) Ask(context.Context, any, map[string]jev.Question) (*jev.Response, error) {
	return &jev.Response{Answers: map[string]jev.Answer{
		"tier": {Type: "choice", Choice: a.tier, Confidence: 0.9, Probabilities: map[string]float64{a.tier: 0.9}},
	}}, nil
}

func TestRouteRecordsMainTierAndTrace(t *testing.T) {
	cfg := setup(t)
	cfg.MainModel = "haiku"
	deps := Deps{Config: cfg, Asker: tierAsker{"opus"}, Skills: func(string) []roster.Skill { return nil }, Now: time.Now}
	out, rec := Route(context.Background(), Input{SessionID: "s", Prompt: "redesign the scheduler"}, deps)
	if out == nil || rec.Schema != stats.Schema || rec.MainTier != "haiku" || rec.MainTierSource != "config" ||
		rec.EscalationsUsed != 0 || len(rec.Advice) != 1 || rec.Advice[0] != "escalate" || rec.Trace != nil {
		t.Fatalf("%+v", rec)
	}

	trace := true
	cfg.Trace = &trace
	deps.Asker = tierAsker{"haiku"} // same tier as the session: no advice, thresholds still traced
	out, rec = Route(context.Background(), Input{SessionID: "s", Prompt: "rename x"}, deps)
	if out != nil || len(rec.Advice) != 0 || rec.Trace == nil || rec.Trace.Context != "" ||
		rec.Trace.MinConf != cfg.MinConfidence || rec.Trace.EscalateConf != cfg.EscalateConfidence ||
		rec.Trace.SkillFit != cfg.SkillFit || rec.Trace.Memory != cfg.MemoryThreshold || rec.Trace.MinConf == 0 {
		t.Fatalf("%+v %+v", out, rec.Trace)
	}
}

func TestNamedModelOverridesJevTier(t *testing.T) {
	cfg := setup(t)
	cfg.MainModel = "haiku"
	deps := Deps{Config: cfg, Asker: tierAsker{"haiku"}, Skills: func(string) []roster.Skill { return nil }, Now: time.Now}
	// Jev says haiku-level, but the user asked for opus: escalate to opus.
	out, rec := Route(context.Background(), Input{SessionID: "s", Prompt: "用 opus 重寫 scheduler"}, deps)
	if out == nil || len(rec.Advice) != 1 || rec.Advice[0] != "escalate" || !strings.Contains(out.Specific.AdditionalContext, "opus-level") {
		t.Fatalf("%+v", rec)
	}
	// Jev says opus-level, but the user asked for haiku: no escalation, and the cheaper ask is delegated.
	cfg.MainModel = "opus"
	cfg.Delegate = true
	deps.Asker = tierAsker{"opus"}
	out, rec = Route(context.Background(), Input{SessionID: "s", Prompt: "haiku 來做這個改名"}, deps)
	if out == nil || len(rec.Advice) != 1 || rec.Advice[0] != "tier" || !strings.Contains(out.Specific.AdditionalContext, "haiku-level") {
		t.Fatalf("%+v", rec)
	}
}
