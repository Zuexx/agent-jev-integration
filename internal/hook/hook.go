// Package hook implements `jevcc hook`, the UserPromptSubmit hook. It answers
// /jev locally (no model call) and, when routing is on for the session, asks
// Jev about the prompt and adds its advice as context.
package hook

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/zuexx/agent-jev-integration/internal/config"
	"github.com/zuexx/agent-jev-integration/internal/find"
	"github.com/zuexx/agent-jev-integration/internal/fsutil"
	"github.com/zuexx/agent-jev-integration/internal/guard"
	"github.com/zuexx/agent-jev-integration/internal/inject"
	"github.com/zuexx/agent-jev-integration/internal/install"
	"github.com/zuexx/agent-jev-integration/internal/jev"
	"github.com/zuexx/agent-jev-integration/internal/journal"
	"github.com/zuexx/agent-jev-integration/internal/logx"
	"github.com/zuexx/agent-jev-integration/internal/paths"
	"github.com/zuexx/agent-jev-integration/internal/roster"
	"github.com/zuexx/agent-jev-integration/internal/router"
	"github.com/zuexx/agent-jev-integration/internal/stats"
	"github.com/zuexx/agent-jev-integration/internal/verify"
)

type Input struct {
	HookEventName  string `json:"hook_event_name"`
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	Prompt         string `json:"prompt"`
	StopHookActive bool   `json:"stop_hook_active"`
	// LastAssistantMessage is set on Stop: Claude's final text for the turn.
	LastAssistantMessage string `json:"last_assistant_message"`
	// ToolName and ToolInput are set on PreToolUse.
	ToolName     string          `json:"tool_name"`
	ToolInput    map[string]any  `json:"tool_input"`
	ToolResponse json.RawMessage `json:"tool_response"`
}

type Specific struct {
	HookEventName            string `json:"hookEventName"`
	AdditionalContext        string `json:"additionalContext,omitempty"`
	PermissionDecision       string `json:"permissionDecision,omitempty"`
	PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
}

type Output struct {
	Decision string    `json:"decision,omitempty"`
	Reason   string    `json:"reason,omitempty"`
	Specific *Specific `json:"hookSpecificOutput,omitempty"`
}

type session struct {
	On      bool      `json:"on"`
	Updated time.Time `json:"updated"`
}

// Enabled resolves whether routing is on: $JEVCC_MODE wins (used by the
// benchmark), then the session's /jev on|off, then config default_on.
func Enabled(cfg *config.Config, sessionID string) bool {
	switch strings.ToLower(os.Getenv("JEVCC_MODE")) {
	case "on", "1", "true":
		return true
	case "off", "0", "false":
		return false
	}
	var s session
	if err := fsutil.ReadJSON(paths.SessionFile(sessionID), &s); err == nil {
		return s.On
	} else if !errors.Is(err, fs.ErrNotExist) {
		logx.Printf("hook: session %s: %v", sessionID, err)
	}
	return cfg.DefaultOn
}

func setEnabled(sessionID string, on bool) error {
	return fsutil.WriteJSON(paths.SessionFile(sessionID), session{On: on, Updated: time.Now()})
}

func block(format string, args ...any) *Output {
	return &Output{Decision: "block", Reason: fmt.Sprintf(format, args...)}
}

