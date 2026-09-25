package main

import (
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// Document unfurls in chat. It follows the share-link embeds
// (chat_embed_state.go): the render effect claims the document IDs on
// screen, one batched GetDocumentPreviews read resolves every claim as the
// current viewer, and the answers are cached for the session. A cached
// answer is reused for a minute and then read again, so a revoked share
// stops showing its title soon after.

// chatDocPreviewLimit bounds one claim and the cache; it is also the
// server's batch bound.
const chatDocPreviewLimit = 50

const chatDocPreviewFreshFor = time.Minute

// chatDocPreviewPendingTimeout bounds how long a claim may sit "loading"
// (C-3). claim() never reissues an entry it still marks pending, so a read
// that never returns -- the client wasn't configured yet, the tab lost the
// connection mid-flight, the server dropped it -- left the card reading
// "Loading document…" forever with no path back to a resolved state. This
// is longer than the 15s GetDocumentPreviews context so a request that is
// still genuinely in flight is not cut off under it.
const chatDocPreviewPendingTimeout = 20 * time.Second

type chatDocPreviewEntry struct {
	value   chatui.DocPreview
	at      time.Time
	pending bool
}

// chatDocPreviewCache is keyed by the viewer: a persona switch drops every
// answer, since each was authorized for the viewer who asked.
type chatDocPreviewCache struct {
	mu       sync.Mutex
	identity string
	epoch    uint64
	entries  map[string]chatDocPreviewEntry
	order    []string
}

var chatDocPreviews = &chatDocPreviewCache{}

// chatDocPreviewIDs lists the documents referenced by the draft and the
// newest messages on screen, newest first, without repeats.
func chatDocPreviewIDs(model chatui.Model) []string {
	seen := map[string]bool{}
	out := make([]string, 0, 8)
	add := func(body string) {
		if !strings.Contains(body, "doc") {
			return
		}
		for _, ref := range chatui.DocReferences(body, model.EmbedOrigin) {
			// The server refuses a batch holding an ID over 128 bytes.
			if !seen[ref.ID] && len(ref.ID) <= 128 && len(out) < chatDocPreviewLimit {
				seen[ref.ID] = true
				out = append(out, ref.ID)
			}
		}
	}
	add(model.Draft)
	for i := len(model.ThreadMessages) - 1; i >= 0 && len(out) < chatDocPreviewLimit; i-- {
		add(model.ThreadMessages[i].Body)
	}
	for i := len(model.Messages) - 1; i >= 0 && len(out) < chatDocPreviewLimit; i-- {
		add(model.Messages[i].Body)
	}
	// CROSS-01: a search hit's snippet resolves doc:<id> tokens to a title
	// the same way the timeline's inline links do, so the previews it needs
	// are claimed too rather than always falling back to the neutral label.
	for i := 0; i < len(model.SearchMessages) && len(out) < chatDocPreviewLimit; i++ {
		add(model.SearchMessages[i].Message.Body)
	}
	return out
}

// chatDocPreviewFingerprint changes when the referenced documents change
// or when the model loses previews it had (a load replaced it), so the
// render effect runs again and restores them from the cache.
func chatDocPreviewFingerprint(model chatui.Model) string {
	ready := 0
	for _, preview := range model.DocPreviews {
		if preview.State == "ready" || preview.State == "unavailable" {
			ready++
		}
	}
	return model.SelectedID + "|" + strconv.Itoa(ready) + "|" + strings.Join(chatDocPreviewIDs(model), "|")
}

// claim returns the IDs that need a read and the previews to show now: a
// fresh or in-flight answer from the cache, or a loading card.
func (c *chatDocPreviewCache) claim(identity string, ids []string, now time.Time) ([]string, uint64, map[string]chatui.DocPreview) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if identity != c.identity || c.entries == nil {
		c.identity, c.entries, c.order = identity, map[string]chatDocPreviewEntry{}, nil
		c.epoch++
	}
	shown := make(map[string]chatui.DocPreview, len(ids))
	var claims []string
	for _, id := range ids {
		entry, ok := c.entries[id]
		if ok && entry.pending && now.Sub(entry.at) > chatDocPreviewPendingTimeout {
			// The read that owned this claim is never coming back. Drop the
			// pending flag so the card resolves to something definite and the
			// slot below can reclaim it for one more attempt.
			entry.pending = false
			if entry.value.State != "ready" {
				entry.value = chatui.DocPreview{ID: id, State: "unavailable"}
			}
			c.entries[id] = entry
		}
		if ok && (entry.pending || now.Sub(entry.at) < chatDocPreviewFreshFor) {
			shown[id] = entry.value
			continue
		}
		if !ok {
			if len(c.order) >= chatDocPreviewLimit {
				delete(c.entries, c.order[0])
				c.order = c.order[1:]
			}
			c.order = append(c.order, id)
		}
		value := chatui.DocPreview{ID: id, State: "loading"}
		if ok && entry.value.State == "ready" {
			// Revalidating: keep showing the last answer, not a spinner.
			value = entry.value
		}
		c.entries[id] = chatDocPreviewEntry{value: value, at: now, pending: true}
		shown[id] = value
		claims = append(claims, id)
	}
	return claims, c.epoch, shown
}

// finish records the answers to one claim; it reports false when the
// viewer changed or the cache was reset while the read was in flight.
func (c *chatDocPreviewCache) finish(identity string, epoch uint64, answers map[string]chatui.DocPreview, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if identity != c.identity || epoch != c.epoch {
		return false
	}
	for id, value := range answers {
		if entry, ok := c.entries[id]; ok && entry.pending {
			c.entries[id] = chatDocPreviewEntry{value: value, at: now}
		}
	}
	return true
}

// snapshot is every answer the cache holds for identity.
func (c *chatDocPreviewCache) snapshot(identity string) map[string]chatui.DocPreview {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := map[string]chatui.DocPreview{}
	if identity != c.identity {
		return out
	}
	for id, entry := range c.entries {
		out[id] = entry.value
	}
	return out
}

// mergeChatDocPreviews writes shown into the model and reports a change.
// It replaces the map rather than writing into it: snapshots handed to a
// render share the old one.
func mergeChatDocPreviews(model *chatui.Model, shown map[string]chatui.DocPreview) bool {
	changed := false
	for id, value := range shown {
		if current, ok := model.DocPreviews[id]; !ok || current != value {
			changed = true
			break
		}
	}
	if !changed {
		return false
	}
	next := make(map[string]chatui.DocPreview, len(model.DocPreviews)+len(shown))
	for id, value := range model.DocPreviews {
		next[id] = value
	}
	for id, value := range shown {
		next[id] = value
	}
	model.DocPreviews = next
	return true
}

// chatDocPreviewAnswer turns one server preview into the card chat shows.
// An unreadable document keeps nothing but its ID.
func chatDocPreviewAnswer(id string, readable bool, title, owner, updated, snippet string) chatui.DocPreview {
	if !readable {
		return chatui.DocPreview{ID: id, State: "ready"}
	}
	return chatui.DocPreview{ID: id, State: "ready", Readable: true, Title: title, Owner: owner, UpdatedAt: updated, Snippet: snippet}
}
