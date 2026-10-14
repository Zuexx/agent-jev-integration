package bench

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBootstrap(t *testing.T) {
	xs := []float64{-1, -1.2, -0.8, -1.1, -0.9, -1, -1, -1.05, -0.95, -1}
	lo, hi := BootstrapMeanCI(xs, 2000, 1)
	if !(lo < -0.95 && hi < -0.9 && lo > -1.1) {
		t.Fatal(lo, hi)
	}
}

func TestPlanCoversEverything(t *testing.T) {
	jobs := Plan([]Task{{Name: "a"}, {Name: "b"}}, []string{ArmOff, ArmHook, ArmRoute}, 2, 7)
	if len(jobs) != 12 {
		t.Fatal(len(jobs))
	}
	seen := map[string]bool{}
	for _, j := range jobs {
		seen[j.Task.Name+j.Arm+string(rune('0'+j.Rep))] = true
	}
	if len(seen) != 12 {
		t.Fatal(seen)
	}
}

// fakeClaude prints a canned result; it lets Do run without the real CLI.
func fakeClaude(t *testing.T, model string) string {
	p := filepath.Join(t.TempDir(), "claude")
	script := `#!/bin/sh
echo "$@" > args.txt
echo ok > out.txt
echo '{"total_cost_usd":0.5,"num_turns":3,"session_id":"S","is_error":false,"modelUsage":{"` + model + `":{"inputTokens":10,"outputTokens":5,"costUSD":0.5}},"subagent_stats":{"spawned":1}}'
`
	os.WriteFile(p, []byte(script), 0o755)
	return p
}

func TestDoRouteArm(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as a fake claude")
	}
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "fixture"), 0o755)
	os.WriteFile(filepath.Join(dir, "fixture", "in.txt"), []byte("x"), 0o644)
	r := &Runner{
		Claude: fakeClaude(t, "claude-haiku-5-5"), MainModel: "opus",
		Pick: func(context.Context, string) (string, float64, error) { return "haiku", 0.0001, nil },
	}
	task := Task{Name: "t", Dir: dir, Prompt: "do it", Check: "test -f in.txt && grep -q ok out.txt && grep -q -- '--model haiku' args.txt", TimeoutS: 30}
	res := r.Do(context.Background(), task, ArmRoute, 1)
	if res.Error != "" || !res.Pass || res.Model != "haiku" || res.TotalUSD() != 0.5001 || res.Subagents != 1 || res.Tokens != 15 {
		t.Fatalf("%+v", res)
	}
	md := Report([]Run{
		{Task: "t", Arm: ArmOff, Rep: 1, Pass: true, ClaudeUSD: 1, Model: "opus"},
		res,
	}, []string{ArmOff, ArmRoute})
	if !strings.Contains(md, "**route vs off** (1 paired runs)") || !strings.Contains(md, "Too few paired runs") {
		t.Fatal(md)
	}
}

func TestLoadTasksGivesAbsoluteDirs(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "tasks", "a"), 0o755)
	os.WriteFile(filepath.Join(root, "tasks", "a", "task.json"), []byte(`{"prompt":"p","check":"true"}`), 0o644)
	wd, _ := os.Getwd()
	defer os.Chdir(wd)
	os.Chdir(root)
	ts, err := LoadTasks("tasks", nil)
	if err != nil || len(ts) != 1 || !filepath.IsAbs(ts[0].Dir) {
		t.Fatal(ts, err)
	}
}