const help = `jevcc /jev commands (handled locally, no model call):
  /jev on       route this session's prompts through Jev (model tier + skill + bulk hints)
  /jev off      back to plain Claude Code
  /jev status   show whether routing is on and the configured tiers
  /jev stats    what Jev decided so far and whether Claude followed it
  /jev mcp on|off     let jev-haiku/sonnet/opus use MCP servers and skills (default off)
  /jev guard on|off   ask before risky commands and writes (default off)
  /jev statusline full|compact|off   how much jevcc shows in the statusline
Other commands: /jev-classify, /jev-lint, /jev-review, /jev-audit`

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// Command handles a /jev prompt. ok is false for any other prompt.
func Command(in Input, cfg *config.Config) (out *Output, ok bool) {
	f := strings.Fields(in.Prompt)
	if len(f) == 0 || f[0] != "/jev" {
		return nil, false
	}
	arg := ""
	if len(f) > 1 {
		arg = f[1]
	}
	switch arg {
	case "on", "off":
		if in.SessionID == "" {
			return block("jevcc: Claude Code did not provide a session id"), true
		}
		if err := setEnabled(in.SessionID, arg == "on"); err != nil {
			return block("jevcc: %v", err), true
		}
		if arg == "off" {
			return block("jevcc: routing OFF for this session — Claude Code runs as usual."), true
		}
		msg := "jevcc: routing ON for this session. Each prompt is checked by Jev first (model tier, skill, bulk work)."
		if cfg.APIKey == "" {
			msg += "\n⚠ no TypeSafe API key: run `jevcc key` in a terminal, or prompts will pass through without advice."
		} else {
			startWarm()
		}
		return block("%s", msg), true
	case "status", "":
		var b strings.Builder
		state := "OFF"
		if Enabled(cfg, in.SessionID) {
			state = "ON"
		}
		if v := os.Getenv("JEVCC_MODE"); v != "" {
			state += " (forced by JEVCC_MODE=" + v + ")"
		} else if cfg.DefaultOn {
			state += " (on by default for new sessions)"
		}
		key := "set"
		if cfg.APIKey == "" {
			key = "MISSING (run `jevcc key`)"
		}
		main, src := SessionTierSource(in, cfg)
		if main == "" {
			main, src = "unknown", "no transcript or settings model"
		}
		fmt.Fprintf(&b, "jevcc: routing %s for this session\n", state)
		fmt.Fprintf(&b, "  session model  %s (from %s)\n", main, src)
		fmt.Fprintf(&b, "  jev model      %s · tiers %s · api key %s\n", cfg.Model, strings.Join(cfg.TierNames(), " < "), key)
		fmt.Fprintf(&b, "  statusline     %s\n\n", cfg.Statusline)
		for _, f := range Features(cfg, in) {
			mark := "✗"
			if f.On {
				mark = "✓"
			}
			fmt.Fprintf(&b, "  %s %-48s %s\n", mark, f.Name, f.Note)
		}
		fmt.Fprintf(&b, "\nchange defaults in %s\n\n%s", config.Path(), help)
		return block("%s", b.String()), true
	case "statusline":
		mode := ""
		if len(f) > 2 {
			mode = f[2]
		}
		switch mode {
		case "full", "compact", "off":
			if err := config.SetField("statusline", mode); err != nil {
				return block("jevcc: %v", err), true
			}
			return block("jevcc: statusline set to %s (applies on the next refresh)", mode), true
		}
		return block("usage: /jev statusline full|compact|off (now: %s)", cfg.Statusline), true
	case "mcp", "guard":
		v := ""
		if len(f) > 2 {
			v = f[2]
		}
		if v != "on" && v != "off" {
			now := cfg.AgentMCP
			if arg == "guard" {
				now = cfg.GuardOn()
			}
			return block("usage: /jev %s on|off (now: %s)", arg, onOff(now)), true
		}
		if arg == "guard" {
			if err := config.SetField("guard", v == "on"); err != nil {
				return block("jevcc: %v", err), true
			}
			return block("jevcc: guard %s — risky commands and writes %s (all sessions, from the next tool call)", v,
				map[string]string{"on": "ask you first", "off": "run without a Jev check"}[v]), true
		}
		if err := config.SetField("agent_mcp", v == "on"); err != nil {
			return block("jevcc: %v", err), true
		}
		n, err := install.WriteAgents(install.UserAgentsDir(), v == "on")
		if err != nil {
			return block("jevcc: %v", err), true
		}
		msg := fmt.Sprintf("jevcc: subagent MCP/Skill %s; rewrote %d agent files in %s. Takes effect in new sessions.", v, n, install.UserAgentsDir())
		if n == 0 {
			msg += "\n(no user-scope agents found: for a project or plugin install, rerun `jevcc install` with the same scope)"
		}
		return block("%s", msg), true
	case "stats":
		recs, err := stats.LoadDecisions()
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return block("jevcc: %v", err), true
		}
		return block("%s", stats.Summary(recs, time.Time{})), true
	default:
		return block("%s", help), true
	}
}

