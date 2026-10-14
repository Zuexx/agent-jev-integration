package fsutil

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJSONRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a", "b.json")
	if err := WriteJSON(p, map[string]string{"k": "<v>"}); err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := ReadJSON(p, &got); err != nil || got["k"] != "<v>" {
		t.Fatal(got, err)
	}
	if err := ReadJSON(p+"x", &got); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
}

func TestAppendJSONL(t *testing.T) {
	p := filepath.Join(t.TempDir(), "l.jsonl")
	for i := 0; i < 2; i++ {
		if err := AppendJSONL(p, map[string]int{"i": i}); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := os.ReadFile(p)
	if strings.Count(string(b), "\n") != 2 {
		t.Fatalf("%q", b)
	}
}
