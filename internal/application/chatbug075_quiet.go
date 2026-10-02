package application

import (
	"context"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// CHATBUG-075: the agent activity of an open conversation is read once a
// second for as long as the conversation is open. Each read used to build the
// whole agent directory to name the agents of its runs, and the directory
// logged "empty" at INFO every time, so an idle conversation with no agent
// wrote a log line a second. The surface now remembers, for the life of the
// process, which findings it has already logged, and keeps the agents' names
// for a minute instead of rebuilding the directory on every read.

// personaSurfaceNamesTTL is how long the names of a conversation's agents are
// kept for one viewer. An agent placed or removed meanwhile is named correctly
// again after at most this long; the directory the page itself reads is never
// served from here.
const personaSurfaceNamesTTL = time.Minute

// personaSurfaceMemory holds what one surface remembers between reads. The
// zero value is ready to use, and a nil memory remembers nothing: every finding
// is then new and every name is read afresh.
type personaSurfaceMemory struct {
	mu     sync.Mutex
	logged map[string]bool
	names  map[string]personaSurfaceNames
}

type personaSurfaceNames struct {
	byPersona map[string]string
	until     time.Time
}

// personaSurfaceMemoryLimit bounds both maps; past it the memory starts over
// rather than grow without end. Starting over costs one repeated log line or
// one rebuilt directory per conversation.
const personaSurfaceMemoryLimit = 4096

// firstTime reports whether key has not been seen by this process before, and
// remembers it.
func (m *personaSurfaceMemory) firstTime(key string) bool {
	if m == nil {
		return true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.logged[key] {
		return false
	}
	if m.logged == nil || len(m.logged) >= personaSurfaceMemoryLimit {
		m.logged = make(map[string]bool)
	}
	m.logged[key] = true
	return true
}

func (m *personaSurfaceMemory) cachedNames(key string, now time.Time) (map[string]string, bool) {
	if m == nil {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	held, ok := m.names[key]
	if !ok || !now.Before(held.until) {
		return nil, false
	}
	return held.byPersona, true
}

func (m *personaSurfaceMemory) keepNames(key string, names map[string]string, now time.Time) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.names == nil || len(m.names) >= personaSurfaceMemoryLimit {
		m.names = make(map[string]personaSurfaceNames)
	}
	m.names[key] = personaSurfaceNames{byPersona: names, until: now.Add(personaSurfaceNamesTTL)}
}

// personaNames is the display name of each agent placed in the conversation,
// by persona id, as this viewer's directory holds them. It is read from the
// directory at most once a minute per viewer and conversation.
func (s *PersonaChatSurface) personaNames(ctx context.Context, p *trust.Principal, room chat.Conversation) map[string]string {
	if s.References == nil || s.Personas == nil || s.Skills == nil {
		return nil
	}
	now := s.personaActionNow()
	key := room.TenantID + "\x00" + room.ID + "\x00" + p.Subject()
	if names, held := s.memory.cachedNames(key, now); held {
		return names
	}
	names := make(map[string]string)
	directory, err := s.Directory(ctx, room.ID)
	if err != nil {
		// Not kept: the next read asks again.
		return names
	}
	for _, profile := range directory.Personas {
		if facts, err := s.References.LookupPersonaReference(ctx, room.TenantID, room.ID, profile.Reference.ID); err == nil {
			names[facts.PersonaID] = profile.Reference.Display
		}
	}
	s.memory.keepNames(key, names, now)
	return names
}