type Deps struct {
	Config *config.Config
	Asker  router.Asker
	Skills func(cwd string) []roster.Skill
	Now    func() time.Time
}

// Route returns the context block for a prompt, and the record to log.
// It returns nil output when there is nothing to add.
func Route(ctx context.Context, in Input, d Deps) (*Output, stats.Record) {
	start := d.Now()
	rec := stats.Record{Time: start, SessionID: in.SessionID, Transcript: in.TranscriptPath, PromptHead: head(in.Prompt)}
	dec, err := router.Route(ctx, d.Asker, d.Config, in.Prompt, d.Skills(in.Cwd))
	rec.HookMS = d.Now().Sub(start).Milliseconds()
	if err != nil {
		rec.Error = err.Error()
		return nil, rec
	}
	rec.Decision = dec
	cfg := *d.Config
	main, source := SessionTierSource(in, d.Config)
	cfg.MainModel = main
	cfg.EscalationsUsed = escalations(in.SessionID)
	rec.Schema, rec.MainTier, rec.MainTierSource = stats.Schema, main, source
	rec.EscalationsUsed = cfg.EscalationsUsed
	text, kinds := router.Build(dec, &cfg)
	rec.Advice = kinds
	if hint := fileHint(ctx, in, d); hint != "" {
		if text == "" {
			text = "<jev_routing>\nThe user turned on Jev routing (jevcc); these are its suggestions for this request.\n</jev_routing>"
		}
		text = strings.Replace(text, "</jev_routing>", hint+"\n</jev_routing>", 1)
		rec.Advice = append(rec.Advice, "files")
	}
	rec.ContextChars = len(text)
	// The thresholds are logged even when no advice was given, so a silent
	// prompt can be checked against them.
	if cfg.TraceOn() {
		rec.Trace = &stats.Trace{Context: text, MinConf: cfg.MinConfidence, EscalateConf: cfg.EscalateConfidence,
			SkillFit: cfg.SkillFit, Memory: cfg.MemoryThreshold}
	}
	if text == "" {
		return nil, rec
	}
	return &Output{Specific: &Specific{HookEventName: "UserPromptSubmit", AdditionalContext: text}}, rec
}

func tierRank(t string, cfg *config.Config) int {
	for i, n := range cfg.TierNames() {
		if n == t {
			return i
		}
	}
	return len(cfg.TierNames())
}

// SessionTier works out which tier the session runs on: config main_model if
// set, else the model of the last assistant message in the transcript (so a
// /model switch is followed), else the "model" in Claude Code settings
// (local, project, user) or $ANTHROPIC_MODEL. "" when unknown.
func SessionTier(in Input, cfg *config.Config) string {
	t, _ := SessionTierSource(in, cfg)
	return t
}

// SessionTierSource is SessionTier plus where the answer came from.
func SessionTierSource(in Input, cfg *config.Config) (tier, source string) {
	if t := router.TierOf(cfg.MainModel, cfg); t != "" {
		return t, "config"
	}
	if t := router.TierOf(lastModel(in.TranscriptPath), cfg); t != "" {
		return t, "this session"
	}
	var files []string
	if in.Cwd != "" {
		files = append(files, filepath.Join(in.Cwd, ".claude", "settings.local.json"), filepath.Join(in.Cwd, ".claude", "settings.json"))
	}
	files = append(files, filepath.Join(paths.ClaudeDir(), "settings.json"))
	for _, f := range files {
		var st struct {
			Model string `json:"model"`
		}
		if fsutil.ReadJSON(f, &st) == nil {
			if t := router.TierOf(st.Model, cfg); t != "" {
				return t, "settings"
			}
		}
	}
	if t := router.TierOf(os.Getenv("ANTHROPIC_MODEL"), cfg); t != "" {
		return t, "env"
	}
	return "", ""
}

// Feature is one jevcc behaviour and whether it is active for a session.
type Feature struct {
	Name, Short string
	On          bool
	Note        string
}

