// Package statusline implements `jevcc statusline`: it runs the statusline
// command it wraps (claude-dashboard, cswitch, …) and adds one line with what
// Jev did in this session.
package statusline

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/zuexx/agent-jev-integration/internal/config"
	"github.com/zuexx/agent-jev-integration/internal/hook"
	"github.com/zuexx/agent-jev-integration/internal/journal"
	"github.com/zuexx/agent-jev-integration/internal/logx"
	"github.com/zuexx/agent-jev-integration/internal/platform"
)

type Input struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	Model          struct {
		ID string `json:"id"`
	} `json:"model"`
}

// Session counts what jevcc did in one session.
type Session struct {
	Prompts, Escalated, Delegated, Skill, Files, Memory, Bulk, Errors int
	Capped                                                            int
	EscalatedTo                                                       map[string]int
	GuardChecked, GuardAsked                                          int
	Reviewed, ReviewEscalated                                         int
	Screened, Warned                                                  int
	CostUSD                                                           float64
}

const tailBytes = 512 << 10

// readTail returns the complete lines in the last tailBytes of a file.
// journalTail returns the recent lines of a daily log: the tail of the newest
// file, and of the one before it when that exists.
func journalTail(kind string) []string {
	files := journal.Files(kind)
	var lines []string
	for i := len(files) - 1; i >= 0 && i >= len(files)-2; i-- {
		lines = append(readTail(files[i]), lines...)
	}
	return lines
}

func readTail(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	fi, _ := f.Stat()
	off := int64(0)
	if fi != nil && fi.Size() > tailBytes {
		off = fi.Size() - tailBytes
	}
	b, _ := io.ReadAll(io.NewSectionReader(f, off, tailBytes+1))
	lines := strings.Split(string(b), "\n")
	if off > 0 && len(lines) > 0 {
		lines = lines[1:] // first line may be cut
	}
	return lines
}

// Count reads the jevcc logs for one session.
func Count(session string) Session {
	var s Session
	if session == "" {
		return s
	}
	needle := `"session_id":"` + session + `"`
	for _, l := range journalTail("decisions") {
		if !strings.Contains(l, needle) {
			continue
		}
		var r struct {
			Advice   []string `json:"advice"`
			Error    string   `json:"error"`
			Decision *struct {
				Tier string  `json:"tier"`
				Cost float64 `json:"jev_cost_usd"`
			} `json:"decision"`
		}
		if json.Unmarshal([]byte(l), &r) != nil {
			continue
		}
		s.Prompts++
		if r.Error != "" {
			s.Errors++
		}
		if r.Decision != nil {
			s.CostUSD += r.Decision.Cost
		}
		for _, a := range r.Advice {
			switch a {
			case "escalate":
				s.Escalated++
				if s.EscalatedTo == nil {
					s.EscalatedTo = map[string]int{}
				}
				if r.Decision != nil {
					s.EscalatedTo[r.Decision.Tier]++
				}
			case "escalate_capped":
				s.Capped++
			case "tier":
				s.Delegated++
			case "skill":
				s.Skill++
			case "files":
				s.Files++
			case "memory":
				s.Memory++
			case "bulk":
				s.Bulk++
			}
		}
	}
	for _, l := range journalTail("guard") {
		if strings.Contains(l, needle) {
			s.GuardChecked++
			if strings.Contains(l, `"asked":true`) {
				s.GuardAsked++
			}
			s.CostUSD += field(l, "jev_cost_usd")
		}
	}
	for _, l := range journalTail("verify") {
		// "skipped" lines (trace only) record a review that did not run.
		if strings.Contains(l, needle) && !strings.Contains(l, `"skipped":`) {
			s.Reviewed++
			if strings.Contains(l, `"escalated":true`) {
				s.ReviewEscalated++
			}
			s.CostUSD += field(l, "jev_cost_usd")
		}
	}
	for _, l := range journalTail("injection") {
		if strings.Contains(l, needle) {
			s.Screened++
			if strings.Contains(l, `"warned":true`) {
				s.Warned++
			}
			s.CostUSD += field(l, "jev_cost_usd")
		}
	}
	return s
}

func field(line, name string) float64 {
	i := strings.Index(line, `"`+name+`":`)
	if i < 0 {
		return 0
	}
	var v float64
	fmt.Sscan(strings.TrimLeft(line[i+len(name)+3:], " "), &v)
	return v
}

