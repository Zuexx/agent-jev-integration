package bench

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSandboxEnvDropsEvaluatorSetup(t *testing.T) {
	sb := sandbox{Root: "/sb", Home: "/sb/home", ConfigDir: "/sb/home/.claude", JevHome: "/sb/jevcc"}
	base := []string{"PATH=/bin", "HOME=/Users/me", "CLAUDE_CONFIG_DIR=/Users/me/.claude", "JEVCC_HOME=/Users/me/.jevcc", "ANTHROPIC_MODEL=opus", "BLENDER=/b"}
	env := strings.Join(sb.env(base, []string{"CLAUDE_CODE_OAUTH_TOKEN=tok"}), "\n")
	for _, want := range []string{"PATH=/bin", "HOME=/sb/home", "CLAUDE_CONFIG_DIR=/sb/home/.claude", "JEVCC_HOME=/sb/jevcc", "BLENDER=/b", "CLAUDE_CODE_OAUTH_TOKEN=tok"} {
		if !strings.Contains(env+"\n", want+"\n") {
			t.Errorf("missing %s in\n%s", want, env)
		}
	}
	for _, bad := range []string{"/Users/me", "ANTHROPIC_MODEL"} {
		if strings.Contains(env, bad) {
			t.Errorf("leaked %s in\n%s", bad, env)
		}
	}
}

const cleanInit = `{"type":"system","subtype":"init","claude_code_version":"2.1.300","model":"claude-haiku-5-5","plugins":[{"name":"cc-plugin-telemetry","path":"builtin"}],"mcp_servers":[],"agents":["general-purpose","jev-opus"],"skills":["init"],"memory_paths":{"auto":"/sb/home/.claude/projects/x/memory/"}}`

func TestCheckIsolation(t *testing.T) {
	sb := sandbox{Root: "/sb"}
	allow := Allow{Plugins: []string{"cc-plugin-telemetry"}, Agents: []string{"general-purpose"}, Skills: []string{"init"}, MCP: []string{"blender"}}
	clean := cleanInit + "\n" +
		`{"type":"system","subtype":"hook_started","hook_name":"UserPromptSubmit"}` + "\n" +
		`{"type":"result","total_cost_usd":0.1,"session_id":"S"}`
	in, hooks, result := parseStream([]byte(clean))
	if in == nil || in.Version != "2.1.300" || string(result) == "" {
		t.Fatal(in, string(result))
	}
	if v := checkIsolation(in, hooks, sb, allow, []string{"jev-opus"}, []string{"UserPromptSubmit"}); len(v) != 0 {
		t.Fatal("clean run flagged:", v)
	}

	dirty := `{"type":"system","subtype":"hook_started","hook_name":"SessionStart:startup"}
{"type":"system","subtype":"init","plugins":[{"name":"superpowers","path":"/p"},{"name":"cc-plugin-telemetry","path":"/elsewhere"}],"mcp_servers":[{"name":"codegraph","status":"connected"},{"name":"blender","status":"failed"}],"agents":["general-purpose","my-agent"],"skills":["init","brainstorming"],"memory_paths":{"auto":"/Users/me/.claude/projects/x/memory/"}}
{"type":"result"}`
	in, hooks, _ = parseStream([]byte(dirty))
	v := strings.Join(checkIsolation(in, hooks, sb, allow, nil, []string{"UserPromptSubmit"}), "\n")
	for _, want := range []string{"plugin superpowers", "plugin cc-plugin-telemetry", "mcp server codegraph", "mcp server blender is failed", "agent my-agent", "skill brainstorming", "hook SessionStart:startup", "memory path /Users/me"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q in\n%s", want, v)
		}
	}

	if v := checkIsolation(nil, nil, sb, allow, nil, nil); len(v) != 1 || !strings.Contains(v[0], "no init") {
		t.Fatal(v)
	}
}

