package config

import (
	"os"
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
	fi, _ := os.Stat(Path())
	if fi.Mode().Perm() != 0o600 {
		t.Fatal(fi.Mode())
	}
}