// Features lists every behaviour with its effective state; shared by
// /jev status and the statusline so both say the same thing.
func Features(cfg *config.Config, in Input) []Feature {
	routing := Enabled(cfg, in.SessionID)
	main, _ := SessionTierSource(in, cfg)
	cheap := main != "" && main != "opus" && main != "fable"
	return []Feature{
		{"routing (skill, files, bulk, memory hints)", "jev", routing, "/jev on|off"},
		{"escalate to a stronger subagent", "↑", routing && cfg.EscalateOn() && cheap, fmt.Sprintf("needs a session cheaper than opus; confidence ≥ %.2f; at most %d per session", cfg.EscalateConfidence, cfg.EscalateMaxPerSession)},
		{"delegate down to a cheaper subagent", "↓", routing && cfg.Delegate, "off by default (measured +14% cost)"},
		{"review finished coding turns (Stop)", "review", routing && cfg.VerifyOn() && cheap, fmt.Sprintf("to %s at risk ≥ %.2f; at most %d per session", cfg.VerifyAgent, cfg.VerifyThreshold, cfg.VerifyMaxPerSession)},
		{"guard risky commands/writes (PreToolUse)", "🛡", cfg.GuardOn(), fmt.Sprintf("off by default (/jev guard on); asks you at risk ≥ %.2f; works even with routing off", cfg.GuardThreshold)},
		{"subagents may use MCP servers and skills", "mcp", cfg.AgentMCP, "off by default (/jev mcp on); applies to new sessions"},
		{"screen fetched content for injection (PostToolUse)", "inject", cfg.InjectionOn(), fmt.Sprintf("warns at p ≥ %.2f", cfg.InjectionThreshold)},
		{"likely-relevant files hint", "files", routing && cfg.FindFilesOn(), "in project folders up to 1500 files"},
		{"skill suggestion", "skill", routing && cfg.SkillAdviceOn(), fmt.Sprintf("fit ≥ %.2f", cfg.SkillFit)},
		{"remember lasting preferences", "remember", routing && cfg.MemoryThreshold > 0, fmt.Sprintf("p ≥ %.2f", cfg.MemoryThreshold)},
	}
}

// modelSwitch is what /model writes to the transcript, e.g.
// "<local-command-stdout>Set model to `Opus 5.5` and saved as ...".
const modelSwitch = "<local-command-stdout>Set model to "

// lastModel is the model of the newest assistant message, or the target of a
// later /model switch: that line lands before the next prompt's hook runs,
// while the first reply from the new model only comes after it.
func lastModel(transcript string) string {
	b, err := os.ReadFile(transcript)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(b), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(lines[i], modelSwitch) {
			var l struct {
				Type    string `json:"type"`
				Message struct {
					Content any `json:"content"`
				} `json:"message"`
			}
			if json.Unmarshal([]byte(lines[i]), &l) == nil && l.Type == "user" {
				if s, ok := l.Message.Content.(string); ok {
					if _, after, ok := strings.Cut(s, modelSwitch); ok {
						m, _, _ := strings.Cut(after, " and ")
						m, _, _ = strings.Cut(m, "</local-command-stdout>")
						return strings.Trim(m, "` ")
					}
				}
			}
			continue
		}
		if !strings.Contains(lines[i], `"assistant"`) {
			continue
		}
		var l struct {
			Type    string `json:"type"`
			Message struct {
				Model string `json:"model"`
			} `json:"message"`
		}
		if json.Unmarshal([]byte(lines[i]), &l) == nil && l.Type == "assistant" && l.Message.Model != "" && l.Message.Model != "<synthetic>" {
			return l.Message.Model
		}
	}
	return ""
}

// maxIndexed keeps the per-prompt file ranking to a few Jev calls.
const maxIndexed = 1500

