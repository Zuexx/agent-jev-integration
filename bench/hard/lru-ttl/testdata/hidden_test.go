package cache

import (
	"reflect"
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time      { return c.t }
func (c *clock) add(d time.Duration) { c.t = c.t.Add(d) }
func newC(cap int) (*Cache, *clock) {
	ck := &clock{time.Unix(1_000_000, 0)}
	return New(cap, 10*time.Second, ck.now), ck
}

func TestHiddenGetDoesNotExtendTTL(t *testing.T) {
	c, ck := newC(0)
	c.Set("a", 1)
	ck.add(6 * time.Second)
	if _, ok := c.Get("a"); !ok {
		t.Fatal("a should be live at 6s")
	}
	ck.add(4 * time.Second) // exactly 10s: expired
	if _, ok := c.Get("a"); ok {
		t.Fatal("a must expire at now >= expiry; Get must not extend TTL")
	}
}

func TestHiddenSetResetsTTL(t *testing.T) {
	c, ck := newC(0)
	c.Set("a", 1)
	ck.add(9 * time.Second)
	c.Set("a", 2)
	ck.add(9 * time.Second)
	if v, ok := c.Get("a"); !ok || v != 2 {
		t.Fatal("Set must replace the value and reset expiry", v, ok)
	}
}

func TestHiddenExpiredEvictedBeforeLRU(t *testing.T) {
	c, ck := newC(2)
	c.Set("old", 1)
	ck.add(5 * time.Second)
	c.Set("fresh", 2)
	c.Get("old")            // old is now most recently used
	ck.add(6 * time.Second) // old expired (11s), fresh live (6s)
	c.Set("new", 3)
	if _, ok := c.Get("fresh"); !ok {
		t.Fatal("expired entries must be dropped before evicting a live LRU entry")
	}
	if c.Len() != 2 {
		t.Fatal("len", c.Len())
	}
}

func TestHiddenLRUOrderAndLen(t *testing.T) {
	c, ck := newC(3)
	c.Set("a", 1)
	c.Set("b", 2)
	c.Set("c", 3)
	c.Get("a")
	c.Set("d", 4) // evicts b
	if got := c.Keys(); !reflect.DeepEqual(got, []string{"d", "a", "c"}) {
		t.Fatal("keys", got)
	}
	ck.add(11 * time.Second)
	if c.Len() != 0 || len(c.Keys()) != 0 {
		t.Fatal("Len/Keys must ignore expired entries", c.Len(), c.Keys())
	}
}

func TestHiddenUnlimited(t *testing.T) {
	c, _ := newC(0)
	for i := 0; i < 1000; i++ {
		c.Set(string(rune('a'+i%26))+string(rune('a'+i/26)), i)
	}
	if c.Len() != 1000 {
		t.Fatal(c.Len())
	}
}
