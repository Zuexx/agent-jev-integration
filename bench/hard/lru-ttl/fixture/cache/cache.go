// Package cache is an in-memory LRU cache whose entries also expire.
package cache

import "time"

// Cache holds at most Capacity live entries (0 = unlimited). Rules:
//   - Set inserts or replaces a value; it resets that entry's expiry to now+TTL
//     and makes it the most recently used.
//   - Get returns a live entry and makes it the most recently used, but does NOT
//     extend its expiry. An expired entry is removed and reported as missing.
//   - An entry is expired when now >= its expiry.
//   - When a Set would exceed Capacity, all expired entries are dropped first;
//     only if still over capacity is the least recently used live entry evicted.
//   - Len counts live entries only. Keys lists live keys, most recently used first.
type Cache struct {
	// add fields
}

// New returns a cache. now is the clock (time.Now in production).
func New(capacity int, ttl time.Duration, now func() time.Time) *Cache {
	panic("not implemented")
}

func (c *Cache) Set(key string, value int)  { panic("not implemented") }
func (c *Cache) Get(key string) (int, bool) { panic("not implemented") }
func (c *Cache) Len() int                   { panic("not implemented") }
func (c *Cache) Keys() []string             { panic("not implemented") }
