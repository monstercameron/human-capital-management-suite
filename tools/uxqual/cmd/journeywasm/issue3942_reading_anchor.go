package main

import (
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// Issue 3942: the place a person was reading when they left a conversation.
//
// The anchor is a message and the offset of that message below the top of the
// view, not a pixel position, so it survives history loading and a changed
// viewport. It lives in this tab's memory only: it is keyed by the signed-in
// tenant and subject (a different person never sees it), bounded, expires, and
// is cleared when the person reaches the newest messages, returns to the
// conversation, or signs out.

const (
	// chatReadingAnchorLimit is how many conversations keep a place. The oldest
	// is forgotten first.
	chatReadingAnchorLimit = 32
	// chatReadingAnchorTTL is how long a place is offered after leaving.
	chatReadingAnchorTTL = 12 * time.Hour
)

type chatReadingAnchor struct {
	PostID   string
	Sequence uint64
	Offset   int
	SavedAt  time.Time
}

type chatReadingAnchorStore struct {
	mu    sync.Mutex
	owner string
	items map[string]chatReadingAnchor
	order []string
}

var chatReadingAnchors chatReadingAnchorStore

func chatReadingOwner(tenant, subject string) string { return tenant + "\x00" + subject }

// useOwner scopes the store to one signed-in person; anyone else's places are
// dropped.
func (s *chatReadingAnchorStore) useOwner(tenant, subject string) {
	owner := chatReadingOwner(tenant, subject)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.owner != owner {
		s.owner, s.items, s.order = owner, nil, nil
	}
}

// remember keeps the place for a conversation, replacing an older one.
func (s *chatReadingAnchorStore) remember(tenant, subject, conversationID string, anchor chatReadingAnchor) {
	if conversationID == "" || anchor.PostID == "" || anchor.Sequence == 0 {
		return
	}
	s.useOwner(tenant, subject)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.items == nil {
		s.items = make(map[string]chatReadingAnchor)
	}
	s.dropLocked(conversationID)
	s.items[conversationID] = anchor
	s.order = append(s.order, conversationID)
	for len(s.order) > chatReadingAnchorLimit {
		delete(s.items, s.order[0])
		s.order = s.order[1:]
	}
}

// take hands back the place for a conversation once and forgets it; a place
// older than its lifetime is not offered.
func (s *chatReadingAnchorStore) take(tenant, subject, conversationID string, now time.Time) (chatReadingAnchor, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.owner != chatReadingOwner(tenant, subject) {
		return chatReadingAnchor{}, false
	}
	anchor, ok := s.items[conversationID]
	if !ok {
		return chatReadingAnchor{}, false
	}
	s.dropLocked(conversationID)
	if now.Sub(anchor.SavedAt) > chatReadingAnchorTTL {
		return chatReadingAnchor{}, false
	}
	return anchor, true
}

// forget drops one conversation's place (the person was at the newest messages).
func (s *chatReadingAnchorStore) forget(conversationID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropLocked(conversationID)
}

// clear drops every place (sign-out or a different person).
func (s *chatReadingAnchorStore) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.owner, s.items, s.order = "", nil, nil
}

func (s *chatReadingAnchorStore) dropLocked(conversationID string) {
	if _, ok := s.items[conversationID]; !ok {
		return
	}
	delete(s.items, conversationID)
	for i, id := range s.order {
		if id == conversationID {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
}

func (s *chatReadingAnchorStore) size() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items)
}

// chatReadingAnchorFor builds the place from what the view reported: the
// message at the top and its offset. A message that is not in the loaded
// window, or has no sequence, cannot anchor anything.
func chatReadingAnchorFor(messages []chatui.Message, postID string, offset int, now time.Time) (chatReadingAnchor, bool) {
	for _, message := range messages {
		if message.ID == postID && message.Sequence > 0 {
			return chatReadingAnchor{PostID: postID, Sequence: message.Sequence, Offset: offset, SavedAt: now}, true
		}
	}
	return chatReadingAnchor{}, false
}

// chatReadingResolve decides where a return lands in the messages just read
// around the anchor: the message itself when it is still there, otherwise the
// closest message the person can open (the first at or after its position, else
// the last before it). ok is false when there is nothing to show.
func chatReadingResolve(anchor chatReadingAnchor, messages []chatui.Message) (postID, status string, offset int, ok bool) {
	var nearest, last string
	for _, message := range messages {
		if message.ID == anchor.PostID {
			return message.ID, chatui.ReadingResumed, anchor.Offset, true
		}
		if message.Sequence == 0 {
			continue
		}
		last = message.ID
		if nearest == "" && message.Sequence >= anchor.Sequence {
			nearest = message.ID
		}
	}
	if nearest == "" {
		nearest = last
	}
	if nearest == "" {
		return "", "", 0, false
	}
	return nearest, chatui.ReadingNearby, 0, true
}

// chatReadingMayResume says whether a return may use the remembered place.
// Unread messages, an explicit target (search result, message link) and a
// conversation the person cannot read all win over it.
func chatReadingMayResume(unread int, explicitTarget, readable bool) bool {
	return unread <= 0 && !explicitTarget && readable
}

// chatConversationUnread is the unread count the model holds for a conversation.
func chatConversationUnread(conversations []chatui.Conversation, id string) int {
	for _, conversation := range conversations {
		if conversation.ID == id {
			return conversation.Unread
		}
	}
	return 0
}
