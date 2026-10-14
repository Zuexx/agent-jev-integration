// Package jev is a small client for TypeSafe's System One endpoint
// (POST /v1/systemone), which serves the Jev model.
package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// PricePerMTok is Jev 1.13's price per million input tokens; output is free.
const PricePerMTok = 0.042

// Question is one typed question. Criteria is a map for choice, a list for
// score, and an optional {"true","false"} map for noul.
type Question struct {
	Type         string `json:"type"`
	Instructions any    `json:"instructions"`
	Criteria     any    `json:"criteria,omitempty"`
}

func Choice(instructions any, options map[string]any) Question {
	return Question{Type: "choice", Instructions: instructions, Criteria: options}
}

func Score(instructions any, levels []string) Question {
	return Question{Type: "score", Instructions: instructions, Criteria: levels}
}

func Noul(instructions any) Question { return Question{Type: "noul", Instructions: instructions} }

type Request struct {
	State     any                 `json:"state"`
	Model     string              `json:"model"`
	Questions map[string]Question `json:"questions"`
}

type Answer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice,omitempty"`
	Score         float64            `json:"score,omitempty"`
	Noul          float64            `json:"noul,omitempty"`
	Confidence    float64            `json:"confidence,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Legend        map[string]string  `json:"legend,omitempty"`
}

// Ranked returns the options of a choice/score answer, most probable first.
func (a Answer) Ranked() []string {
	out := make([]string, 0, len(a.Probabilities))
	for k := range a.Probabilities {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		pi, pj := a.Probabilities[out[i]], a.Probabilities[out[j]]
		if pi != pj {
			return pi > pj
		}
		return out[i] < out[j]
	})
	return out
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

func (u Usage) CostUSD() float64 { return float64(u.InputTokens) * PricePerMTok / 1e6 }

type Response struct {
	Model   string            `json:"model"`
	Answers map[string]Answer `json:"answers"`
	Usage   Usage             `json:"usage"`
	// Latency is measured by the client around the HTTP call(s), retries included.
	Latency time.Duration `json:"-"`
}

// APIError is a non-2xx answer from the API.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("typesafe: HTTP %d: %s", e.Status, strings.TrimSpace(e.Body))
}

var ErrNoKey = errors.New("typesafe: no API key (set TYPESAFE_API_KEY or run `jevcc key`)")

type Client struct {
	Endpoint string
	Key      string
	Model    string
	HTTP     *http.Client
	// Attempts is the total tries for 429/529/connection errors (default 1).
	Attempts int
	// Sleep is time.Sleep, replaceable in tests.
	Sleep func(time.Duration)
}

func (c *Client) Ask(ctx context.Context, state any, qs map[string]Question) (*Response, error) {
	if c.Key == "" {
		return nil, ErrNoKey
	}
	body, err := json.Marshal(Request{State: state, Model: c.Model, Questions: qs})
	if err != nil {
		return nil, err
	}
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	sleep := c.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	attempts := max(c.Attempts, 1)
	start := time.Now()
	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			sleep(backoff(i, lastErr))
		}
		resp, err := c.once(ctx, hc, body)
		if err == nil {
			resp.Latency = time.Since(start)
			return resp, nil
		}
		lastErr = err
		if ctx.Err() != nil || !retryable(err) {
			break
		}
	}
	return nil, lastErr
}

func (c *Client) once(ctx context.Context, hc *http.Client, body []byte) (*Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.Endpoint, "/")+"/v1/systemone", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	req.Header.Set("Content-Type", "application/json")
	res, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode/100 != 2 {
		e := &APIError{Status: res.StatusCode, Body: string(raw)}
		if s, err := strconv.Atoi(res.Header.Get("Retry-After")); err == nil {
			return nil, &retryAfter{e, time.Duration(s) * time.Second}
		}
		return nil, e
	}
	var out Response
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("typesafe: bad response: %w", err)
	}
	return &out, nil
}

type retryAfter struct {
	*APIError
	wait time.Duration
}

func (r *retryAfter) Unwrap() error { return r.APIError }

func retryable(err error) bool {
	var ae *APIError
	if errors.As(err, &ae) {
		return ae.Status == 429 || ae.Status == 529 || ae.Status >= 500
	}
	return true // network error
}

func backoff(attempt int, err error) time.Duration {
	var ra *retryAfter
	if errors.As(err, &ra) && ra.wait > 0 {
		return ra.wait
	}
	return time.Duration(math.Min(8, 0.5*math.Pow(2, float64(attempt-1))) * float64(time.Second))
}

// Parallel calls fn(i) for i in [0,n) on at most workers goroutines.
func Parallel(n, workers int, fn func(i int)) {
	jobs := make(chan int)
	done := make(chan struct{})
	workers = max(1, min(workers, n))
	for w := 0; w < workers; w++ {
		go func() {
			for i := range jobs {
				fn(i)
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < n; i++ {
		jobs <- i
	}
	close(jobs)
	for w := 0; w < workers; w++ {
		<-done
	}
}