// fileHint ranks the project's files for a code question and names the top
// ones, so Claude opens them first instead of searching widely.
func fileHint(ctx context.Context, in Input, d Deps) string {
	if !d.Config.FindFilesOn() || in.Cwd == "" || !find.IsProject(in.Cwd) {
		return ""
	}
	files := find.Index(in.Cwd)
	if len(files) < 3 || len(files) > maxIndexed {
		return ""
	}
	res, err := find.Files(ctx, d.Asker, in.Prompt, files, 3)
	if err != nil || res.Exists < 0.5 || len(res.Hits) == 0 || res.Hits[0].P < 0.2 {
		return ""
	}
	var parts []string
	for _, h := range res.Hits {
		parts = append(parts, fmt.Sprintf("%s (%.2f)", h.Path, h.P))
	}
	return "- Files: most likely relevant here: " + strings.Join(parts, ", ") + ". Open these first; search wider only if they do not have it."
}

// startWarm is Warm, replaced in tests: there os.Executable is the test
// binary, and a detached copy of it would keep running after the test.
var startWarm = Warm

// Warm starts `jevcc warm` in the background: one small Jev call, so a slow
// first response lands before the user's next prompt instead of timing it out.
func Warm() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(exe, "warm")
	detach(cmd)
	if err := cmd.Start(); err != nil {
		logx.Printf("hook: warm: %v", err)
		return
	}
	_ = cmd.Process.Release()
}

// postTool screens text a tool brought back for instructions aimed at the
// agent, and warns Claude when it finds some. Errors stay silent.
func postTool(in Input, cfg *config.Config) *Output {
	if !cfg.InjectionOn() || cfg.APIKey == "" || !inject.Screened(in.ToolName) {
		return nil
	}
	text := inject.Text(in.ToolResponse)
	if len(strings.TrimSpace(text)) < 40 {
		return nil
	}
	source := in.ToolName
	for _, k := range []string{"url", "file_path", "query"} {
		if v, ok := in.ToolInput[k].(string); ok {
			source += " " + v
			break
		}
	}
	timeout := 4 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	client := &jev.Client{Endpoint: cfg.Endpoint, Key: cfg.APIKey, Model: cfg.Model, HTTP: &http.Client{Timeout: timeout}}
	v, err := inject.Check(ctx, client, source, text)
	rec := map[string]any{"time": time.Now(), "session_id": in.SessionID, "tool": in.ToolName, "source": head(source)}
	defer func() {
		if err := journal.Append("injection", rec, cfg.KeepDays(), time.Now()); err != nil {
			logx.Printf("hook: log injection: %v", err)
		}
	}()
	if err != nil {
		rec["error"] = err.Error()
		return nil
	}
	rec["verdict"] = v
	if v.Risk < cfg.InjectionThreshold {
		return nil
	}
	rec["warned"] = true
	return &Output{Specific: &Specific{HookEventName: "PostToolUse", AdditionalContext: inject.Warning(in.ToolName, v)}}
}

// preTool asks Jev about a command or file write before it runs; a risky one
// becomes a confirmation prompt. Errors and timeouts let the call through.
func preTool(in Input, cfg *config.Config) *Output {
	if !cfg.GuardOn() || cfg.APIKey == "" || !guard.Guarded(in.ToolName) {
		return nil
	}
	timeout := 2500 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	client := &jev.Client{Endpoint: cfg.Endpoint, Key: cfg.APIKey, Model: cfg.Model, HTTP: &http.Client{Timeout: timeout}}
	a := guard.FromToolInput(in.ToolName, in.ToolInput, in.Cwd)
	v, err := guard.Check(ctx, client, a, cfg.GuardThreshold)
	rec := map[string]any{"time": time.Now(), "session_id": in.SessionID, "tool": in.ToolName, "command": head(a.Command), "file": a.File}
	defer func() {
		if err := journal.Append("guard", rec, cfg.KeepDays(), time.Now()); err != nil {
			logx.Printf("hook: log guard: %v", err)
		}
	}()
	if err != nil {
		rec["error"] = err.Error()
		return nil
	}
	rec["verdict"] = v
	if len(v.Reasons) == 0 {
		return nil
	}
	rec["asked"] = true
	return &Output{Specific: &Specific{HookEventName: "PreToolUse", PermissionDecision: "ask", PermissionDecisionReason: guard.Reason(v)}}
}

