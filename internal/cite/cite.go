// Package cite checks citations against their sources, after TypeSafe's
// "Double-checking citations" cookbook: a plain string match finds quotes that
// are not in the source at all, then one Choice reads the quote's context and
// decides whether it supports the claim.
package cite

import (
	"context"
	"os"
	"regexp"
	"strings"

	"github.com/zuexx/agent-jev-integration/internal/jev"
)

type Asker interface {
	Ask(ctx context.Context, state any, qs map[string]jev.Question) (*jev.Response, error)
}

type Claim struct {
	Claim  string `json:"claim"`
	Quote  string `json:"quote"`
	Source string `json:"source"` // file path of the source text
}

const (
	Verified     = "verified"
	Contradicted = "contradicted"
	Unsupported  = "unsupported"
	Fabricated   = "fabricated"
	Review       = "needs review"
	contextChars = 700
)

type Result struct {
	Claim      Claim   `json:"claim"`
	Verdict    string  `json:"verdict"`
	Jev        string  `json:"jev_choice,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
	Error      string  `json:"error,omitempty"`
}

var space = regexp.MustCompile(`\s+`)

func norm(s string) string {
	s = strings.NewReplacer("“", `"`, "”", `"`, "‘", "'", "’", "'").Replace(s)
	return strings.ToLower(strings.TrimSpace(space.ReplaceAllString(s, " ")))
}

// Locate returns the context around quote in source, or "" if it is not there.
func Locate(source, quote string) string {
	ns, nq := norm(source), norm(quote)
	if nq == "" {
		return ""
	}
	i := strings.Index(ns, nq)
	if i < 0 {
		return ""
	}
	from := max(0, i-contextChars)
	to := min(len(ns), i+len(nq)+contextChars)
	return ns[from:to]
}

// Check verifies each claim. minConf sends unsure verdicts to review.
func Check(ctx context.Context, c Asker, claims []Claim, minConf float64, workers int) []Result {
	out := make([]Result, len(claims))
	cache := map[string]string{}
	for _, cl := range claims {
		if _, ok := cache[cl.Source]; !ok {
			b, _ := os.ReadFile(cl.Source)
			cache[cl.Source] = string(b)
		}
	}
	jev.Parallel(len(claims), workers, func(i int) {
		cl := claims[i]
		r := Result{Claim: cl}
		src := cache[cl.Source]
		if src == "" {
			r.Verdict, r.Error = Review, "source not readable"
			out[i] = r
			return
		}
		section := Locate(src, cl.Quote)
		if section == "" {
			r.Verdict = Fabricated
			out[i] = r
			return
		}
		res, err := c.Ask(ctx, map[string]string{"claim": cl.Claim, "quote": cl.Quote, "section": section}, map[string]jev.Question{
			"relation": jev.Choice("How does `section` of the source (which contains `quote`) relate to `claim`?", map[string]any{
				"supports":     "The section states what the claim says, or clearly implies it.",
				"contradicts":  "The section says the opposite of the claim, or something incompatible with it.",
				"says_nothing": "The section is about something else and neither supports nor contradicts the claim.",
			}),
		})
		if err != nil {
			r.Verdict, r.Error = Review, err.Error()
			out[i] = r
			return
		}
		a := res.Answers["relation"]
		r.Jev, r.Confidence = a.Choice, a.Confidence
		switch {
		case a.Confidence < minConf:
			r.Verdict = Review
		case a.Choice == "supports":
			r.Verdict = Verified
		case a.Choice == "contradicts":
			r.Verdict = Contradicted
		default:
			r.Verdict = Unsupported
		}
		out[i] = r
	})
	return out
}
