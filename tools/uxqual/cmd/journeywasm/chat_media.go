package main

import (
	"path"
	"strings"
	"sync"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// This file is the media half of a post, and none of it fetches anything.
//
// A post carries its attachments as REFERENCE_KIND_MEDIA references. The bytes
// behind one are protected: a read needs a short-lived grant, minted per
// principal and per artifact, and the handler reads that grant from a header
// only (internal/transport/chatmedia/handler.go:302-307 checks
// X-Chat-Media-Grant and Authorization, never a query parameter). So the URL an
// <img> can use is not a signed link -- it is made on the client after a
// granted fetch (see chat_media_wasm.go).
//
// The mapping and the grant cache live here so both are decided without a
// browser: which references are attachments, what they are called, and when a
// grant has to be minted again.

// chatGrantSkew is how long before a grant's stated expiry it is treated as
// spent. The clock the server minted against is not this one, and a grant that
// expires mid-fetch costs a 403 and a round trip.
const chatGrantSkew = 20 * time.Second

const (
	chatMediaPreviewMaxBytes = 16 << 20
	chatMediaCacheMaxBytes   = 64 << 20
	chatMediaCacheMaxItems   = 32
)

// chatMediaGrant is one artifact's download authorization.
type chatMediaGrant struct {
	Token     string
	ExpiresAt time.Time
	// URL is the object URL made from the granted bytes, which is what the
	// renderer puts in an <img src>. It is cached with the grant because it is
	// only valid for as long as the page holds it.
	URL          string
	Bytes        int64
	EncodedBytes int64
}

// chatMediaGrants caches grants per artifact for this session.
//
// Per artifact rather than per post: the same image forwarded into three
// conversations is one artifact and one grant. A grant is dropped when it
// expires, and explicitly when a read comes back 403 -- the server is entitled
// to revoke access before the stated expiry, and the stated expiry is the only
// thing the client could otherwise believe.
type chatMediaGrants struct {
	mu          sync.Mutex
	grants      map[string]chatMediaGrant
	epoch       uint64
	revoke      func(string)
	order       []string
	wanted      map[string]bool
	wantKnown   bool
	suppressed  map[string]bool
	failedUntil map[string]time.Time
	// inflight stops two renders of the same timeline from fetching one
	// artifact twice.
	inflight map[string]bool
}

func (c *chatMediaGrants) SetRevoker(revoke func(string)) {
	c.mu.Lock()
	c.revoke = revoke
	c.mu.Unlock()
}

// Clear drops protected bytes on a room or principal change. The epoch also
// fences downloads that finish after the change.
func (c *chatMediaGrants) Clear() {
	c.mu.Lock()
	c.epoch++
	old := c.grants
	c.grants = map[string]chatMediaGrant{}
	c.inflight = map[string]bool{}
	c.order = nil
	c.wanted, c.wantKnown, c.suppressed, c.failedUntil = nil, false, nil, nil
	revoke := c.revoke
	c.mu.Unlock()
	if revoke != nil {
		for _, grant := range old {
			if grant.URL != "" {
				revoke(grant.URL)
			}
		}
	}
}

// SetWanted retains only artifacts used by mounted messages. The caller
// orders IDs by display priority and the cache admits at most its item bound.
func (c *chatMediaGrants) SetWanted(ids []string) {
	wanted := make(map[string]bool, chatMediaCacheMaxItems)
	for _, id := range ids {
		if id != "" && len(wanted) < chatMediaCacheMaxItems {
			wanted[id] = true
		}
	}
	c.mu.Lock()
	if c.wantKnown && len(c.wanted) == len(wanted) {
		same := true
		for id := range wanted {
			if !c.wanted[id] {
				same = false
				break
			}
		}
		if same {
			c.mu.Unlock()
			return
		}
	}
	c.wanted, c.wantKnown = wanted, true
	c.suppressed = map[string]bool{}
	c.failedUntil = map[string]time.Time{}
	var evicted []chatMediaGrant
	for id, grant := range c.grants {
		if !wanted[id] {
			evicted = append(evicted, grant)
			delete(c.grants, id)
			c.removeOrder(id)
		}
	}
	for id := range c.inflight {
		if !wanted[id] {
			delete(c.inflight, id)
		}
	}
	revoke := c.revoke
	c.mu.Unlock()
	if revoke != nil {
		for _, grant := range evicted {
			if grant.URL != "" {
				revoke(grant.URL)
			}
		}
	}
}

func (c *chatMediaGrants) Wanted(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.wantKnown && c.wanted[id]
}

func newChatMediaGrants() *chatMediaGrants {
	return &chatMediaGrants{grants: map[string]chatMediaGrant{}, inflight: map[string]bool{}}
}

// Get returns a usable grant for id.
func (c *chatMediaGrants) Get(id string, now time.Time) (chatMediaGrant, bool) {
	c.mu.Lock()
	grant, ok := c.grants[id]
	if !ok || grant.Token == "" {
		c.mu.Unlock()
		return chatMediaGrant{}, false
	}
	if !grant.ExpiresAt.IsZero() && !now.Before(grant.ExpiresAt.Add(-chatGrantSkew)) {
		delete(c.grants, id)
		c.removeOrder(id)
		revoke := c.revoke
		c.mu.Unlock()
		if revoke != nil && grant.URL != "" {
			revoke(grant.URL)
		}
		return chatMediaGrant{}, false
	}
	c.touch(id)
	c.mu.Unlock()
	return grant, true
}

// Put caches a grant.
func (c *chatMediaGrants) Put(id string, grant chatMediaGrant) {
	if id == "" {
		return
	}
	c.mu.Lock()
	previous := c.grants[id]
	c.grants[id] = grant
	c.touch(id)
	delete(c.inflight, id)
	revoke := c.revoke
	c.mu.Unlock()
	if revoke != nil && previous.URL != "" && previous.URL != grant.URL {
		revoke(previous.URL)
	}
}

func (c *chatMediaGrants) PutIfEpoch(id string, grant chatMediaGrant, epoch uint64) bool {
	if id == "" || grant.EncodedBytes > chatMediaPreviewMaxBytes {
		return false
	}
	c.mu.Lock()
	if c.epoch != epoch || (c.wantKnown && !c.wanted[id]) {
		c.mu.Unlock()
		return false
	}
	previous := c.grants[id]
	c.grants[id] = grant
	c.touch(id)
	var evicted []chatMediaGrant
	for len(c.grants) > chatMediaCacheMaxItems || c.byteSize() > chatMediaCacheMaxBytes {
		oldest := c.order[0]
		c.order = c.order[1:]
		if oldest == id && len(c.order) == 0 {
			break
		}
		if old, ok := c.grants[oldest]; ok {
			evicted = append(evicted, old)
			delete(c.grants, oldest)
			if c.suppressed == nil {
				c.suppressed = map[string]bool{}
			}
			c.suppressed[oldest] = true
		}
	}
	delete(c.inflight, id)
	revoke := c.revoke
	c.mu.Unlock()
	if revoke != nil && previous.URL != "" && previous.URL != grant.URL {
		revoke(previous.URL)
	}
	if revoke != nil {
		for _, old := range evicted {
			if old.URL != "" {
				revoke(old.URL)
			}
		}
	}
	return true
}

func (c *chatMediaGrants) touch(id string) {
	c.removeOrder(id)
	c.order = append(c.order, id)
}

func (c *chatMediaGrants) removeOrder(id string) {
	for i, existing := range c.order {
		if existing == id {
			c.order = append(c.order[:i], c.order[i+1:]...)
			return
		}
	}
}

func (c *chatMediaGrants) byteSize() int64 {
	var total int64
	for _, grant := range c.grants {
		total += grant.Bytes
	}
	return total
}

func (c *chatMediaGrants) ApplyIfEpoch(id string, epoch uint64, apply func() bool) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.epoch != epoch || (c.wantKnown && !c.wanted[id]) {
		return false
	}
	return apply()
}

