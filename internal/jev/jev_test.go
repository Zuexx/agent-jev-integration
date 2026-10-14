package jev

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAskSendsTypedQuestions(t *testing.T) {
	var got Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/systemone" || r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("%s %s", r.URL.Path, r.Header.Get("Authorization"))
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"model":"jev-1.13.0","answers":{"d":{"type":"choice","choice":"b","confidence":0.8,"probabilities":{"a":0.1,"b":0.9}}},"usage":{"input_tokens":1000000,"output_tokens":5}}`))
	}))
	defer srv.Close()
	c := &Client{Endpoint: srv.URL, Key: "k", Model: "jev-latest"}
	res, err := c.Ask(context.Background(), "s", map[string]Question{"d": Choice("pick", map[string]any{"a": "A", "b": nil})})
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "jev-latest" || got.Questions["d"].Type != "choice" || got.State != "s" {
		t.Fatalf("%+v", got)
	}
	a := res.Answers["d"]
	if a.Choice != "b" || a.Ranked()[0] != "b" || res.Usage.CostUSD() != PricePerMTok {
		t.Fatalf("%+v %v", a, res.Usage.CostUSD())
	}
}

func TestRetriesOn429ThenFailsOn422(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(429)
			return
		}
		w.WriteHeader(422)
		w.Write([]byte(`{"detail":"bad"}`))
	}))
	defer srv.Close()
	var slept []time.Duration
	c := &Client{Endpoint: srv.URL, Key: "k", Attempts: 5, Sleep: func(d time.Duration) { slept = append(slept, d) }}
	_, err := c.Ask(context.Background(), "s", nil)
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 422 || calls != 2 || len(slept) != 1 || slept[0] != 2*time.Second {
		t.Fatalf("err=%v calls=%d slept=%v", err, calls, slept)
	}
}

func TestNoKey(t *testing.T) {
	if _, err := (&Client{}).Ask(context.Background(), "s", nil); !errors.Is(err, ErrNoKey) {
		t.Fatal(err)
	}
}
