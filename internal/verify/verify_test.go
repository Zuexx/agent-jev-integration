package verify

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zuexx/agent-jev-integration/internal/jev"
)

const tr = `{"type":"user","message":{"role":"user","content":"old request"}}
{"type":"assistant","message":{"content":[{"type":"text","text":"old answer"}]}}
{"type":"user","message":{"role":"user","content":"Implement Compare per semver"}}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Write","input":{"file_path":"semver/semver.go"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","content":"ok"}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"go test ./..."}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","content":[{"type":"text","text":"--- FAIL: TestX"}]}]}}
{"type":"assistant","message":{"content":[{"type":"text","text":"Done, all good."}]}}
`

func TestReadTurn(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	os.WriteFile(p, []byte(tr), 0o600)
	turn, ok := ReadTurn(p)
	if !ok || turn.Request != "Implement Compare per semver" || turn.Final != "Done, all good." || len(turn.Commands) != 2 || !strings.Contains(turn.Outputs[1], "FAIL") {
		t.Fatalf("%+v", turn)
	}
}

type fake map[string]float64

func (f fake) Ask(context.Context, any, map[string]jev.Question) (*jev.Response, error) {
	a := map[string]jev.Answer{}
	for k, v := range f {
		a[k] = jev.Answer{Type: "noul", Noul: v}
	}
	return &jev.Response{Answers: a}, nil
}

func TestCheck(t *testing.T) {
	ok := fake{"complete": 0.9, "unverified": 0.1, "failing": 0.1, "hedging": 0.1, "untested_rules": 0.2}
	v, _ := Check(context.Background(), ok, Turn{}, 0.5)
	if len(v.Reasons) != 0 || v.Risk > 0.21 {
		t.Fatalf("%+v", v)
	}
	bad := fake{"complete": 0.9, "unverified": 0.7, "failing": 0.8, "hedging": 0.1, "untested_rules": 0.2}
	v, _ = Check(context.Background(), bad, Turn{}, 0.5)
	if len(v.Reasons) != 2 || !strings.HasPrefix(v.Reasons[0], "unresolved failures") {
		t.Fatalf("%+v", v)
	}
	if !strings.Contains(Escalation(v, "jev-opus"), `"jev-opus"`) {
		t.Fatal("escalation text")
	}
}
