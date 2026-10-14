// Package roster finds the skills Claude Code would offer in a directory:
// user skills, project skills, and skills of enabled plugins.
package roster

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/zuexx/agent-jev-integration/internal/fsutil"
)

// ExcerptChars is how much of a SKILL.md body is kept for the verification call,
// the same budget the TypeSafe skill-suggestion cookbook uses.
const ExcerptChars = 700

type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Excerpt     string `json:"excerpt,omitempty"`
	Path        string `json:"path,omitempty"`
}

// Discover lists skills visible from cwd. claudeDir is ~/.claude.
func Discover(claudeDir, cwd string) []Skill {
	seen := map[string]bool{}
	var out []Skill
	add := func(prefix, dir string) {
		for _, s := range scanDir(dir) {
			if prefix != "" {
				s.Name = prefix + ":" + s.Name
			}
			if !seen[s.Name] {
				seen[s.Name] = true
				out = append(out, s)
			}
		}
	}
	for _, d := range projectDirs(cwd) {
		add("", filepath.Join(d, ".claude", "skills"))
	}
	add("", filepath.Join(claudeDir, "skills"))
	for _, p := range enabledPlugins(claudeDir, cwd) {
		add(p.name, filepath.Join(p.path, "skills"))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// projectDirs is cwd and its ancestors, nearest first, stopping before $HOME.
func projectDirs(cwd string) []string {
	if cwd == "" {
		return nil
	}
	home, _ := os.UserHomeDir()
	var out []string
	for d := filepath.Clean(cwd); ; d = filepath.Dir(d) {
		if d == home {
			break
		}
		out = append(out, d)
		if filepath.Dir(d) == d {
			break
		}
	}
	return out
}

func scanDir(dir string) []Skill {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Skill
	for _, e := range entries {
		p := filepath.Join(dir, e.Name(), "SKILL.md")
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		fm, body := Frontmatter(string(raw))
		s := Skill{Name: fm["name"], Description: fm["description"], Path: p, Excerpt: excerpt(body)}
		if s.Name == "" {
			s.Name = e.Name()
		}
		if fm["disable-model-invocation"] == "true" {
			continue
		}
		out = append(out, s)
	}
	return out
}

func excerpt(body string) string {
	body = strings.TrimSpace(body)
	r := []rune(body)
	if len(r) > ExcerptChars {
		return string(r[:ExcerptChars])
	}
	return body
}

type plugin struct{ name, path string }

type installed struct {
	Plugins map[string][]struct {
		Scope       string `json:"scope"`
		ProjectPath string `json:"projectPath"`
		InstallPath string `json:"installPath"`
	} `json:"plugins"`
}

func enabledPlugins(claudeDir, cwd string) []plugin {
	var inst installed
	if err := fsutil.ReadJSON(filepath.Join(claudeDir, "plugins", "installed_plugins.json"), &inst); err != nil {
		return nil
	}
	var settings struct {
		Enabled map[string]bool `json:"enabledPlugins"`
	}
	_ = fsutil.ReadJSON(filepath.Join(claudeDir, "settings.json"), &settings)
	keys := make([]string, 0, len(inst.Plugins))
	for k := range inst.Plugins {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []plugin
	for _, key := range keys {
		if on, ok := settings.Enabled[key]; ok && !on {
			continue
		}
		name, _, _ := strings.Cut(key, "@")
		for _, e := range inst.Plugins[key] {
			if e.Scope != "user" && !within(cwd, e.ProjectPath) {
				continue
			}
			out = append(out, plugin{name, e.InstallPath})
			break
		}
	}
	return out
}

func within(cwd, root string) bool {
	if cwd == "" || root == "" {
		return false
	}
	rel, err := filepath.Rel(root, cwd)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Frontmatter parses the simple YAML header of a SKILL.md: one-line
// `key: value` pairs, quoted values, and `|` / `>` block scalars.
func Frontmatter(doc string) (map[string]string, string) {
	fm := map[string]string{}
	doc = strings.TrimPrefix(doc, "\uFEFF")
	if !strings.HasPrefix(doc, "---") {
		return fm, doc
	}
	lines := strings.Split(doc, "\n")
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], "\r ") == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return fm, doc
	}
	for i := 1; i < end; i++ {
		line := strings.TrimRight(lines[i], "\r")
		if line == "" || line[0] == ' ' || line[0] == '\t' || line[0] == '#' {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if v == "|" || v == ">" || v == "|-" || v == ">-" {
			var parts []string
			for i+1 < end && (strings.HasPrefix(lines[i+1], " ") || strings.HasPrefix(lines[i+1], "\t") || strings.TrimSpace(lines[i+1]) == "") {
				i++
				parts = append(parts, strings.TrimSpace(lines[i]))
			}
			sep := " "
			if v[0] == '|' {
				sep = "\n"
			}
			v = strings.TrimSpace(strings.Join(parts, sep))
		} else if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"') {
			var s string
			if json.Unmarshal([]byte(v), &s) == nil {
				v = s
			}
		} else if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
			v = strings.ReplaceAll(v[1:len(v)-1], "''", "'")
		}
		fm[k] = v
	}
	return fm, strings.Join(lines[end+1:], "\n")
}
