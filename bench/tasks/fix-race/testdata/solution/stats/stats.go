// Package stats counts events from many goroutines.
package stats

import "sync"

type Counter struct {
	mu     sync.Mutex
	counts map[string]int
	total  int
}

func New() *Counter { return &Counter{counts: map[string]int{}} }

func (c *Counter) Add(name string) {
	c.mu.Lock()
	c.counts[name]++
	c.total++
	c.mu.Unlock()
}

// Snapshot returns a copy of the counts and the total.
func (c *Counter) Snapshot() (map[string]int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]int, len(c.counts))
	for k, v := range c.counts {
		out[k] = v
	}
	return out, c.total
}