// Settings renders which behaviours are active, e.g.
// "⚙ jev on · haiku (this session) · ↑✓ ↓✗ review✓ 🛡✓ inject✓ files✓ skill✓ remember✓".
func Settings(cfg *config.Config, in Input) string {
	hin := hook.Input{SessionID: in.SessionID, TranscriptPath: in.TranscriptPath, Cwd: in.Cwd}
	fs := hook.Features(cfg, hin)
	state := "off"
	if fs[0].On {
		state = "on"
	}
	main, src := hook.SessionTierSource(hin, cfg)
	if main == "" {
		main, src = "?", "unknown"
	}
	var flags []string
	for _, f := range fs[1:] {
		mark := "✗"
		if f.On {
			mark = "✓"
		}
		flags = append(flags, f.Short+mark)
	}
	return fmt.Sprintf("⚙ jev %s · %s (%s) · %s", state, main, src, strings.Join(flags, " "))
}

// Activity renders what jevcc did in this session ("" when nothing yet).
func Activity(s Session) string {
	var parts []string
	add := func(n int, f string) {
		if n > 0 {
			parts = append(parts, fmt.Sprintf(f, n))
		}
	}
	add(s.Prompts, "%d prompts")
	for _, t := range []string{"fable", "opus", "sonnet"} {
		add(s.EscalatedTo[t], "↑"+t+" %d")
	}
	add(s.Capped, "↑capped %d")
	add(s.Delegated, "↓cheaper %d")
	add(s.Skill, "skill %d")
	add(s.Files, "files %d")
	add(s.Memory, "remember %d")
	add(s.Bulk, "bulk %d")
	if s.Reviewed > 0 {
		parts = append(parts, fmt.Sprintf("review %d (→jev-review %d)", s.Reviewed, s.ReviewEscalated))
	}
	if s.GuardChecked > 0 {
		parts = append(parts, fmt.Sprintf("🛡 %d/%d asked", s.GuardAsked, s.GuardChecked))
	}
	if s.Screened > 0 {
		parts = append(parts, fmt.Sprintf("inject %d/%d warned", s.Warned, s.Screened))
	}
	add(s.Errors, "⚠ %d timeouts")
	if s.CostUSD > 0 {
		parts = append(parts, fmt.Sprintf("jev $%.4f", s.CostUSD))
	}
	if len(parts) == 0 {
		return ""
	}
	return "📊 " + strings.Join(parts, " · ")
}

// Line renders the compact single line.
func Line(cfg *config.Config, in Input, s Session) string {
	if cfg.APIKey == "" {
		return "🧭 jev: no TypeSafe key (run `jevcc key`)"
	}
	hin := hook.Input{SessionID: in.SessionID, TranscriptPath: in.TranscriptPath, Cwd: in.Cwd}
	if !hook.Enabled(cfg, in.SessionID) {
		g := ""
		if cfg.GuardOn() && s.GuardAsked > 0 {
			g = fmt.Sprintf(" · 🛡 %d asked", s.GuardAsked)
		}
		return "🧭 jev off (/jev on)" + g
	}
	main := hook.SessionTier(hin, cfg)
	if main == "" {
		main = "?"
	}
	line := "🧭 jev on · " + main
	if a := Activity(s); a != "" {
		line += " · " + strings.TrimPrefix(a, "📊 ")
	}
	return line
}

// Render returns the lines for the configured mode (none for "off").
func Render(cfg *config.Config, in Input, s Session) []string {
	switch {
	case cfg.Statusline == "off":
		return nil
	case cfg.APIKey == "":
		return []string{"🧭 jev: no TypeSafe key (run `jevcc key`)"}
	case cfg.Statusline == "compact":
		return []string{Line(cfg, in, s)}
	}
	out := []string{Settings(cfg, in)}
	if a := Activity(s); a != "" {
		out = append(out, a)
	}
	return out
}

// Main is `jevcc statusline`. It never fails: the wrapped line is printed
// even when the jev line cannot be built.
func Main(stdin io.Reader, stdout io.Writer) (code int) {
	defer func() {
		if r := recover(); r != nil {
			logx.Printf("statusline panic: %v", r)
			code = 0
		}
	}()
	raw, _ := io.ReadAll(stdin)
	cfg, err := config.Load()
	if err != nil {
		logx.Printf("statusline: %v", err)
		return 0
	}
	if cfg.StatuslineInner != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := platform.ShellCmd(ctx, cfg.StatuslineInner)
		cmd.Stdin = bytes.NewReader(raw)
		out, err := cmd.Output()
		if err != nil {
			logx.Printf("statusline inner: %v", err)
		}
		out = bytes.TrimRight(out, "\r\n")
		if len(out) > 0 {
			stdout.Write(out)
			stdout.Write([]byte("\n"))
		}
	}
	var in Input
	_ = json.Unmarshal(raw, &in)
	if cfg.Statusline == "off" {
		return 0
	}
	for _, l := range Render(cfg, in, Count(in.SessionID)) {
		fmt.Fprintln(stdout, l)
	}
	return 0
}
