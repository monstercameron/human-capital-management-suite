package main

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/ambientagents"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// ambientReadFreshness is how long one answer about a conversation is reused.
// Chat refreshes the agent directory of the open conversation several times
// while it loads (a retry, a membership change, a stream reconnect); the
// ambient read beside it must be asked once per open, not once per refresh.
const ambientReadFreshness = 15 * time.Second

// ambientReads is the one cache of the page.
var ambientReads ambientReadCache

// ambientReadKey names one person in one conversation.
func ambientReadKey(cfg journeyclient.Config, conversation string) string {
	return cfg.Tenant + "|" + cfg.Subject + "|" + conversation
}

type ambientReadEntry struct {
	at       time.Time
	inflight bool
	have     bool
	snapshot ambientagents.Snapshot
}

type ambientReadCache struct {
	mu      sync.Mutex
	entries map[string]ambientReadEntry
}

// begin says whether the caller should ask the server. When it should not, the
// last good snapshot of the conversation is returned if there is one, so a
// conversation opened again soon after shows what it showed before.
func (c *ambientReadCache) begin(conversation string, now time.Time) (snapshot ambientagents.Snapshot, have, fetch bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[string]ambientReadEntry{}
	}
	entry := c.entries[conversation]
	if entry.inflight || !entry.at.IsZero() && now.Sub(entry.at) < ambientReadFreshness {
		return entry.snapshot, entry.have, false
	}
	entry.inflight = true
	c.entries[conversation] = entry
	return entry.snapshot, entry.have, true
}

// finish records the answer. A failed read keeps the last good snapshot and is
// not repeated until the answer is stale, so a server that does not answer
// stays quiet instead of being asked again on every refresh.
func (c *ambientReadCache) finish(conversation string, snapshot ambientagents.Snapshot, err error, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.entries[conversation]
	entry.inflight, entry.at = false, now
	if err == nil {
		entry.snapshot, entry.have = snapshot, true
	} else if errors.Is(err, context.Canceled) {
		// A read that was cut short is not an answer: the next refresh asks again.
		entry.at = time.Time{}
	}
	c.entries[conversation] = entry
}

// forget drops what is known about a conversation, for a change the server made
// (the person's own switch), so the next read asks again.
func (c *ambientReadCache) forget(conversation string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, conversation)
}
