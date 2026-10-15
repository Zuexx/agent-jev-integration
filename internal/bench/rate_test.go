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
	if err := WriteRatingPage(dir, streams, sample, tasks); err != nil {
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
