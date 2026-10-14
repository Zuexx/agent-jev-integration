package install

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestApplyAndRevertKeepOtherHooks(t *testing.T) {
	in := []byte(`{"model":"opus","hooks":{"UserPromptSubmit":[{"hooks":[{"type":"command","command":"/bin/cswitch hook"}]}],"Stop":[]}}`)
	out, err := ApplySettings(in, "/usr/local/bin/jevcc")
	if err != nil {
		t.Fatal(err)
	}
	if !HasOurHook(out) || !strings.Contains(string(out), "cswitch hook") || ours(t, out) != 4 || !strings.Contains(string(out), `"Stop"`) {
		t.Fatal(string(out))
	}
	again, _ := ApplySettings(out, "/usr/local/bin/jevcc")
	if ours(t, again) != 4 {
		t.Fatal("install is not idempotent")
	}
	back, err := RevertSettings(again)
	if err != nil || HasOurHook(back) || strings.Contains(string(back), "jevcc") || !strings.Contains(string(back), "cswitch hook") || !strings.Contains(string(back), `"model": "opus"`) {
		t.Fatal(string(back), err)
	}
	empty, _ := RevertSettings([]byte(`{"hooks":{"UserPromptSubmit":[{"hooks":[{"type":"command","command":"jevcc hook"}]}]}}`))
	if strings.Contains(string(empty), "hooks") {
		t.Fatal(string(empty))
	}
}

func TestHookCommandIsOurs(t *testing.T) {
	for _, exe := range []string{"/a b/jevcc", "/x/jevcc", `C:\Users\me\go\bin\jevcc.exe`} {
		if c := HookCommand(exe); !isOurs(c) {
			t.Fatalf("%q not recognised as ours", c)
		}
	}
}

func TestInstallProjectAndLocalScopes(t *testing.T) {
	t.Setenv("JEVCC_HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("TYPESAFE_API_KEY", "k")
	dir := t.TempDir()
	for _, s := range []Scope{Project, Local} {
		var buf bytes.Buffer
		o := Options{Exe: "/opt/jevcc", Scope: s, Dir: dir, Out: &buf, Now: time.Now()}
		if err := Install(o); err != nil {
			t.Fatal(err)
		}
		raw, _ := os.ReadFile(o.settingsPath())
		if !HasOurHook(raw) {
			t.Fatal(s, string(raw))
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "settings.local.json")); err != nil {
		t.Fatal(err)
	}
	cls, err := os.ReadFile(filepath.Join(dir, ".claude", "commands", "jev-classify.md"))
	if err != nil || !strings.Contains(string(cls), quote("/opt/jevcc")+" classify") || strings.Contains(string(cls), "{{JEVCC}}") {
		t.Fatal(string(cls), err)
	}
	var doc bytes.Buffer
	if Doctor(Options{Dir: dir}, &doc) != 0 && !strings.Contains(doc.String(), "claude on PATH") {
		t.Fatal(doc.String())
	}
	if !strings.Contains(doc.String(), "project scope") || !strings.Contains(doc.String(), "local scope") {
		t.Fatal(doc.String())
	}
	var buf bytes.Buffer
	if err := Uninstall(Options{Scope: Project, Dir: dir, Out: &buf}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	if HasOurHook(raw) {
		t.Fatal(string(raw))
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "commands", "jev.md")); err == nil {
		t.Fatal("command left behind")
	}
}

// The plugin directory ships the same command files, invoking jevcc from PATH.
func TestPluginCommandsInSync(t *testing.T) {
	for name, want := range Commands("jevcc") {
		got, err := os.ReadFile(filepath.Join("..", "..", "plugin", "commands", name))
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("plugin/commands/%s is stale: run `go run ./cmd/jevcc plugin-files plugin`", name)
		}
	}
}

func TestAgentsJSONAndPluginSync(t *testing.T) {
	var m map[string]map[string]any
	if err := json.Unmarshal([]byte(AgentsJSON()), &m); err != nil || m["jev-haiku"]["model"] != "haiku" || m["jev-sonnet"]["prompt"] == "" {
		t.Fatal(m, err)
	}
	for name, want := range Agents() {
		got, err := os.ReadFile(filepath.Join("..", "..", "plugin", "agents", name))
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("plugin/agents/%s is stale: run `go run ./cmd/jevcc plugin-files plugin`", name)
		}
	}
}

