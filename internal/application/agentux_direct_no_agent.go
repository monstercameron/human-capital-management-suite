package application

import (
	"sync"
	"time"
)

// A message in a direct conversation between two people names no agent, yet
// every one of them was taken for a possible question to an agent: the other
// person was looked up as an agent, the lookup failed, and the failure was
// logged as an agent invocation failure. Two people talking wrote a warning
// per message.
//
// The other member of a two-person direct conversation is looked up once. When
// the registry says it is not an agent, the conversation is remembered as
// holding none, and its messages are left alone until the memory lapses.

// personaDirectQuietTTL is how long a direct conversation found to hold no
// agent is not looked at again. An agent's own conversation is created with the
// agent in it, so a conversation between two people does not turn into one; the
// short life only bounds how long a late registration could go unnoticed.
const personaDirectQuietTTL = time.Minute

// personaDirectQuietLimit bounds the memory; past it the memory starts over,
// which costs one more lookup per conversation.
const personaDirectQuietLimit = 4096

// personaDirectQuiet remembers direct conversations that hold no agent. A nil
// memory remembers nothing.
type personaDirectQuiet struct {
	mu    sync.Mutex
	until map[string]time.Time
}

func (q *personaDirectQuiet) known(tenant, conversation string, now time.Time) bool {
	if q == nil {
		return false
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	until, held := q.until[tenant+"\x00"+conversation]
	return held && now.Before(until)
}

func (q *personaDirectQuiet) remember(tenant, conversation string, now time.Time) {
	if q == nil {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.until == nil || len(q.until) >= personaDirectQuietLimit {
		q.until = make(map[string]time.Time)
	}
	q.until[tenant+"\x00"+conversation] = now.Add(personaDirectQuietTTL)
}
