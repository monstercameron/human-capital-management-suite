package main

import (
	"sort"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

const chatThreadWindow = 150

func boundLiveChatThread(model *chatui.Model) bool {
	if len(model.ThreadMessages) <= chatThreadWindow {
		return false
	}
	model.ThreadMessages = append([]chatui.Message(nil), model.ThreadMessages[len(model.ThreadMessages)-chatThreadWindow:]...)
	model.ThreadHasOlder = true
	return true
}

func liveChatThreadAccepts(model *chatui.Model, sequence uint64) bool {
	return !model.ThreadHasNewer || len(model.ThreadMessages) == 0 || sequence <= model.ThreadMessages[len(model.ThreadMessages)-1].Sequence
}

func (s *chatState) boundLiveThread() {
	if boundLiveChatThread(&s.model) && len(s.model.ThreadMessages) > 0 {
		s.threadBeforeSequence = s.model.ThreadMessages[0].Sequence
	}
}

type chatThreadPageDirection uint8

const (
	chatThreadLatest chatThreadPageDirection = iota
	chatThreadOlder
	chatThreadNewer
)

// applyThreadPage folds one bounded conversation page into an open thread.
// A page with no matching replies still advances its scan edge, so a sparse
// thread remains reachable without scanning the whole conversation at once.
func (s *chatState) applyThreadPage(conversation, root string, generation uint64, direction chatThreadPageDirection, posts []*chatv1.Post, nextCursor, locale string, directory map[string]string, now time.Time, expectedEdge ...uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.generation != generation || s.model.SelectedID != conversation || !s.model.ShowThread || s.model.ThreadParentID != root {
		return false
	}
	if len(expectedEdge) > 0 {
		if !s.threadPaging {
			return false
		}
		if direction == chatThreadOlder && s.threadBeforeSequence != expectedEdge[0] {
			return false
		}
		if direction == chatThreadNewer && s.threadAfterSequence != expectedEdge[0] {
			return false
		}
	}
	var first, last uint64
	for _, post := range posts {
		if post == nil || post.GetSequence() == 0 {
			continue
		}
		if first == 0 || post.GetSequence() < first {
			first = post.GetSequence()
		}
		if post.GetSequence() > last {
			last = post.GetSequence()
		}
	}
	replies := chatThreadMessages(posts, root, locale, directory, now)
	sort.Slice(replies, func(i, j int) bool { return replies[i].Sequence < replies[j].Sequence })
	if direction == chatThreadLatest {
		s.model.ThreadMessages = replies
		s.model.ThreadLoading = false
		s.model.ThreadHasOlder = nextCursor != ""
		s.model.ThreadHasNewer = false
		s.threadBeforeSequence, s.threadAfterSequence = first, last
	} else {
		seen := make(map[string]bool, len(s.model.ThreadMessages))
		for _, message := range s.model.ThreadMessages {
			seen[message.ID] = true
		}
		for _, message := range replies {
			if !seen[message.ID] {
				s.model.ThreadMessages = append(s.model.ThreadMessages, message)
			}
		}
		sort.Slice(s.model.ThreadMessages, func(i, j int) bool { return s.model.ThreadMessages[i].Sequence < s.model.ThreadMessages[j].Sequence })
		if direction == chatThreadOlder {
			if first > 0 {
				s.threadBeforeSequence = first
			}
			s.model.ThreadHasOlder = nextCursor != ""
		} else {
			if last > 0 {
				s.threadAfterSequence = last
			}
			s.model.ThreadHasNewer = nextCursor != ""
		}
	}
	if len(s.model.ThreadMessages) > chatThreadWindow {
		if direction == chatThreadOlder {
			s.model.ThreadMessages = append([]chatui.Message(nil), s.model.ThreadMessages[:chatThreadWindow]...)
			s.model.ThreadHasNewer = true
			s.threadAfterSequence = s.model.ThreadMessages[len(s.model.ThreadMessages)-1].Sequence
		} else {
			s.model.ThreadMessages = append([]chatui.Message(nil), s.model.ThreadMessages[len(s.model.ThreadMessages)-chatThreadWindow:]...)
			s.model.ThreadHasOlder = true
			s.threadBeforeSequence = s.model.ThreadMessages[0].Sequence
		}
	}
	pruneChatSeen(&s.cursor, s.model.Messages, s.model.ThreadMessages)
	return true
}

func (s *chatState) threadPageEdge(direction chatThreadPageDirection) (conversation, root string, generation, sequence uint64, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.threadPaging || !s.model.ShowThread || s.model.ThreadParentID == "" || s.model.SelectedID == "" {
		return "", "", 0, 0, false
	}
	if direction == chatThreadOlder {
		ok = s.model.ThreadHasOlder && s.threadBeforeSequence > 0
		if ok {
			s.threadPaging = true
		}
		return s.model.SelectedID, s.model.ThreadParentID, s.generation, s.threadBeforeSequence, ok
	}
	ok = s.model.ThreadHasNewer && s.threadAfterSequence > 0
	if ok {
		s.threadPaging = true
	}
	return s.model.SelectedID, s.model.ThreadParentID, s.generation, s.threadAfterSequence, ok
}

func (s *chatState) finishThreadPage(generation uint64) {
	s.mu.Lock()
	if s.generation == generation {
		s.threadPaging = false
	}
	s.mu.Unlock()
}
