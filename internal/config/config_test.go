package config

import (
	"os"
	"runtime"
	"testing"
)

func TestDefaultsAndEnvOverride(t *testing.T) {
	t.Setenv("JEVCC_HOME", t.TempDir())
	t.Setenv("TYPESAFE_API_KEY", "env-key")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.APIKey != "env-key" || c.Model != "jev-latest" || len(c.Tiers) != 3 || c.HookTimeoutMS != 4000 {
		t.Fatalf("%+v", c)
	}
}

func TestSaveKeyKeepsOtherFields(t *testing.T) {
	t.Setenv("JEVCC_HOME", t.TempDir())
	t.Setenv("TYPESAFE_API_KEY", "")
	if err := os.WriteFile(Path(), []byte(`{"default_on":true,"tiers":[{"model":"haiku"},{"model":"fable"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveKey("k1"); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.APIKey != "k1" || !c.DefaultOn || c.TierNames()[1] != "fable" {
		t.Fatalf("%+v", c)
	}
	if runtime.GOOS != "windows" { // Windows has no Unix permission bits
		fi, _ := os.Stat(Path())
		if fi.Mode().Perm() != 0o600 {
			t.Fatal(fi.Mode())
		}
	}
}

// The bench needs delegation down without escalation up (and the reverse),
// so JEVCC_ESCALATE must win over what JEVCC_DELEGATE implies.
func TestEscalateEnvOverridesDelegate(t *testing.T) {
	t.Setenv("JEVCC_HOME", t.TempDir())
	t.Setenv("JEVCC_DELEGATE", "agent")
	t.Setenv("JEVCC_ESCALATE", "0")
	c, err := Load()
	if err != nil || !c.Delegate || c.EscalateOn() {
		t.Fatal(c.Delegate, c.EscalateOn(), err)
	}
	t.Setenv("JEVCC_DELEGATE", "off")
	t.Setenv("JEVCC_ESCALATE", "1")
	if c, _ = Load(); c.Delegate || !c.EscalateOn() {
		t.Fatal(c.Delegate, c.EscalateOn())
	}
	t.Setenv("JEVCC_ESCALATE", "")
	if c, _ = Load(); c.EscalateOn() {
		t.Fatal("JEVCC_DELEGATE=off alone should still turn escalation off")
	}
}
