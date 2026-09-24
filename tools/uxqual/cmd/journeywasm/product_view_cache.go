package main

import (
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// productViewCache remembers the last resolved view per canonical address,
// so returning to a page already seen in this session (Back, Forward, the
// header arrows, or a link back to it) paints that page at once instead of
// a loading proxy. It is only a paint: the route loader still re-reads every
// authoritative dataset and replaces the view when it answers, exactly as a
// same-page warm refresh does. Entries age out, and the cache is bounded.
type productViewCache struct {
	mu      sync.Mutex
	max     int
	ttl     time.Duration
	now     func() time.Time
	order   []string
	entries map[string]productViewCacheEntry
}

type productViewCacheEntry struct {
	view productui.View
	at   time.Time
}

func newProductViewCache(max int, ttl time.Duration, now func() time.Time) *productViewCache {
	return &productViewCache{max: max, ttl: ttl, now: now, entries: map[string]productViewCacheEntry{}}
}

// productViewCacheable reports whether a page's resolved view can stand in
// for it while it reloads. Journeys render from the live journey store, not
// from the view, and a failed read is not a page to show again.
func productViewCacheable(view productui.View) bool {
	return view.Page != "" && view.Page != productui.PageJourneys && view.LoadError == ""
}

func (c *productViewCache) Put(href string, view productui.View) {
	if c == nil || href == "" || !productViewCacheable(view) {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.entries[href]; ok {
		c.remove(href)
	}
	c.entries[href] = productViewCacheEntry{view: view, at: c.now()}
	c.order = append(c.order, href)
	for len(c.order) > c.max {
		delete(c.entries, c.order[0])
		c.order = c.order[1:]
	}
}

func (c *productViewCache) Get(href string) (productui.View, bool) {
	if c == nil {
		return productui.View{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[href]
	if !ok {
		return productui.View{}, false
	}
	if c.now().Sub(entry.at) > c.ttl {
		delete(c.entries, href)
		c.remove(href)
		return productui.View{}, false
	}
	return entry.view, true
}

func (c *productViewCache) remove(href string) {
	for i, h := range c.order {
		if h == href {
			c.order = append(c.order[:i], c.order[i+1:]...)
			return
		}
	}
}
