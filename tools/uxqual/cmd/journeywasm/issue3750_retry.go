package main

import "time"

// retryFailed makes every failed ticket answer stale so the next pass reads it
// again (issue 3750). Settled and in-flight answers are left alone.
func (c *chatProjectTaskPreviewCache) retryFailed(identity string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if identity != c.identity {
		return
	}
	for key, entry := range c.entries {
		if !entry.pending && entry.value.State == "failed" {
			entry.at = time.Time{}
			c.entries[key] = entry
		}
	}
}
