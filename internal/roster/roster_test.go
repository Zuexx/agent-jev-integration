package roster

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFrontmatter(t *testing.T) {
	fm, body := Frontmatter("---\nname: tdd\ndescription: \"Use when: \\\"x\\\"\"\nother: >\n  folded\n  text\nq: 'it''s'\n---\nBody here\n")
	if fm["name"] != "tdd" || fm["description"] != `Use when: "x"` || fm["other"] != "folded text" || fm["q"] != "it's" {
		t.Fatalf("%q", fm)
	}
	if body != "Body here\n" {
		t.Fatalf("%q", body)
	}
	if fm, _ := Frontmatter("no header"); len(fm) != 0 {
		t.Fatal(fm)
	}
}

func TestDiscover(t *testing.T) {
	root := t.TempDir()
	claude := filepath.Join(root, "claude")
	proj := filepath.Join(root, "proj", "sub")
	write(t, filepath.Join(claude, "skills", "alpha", "SKILL.md"), "---\nname: alpha\ndescription: user skill\n---\nA body")
	write(t, filepath.Join(claude, "skills", "hidden", "SKILL.md"), "---\nname: hidden\ndisable-model-invocation: true\n---\n")
	write(t, filepath.Join(root, "proj", ".claude", "skills", "beta", "SKILL.md"), "---\ndescription: project skill\n---\n")
	pl := filepath.Join(root, "cache", "pw")
	off := filepath.Join(root, "cache", "off")
	other := filepath.Join(root, "cache", "other")
	write(t, filepath.Join(pl, "skills", "brainstorm", "SKILL.md"), "---\nname: brainstorm\ndescription: plugin skill\n---\n")
	write(t, filepath.Join(off, "skills", "x", "SKILL.md"), "---\nname: x\n---\n")
	write(t, filepath.Join(other, "skills", "y", "SKILL.md"), "---\nname: y\n---\n")
	write(t, filepath.Join(claude, "plugins", "installed_plugins.json"), `{"version":2,"plugins":{
	  "sp@official":[{"scope":"user","installPath":"`+pl+`"}],
	  "off@m":[{"scope":"user","installPath":"`+off+`"}],
	  "other@m":[{"scope":"project","projectPath":"/elsewhere","installPath":"`+other+`"}]}}`)
	write(t, filepath.Join(claude, "settings.json"), `{"enabledPlugins":{"off@m":false}}`)

	got := Discover(claude, proj)
	names := []string{}
	for _, s := range got {
		names = append(names, s.Name)
	}
	want := []string{"alpha", "beta", "sp:brainstorm"}
	if len(names) != len(want) {
		t.Fatalf("%v", names)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("%v", names)
		}
	}
	if got[0].Excerpt != "A body" {
		t.Fatalf("%q", got[0].Excerpt)
	}
}
