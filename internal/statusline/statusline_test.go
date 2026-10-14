package statusline

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zuexx/agent-jev-integration/internal/config"
	"github.com/zuexx/agent-jev-integration/internal/paths"
)

func TestCountAndLine(t *testing.T) {
	home := t.TempDir()
	t.Setenv("JEVCC_HOME", home)
	t.Setenv("TYPESAFE_API_KEY", "k")
	t.Setenv("JEVCC_MODE", "on")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	os.WriteFile(paths.DecisionLog(), []byte(`{"session_id":"S","advice":["escalate","skill"],"decision":{"tier":"opus","jev_cost_usd":0.0002}}
{"session_id":"S","advice":["files"],"decision":{"tier":"haiku","jev_cost_usd":0.0001}}
{"session_id":"OTHER","advice":["tier"],"decision":{"tier":"opus"}}
`), 0o600)
	os.WriteFile(filepath.Join(home, "guard.jsonl"), []byte(`{"session_id":"S","asked":true,"verdict":{"jev_cost_usd":0.00002}}
{"session_id":"S","verdict":{"jev_cost_usd":0.00002}}
`), 0o600)
	s := Count("S")
	if s.Prompts != 2 || s.Escalated != 1 || s.Skill != 1 || s.Files != 1 || s.GuardAsked != 1 || s.GuardChecked != 2 {
		t.Fatalf("%+v", s)
	}
	cfg, _ := config.Load()
	cfg.MainModel = "haiku"
	line := Line(cfg, Input{SessionID: "S"}, s)
	for _, want := range []string{"jev on · haiku", "↑opus 1", "skill 1", "files 1", "🛡 1/2 asked", "$0.0003"} {
		if !strings.Contains(line, want) {
			t.Fatalf("missing %q in %q", want, line)
		}
	}
}

func TestMainWrapsInner(t *testing.T) {
	t.Setenv("JEVCC_HOME", t.TempDir())
	t.Setenv("TYPESAFE_API_KEY", "")
	os.WriteFile(config.Path(), []byte(`{"statusline_inner":"echo dashboard-line"}`), 0o600)
	var out bytes.Buffer
	Main(strings.NewReader(`{"session_id":"S"}`), &out)
	if !strings.HasPrefix(out.String(), "dashboard-line\n🧭 jev: no TypeSafe key") {
		t.Fatalf("%q", out.String())
	}
}

func TestRenderModes(t *testing.T) {
	t.Setenv("JEVCC_HOME", t.TempDir())
	t.Setenv("TYPESAFE_API_KEY", "k")
	t.Setenv("JEVCC_MODE", "on")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	cfg, _ := config.Load()
	cfg.MainModel = "haiku"
	s := Session{Prompts: 3, Escalated: 1, EscalatedTo: map[string]int{"opus": 1}, GuardChecked: 4, GuardAsked: 1, Screened: 2, CostUSD: 0.0004}
	full := Render(cfg, Input{SessionID: "S"}, s)
	if len(full) != 2 || !strings.Contains(full[0], "⚙ jev on · haiku (config) · ↑✓ ↓✗ review✓ 🛡✓ inject✓") || !strings.Contains(full[1], "inject 0/2 warned") {
		t.Fatalf("%q", full)
	}
	cfg.Statusline = "compact"
	if got := Render(cfg, Input{SessionID: "S"}, s); len(got) != 1 || !strings.HasPrefix(got[0], "🧭 jev on · haiku · 3 prompts") {
		t.Fatalf("%q", got)
	}
	cfg.Statusline = "off"
	if got := Render(cfg, Input{SessionID: "S"}, s); len(got) != 0 {
		t.Fatal(got)
	}
}
