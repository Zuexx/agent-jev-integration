// Package audit lists routing decisions that look wrong, so a person can check
// them. It flags candidates; a flag is a reason to look, not a verdict.
package audit

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/zuexx/agent-jev-integration/internal/config"
	"github.com/zuexx/agent-jev-integration/internal/stats"
)

// Kinds of case, in report order.
const (
	MissedEscalation   = "missed escalation"
	AdvisedNotFollowed = "advised, not followed"
	CorrectedNext      = "next prompt says it failed"
	NearTie            = "near tie"
)

var kinds = []string{MissedEscalation, AdvisedNotFollowed, CorrectedNext, NearTie}

// nearTie is the probability gap under which the top two tiers are a tie.
const nearTie = 0.10

// followWindow is how long after a decision a correction still counts.
const followWindow = 30 * time.Minute

// corrections are phrases that mean the previous answer did not work. They are
// matched case-insensitively, so a question that happens to contain one is a
// candidate too; that is why the case is only flagged.
var corrections = []string{
	"还是不行", "還是不行", "还是不对", "還是不對", "不对", "不對", "错了", "錯了",
	"没用", "沒用", "不行", "still not", "still broken", "still wrong", "still failing",
	"doesn't work", "does not work", "didn't work", "did not work", "revert",
}

// Case is one decision flagged for review.
type Case struct {
	Kind string
	Rec  stats.Record
	Note string
}

// Result is what Find produced for a window of decisions.
type Result struct {
	Checked    int // decisions in the window
	NoMainTier int // decisions recorded before the main tier was logged
	Cases      []Case
	ByKind     map[string]int
}

// Find flags the decisions in recs (oldest first) that are not before since.
// cfg supplies the tier order and thresholds in effect now; the advice in each
// record was produced with the config at the time, so a changed threshold can
// make an old record look wrong. Read the report with that in mind.
func Find(recs []stats.Record, cfg *config.Config, since time.Time) Result {
	res := Result{ByKind: map[string]int{}}
	names := cfg.TierNames()
	rank := map[string]int{}
	for i, n := range names {
		rank[n] = i
	}
	var window []stats.Record
	for _, r := range recs {
		if r.Time.Before(since) || r.Decision == nil || r.Error != "" {
			continue
		}
		window = append(window, r)
	}
	res.Checked = len(window)
	for i, r := range window {
		d := r.Decision
		if r.MainTier == "" {
			res.NoMainTier++
		} else if cfg.EscalateOn() && rank[d.Tier] > rank[r.MainTier] && d.TierConf >= cfg.EscalateConfidence && !hasAdvice(r, "escalate", "escalate_capped") {
			res.add(MissedEscalation, r, fmt.Sprintf("main %s, tier %s p=%.2f, no escalate advice", r.MainTier, d.Tier, d.TierProbs[d.Tier]))
		}
		if hasAdvice(r, "escalate") && r.Transcript != "" {
			if delegated, _, found := stats.Followed(r.Transcript, r.PromptHead, d.Tier, ""); found && !delegated {
				res.add(AdvisedNotFollowed, r, fmt.Sprintf("advised jev-%s, no Agent call for it in the turn", d.Tier))
			}
		}
		if next, ok := nextInSession(window, i); ok {
			head := strings.ToLower(next.PromptHead)
			for _, c := range corrections {
				if strings.Contains(head, c) {
					res.add(CorrectedNext, r, fmt.Sprintf("next prompt: %q", clip(next.PromptHead, 60)))
					break
				}
			}
		}
		if gap, ok := topGap(d.TierProbs); ok && gap < nearTie {
			res.add(NearTie, r, fmt.Sprintf("top two tiers differ by %.2f", gap))
		}
	}
	return res
}

func (res *Result) add(kind string, r stats.Record, note string) {
	res.Cases = append(res.Cases, Case{Kind: kind, Rec: r, Note: note})
	res.ByKind[kind]++
}

func hasAdvice(r stats.Record, want ...string) bool {
	for _, a := range r.Advice {
		for _, w := range want {
			if a == w {
				return true
			}
		}
	}
	return false
}

// nextInSession returns the decision after window[i] in the same session, if
// it came within followWindow.
func nextInSession(window []stats.Record, i int) (stats.Record, bool) {
	for j := i + 1; j < len(window); j++ {
		if window[j].SessionID != window[i].SessionID {
			continue
		}
		if window[j].Time.Sub(window[i].Time) > followWindow {
			return stats.Record{}, false
		}
		return window[j], true
	}
	return stats.Record{}, false
}

// topGap is the difference between the two most likely tiers.
func topGap(probs map[string]float64) (float64, bool) {
	if len(probs) < 2 {
		return 0, false
	}
	vals := make([]float64, 0, len(probs))
	for _, p := range probs {
		vals = append(vals, p)
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(vals)))
	return vals[0] - vals[1], true
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// Report renders the result, showing up to limit examples of each kind.
func Report(res Result, since time.Time, limit int) string {
	var b strings.Builder
	total := len(res.Cases)
	fmt.Fprintf(&b, "jevcc audit: %d decisions since %s, %d to check\n", res.Checked, since.Format("2006-01-02"), total)
	if res.NoMainTier > 0 {
		fmt.Fprintf(&b, "not checked: %d decisions from before the main tier was logged\n", res.NoMainTier)
	}
	for _, kind := range kinds {
		n := res.ByKind[kind]
		fmt.Fprintf(&b, "\n%s (%d)\n", kind, n)
		shown := 0
		for _, c := range res.Cases {
			if c.Kind != kind {
				continue
			}
			if shown == limit {
				fmt.Fprintf(&b, "  … %d more\n", n-shown)
				break
			}
			shown++
			fmt.Fprintf(&b, "  %s  session %s  %s\n", c.Rec.Time.Local().Format("01-02 15:04"), short(c.Rec.SessionID), c.Note)
			fmt.Fprintf(&b, "      %q\n", clip(c.Rec.PromptHead, 80))
		}
	}
	if total == 0 {
		b.WriteString("\nnothing flagged\n")
	}
	b.WriteString("\nA flag is a reason to look at the case, not a verdict. Config changes since a decision can make it look wrong.\n")
	return b.String()
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