func TestDoIsolated(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as a fake claude")
	}
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "fixture"), 0o755)
	streams := t.TempDir()
	fake := filepath.Join(t.TempDir(), "claude")
	// Records where it ran, writes into its HOME, and speaks stream-json.
	os.WriteFile(fake, []byte(`#!/bin/sh
echo "$@" > args.txt
echo "$HOME|$CLAUDE_CONFIG_DIR|$JEVCC_HOME|$CLAUDE_CODE_OAUTH_TOKEN" > env.txt
touch "$HOME/written"
echo '{"type":"system","subtype":"init","claude_code_version":"9.9","plugins":[],"mcp_servers":[],"agents":[],"skills":[],"memory_paths":{"auto":"'"$HOME"'/m"}}'
echo '{"type":"rate_limit_event","rate_limit_info":{"unifiedWindows":{"five_hour":{"utilization":3,"resetsAt":100},"seven_day":{"utilization":12,"resetsAt":200}}}}'
echo '{"type":"result","total_cost_usd":0.25,"num_turns":2,"session_id":"S1","is_error":false,"modelUsage":{"claude-haiku-5-5":{"inputTokens":4,"outputTokens":1,"costUSD":0.25}}}'
`), 0o755)
	var jevHome string
	r := &Runner{
		Claude: fake, MainModel: "haiku", Isolate: true, PassEnv: []string{"CLAUDE_CODE_OAUTH_TOKEN=tok"}, SaveDir: streams,
		JevUse: func(session, home string) JevUse {
			jevHome = home
			return JevUse{USD: 0.01, Decisions: 1, Advice: []string{"files"}}
		},
	}
	task := Task{Name: "t", Dir: dir, Prompt: "p", Check: "grep -q stream-json args.txt && grep -q strict-mcp-config args.txt", TimeoutS: 30}
	res := r.Do(context.Background(), task, ArmHook, 1)
	if res.Error != "" || !res.Pass || res.ClaudeUSD != 0.25 || res.SessionID != "S1" || res.ClaudeVersion != "9.9" {
		t.Fatalf("%+v", res)
	}
	if q := res.Quota; q == nil || q.FiveHour != 3 || q.SevenDay != 12 || q.SevenDayReset != 200 {
		t.Fatalf("quota %+v", res.Quota)
	}
	if res.JevUSD != 0.01 || res.JevDecisions != 1 || len(res.JevAdvice) != 1 || jevHome == "" {
		t.Fatal("jev cost not read from the sandbox", res.JevUSD, jevHome)
	}
	if _, err := os.Stat(jevHome); !os.IsNotExist(err) {
		t.Fatal("sandbox left behind:", jevHome)
	}
	saved, _ := filepath.Glob(filepath.Join(streams, "*.jsonl"))
	if len(saved) != 1 {
		t.Fatal("stream not saved", saved)
	}
	if b, _ := os.ReadFile(saved[0]); !strings.Contains(string(b), `"subtype":"init"`) {
		t.Fatal(string(b))
	}
}

func TestValidate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("checks are sh")
	}
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "fixture"), 0o755)
	os.WriteFile(filepath.Join(dir, "fixture", "a.txt"), []byte("todo"), 0o644)
	os.MkdirAll(filepath.Join(dir, "testdata", "solution"), 0o755)
	os.WriteFile(filepath.Join(dir, "testdata", "solution", "a.txt"), []byte("done"), 0o644)
	good := Task{Name: "g", Dir: dir, Check: "grep -q done a.txt && echo 'METRIC hits=2'", TimeoutS: 10}
	v := Validate(context.Background(), good)
	if !v.FailsOnFixture || !v.PassesWithSolution || v.Metrics["hits"] != 2 {
		t.Fatalf("%+v", v)
	}
	// A check that passes on the untouched fixture proves nothing.
	weak := Task{Name: "w", Dir: dir, Check: "true", TimeoutS: 10}
	if v := Validate(context.Background(), weak); v.FailsOnFixture || v.OK() {
		t.Fatalf("%+v", v)
	}
}

func TestParseMetrics(t *testing.T) {
	m := parseMetrics("x\nMETRIC risk_hits=3\nMETRIC  bad\nMETRIC words=1200.5\n")
	if len(m) != 2 || m["risk_hits"] != 3 || m["words"] != 1200.5 {
		t.Fatal(m)
	}
}

func TestTaskSkillsInstalledAndAllowed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as a fake claude")
	}
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "fixture"), 0o755)
	os.MkdirAll(filepath.Join(dir, "skills", "release-notes"), 0o755)
	os.WriteFile(filepath.Join(dir, "skills", "release-notes", "SKILL.md"), []byte("---\nname: release-notes\n---\n"), 0o644)
	fake := filepath.Join(t.TempDir(), "claude")
	os.WriteFile(fake, []byte(`#!/bin/sh
test -f "$CLAUDE_CONFIG_DIR/skills/release-notes/SKILL.md" && echo installed > seen.txt
echo '{"type":"system","subtype":"init","plugins":[],"mcp_servers":[],"agents":[],"skills":["release-notes"],"memory_paths":{}}'
echo '{"type":"result","total_cost_usd":0.1,"session_id":"S","is_error":false}'
`), 0o755)
	r := &Runner{Claude: fake, MainModel: "haiku", Isolate: true}
	task := Task{Name: "s", Dir: dir, Prompt: "p", Check: "grep -q installed seen.txt", Skills: []string{"release-notes"}, TimeoutS: 30}
	res := r.Do(context.Background(), task, ArmOff, 1)
	if res.Error != "" || !res.Pass {
		t.Fatalf("%+v", res)
	}
}

func TestToolUses(t *testing.T) {
	out := `{"type":"assistant","message":{"content":[{"type":"text"},{"type":"tool_use","name":"mcp__blender__render"}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Agent"},{"type":"tool_use","name":"mcp__blender__render"}]}}
{"type":"user","message":{"content":[{"type":"tool_result"}]}}`
	n := toolUses([]byte(out))
	if n["mcp__blender__render"] != 2 || n["Agent"] != 1 || len(n) != 2 {
		t.Fatal(n)
	}
}
