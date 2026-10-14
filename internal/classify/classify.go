// Package classify runs the same Jev questions over every row of a dataset:
// "Jev does the coarse pass, Claude only handles what it surfaces".
package classify

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zuexx/agent-jev-integration/internal/jev"
)

type Asker interface {
	Ask(ctx context.Context, state any, qs map[string]jev.Question) (*jev.Response, error)
}

type Options struct {
	Input     string
	Questions map[string]jev.Question
	// Field, if set, sends only that column/key as the state; otherwise the whole row.
	Field   string
	Workers int
	// Limit > 0 processes only that many rows, chosen at random with Seed (a calibration sample).
	Limit int
	Seed  int64
	// MinConfidence marks choice/score answers below it as needs_review.
	MinConfidence float64
	Output        string // .jsonl or .csv
}

type Result struct {
	Index       int                   `json:"index"`
	Row         map[string]any        `json:"row"`
	Answers     map[string]jev.Answer `json:"answers,omitempty"`
	NeedsReview bool                  `json:"needs_review"`
	Error       string                `json:"error,omitempty"`
}

type Summary struct {
	Rows        int
	Failed      int
	NeedsReview int
	Tokens      int
	CostUSD     float64
	Wall        time.Duration
	Choices     map[string]map[string]int
}

// Load reads .jsonl, .json (array) or .csv into rows.
func Load(path string) ([]map[string]any, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	switch strings.ToLower(filepath.Ext(path)) {
	case ".csv":
		r := csv.NewReader(f)
		r.FieldsPerRecord = -1
		recs, err := r.ReadAll()
		if err != nil {
			return nil, err
		}
		if len(recs) == 0 {
			return nil, nil
		}
		var rows []map[string]any
		for _, rec := range recs[1:] {
			row := map[string]any{}
			for i, h := range recs[0] {
				if i < len(rec) {
					row[h] = rec[i]
				}
			}
			rows = append(rows, row)
		}
		return rows, nil
	case ".json":
		var rows []map[string]any
		return rows, json.NewDecoder(f).Decode(&rows)
	default:
		var rows []map[string]any
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<26)
		for n := 1; sc.Scan(); n++ {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			var row map[string]any
			if err := json.Unmarshal([]byte(line), &row); err != nil {
				return nil, fmt.Errorf("%s:%d: %w", path, n, err)
			}
			rows = append(rows, row)
		}
		return rows, sc.Err()
	}
}

func Run(ctx context.Context, c Asker, rows []map[string]any, o Options) ([]Result, Summary) {
	idx := make([]int, len(rows))
	for i := range idx {
		idx[i] = i
	}
	if o.Limit > 0 && o.Limit < len(idx) {
		rand.New(rand.NewSource(o.Seed)).Shuffle(len(idx), func(i, j int) { idx[i], idx[j] = idx[j], idx[i] })
		idx = idx[:o.Limit]
		sort.Ints(idx)
	}
	results := make([]Result, len(idx))
	var mu sync.Mutex
	sum := Summary{Rows: len(idx), Choices: map[string]map[string]int{}}
	start := time.Now()
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < max(1, o.Workers); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := range jobs {
				row := rows[idx[k]]
				var state any = row
				if o.Field != "" {
					state = row[o.Field]
				}
				res := Result{Index: idx[k], Row: row}
				r, err := c.Ask(ctx, state, o.Questions)
				if err != nil {
					res.Error = err.Error()
				} else {
					res.Answers = r.Answers
					for _, a := range r.Answers {
						if a.Type != "noul" && a.Confidence < o.MinConfidence {
							res.NeedsReview = true
						}
					}
				}
				mu.Lock()
				results[k] = res
				if err != nil {
					sum.Failed++
				} else {
					sum.Tokens += r.Usage.InputTokens
					sum.CostUSD += r.Usage.CostUSD()
					if res.NeedsReview {
						sum.NeedsReview++
					}
					for q, a := range r.Answers {
						if a.Type == "choice" {
							if sum.Choices[q] == nil {
								sum.Choices[q] = map[string]int{}
							}
							sum.Choices[q][a.Choice]++
						}
					}
				}
				mu.Unlock()
			}
		}()
	}
	for k := range idx {
		jobs <- k
	}
	close(jobs)
	wg.Wait()
	sum.Wall = time.Since(start)
	return results, sum
}

