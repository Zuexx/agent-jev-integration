package journal

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDailyFilesAndRetention(t *testing.T) {
	t.Setenv("JEVCC_HOME", t.TempDir())
	day := func(d int) time.Time { return time.Date(2026, 10, d, 12, 0, 0, 0, time.Local) }

	// An old file from before the window, and the legacy single file.
	old := filepath.Join(os.Getenv("JEVCC_HOME"), "decisions-2026-08-01.jsonl")
	os.WriteFile(old, []byte("{}\n"), 0o600)
	legacy := filepath.Join(os.Getenv("JEVCC_HOME"), "decisions.jsonl")
	os.WriteFile(legacy, []byte("{}\n"), 0o600)

	if err := Append("decisions", map[string]int{"n": 1}, 3, day(12)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("file older than the retention was kept")
	}
	if err := Append("decisions", map[string]int{"n": 2}, 3, day(14)); err != nil {
		t.Fatal(err)
	}
	// Day 12 is inside a 3-day window ending on day 14, so it stays.
	files := Files("decisions")
	want := []string{legacy, Path("decisions", day(12)), Path("decisions", day(14))}
	if len(files) != len(want) {
		t.Fatalf("files = %v", files)
	}
	for i := range want {
		if files[i] != want[i] {
			t.Fatalf("files[%d] = %s, want %s", i, files[i], want[i])
		}
	}
	// The legacy file is never pruned.
	if _, err := os.Stat(legacy); err != nil {
		t.Fatal("legacy file was removed")
	}
}