func TestHideAndShowSkillsKeepsUserOverrides(t *testing.T) {
	t.Setenv("JEVCC_HOME", t.TempDir())
	dir := t.TempDir()
	sp := filepath.Join(dir, ".claude", "settings.local.json")
	os.MkdirAll(filepath.Dir(sp), 0o755)
	os.WriteFile(sp, []byte(`{"skillOverrides":{"deploy":"off"}}`), 0o644)
	var buf bytes.Buffer
	o := Options{Scope: Local, Dir: dir, Out: &buf}
	if err := HideSkills(o, []string{"deploy", "tdd", "sp:brainstorming"}, "name-only"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(sp)
	if !strings.Contains(string(raw), `"tdd": "name-only"`) || !strings.Contains(string(raw), `"deploy": "off"`) {
		t.Fatal(string(raw))
	}
	if err := ShowSkills(o); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(sp)
	if strings.Contains(string(raw), "name-only") || !strings.Contains(string(raw), `"deploy": "off"`) {
		t.Fatal(string(raw))
	}
}

func TestDefaultModelSetAndRestored(t *testing.T) {
	t.Setenv("JEVCC_HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	sp := filepath.Join(dir, ".claude", "settings.local.json")
	os.MkdirAll(filepath.Dir(sp), 0o755)
	os.WriteFile(sp, []byte(`{"model":"opus"}`), 0o644)
	var buf bytes.Buffer
	o := Options{Exe: "/opt/jevcc", Scope: Local, Dir: dir, Out: &buf, Now: time.Now(), DefaultModel: "haiku", OnByDefault: true}
	if err := Install(o); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(sp)
	if !strings.Contains(string(raw), `"model": "haiku"`) {
		t.Fatal(string(raw))
	}
	if err := Install(o); err != nil { // reinstall must not record haiku as the previous model
		t.Fatal(err)
	}
	if err := Uninstall(o); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(sp)
	if !strings.Contains(string(raw), `"model": "opus"`) {
		t.Fatal("not restored:", string(raw))
	}
}

func TestStatuslineWrapRoundTrip(t *testing.T) {
	in := []byte(`{"statusLine":{"type":"command","command":"'/Users/x/go/bin/cswitch' statusline"}}`)
	out, inner, err := WrapStatusline(in, "/opt/jevcc")
	if err != nil || inner != "'/Users/x/go/bin/cswitch' statusline" || !isOurStatusline(statuslineCmd(t, out)) {
		t.Fatal(string(out), inner, err)
	}
	again, inner2, _ := WrapStatusline(out, "/opt/jevcc")
	if inner2 != "" || !bytes.Equal(again, out) {
		t.Fatal("rewrapping must be a no-op", inner2)
	}
	back, _ := UnwrapStatusline(out, inner)
	if !strings.Contains(string(back), "cswitch' statusline") || strings.Contains(string(back), "jevcc") {
		t.Fatal(string(back))
	}
}

// ours counts jevcc hook entries across all events in a settings document.
func ours(t *testing.T, raw []byte) int {
	t.Helper()
	doc, err := parseSettings(raw)
	if err != nil {
		t.Fatal(err)
	}
	hooks, _ := doc["hooks"].(map[string]any)
	n := 0
	for _, ev := range Events {
		n += countOurs(asSlice(hooks[ev]))
	}
	return n
}

func statuslineCmd(t *testing.T, raw []byte) string {
	t.Helper()
	doc, err := parseSettings(raw)
	if err != nil {
		t.Fatal(err)
	}
	sl, _ := doc["statusLine"].(map[string]any)
	c, _ := sl["command"].(string)
	return c
}

func TestBackupNameIsPortable(t *testing.T) {
	got := backupName(`C:\Users\me\.claude\settings.json`)
	if strings.ContainsAny(got, `:\/`) || !strings.HasSuffix(got, "settings.json") {
		t.Fatal(got)
	}
}
