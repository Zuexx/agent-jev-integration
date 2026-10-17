package bench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRatingPageIsBlind(t *testing.T) {
	taskDir := t.TempDir()
	os.WriteFile(filepath.Join(taskDir, "human.json"), []byte(`{"question":"好嗎？","options":[{"score":1,"label":"不好"},{"score":5,"label":"很好"}]}`), 0o644)
	tasks := map[string]Task{"t": {Name: "t", Dir: taskDir, Prompt: "write a plan"}}
	dir := t.TempDir()
	streams := filepath.Join(dir, "bench-x-streams")
	var runs []Run
	for i, arm := range []string{"opus/off", "haiku/off", "haiku/up"} {
		art := filepath.Join("artifacts", "t_"+strings.ReplaceAll(arm, "/", "-"))
		os.MkdirAll(filepath.Join(streams, art), 0o755)
		os.WriteFile(filepath.Join(streams, art, "plan.md"), []byte("plan from run"), 0o644)
		os.WriteFile(filepath.Join(streams, art, "shot.png"), []byte("png"), 0o644)
		runs = append(runs, Run{Task: "t", Arm: arm, Rep: i + 1, Artifacts: art})
	}
	runs = append(runs, Run{Task: "t", Arm: "sonnet/off", Rep: 1, Error: "x", Artifacts: "nope"})
	sample := SampleForRating(runs, tasks, 2, 1)
	if len(sample) != 2 || sample[0].Arm == sample[1].Arm {
		t.Fatalf("want 2 runs from different arms: %+v", sample)
	}
	if err := WriteRatingPage(dir, streams, sample, tasks, false); err != nil {
		t.Fatal(err)
	}
	page, _ := os.ReadFile(filepath.Join(dir, "rate.html"))
	for _, leak := range []string{"opus", "haiku", "sonnet", "/off", "/up"} {
		if strings.Contains(string(page), leak) {
			t.Errorf("page reveals %q", leak)
		}
	}
	if !strings.Contains(string(page), "plan from run") || !strings.Contains(string(page), `src="rate-media/r0`) || !strings.Contains(string(page), "很好") {
		t.Error("page lacks the outputs or the options")
	}
	var key []RateKeyEntry
	b, _ := os.ReadFile(filepath.Join(dir, "rate-key.json"))
	if json.Unmarshal(b, &key) != nil || len(key) != 2 || key[0].Arm == "" {
		t.Fatal(string(b))
	}
}

func TestSampleTakesWholeRepetitions(t *testing.T) {
	taskDir := t.TempDir()
	os.WriteFile(filepath.Join(taskDir, "human.json"), []byte(`{}`), 0o644)
	tasks := map[string]Task{"t": {Name: "t", Dir: taskDir}}
	var runs []Run
	for rep := 1; rep <= 3; rep++ {
		for _, arm := range []string{"opus/off", "sonnet/off", "haiku/off", "haiku/up", "opus/down"} {
			runs = append(runs, Run{Task: "t", Arm: arm, Rep: rep, Artifacts: "a"})
		}
	}
	sample := SampleForRating(runs, tasks, 10, 1)
	reps := map[int]int{}
	for _, r := range sample {
		reps[r.Rep]++
	}
	if len(sample) != 10 || len(reps) != 2 {
		t.Fatalf("want two whole repetitions: %v", reps)
	}
	for rep, n := range reps {
		if n != 5 {
			t.Errorf("rep %d has %d of 5 arms", rep, n)
		}
	}
}

func TestMinimalRatingPageShowsOnlyMedia(t *testing.T) {
	taskDir := t.TempDir()
	os.WriteFile(filepath.Join(taskDir, "human.json"), []byte(`{"question":"QUESTION","options":[{"score":1,"label":"LABEL-ONE"},{"score":5,"label":"LABEL-FIVE"}]}`), 0o644)
	tasks := map[string]Task{"t": {Name: "t", Dir: taskDir, Prompt: "PROMPT-TEXT"}}
	dir := t.TempDir()
	streams := filepath.Join(dir, "s")
	art := filepath.Join("artifacts", "a")
	os.MkdirAll(filepath.Join(streams, art), 0o755)
	os.WriteFile(filepath.Join(streams, art, "render.png"), []byte("png"), 0o644)
	os.WriteFile(filepath.Join(streams, art, "fix.py"), []byte("CODE-TEXT"), 0o644)
	if err := WriteRatingPage(dir, streams, []Run{{Task: "t", Arm: "opus/off", Rep: 1, Artifacts: art}}, tasks, true); err != nil {
		t.Fatal(err)
	}
	page, _ := os.ReadFile(filepath.Join(dir, "rate.html"))
	for _, hidden := range []string{"PROMPT-TEXT", "QUESTION", "LABEL-ONE", "CODE-TEXT"} {
		if strings.Contains(string(page), hidden) {
			t.Errorf("minimal page shows %q", hidden)
		}
	}
	if !strings.Contains(string(page), `src="rate-media/r01/render.png"`) || !strings.Contains(string(page), `value="5"`) {
		t.Error("minimal page lacks the image or the score buttons")
	}
}
