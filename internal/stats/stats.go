// Package stats records routing decisions and summarises them, including
// whether Claude actually followed the advice (read from session transcripts).
package stats

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/zuexx/agent-jev-integration/internal/journal"
	"github.com/zuexx/agent-jev-integration/internal/router"
)

// Schema is the version of Record. Version 2 adds the fields from MainTier on.
const Schema = 2

// Record is one line of ~/.jevcc/decisions-YYYY-MM-DD.jsonl.
type Record struct {
	Schema     int              `json:"schema,omitempty"`
	Time       time.Time        `json:"time"`
	SessionID  string           `json:"session_id"`
	Transcript string           `json:"transcript_path,omitempty"`
	PromptHead string           `json:"prompt_head"`
	Decision   *router.Decision `json:"decision,omitempty"`
	// Advice lists what the hook told Claude: "tier", "escalate",
	// "escalate_capped", "skill", "memory", "bulk", "files".
	Advice []string `json:"advice,omitempty"`
	// MainTier is the tier the session ran on when the prompt arrived, and
	// MainTierSource says where that came from ("config", "this session",
	// "settings", "env"). Empty means it could not be told.
	MainTier        string `json:"main_tier,omitempty"`
	MainTierSource  string `json:"main_tier_source,omitempty"`
	EscalationsUsed int    `json:"escalations_used,omitempty"`
	ContextChars    int    `json:"context_chars"`
	Error           string `json:"error,omitempty"`
	HookMS          int64  `json:"hook_ms"`
	// Trace is written only when tracing is on (config "trace" or JEVCC_TRACE=1).
	Trace *Trace `json:"trace,omitempty"`
}

// Trace is the full input to a decision: the text Claude saw and the
// thresholds it was judged against.
type Trace struct {
	Context      string  `json:"context"`
	MinConf      float64 `json:"min_conf"`
	EscalateConf float64 `json:"escalate_conf"`
	SkillFit     float64 `json:"skill_fit"`
	Memory       float64 `json:"memory"`
}

// Append writes r to today's decisions file and prunes days past keepDays.
func Append(r Record, keepDays int) error {
	return journal.Append("decisions", r, keepDays, time.Now())
}

// LoadDecisions reads every decisions file that is kept, oldest first.
func LoadDecisions() ([]Record, error) { return loadDecisions(journal.Files("decisions")) }

// LoadDecisionsIn reads the decisions logs of another state directory.
func LoadDecisionsIn(dir string) ([]Record, error) {
	return loadDecisions(journal.FilesIn(dir, "decisions"))
}

func loadDecisions(files []string) ([]Record, error) {
	var out []Record
	for _, f := range files {
		recs, err := Load(f)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return out, err
		}
		out = append(out, recs...)
	}
	return out, nil
}

func Load(path string) ([]Record, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var r Record
		if json.Unmarshal(sc.Bytes(), &r) == nil {
			out = append(out, r)
		}
	}
	return out, sc.Err()
}