// Value is the single number/label a question contributes to a flat table.
func Value(a jev.Answer) string {
	switch a.Type {
	case "choice":
		return a.Choice
	case "score":
		return fmt.Sprintf("%.3f", a.Score)
	default:
		return fmt.Sprintf("%.3f", a.Noul)
	}
}

// Rank sorts results by a score/noul question, highest first (errors last).
func Rank(results []Result, q string) []Result {
	out := append([]Result(nil), results...)
	num := func(r Result) float64 {
		a, ok := r.Answers[q]
		if !ok {
			return -1
		}
		if a.Type == "score" {
			return a.Score
		}
		if a.Type == "choice" {
			return a.Confidence
		}
		return a.Noul
	}
	sort.SliceStable(out, func(i, j int) bool { return num(out[i]) > num(out[j]) })
	return out
}

func questionIDs(qs map[string]jev.Question) []string {
	ids := make([]string, 0, len(qs))
	for k := range qs {
		ids = append(ids, k)
	}
	sort.Strings(ids)
	return ids
}

// Write saves results as .csv (original columns + one column per question,
// plus <q>_confidence) or .jsonl (full answers).
func Write(w io.Writer, format string, results []Result, qs map[string]jev.Question) error {
	if format != "csv" {
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		for _, r := range results {
			if err := enc.Encode(r); err != nil {
				return err
			}
		}
		return nil
	}
	cols := map[string]bool{}
	for _, r := range results {
		for k := range r.Row {
			cols[k] = true
		}
	}
	var rowCols []string
	for k := range cols {
		rowCols = append(rowCols, k)
	}
	sort.Strings(rowCols)
	ids := questionIDs(qs)
	header := append([]string{}, rowCols...)
	for _, q := range ids {
		header = append(header, "jev_"+q)
		if qs[q].Type != "noul" {
			header = append(header, "jev_"+q+"_confidence")
		}
	}
	header = append(header, "jev_needs_review", "jev_error")
	cw := csv.NewWriter(w)
	if err := cw.Write(header); err != nil {
		return err
	}
	for _, r := range results {
		var rec []string
		for _, k := range rowCols {
			rec = append(rec, cell(r.Row[k]))
		}
		for _, q := range ids {
			a, ok := r.Answers[q]
			v, conf := "", ""
			if ok {
				v, conf = Value(a), fmt.Sprintf("%.3f", a.Confidence)
			}
			rec = append(rec, v)
			if qs[q].Type != "noul" {
				rec = append(rec, conf)
			}
		}
		rec = append(rec, fmt.Sprint(r.NeedsReview), r.Error)
		if err := cw.Write(rec); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func cell(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	default:
		b, _ := json.Marshal(x)
		return string(b)
	}
}

func (s Summary) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "rows %d, failed %d, needs_review %d\n", s.Rows, s.Failed, s.NeedsReview)
	rate := float64(s.Rows) / max(s.Wall.Seconds(), 1e-9)
	fmt.Fprintf(&b, "jev: %d input tokens, $%.5f, wall %.1fs (%.1f rows/s)\n", s.Tokens, s.CostUSD, s.Wall.Seconds(), rate)
	for _, q := range sortedKeys(s.Choices) {
		dist := s.Choices[q]
		var parts []string
		for _, k := range sortedKeys(dist) {
			parts = append(parts, fmt.Sprintf("%s=%d", k, dist[k]))
		}
		fmt.Fprintf(&b, "  %s: %s\n", q, strings.Join(parts, " "))
	}
	return strings.TrimRight(b.String(), "\n")
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