// skipVerify records why a finished turn was not reviewed, so a review that
// should have happened can be told apart from one that never applied. Written
// only with tracing on: a line per turn is too much for the default logs.
func skipVerify(in Input, cfg *config.Config, reason string) {
	if !cfg.TraceOn() {
		return
	}
	// Each installed scope runs this hook for the same Stop event. The
	// transcript grows every turn, so its size identifies the event.
	size := int64(0)
	if fi, err := os.Stat(in.TranscriptPath); err == nil {
		size = fi.Size()
	}
	if !First(Input{SessionID: in.SessionID, Prompt: fmt.Sprintf("skip\x00%s\x00%d", reason, size)}) {
		return
	}
	turn, _ := verify.ReadTurn(in.TranscriptPath)
	rec := map[string]any{"time": time.Now(), "session_id": in.SessionID, "skipped": reason, "request_head": head(turn.Request)}
	if err := journal.Append("verify", rec, cfg.KeepDays(), time.Now()); err != nil {
		logx.Printf("hook: log verify skip: %v", err)
	}
}

// stop is the Stop-event check: with verify on, Jev reviews the finished turn
// and, if it looks risky, asks Claude to escalate once before stopping.
func stop(in Input, cfg *config.Config) *Output {
	if !cfg.VerifyOn() || in.StopHookActive || cfg.APIKey == "" || !Enabled(cfg, in.SessionID) {
		return nil
	}
	// Reviewing only pays when the session is cheaper than the reviewer.
	if main := SessionTier(in, cfg); main == "" || tierRank(main, cfg) >= tierRank(cfg.VerifyTier, cfg) {
		skipVerify(in, cfg, "main_not_cheaper")
		return nil
	}
	turn, _ := verify.ReadTurn(in.TranscriptPath)
	if in.LastAssistantMessage != "" {
		turn.Final = in.LastAssistantMessage
	}
	if turn.Request == "" || turn.Final == "" {
		logx.Printf("hook: verify: no transcript turn for %s", in.SessionID)
		skipVerify(in, cfg, "no_turn")
		return nil
	}
	if strings.HasPrefix(strings.TrimSpace(turn.Request), "/") || router.IsSystemMessage(turn.Request) {
		skipVerify(in, cfg, "slash")
		return nil
	}
	// Only finished coding work is reviewed: not investigation or answers,
	// not turns that already handed the work to a subagent, and at most
	// VerifyMaxPerSession escalations per session.
	if !turn.EditedCode() {
		skipVerify(in, cfg, "not_code")
		return nil
	}
	if turn.HandedOff() {
		skipVerify(in, cfg, "handed_off")
		return nil
	}
	if reviewEscalations(in.SessionID) >= cfg.VerifyMaxPerSession {
		skipVerify(in, cfg, "cap")
		return nil
	}
	if !First(Input{SessionID: in.SessionID, Prompt: "stop\x00" + turn.Request + turn.Final}) {
		return nil
	}
	timeout := time.Duration(cfg.HookTimeoutMS) * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	client := &jev.Client{Endpoint: cfg.Endpoint, Key: cfg.APIKey, Model: cfg.Model, HTTP: &http.Client{Timeout: timeout}}
	v, err := verify.Check(ctx, client, turn, cfg.VerifyThreshold)
	rec := map[string]any{"time": time.Now(), "session_id": in.SessionID, "request_head": head(turn.Request)}
	defer func() {
		if err := journal.Append("verify", rec, cfg.KeepDays(), time.Now()); err != nil {
			logx.Printf("hook: log verify: %v", err)
		}
	}()
	if err != nil {
		rec["error"] = err.Error()
		logx.Printf("hook: verify: %v", err)
		return nil
	}
	rec["verdict"] = v
	if v.Risk < cfg.VerifyThreshold {
		return nil
	}
	rec["escalated"] = true
	return &Output{Decision: "block", Reason: verify.Escalation(v, cfg.VerifyAgent)}
}

