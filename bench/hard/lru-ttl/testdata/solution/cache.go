// Package cache is an in-memory LRU cache whose entries also expire.
package cache

import (
	"container/list"
	"time"
)

type entry struct {
	key    string
	value  int
	expiry time.Time
}

type Cache struct {
	cap   int
	ttl   time.Duration
	now   func() time.Time
	order *list.List
	items map[string]*list.Element
}

func New(capacity int, ttl time.Duration, now func() time.Time) *Cache {
	return &Cache{cap: capacity, ttl: ttl, now: now, order: list.New(), items: map[string]*list.Element{}}
}

func (c *Cache) expired(e *list.Element) bool { return !c.now().Before(e.Value.(*entry).expiry) }

func (c *Cache) remove(e *list.Element) {
	c.order.Remove(e)
	delete(c.items, e.Value.(*entry).key)
}

func (c *Cache) purge() {
	for e := c.order.Front(); e != nil; {
		next := e.Next()
		if c.expired(e) {
			c.remove(e)
		}
		e = next
	}
}

func (c *Cache) Set(key string, value int) {
	if e, ok := c.items[key]; ok {
		en := e.Value.(*entry)
		en.value, en.expiry = value, c.now().Add(c.ttl)
		c.order.MoveToFront(e)
		return
	}
	c.items[key] = c.order.PushFront(&entry{key, value, c.now().Add(c.ttl)})
	if c.cap > 0 && c.order.Len() > c.cap {
		c.purge()
		for c.order.Len() > c.cap {
			c.remove(c.order.Back())
		}
	}
}

func (c *Cache) Get(key string) (int, bool) {
	e, ok := c.items[key]
	if !ok {
		return 0, false
	}
	if c.expired(e) {
		c.remove(e)
		return 0, false
	}
	c.order.MoveToFront(e)
	return e.Value.(*entry).value, true
}

func (c *Cache) Len() int { c.purge(); return c.order.Len() }

func (c *Cache) Keys() []string {
	c.purge()
	out := []string{}
	for e := c.order.Front(); e != nil; e = e.Next() {
		out = append(out, e.Value.(*entry).key)
	}
	return out
}
