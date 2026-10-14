package install

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/zuexx/agent-jev-integration/internal/fsutil"
	"github.com/zuexx/agent-jev-integration/internal/paths"
)

// hiddenRecord remembers which skillOverrides entries jevcc wrote, per
// settings file, so `jevcc skills show` removes exactly those.
func hiddenRecord() string { return filepath.Join(paths.StateDir(), "hidden-skills.json") }

// ApplyOverrides sets every name without an override to mode. It returns the
// names it set (existing entries, the user's own, are left alone).
func ApplyOverrides(raw []byte, names []string, mode string) ([]byte, []string, error) {
	doc, err := parseSettings(raw)
	if err != nil {
		return nil, nil, err
	}
	ov, _ := doc["skillOverrides"].(map[string]any)
	if ov == nil {
		ov = map[string]any{}
	}
	var set []string
	for _, n := range names {
		if _, ok := ov[n]; ok {
			continue
		}
		ov[n] = mode
		set = append(set, n)
	}
	doc["skillOverrides"] = ov
	out, err := fsutil.Marshal(doc)
	return out, set, err
}

// RevertOverrides removes the given entries if they still hold mode.
func RevertOverrides(raw []byte, names []string, mode string) ([]byte, error) {
	doc, err := parseSettings(raw)
	if err != nil {
		return nil, err
	}
	if ov, ok := doc["skillOverrides"].(map[string]any); ok {
		for _, n := range names {
			if ov[n] == mode {
				delete(ov, n)
			}
		}
		if len(ov) == 0 {
			delete(doc, "skillOverrides")
		}
	}
	return fsutil.Marshal(doc)
}

type hidden struct {
	Mode  string   `json:"mode"`
	Names []string `json:"names"`
}

// HideSkills writes skillOverrides for names into the scope's settings file.
func HideSkills(o Options, names []string, mode string) error {
	sp := o.settingsPath()
	raw, err := os.ReadFile(sp)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	out, set, err := ApplyOverrides(raw, names, mode)
	if err != nil {
		return fmt.Errorf("%s: %w", sp, err)
	}
	rec := map[string]hidden{}
	_ = fsutil.ReadJSON(hiddenRecord(), &rec)
	prev := rec[sp]
	prev.Mode = mode
	prev.Names = uniq(append(prev.Names, set...))
	rec[sp] = prev
	if err := fsutil.WriteJSON(hiddenRecord(), rec); err != nil {
		return err
	}
	if err := writeFollowingLinks(sp, out, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(o.Out, "✓ %d skills set to %q in %s (%d already had an override and were left alone)\n", len(set), mode, sp, len(names)-len(set))
	fmt.Fprintln(o.Out, "  Takes effect in new Claude Code sessions. Undo with `jevcc skills show` (same --scope).")
	return nil
}

// ShowSkills removes the overrides HideSkills wrote for the scope.
func ShowSkills(o Options) error {
	sp := o.settingsPath()
	rec := map[string]hidden{}
	_ = fsutil.ReadJSON(hiddenRecord(), &rec)
	h, ok := rec[sp]
	if !ok {
		fmt.Fprintln(o.Out, "jevcc: no skills hidden by jevcc in", sp)
		return nil
	}
	raw, err := os.ReadFile(sp)
	if err != nil {
		return err
	}
	out, err := RevertOverrides(raw, h.Names, h.Mode)
	if err != nil {
		return err
	}
	if err := writeFollowingLinks(sp, out, 0o644); err != nil {
		return err
	}
	delete(rec, sp)
	if err := fsutil.WriteJSON(hiddenRecord(), rec); err != nil {
		return err
	}
	fmt.Fprintf(o.Out, "✓ restored %d skills in %s\n", len(h.Names), sp)
	return nil
}

func uniq(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	sort.Strings(out)
	return out
}