// escalations counts the routing escalations already advised in a session.
func escalations(session string) int {
	recs, _ := stats.LoadDecisions()
	n := 0
	for _, r := range recs {
		if r.SessionID != session {
			continue
		}
		for _, a := range r.Advice {
			if a == "escalate" {
				n++
			}
		}
	}
	return n
}

func reviewEscalations(session string) int {
	n := 0
	needle := `"session_id":"` + session + `"`
	for _, f := range journal.Files("verify") {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, l := range strings.Split(string(b), "\n") {
			if strings.Contains(l, needle) && strings.Contains(l, `"escalated":true`) {
				n++
			}
		}
	}
	return n
}

// First reports whether this is the first jevcc hook run for this prompt, so
// that installing in several scopes (user + project) routes a prompt once.
func First(in Input) bool {
	dir := filepath.Join(paths.StateDir(), "once")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return true
	}
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if fi, err := e.Info(); err == nil && time.Since(fi.ModTime()) > time.Hour {
				os.Remove(filepath.Join(dir, e.Name()))
			}
		}
	}
	sum := sha256.Sum256([]byte(in.SessionID + "\x00" + in.Prompt))
	f, err := os.OpenFile(filepath.Join(dir, hex.EncodeToString(sum[:12])), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return !os.IsExist(err)
	}
	f.Close()
	return true
}

func head(s string) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > 200 {
		r = r[:200]
	}
	return string(r)
}

// Main is `jevcc hook`. It never returns non-zero and never panics:
// a broken hook must not break claude.
func Main(stdin io.Reader, stdout io.Writer) (code int) {
	defer func() {
		if r := recover(); r != nil {
			logx.Printf("hook panic: %v", r)
			code = 0
		}
	}()
	var in Input
	if err := json.NewDecoder(stdin).Decode(&in); err != nil {
		logx.Printf("hook: bad input: %v", err)
		return 0
	}
	cfg, err := config.Load()
	if err != nil {
		logx.Printf("hook: %v", err)
		return 0
	}
	if in.HookEventName == "PostToolUse" {
		if out := postTool(in, cfg); out != nil {
			if err := json.NewEncoder(stdout).Encode(out); err != nil {
				logx.Printf("hook: write output: %v", err)
			}
		}
		return 0
	}
	if in.HookEventName == "PreToolUse" {
		if out := preTool(in, cfg); out != nil {
			if err := json.NewEncoder(stdout).Encode(out); err != nil {
				logx.Printf("hook: write output: %v", err)
			}
		}
		return 0
	}
	if in.HookEventName == "Stop" {
		if out := stop(in, cfg); out != nil {
			if err := json.NewEncoder(stdout).Encode(out); err != nil {
				logx.Printf("hook: write output: %v", err)
			}
		}
		return 0
	}
	out, ok := Command(in, cfg)
	if !ok {
		p := strings.TrimSpace(in.Prompt)
		if p == "" || strings.HasPrefix(p, "/") || router.IsSystemMessage(p) || !Enabled(cfg, in.SessionID) {
			return 0
		}
		if cfg.APIKey == "" {
			logx.Printf("hook: routing on but no API key")
			return 0
		}
		if !First(in) {
			return 0 // the same hook is installed in more than one scope
		}
		timeout := time.Duration(cfg.HookTimeoutMS) * time.Millisecond
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		client := &jev.Client{Endpoint: cfg.Endpoint, Key: cfg.APIKey, Model: cfg.Model, HTTP: &http.Client{Timeout: timeout}}
		var rec stats.Record
		out, rec = Route(ctx, in, Deps{
			Config: cfg,
			Asker:  client,
			Skills: func(cwd string) []roster.Skill { return roster.Discover(paths.ClaudeDir(), cwd) },
			Now:    time.Now,
		})
		if err := stats.Append(rec, cfg.KeepDays()); err != nil {
			logx.Printf("hook: log decision: %v", err)
		}
		if rec.Error != "" {
			logx.Printf("hook: route: %s", rec.Error)
		}
	}
	if out == nil {
		return 0
	}
	if err := json.NewEncoder(stdout).Encode(out); err != nil {
		logx.Printf("hook: write output: %v", err)
	}
	return 0
}
