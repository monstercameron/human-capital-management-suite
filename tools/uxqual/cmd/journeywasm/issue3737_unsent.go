package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// Issue 3737: the sender's messages that have not been delivered.
//
// A send used to empty the composer and, when it failed, leave nothing on the
// page. Now every send is an entry here from the moment Enter is pressed: it
// shows as a row while it is in flight, stays as "Not sent" when it fails, and
// is removed when the server confirms it (the confirmed post replaces it).
//
// Each entry owns one idempotency key for its whole life. Retry, and the
// automatic resend after the connection comes back, send the same key, so a
// post the server did commit before the connection dropped is returned, not
// posted a second time. Two sends of the same words are two entries with two
// keys: they are two messages.
//
// The store is memory only. It holds the sender's text for this page session,
// belongs to one signed-in person, and is emptied on sign-out or when another
// person signs in.

// chatUnsentAutoTries is how many times the client resends an offline entry by
// itself before leaving it to the person's own Retry.
const chatUnsentAutoTries = 5

type chatUnsentEntry struct {
	ID, Conversation, Body, Key string
	Tenant, Subject             string
	Audience                    []string
	Restricted                  bool

	reason   chatui.UnsentReason
	inFlight bool
	autos    int
}

type chatUnsentStore struct {
	mu      sync.Mutex
	seq     int
	entries []*chatUnsentEntry
}

var chatUnsent chatUnsentStore

// add records a send that is about to leave and returns its id. The entry is
// in flight until begin/fail say otherwise.
func (s *chatUnsentStore) add(e chatUnsentEntry) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	e.ID = fmt.Sprintf("u%d", s.seq)
	e.Key = fmt.Sprintf("wasm-send-%d-%d", time.Now().UnixNano(), s.seq)
	e.reason, e.inFlight, e.autos = chatui.UnsentSending, false, 0
	s.entries = append(s.entries, &e)
	return e.ID
}

func (s *chatUnsentStore) find(id string) *chatUnsentEntry {
	for _, e := range s.entries {
		if e.ID == id {
			return e
		}
	}
	return nil
}

// get returns a copy of the entry.
func (s *chatUnsentStore) get(id string) (chatUnsentEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.find(id); e != nil {
		return *e, true
	}
	return chatUnsentEntry{}, false
}

// begin claims the entry for one delivery attempt. It refuses while another
// attempt is in flight, which is what stops a double click on Retry, or Retry
// racing the automatic resend, from posting twice.
func (s *chatUnsentStore) begin(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.find(id)
	if e == nil || e.inFlight {
		return false
	}
	e.inFlight, e.reason = true, chatui.UnsentSending
	return true
}

// fail ends an attempt and keeps the entry as a "Not sent" row.
func (s *chatUnsentStore) fail(id string, reason chatui.UnsentReason) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.find(id); e != nil {
		e.inFlight, e.reason = false, reason
		if reason == chatui.UnsentOffline {
			e.autos++
		}
	}
}

// manual re-arms the automatic resend: the person asked for this entry.
func (s *chatUnsentStore) manual(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.find(id); e != nil {
		e.autos = 0
	}
}

// rearmOffline gives every offline entry its automatic tries again. The browser
// saying the connection is back is new information, whatever was tried before.
func (s *chatUnsentStore) rearmOffline() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.entries {
		if e.reason == chatui.UnsentOffline {
			e.autos = 0
		}
	}
}

func (s *chatUnsentStore) remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, e := range s.entries {
		if e.ID == id {
			s.entries = append(s.entries[:i], s.entries[i+1:]...)
			return
		}
	}
}

// forget drops everything: sign-out, or another person signing in.
func (s *chatUnsentStore) forget() {
	s.mu.Lock()
	s.entries = nil
	s.mu.Unlock()
}

// rows is what the page shows, in the order the messages were sent.
func (s *chatUnsentStore) rows() []chatui.UnsentMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.entries) == 0 {
		return nil
	}
	out := make([]chatui.UnsentMessage, 0, len(s.entries))
	for _, e := range s.entries {
		reason := e.reason
		if e.inFlight {
			reason = chatui.UnsentSending
		}
		out = append(out, chatui.UnsentMessage{ID: e.ID, ConversationID: e.Conversation, Body: e.Body, Reason: reason})
	}
	return out
}

// resendable lists, oldest first, the offline entries the client may resend by
// itself for this person. Entries of anyone else are dropped on the way: the
// text of one person is never sent under another's session.
func (s *chatUnsentStore) resendable(tenant, subject string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.entries[:0]
	var ids []string
	for _, e := range s.entries {
		if !chatDraftIdentityMatches(e.Tenant, e.Subject, tenant, subject) {
			continue
		}
		kept = append(kept, e)
		if !e.inFlight && e.reason == chatui.UnsentOffline && e.autos < chatUnsentAutoTries {
			ids = append(ids, e.ID)
		}
	}
	s.entries = kept
	return ids
}

// chatUnsentReason says whether a failed send looks like a connection problem,
// which the client will retry by itself, or an answer from the server, which
// only the person decides about.
func chatUnsentReason(err error, online bool) chatui.UnsentReason {
	if !online {
		return chatui.UnsentOffline
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return chatui.UnsentOffline
	}
	// An error that is not a server answer at all is the transport failing.
	if _, answered := status.FromError(err); !answered {
		return chatui.UnsentOffline
	}
	switch status.Code(err) {
	case codes.Unavailable, codes.DeadlineExceeded:
		return chatui.UnsentOffline
	}
	return chatui.UnsentFailed
}
