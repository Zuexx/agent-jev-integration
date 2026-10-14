// Package config loads ~/.jevcc/config.json. Every field has a default, so a
// missing file is a valid config; only the API key has no default.
package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/zuexx/agent-jev-integration/internal/fsutil"
	"github.com/zuexx/agent-jev-integration/internal/paths"
)

// Tier is one model the router may pick, cheapest first.
type Tier struct {
	Model       string `json:"model"`
	Description string `json:"description"`
}

type Config struct {
	APIKey   string `json:"api_key,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
	Model    string `json:"model,omitempty"`
	// Tiers are ordered cheapest first; the last one is "handle it yourself".
	Tiers []Tier `json:"tiers,omitempty"`
	// Delegate makes the hook tell Claude to hand cheaper-tier prompts to a
	// subagent on that model. Off by default: in the A/B bench, delegation
	// raised cost and time on short tasks (the main model's fixed context
	// cost remains). Model savings come from `jevcc ask` instead.
	Delegate bool `json:"delegate,omitempty"`
	// Escalate makes the hook tell a cheaper session to hand prompts that need
	// a stronger tier to jev-<tier> (default on: 17/17 hand-offs, -96% cost).
	Escalate *bool `json:"escalate,omitempty"`
	// DelegateVia is "agent" (the light jev-<tier> subagents jevcc installs:
	// own short system prompt, no CLAUDE.md) or "model" (the general-purpose
	// subagent with a model override).
	DelegateVia string `json:"delegate_via,omitempty"`
	// MainModel is the tier the session itself runs on. With delegation on, a
	// cheaper tier is delegated down and a stronger one escalated up to
	// jev-<tier>. Empty means the top tier (only delegate down).
	MainModel string `json:"main_model,omitempty"`
	// EscalateConfidence gates upward advice; lower than MinConfidence
	// because a wrong escalation costs money, a missed one costs quality.
	EscalateConfidence float64 `json:"escalate_confidence,omitempty"`
	// MemoryThreshold: a message whose "worth remembering" noul reaches it
	// gets a save-to-memory nudge (0 in the file means the default; set a
	// negative value to turn it off).
	MemoryThreshold float64 `json:"memory_threshold,omitempty"`
	// Statusline is how much jevcc adds to the statusline: "full" (settings +
	// activity lines, default), "compact" (one line) or "off".
	Statusline string `json:"statusline,omitempty"`
	// StatuslineInner is the statusline command jevcc wraps (set by install).
	StatuslineInner string `json:"statusline_inner,omitempty"`
	// FindFiles adds "likely relevant files" to the routing advice for code
	// questions in a project directory (default on; JEVCC_FIND=0 turns it off).
	FindFiles *bool `json:"find_files,omitempty"`
	// SkillAdvice can switch the skill line off (default on; JEVCC_SKILLS=0).
	SkillAdvice *bool `json:"skill_advice,omitempty"`
	// Guard turns on the PreToolUse check: risky commands and file writes
	// become a confirmation prompt. Independent of /jev on.
	Guard          *bool   `json:"guard,omitempty"`
	GuardThreshold float64 `json:"guard_threshold,omitempty"`
	// Injection screens fetched content (PostToolUse) for text aimed at the
	// agent (default on).
	Injection          *bool   `json:"injection,omitempty"`
	InjectionThreshold float64 `json:"injection_threshold,omitempty"`
	// Verify turns on the Stop-hook check: when the session finishes a turn,
	// Jev reviews it and, past VerifyThreshold, asks Claude to hand the work
	// to the VerifyAgent subagent.
	Verify          *bool   `json:"verify,omitempty"`
	VerifyThreshold float64 `json:"verify_threshold,omitempty"`
	VerifyAgent     string  `json:"verify_agent,omitempty"`
	// DefaultOn turns routing on for sessions that never ran /jev on|off.
	DefaultOn bool `json:"default_on,omitempty"`
	// MinConfidence below which the hook gives no tier advice.
	MinConfidence float64 `json:"min_confidence,omitempty"`
	// SkillGate is the mean "needs a skill" noul below which no skill is suggested.
	SkillGate float64 `json:"skill_gate,omitempty"`
	// SkillFit is the minimum "this skill does it" noul for the final pick.
	SkillFit float64 `json:"skill_fit,omitempty"`
	// HookTimeoutMS bounds all Jev calls made by one hook run.
	HookTimeoutMS int `json:"hook_timeout_ms,omitempty"`
}

var DefaultTiers = []Tier{
	{"haiku", "Trivial, mechanical or lookup work: find a file or symbol, read or explain a short snippet, rename, reformat or convert text, run one obvious command, answer a factual question about existing code. No design decisions and no debugging."},
	{"sonnet", "Routine engineering with a clear target: implement or change a well-specified feature, write tests, fix a bug whose cause is known or easy to find, refactor within a few files, write documentation."},
	{"opus", "Hard reasoning: architecture or design decisions, ambiguous or underspecified requirements, debugging an unclear root cause, security or concurrency analysis, cross-cutting changes, planning multi-step work."},
}

func Path() string { return filepath.Join(paths.StateDir(), "config.json") }

// Load reads the config file (if any), applies defaults, and lets
// TYPESAFE_API_KEY / TYPESAFE_ENDPOINT override the file.
func Load() (*Config, error) {
	c := &Config{}
	if err := fsutil.ReadJSON(Path(), c); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if v := os.Getenv("TYPESAFE_API_KEY"); v != "" {
		c.APIKey = v
	}
	if v := os.Getenv("TYPESAFE_ENDPOINT"); v != "" {
		c.Endpoint = v
	}
	envBool := func(name string, dst **bool) {
		switch os.Getenv(name) {
		case "1", "on", "true":
			t := true
			*dst = &t
		case "0", "off", "false":
			f := false
			*dst = &f
		}
	}
	envBool("JEVCC_FIND", &c.FindFiles)
	envBool("JEVCC_SKILLS", &c.SkillAdvice)
	envBool("JEVCC_GUARD", &c.Guard)
	envBool("JEVCC_INJECTION", &c.Injection)
	envBool("JEVCC_VERIFY", &c.Verify)
	envBool("JEVCC_ESCALATE", &c.Escalate)
	if v := os.Getenv("JEVCC_MAIN"); v != "" {
		c.MainModel = v
	}
	// JEVCC_DELEGATE=agent|model|off overrides delegation (used by the benchmark).
	switch v := os.Getenv("JEVCC_DELEGATE"); v {
	case "agent", "model":
		t := true
		c.Delegate, c.DelegateVia, c.Escalate = true, v, &t
	case "off":
		f := false
		c.Delegate, c.Escalate = false, &f
	}
	c.fill()
	return c, nil
}

func (c *Config) fill() {
	if c.Endpoint == "" {
		c.Endpoint = "https://api.typesafe.ai"
	}
	if c.Model == "" {
		c.Model = "jev-latest"
	}
	if len(c.Tiers) == 0 {
		c.Tiers = DefaultTiers
	}
	if c.DelegateVia == "" {
		c.DelegateVia = "agent"
	}
	if c.EscalateConfidence == 0 {
		c.EscalateConfidence = 0.5
	}
	if c.Statusline == "" {
		c.Statusline = "full"
	}
	if c.MemoryThreshold == 0 {
		c.MemoryThreshold = 0.7
	}
	if c.InjectionThreshold == 0 {
		c.InjectionThreshold = 0.7
	}
	if c.GuardThreshold == 0 {
		c.GuardThreshold = 0.7
	}
	if c.VerifyThreshold == 0 {
		c.VerifyThreshold = 0.5
	}
	if c.VerifyAgent == "" {
		c.VerifyAgent = "jev-opus"
	}
	if c.MinConfidence == 0 {
		c.MinConfidence = 0.75
	}
	if c.SkillGate == 0 {
		c.SkillGate = 0.3
	}
	if c.SkillFit == 0 {
		c.SkillFit = 0.7
	}
	if c.HookTimeoutMS == 0 {
		c.HookTimeoutMS = 4000
	}
}

// SetField writes one field into the config file, keeping the others.
func SetField(name string, v any) error {
	doc := map[string]any{}
	if err := fsutil.ReadJSON(Path(), &doc); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	doc[name] = v
	return fsutil.WriteJSON(Path(), doc)
}

// SaveKey stores the API key in the config file, keeping other fields as written.
func SaveKey(key string) error {
	doc := map[string]any{}
	if err := fsutil.ReadJSON(Path(), &doc); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	doc["api_key"] = key
	return fsutil.WriteJSON(Path(), doc)
}

func on(b *bool) bool { return b == nil || *b }

func (c *Config) FindFilesOn() bool   { return on(c.FindFiles) }
func (c *Config) SkillAdviceOn() bool { return on(c.SkillAdvice) }
func (c *Config) EscalateOn() bool    { return on(c.Escalate) }
func (c *Config) VerifyOn() bool      { return on(c.Verify) }
func (c *Config) GuardOn() bool       { return on(c.Guard) }
func (c *Config) InjectionOn() bool   { return on(c.Injection) }

// TierNames returns the tier models, cheapest first.
func (c *Config) TierNames() []string {
	out := make([]string, len(c.Tiers))
	for i, t := range c.Tiers {
		out[i] = t.Model
	}
	return out
}