// Followed reports, for one recorded prompt, whether the turn that answered
// it delegated to a subagent with the given model and whether it loaded the
// given skill. found is false when the prompt is not in the transcript.
func Followed(transcript, promptHead, tier, skill string) (delegated, loaded, found bool) {
	f, err := os.Open(transcript)
	if err != nil {
		return false, false, false
	}
	defer f.Close()
	needle := strings.TrimSpace(firstRunes(promptHead, 80))
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<26)
	in := false
	for sc.Scan() {
		var line struct {
			Type    string `json:"type"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &line) != nil {
			continue
		}
		switch line.Type {
		case "user":
			text, isPrompt := userText(line.Message.Content)
			if !isPrompt {
				continue
			}
			if in { // next real prompt: the turn is over
				return delegated, loaded, true
			}
			if needle != "" && strings.Contains(text, needle) {
				in, found = true, true
			}
		case "assistant":
			if !in {
				continue
			}
			var blocks []struct {
				Type  string         `json:"type"`
				Name  string         `json:"name"`
				Input map[string]any `json:"input"`
			}
			_ = json.Unmarshal(line.Message.Content, &blocks)
			for _, b := range blocks {
				if b.Type != "tool_use" {
					continue
				}
				// delegate_via "agent" sends subagent_type "jev-<tier>";
				// delegate_via "model" sends model "<tier>".
				if (b.Name == "Agent" || b.Name == "Task") && tier != "" &&
					(b.Input["model"] == tier || b.Input["subagent_type"] == "jev-"+tier) {
					delegated = true
				}
				if b.Name == "Skill" && skill != "" {
					if s, _ := b.Input["skill"].(string); s == skill || strings.HasSuffix(skill, ":"+s) || strings.HasSuffix(s, ":"+skill) {
						loaded = true
					}
				}
			}
		}
	}
	return delegated, loaded, found
}

// userText returns the text of a user line and whether it is a typed prompt
// (not a tool result).
func userText(raw json.RawMessage) (string, bool) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, true
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return "", false
	}
	var b strings.Builder
	prompt := false
	for _, x := range blocks {
		if x.Type == "text" {
			prompt = true
			b.WriteString(x.Text)
		}
	}
	return b.String(), prompt
}

func firstRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

func pct(k, n int) string {
	if n == 0 {
		return "-"
	}
	return fmt.Sprintf("%.0f%% (%d/%d)", 100*float64(k)/float64(n), k, n)
}

// Percentile returns the p-th percentile (0..100) of xs by nearest rank.
func Percentile(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return math.NaN()
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	i := int(math.Ceil(p/100*float64(len(s)))) - 1
	return s[max(0, min(i, len(s)-1))]
}

// Summary renders the /jev stats report. since filters records (zero = all).
func Summary(recs []Record, since time.Time) string {
	var b strings.Builder
	sessions := map[string]bool{}
	tiers := map[string]int{}
	var lat []float64
	var cost float64
	var n, errs, ctxChars, adviceTier, adviceSkill, adviceBulk int
	var checked, delegated, skillChecked, skillLoaded int
	for _, r := range recs {
		if r.Time.Before(since) {
			continue
		}
		n++
		sessions[r.SessionID] = true
		if r.Error != "" || r.Decision == nil {
			errs++
			continue
		}
		d := r.Decision
		tiers[d.Tier]++
		lat = append(lat, float64(d.LatencyMS))
		cost += d.CostUSD
		ctxChars += r.ContextChars
		var adv = map[string]bool{}
		for _, a := range r.Advice {
			adv[a] = true
		}
		if adv["bulk"] {
			adviceBulk++
		}
		// "escalate" is the default delegation advice (see router.Context),
		// so it counts as a tier suggestion here.
		delegateAdv := adv["tier"] || adv["escalate"]
		if !delegateAdv && !adv["skill"] {
			continue
		}
		if delegateAdv {
			adviceTier++
		}
		if adv["skill"] {
			adviceSkill++
		}
		tier, skill := "", ""
		if delegateAdv {
			tier = d.Tier
		}
		if adv["skill"] {
			skill = d.Skill
		}
		del, load, found := Followed(r.Transcript, r.PromptHead, tier, skill)
		if !found {
			continue
		}
		if delegateAdv {
			checked++
			if del {
				delegated++
			}
		}
		if adv["skill"] {
			skillChecked++
			if load {
				skillLoaded++
			}
		}
	}
	if n == 0 {
		return "jevcc: no routing decisions recorded yet. Turn routing on with /jev on and send a few prompts."
	}
	fmt.Fprintf(&b, "jev routing: %d prompts in %d sessions\n", n, len(sessions))
	names := make([]string, 0, len(tiers))
	for k := range tiers {
		names = append(names, k)
	}
	sort.Strings(names)
	var parts []string
	for _, k := range names {
		parts = append(parts, fmt.Sprintf("%s %s", k, pct(tiers[k], n-errs)))
	}
	fmt.Fprintf(&b, "  tier picked:      %s\n", strings.Join(parts, ", "))
	fmt.Fprintf(&b, "  advice given:     delegate %d, skill %d, bulk→classify %d (silent on %d)\n", adviceTier, adviceSkill, adviceBulk, n-errs-countAny(recs, since))
	if len(lat) > 0 {
		fmt.Fprintf(&b, "  jev latency:      p50 %.0f ms, p95 %.0f ms (per prompt, both calls)\n", Percentile(lat, 50), Percentile(lat, 95))
	}
	fmt.Fprintf(&b, "  jev cost:         $%.5f total, $%.6f per prompt\n", cost, cost/float64(max(1, n-errs)))
	fmt.Fprintf(&b, "  added context:    %d chars total (~%d tokens) sent to the main model\n", ctxChars, ctxChars/4)
	fmt.Fprintf(&b, "  errors/timeouts:  %d (the prompt went through without advice)\n", errs)
	fmt.Fprintf(&b, "  Claude followed:  delegation %s, skill %s (turns found in transcripts)\n", pct(delegated, checked), pct(skillLoaded, skillChecked))
	b.WriteString("This is what Jev decided and what Claude did with it. For cost/quality impact, run `jevcc bench`.")
	return b.String()
}

func countAny(recs []Record, since time.Time) int {
	k := 0
	for _, r := range recs {
		if !r.Time.Before(since) && r.Error == "" && len(r.Advice) > 0 {
			k++
		}
	}
	return k
}
