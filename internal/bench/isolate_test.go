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
	allow := Allow{Plugins: []string{"cc-plugin-telemetry"}, Agents: []string{"general-purpose"}, Skills: []string{"init"}}
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
{"type":"system","subtype":"init","plugins":[{"name":"superpowers","path":"/p"},{"name":"cc-plugin-telemetry","path":"/elsewhere"}],"mcp_servers":[{"name":"codegraph","status":"connected"}],"agents":["general-purpose","my-agent"],"skills":["init","brainstorming"],"memory_paths":{"auto":"/Users/me/.claude/projects/x/memory/"}}
{"type":"result"}`
	in, hooks, _ = parseStream([]byte(dirty))
	v := strings.Join(checkIsolation(in, hooks, sb, allow, nil, []string{"UserPromptSubmit"}), "\n")
	for _, want := range []string{"plugin superpowers", "plugin cc-plugin-telemetry", "mcp server codegraph", "agent my-agent", "skill brainstorming", "hook SessionStart:startup", "memory path /Users/me"} {
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
		JevCost: func(session, home string) float64 { jevHome = home; return 0.01 },
	}
	task := Task{Name: "t", Dir: dir, Prompt: "p", Check: "grep -q stream-json args.txt && grep -q strict-mcp-config args.txt", TimeoutS: 30}
	res := r.Do(context.Background(), task, ArmHook, 1)
	if res.Error != "" || !res.Pass || res.ClaudeUSD != 0.25 || res.SessionID != "S1" || res.ClaudeVersion != "9.9" {
		t.Fatalf("%+v", res)
	}
	if q := res.Quota; q == nil || q.FiveHour != 3 || q.SevenDay != 12 || q.SevenDayReset != 200 {
		t.Fatalf("quota %+v", res.Quota)
	}
	if res.JevUSD != 0.01 || jevHome == "" {
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