// Invalidate drops a grant the server has refused.
func (c *chatMediaGrants) Invalidate(id string) {
	c.mu.Lock()
	previous := c.grants[id]
	delete(c.grants, id)
	c.removeOrder(id)
	delete(c.inflight, id)
	revoke := c.revoke
	c.mu.Unlock()
	if revoke != nil && previous.URL != "" {
		revoke(previous.URL)
	}
}

func (c *chatMediaGrants) InvalidateIfEpoch(id string, epoch uint64) bool {
	c.mu.Lock()
	if c.epoch != epoch {
		c.mu.Unlock()
		return false
	}
	previous := c.grants[id]
	delete(c.grants, id)
	c.removeOrder(id)
	delete(c.inflight, id)
	revoke := c.revoke
	c.mu.Unlock()
	if revoke != nil && previous.URL != "" {
		revoke(previous.URL)
	}
	return true
}

// Claim reports whether the caller should fetch id: there is no usable grant
// and nobody else is already fetching it.
func (c *chatMediaGrants) Claim(id string, now time.Time) bool {
	_, ok := c.ClaimEpoch(id, now)
	return ok
}

func (c *chatMediaGrants) ClaimEpoch(id string, now time.Time) (uint64, bool) {
	if id == "" {
		return 0, false
	}
	if _, ok := c.Get(id, now); ok {
		return 0, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.wantKnown && (!c.wanted[id] || c.suppressed[id]) {
		return 0, false
	}
	if until := c.failedUntil[id]; !until.IsZero() && now.Before(until) {
		return 0, false
	}
	if c.inflight[id] {
		return 0, false
	}
	c.inflight[id] = true
	return c.epoch, true
}

// Release ends a claim that produced nothing, so the next render retries.
func (c *chatMediaGrants) Release(id string) {
	c.mu.Lock()
	delete(c.inflight, id)
	c.mu.Unlock()
}

func (c *chatMediaGrants) ReleaseIfEpoch(id string, epoch uint64) {
	c.mu.Lock()
	if c.epoch == epoch {
		delete(c.inflight, id)
	}
	c.mu.Unlock()
}

func (c *chatMediaGrants) FailIfEpoch(id string, epoch uint64, until time.Time) {
	c.mu.Lock()
	if c.epoch == epoch {
		delete(c.inflight, id)
		if c.failedUntil == nil {
			c.failedUntil = map[string]time.Time{}
		}
		c.failedUntil[id] = until
	}
	c.mu.Unlock()
}

// chatMediaAttachments projects a post's media references.
//
// Only Kind, Id and Display are read: the proto describes content_type,
// byte_size, width and height on a media reference, but the generated Go does
// not carry them yet (see the report), so the content type is inferred from the
// file name until it does. URL is deliberately empty -- the renderer shows a
// chip until a grant produces one, and the timeline never waits for bytes.
func chatMediaAttachments(post *chatv1.Post) []chatui.Attachment {
	references := post.GetReferences()
	if len(references) == 0 {
		return nil
	}
	out := make([]chatui.Attachment, 0, len(references))
	seen := make(map[string]bool, len(references))
	for _, reference := range references {
		if reference.GetKind() != chatv1.ReferenceKind_REFERENCE_KIND_MEDIA {
			continue
		}
		id := strings.TrimSpace(reference.GetId())
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		name := strings.TrimSpace(reference.GetDisplay())
		if name == "" {
			name = id
		}
		contentType := strings.TrimSpace(reference.GetContentType())
		if contentType == "" {
			contentType = chatMediaContentType(name)
		}
		out = append(out, chatui.Attachment{ID: id, Name: name, ContentType: contentType, Bytes: int64(reference.GetByteSize()), Width: int(reference.GetWidth()), Height: int(reference.GetHeight())})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// chatMediaContentTypes maps the extensions this surface renders inline.
var chatMediaContentTypes = map[string]string{
	".gif": "image/gif", ".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".webp": "image/webp", ".avif": "image/avif", ".svg": "image/svg+xml",
	".pdf": "application/pdf", ".mp4": "video/mp4", ".webm": "video/webm",
}

// chatMediaContentType guesses a media type from a file name.
//
// A guess, and only until the reference carries the real one: the media service
// is the authority, and the handler sends its own Content-Type with the bytes.
// An unknown extension returns nothing, which renders as a chip rather than as
// an image the browser would refuse.
func chatMediaContentType(name string) string {
	return chatMediaContentTypes[strings.ToLower(path.Ext(name))]
}

// chatAttachmentsNeedingGrants is the artifacts on screen with no URL yet.
func chatAttachmentsNeedingGrants(messages []chatui.Message) []string {
	var out []string
	seen := map[string]bool{}
	for _, message := range messages {
		for _, attachment := range message.Attachments {
			if attachment.ID == "" || attachment.URL != "" || seen[attachment.ID] {
				continue
			}
			seen[attachment.ID] = true
			out = append(out, attachment.ID)
		}
	}
	return out
}

// applyChatMediaURL puts a resolved object URL on every attachment for one
// artifact, wherever it appears.
func applyChatMediaURL(messages []chatui.Message, id, url string) bool {
	if id == "" || url == "" {
		return false
	}
	changed := false
	for i := range messages {
		for j := range messages[i].Attachments {
			if messages[i].Attachments[j].ID == id && messages[i].Attachments[j].URL != url {
				messages[i].Attachments[j].URL = url
				changed = true
			}
		}
	}
	return changed
}
