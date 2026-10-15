package bench

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPairs(t *testing.T) {
	runs := []Run{
		{Task: "t", Arm: "opus/off", Rep: 1, Artifacts: "a1"},
		{Task: "t", Arm: "haiku/off", Rep: 1, Artifacts: "a2"},
		{Task: "t", Arm: "haiku/off", Rep: 2, Artifacts: "a3"},
		{Task: "t", Arm: "opus/off", Rep: 2, Error: "boom", Artifacts: "a4"},
		{Task: "u", Arm: "opus/off", Rep: 1},
		{Task: "u", Arm: "haiku/off", Rep: 1, Artifacts: "a5"},
	}
	ps := Pairs(runs, [][2]string{{"haiku/off", "opus/off"}})
	if len(ps) != 1 || ps[0].X.Artifacts != "a2" || ps[0].Y.Artifacts != "a1" {
		t.Fatalf("%+v", ps)
	}
}

func judgeFixture(t *testing.T, script string) (*Runner, Task, Pair, string) {
	t.Helper()
	streams := t.TempDir()
	for name, content := range map[string]string{"x": "good", "y": "bad"} {
		os.MkdirAll(filepath.Join(streams, name), 0o755)
		os.WriteFile(filepath.Join(streams, name, "out.txt"), []byte(content), 0o644)
		os.WriteFile(filepath.Join(streams, name, "scene.blend"), []byte("binary"), 0o644)
	}
	task := t.TempDir()
	os.WriteFile(filepath.Join(task, "rubric.md"), []byte("good beats bad"), 0o644)
	fake := filepath.Join(t.TempDir(), "claude")
	os.WriteFile(fake, []byte("#!/bin/sh\n"+script), 0o755)
	p := Pair{Task: "t", Rep: 1, X: Run{Arm: "haiku/off", Artifacts: "x"}, Y: Run{Arm: "opus/off", Artifacts: "y"}}
	return &Runner{Claude: fake}, Task{Name: "t", Dir: task, Prompt: "do it"}, p, streams
}

func TestJudgeKeepsOnlyOrderStableWinners(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as a fake claude")
	}
	// Reads the outputs: picks whichever side holds "good", in both orders.
	r, task, p, streams := judgeFixture(t, `test -f A/scene.blend && exit 3
if grep -q good A/out.txt; then w=A; else w=B; fi
printf '{"result":"looked\\n{\\"winner\\": \\"%s\\", \\"reason\\": \\"good is better\\"}","total_cost_usd":0.5}' "$w"
`)
	v := r.Judge(context.Background(), task, p, streams, "opus")
	if v.Error != "" || v.Winner != "haiku/off" || v.USD != 1 || len(v.Reasons) != 2 {
		t.Fatalf("%+v", v)
	}
	// Always says A: position bias, so the swap turns it into a tie.
	r, task, p, streams = judgeFixture(t, `printf '{"result":"{\\"winner\\": \\"A\\", \\"reason\\": \\"first\\"}","total_cost_usd":0.1}'`)
	if v := r.Judge(context.Background(), task, p, streams, "opus"); v.Winner != "tie" {
		t.Fatalf("%+v", v)
	}
}
