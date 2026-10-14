package guard

import (
	"context"
	"strings"
	"testing"

	"github.com/zuexx/agent-jev-integration/internal/jev"
)

type fake map[string]float64

func (f fake) Ask(context.Context, any, map[string]jev.Question) (*jev.Response, error) {
	a := map[string]jev.Answer{}
	for k, v := range f {
		a[k] = jev.Answer{Type: "noul", Noul: v}
	}
	return &jev.Response{Answers: a}, nil
}

func TestCheck(t *testing.T) {
	v, _ := Check(context.Background(), fake{"destructive": 0.95, "exfiltration": 0.1}, Action{Tool: "Bash", Command: "rm -rf ~"}, 0.7)
	if len(v.Reasons) != 1 || !strings.Contains(Reason(v), "deletes or overwrites") {
		t.Fatal(v)
	}
	v, _ = Check(context.Background(), fake{"destructive": 0.2}, Action{Tool: "Bash", Command: "ls"}, 0.7)
	if len(v.Reasons) != 0 {
		t.Fatal(v)
	}
}

func TestFromToolInput(t *testing.T) {
	a := FromToolInput("Edit", map[string]any{"file_path": "/x/.env", "new_string": "KEY=abc"}, "/x")
	if a.File != "/x/.env" || a.Content != "KEY=abc" || !Guarded("Edit") || Guarded("Read") {
		t.Fatalf("%+v", a)
	}
}
