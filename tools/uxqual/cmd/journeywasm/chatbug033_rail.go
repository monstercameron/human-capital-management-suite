package main

import (
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// agentRailEntry is one direct conversation with an agent, delivered by the
// server together with the conversation list: the agent's name, its own
// description and its stored icon. It is what lets the sidebar row, the header
// and the messages draw the agent's own icon from the first paint, instead of a
// glyph every agent shares until a per-conversation request lands.
type agentRailEntry struct {
	ConversationID string          `json:"conversation_id"`
	AgentID        string          `json:"agent_id"`
	Name           string          `json:"name"`
	Purpose        string          `json:"purpose"`
	Icon           agenticon.Value `json:"icon"`
	IconRevision   int64           `json:"icon_revision"`
}

type agentRailPayload struct {
	Agents []agentRailEntry `json:"agents"`
}

// applyAgentRail marks the listed direct conversations as agent conversations
// with their name, description and icon, and records that the stored icons have
// been read: an agent with none is now drawn with its own fallback, and a direct
// row that is not an agent's is a person's. It reports whether anything changed.
func applyAgentRail(model *chatui.Model, entries []agentRailEntry) bool {
	if model == nil {
		return false
	}
	changed := applyAgentRailEntries(model, entries)
	if model.AgentRailPending || !model.AgentIconsReady {
		changed = true
	}
	model.AgentRailPending, model.AgentIconsReady = false, true
	return changed
}

// applyAgentRailEntries adopts what an answer says about each conversation and
// leaves the "read" flags alone, for an answer that is known to be incomplete.
func applyAgentRailEntries(model *chatui.Model, entries []agentRailEntry) bool {
	changed := false
	for _, entry := range entries {
		if applyAgentDirectConversation(model, entry.ConversationID, entry.AgentID, entry.Name, agentDirectIdentity{Icon: entry.Icon, Revision: entry.IconRevision, Purpose: entry.Purpose}) {
			changed = true
		}
	}
	return changed
}

// agentRailCache remembers what the server last said, per signed-in person, so
// a conversation list read that repeats on every revalidation does not repeat
// the request: it asks again only when the list holds a direct conversation the
// last answer did not cover.
type agentRailCache struct {
	mu       sync.Mutex
	identity string
	entries  []agentRailEntry
	covered  map[string]bool
	settled  bool
	at       time.Time
}

// agentRailFreshFor is how long an answer is reused; an agent whose icon was
// changed shows the new one after at most this long, with no reload.
const agentRailFreshFor = 5 * time.Minute

// state returns the cached entries and whether the answer is still current for
// this person and these direct conversations.
func (c *agentRailCache) state(identity string, rooms []chatui.Conversation) (entries []agentRailEntry, current bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.identity != identity {
		return nil, false
	}
	if time.Since(c.at) > agentRailFreshFor {
		return append([]agentRailEntry(nil), c.entries...), false
	}
	for _, room := range rooms {
		if room.Kind == chatui.DirectMessage && !c.covered[room.ID] {
			return append([]agentRailEntry(nil), c.entries...), false
		}
	}
	return append([]agentRailEntry(nil), c.entries...), c.settled
}

// has reports whether any answer, however old, is held for this person.
func (c *agentRailCache) has(identity string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.identity == identity && c.settled
}

func (c *agentRailCache) store(identity string, rooms []chatui.Conversation, entries []agentRailEntry) {
	covered := make(map[string]bool, len(rooms))
	for _, room := range rooms {
		if room.Kind == chatui.DirectMessage {
			covered[room.ID] = true
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.identity, c.entries, c.covered, c.settled, c.at = identity, append([]agentRailEntry(nil), entries...), covered, true, time.Now()
}

// agentRailIdentity keys the cache to the signed-in person.
func agentRailIdentity(tenant, subject string) string {
	return strings.TrimSpace(tenant) + "\x00" + strings.TrimSpace(subject)
}
