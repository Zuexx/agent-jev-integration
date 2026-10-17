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
	for _, want := range []string{"PATH=/bin", "HOME=/sb/home", "CLAUDE_CONFIG_DIR=/sb/home/.claude", "JEVCC_HOME=/sb/jevcc", "CLAUDE_CODE_OAUTH_TOKEN=tok", "DISABLE_AUTOUPDATER=1"} {
		if !strings.Contains(env+"\n", want+"\n") {
			t.Errorf("missing %s in\n%s", want, env)
		}
	}
	for _, bad := range []string{"/Users/me", "ANTHROPIC_MODEL", "BLENDER"} {
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
	task := Task{Name: "t", Dir: dir, Prompt: "p", Check: "grep -q stream-json args.txt && grep -q strict-mcp-config args.txt && grep -q 'disallowedTools WebSearch WebFetch --model' args.txt", TimeoutS: 30}
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
	// args.txt and env.txt are new files the run wrote: kept as artifacts.
	if res.Artifacts == "" {
		t.Fatal("no artifacts recorded")
	}
	if _, err := os.Stat(filepath.Join(streams, res.Artifacts, "env.txt")); err != nil {
		t.Fatal(err)
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
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Agent","input":{"subagent_type":"jev-review"}},{"type":"tool_use","name":"mcp__blender__render"}]}}
{"type":"assistant","parent_tool_use_id":"t1","message":{"content":[{"type":"tool_use","name":"Edit"},{"type":"tool_use","name":"Edit"}]}}
{"type":"assistant","parent_tool_use_id":"unknown","message":{"content":[{"type":"tool_use","name":"Read"}]}}
{"type":"user","message":{"content":[{"type":"tool_result"}]}}`
	u := toolUses([]byte(out))
	if u.Main["mcp__blender__render"] != 2 || u.Main["Agent"] != 1 || len(u.Main) != 2 {
		t.Fatal(u.Main)
	}
	if u.Spawns["jev-review"] != 1 || u.Subagent["jev-review/Edit"] != 2 || u.Subagent["subagent/Read"] != 1 {
		t.Fatal(u.Spawns, u.Subagent)
	}
}

func TestTaskCacheCopied(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as a fake claude")
	}
	dir, cache := t.TempDir(), t.TempDir()
	os.MkdirAll(filepath.Join(dir, "fixture"), 0o755)
	os.MkdirAll(filepath.Join(cache, "deps", "node_modules", "pkg"), 0o755)
	os.WriteFile(filepath.Join(cache, "deps", "node_modules", "pkg", "index.js"), []byte("x"), 0o644)
	fake := filepath.Join(t.TempDir(), "claude")
	os.WriteFile(fake, []byte(`#!/bin/sh
echo '{"total_cost_usd":0.1,"session_id":"S","is_error":false}'
`), 0o755)
	r := &Runner{Claude: fake, MainModel: "haiku", CacheDir: cache, SaveDir: t.TempDir()}
	task := Task{Name: "c", Dir: dir, Prompt: "p", Check: "test -f node_modules/pkg/index.js", Cache: []string{"deps"}, TimeoutS: 30}
	if res := r.Do(context.Background(), task, ArmOff, 1); res.Error != "" || !res.Pass || res.Artifacts != "" {
		t.Fatalf("%+v", res)
	}
	task.Cache = []string{"absent"}
	if res := r.Do(context.Background(), task, ArmOff, 1); !strings.Contains(res.Error, "cache absent missing") {
		t.Fatalf("%+v", res)
	}
}

func TestParseLeadArm(t *testing.T) {
	for _, c := range []struct {
		arm             string
		lead, dir       string
		open, ok, apply bool
	}{
		{"opus/off", "opus", "off", false, true, true},
		{"haiku/up", "haiku", "up", false, true, true},
		{"sonnet/both/open", "sonnet", "both", true, true, true},
		{"opus/up", "opus", "up", false, true, false},
		{"haiku/down", "haiku", "down", false, true, false},
		{"opus/both", "opus", "both", false, true, false},
		{"gpt/off", "", "", false, false, false},
		{"opus/sideways", "", "", false, false, false},
		{"off", "", "", false, false, false},
	} {
		lead, dir, open, ok := ParseLeadArm(c.arm)
		if lead != c.lead || dir != c.dir || open != c.open || ok != c.ok || LeadArmApplicable(c.arm) != c.apply {
			t.Errorf("%s: %q %q %v %v applicable=%v", c.arm, lead, dir, open, ok, LeadArmApplicable(c.arm))
		}
	}
}

func TestLeadArmConfiguresRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as a fake claude")
	}
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "fixture"), 0o755)
	fake := filepath.Join(t.TempDir(), "claude")
	os.WriteFile(fake, []byte(`#!/bin/sh
printf '%s\n' "$@" > args.txt
env | grep '^JEVCC_' | sort > env.txt
echo '{"total_cost_usd":0.1,"session_id":"S","is_error":false}'
`), 0o755)
	r := &Runner{Claude: fake, HookCmd: "jevcc hook", Agents: `{"jev-haiku":{"tools":["Read"]}}`, AgentsOpen: `{"jev-haiku":{}}`, Effort: "medium",
		JevUse: func(string, string) JevUse { return JevUse{Decisions: 1} }}
	task := Task{Name: "a", Dir: dir, Prompt: "p", TimeoutS: 30, Check: `grep -qx sonnet args.txt && grep -qx '{"jev-haiku":{}}' args.txt && ` +
		`grep -qx JEVCC_DELEGATE=agent env.txt && grep -qx JEVCC_ESCALATE=0 env.txt && grep -qx JEVCC_MAIN=sonnet env.txt && grep -qx JEVCC_MODE=on env.txt && ! grep -q Stop args.txt`}
	r.SaveDir = t.TempDir()
	if res := r.Do(context.Background(), task, "sonnet/down/open", 1); !res.Pass || res.Model != "sonnet" || !strings.HasPrefix(res.Artifacts, "artifacts/a_sonnet-down-open_1_") {
		t.Fatalf("down: %+v", res)
	}
	task.Check = `grep -qx haiku args.txt && grep -q Stop args.txt && ! grep -qx -- --no-session-persistence args.txt && ` +
		`grep -qx JEVCC_DELEGATE=off env.txt && grep -qx JEVCC_ESCALATE=1 env.txt && grep -qx JEVCC_VERIFY=1 env.txt && grep -q '"tools"' args.txt`
	if res := r.Do(context.Background(), task, "haiku/up", 1); !res.Pass {
		t.Fatalf("up: %+v", res)
	}
	task.Check = `grep -qx opus args.txt && grep -qx JEVCC_MODE=off env.txt && ! grep -q -- --agents args.txt && grep -A1 -x -- --effort args.txt | grep -qx medium`
	if res := r.Do(context.Background(), task, "opus/off", 1); !res.Pass {
		t.Fatalf("off: %+v", res)
	}
}

func TestRunWithEveryJevCallFailingIsInvalid(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as a fake claude")
	}
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "fixture"), 0o755)
	fake := filepath.Join(t.TempDir(), "claude")
	os.WriteFile(fake, []byte("#!/bin/sh\necho '{\"total_cost_usd\":0.1,\"session_id\":\"S\",\"is_error\":false}'\n"), 0o755)
	tlsErr := "tls: failed to verify certificate: x509: OSStatus -26276"
	r := &Runner{Claude: fake, HookCmd: "jevcc hook", Effort: "medium", SaveDir: t.TempDir(),
		JevUse: func(string, string) JevUse { return JevUse{Errors: []string{tlsErr}} }}
	task := Task{Name: "a", Dir: dir, Prompt: "p", TimeoutS: 30, Check: "true"}
	if res := r.Do(context.Background(), task, "haiku/up", 1); res.Error != JevDownPrefix+tlsErr {
		t.Fatalf("all calls failed: %+v", res)
	}
	// One call failing among others is recorded, not fatal.
	r.JevUse = func(string, string) JevUse { return JevUse{Decisions: 2, Errors: []string{tlsErr}} }
	if res := r.Do(context.Background(), task, "haiku/up", 1); res.Error != "" || len(res.JevErrors) != 1 {
		t.Fatalf("partial failure: %+v", res)
	}
}
