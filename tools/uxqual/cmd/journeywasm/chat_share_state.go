package main

import (
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

type chatShareAttempt struct {
	Generation                               uint64
	SourceRoom, SourcePost, Destination, Key string
	// Note is the sharer's trimmed note (issue 3919); the key is minted for the
	// destination and this exact note, so a retry of the same share is the same
	// request and an edited note is a new one.
	Note   string
	Config journeyclient.Config
}

func chatShareKey(destination, note string) string {
	return destination + "\x00" + strings.TrimSpace(note)
}

func (s *chatState) openShare(postID string) (uint64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.model.SelectedID == "" {
		return 0, false
	}
	for _, message := range s.model.Messages {
		if message.ID != postID || strings.TrimSpace(message.Body) == "" {
			continue
		}
		copy := message
		s.shareGeneration++
		s.shareAttemptKey, s.shareAttemptDestination = "", ""
		s.shareAttemptKeys = nil
		s.model.SharePostID, s.model.ShareSourceRoomID, s.model.ShareSource = postID, s.model.SelectedID, &copy
		s.model.ShareDestinationID, s.model.ShareQuery, s.model.ShareError, s.model.ShareNote = "", "", "", ""
		s.model.ShareDestinations = nil
		s.model.ShareLoading, s.model.SharePending = true, false
		s.model.ShareVersion++
		s.model.MenuID = ""
		return s.shareGeneration, true
	}
	return 0, false
}

func (s *chatState) closeShare() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.model.SharePending {
		return
	}
	s.shareGeneration++
	s.model.SharePostID, s.model.ShareSourceRoomID, s.model.ShareDestinationID, s.model.ShareQuery, s.model.ShareError, s.model.ShareNote = "", "", "", "", "", ""
	s.model.ShareSource, s.model.ShareDestinations = nil, nil
	s.model.ShareLoading = false
	s.model.ShareVersion++
	s.shareAttemptKey, s.shareAttemptDestination = "", ""
	s.shareAttemptKeys = nil
}

func (s *chatState) shareListing(generation uint64, rooms []chatui.Conversation, errText string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if generation != s.shareGeneration || s.model.SharePostID == "" || s.model.SelectedID != s.model.ShareSourceRoomID {
		return false
	}
	s.model.ShareLoading = false
	s.model.ShareError = errText
	s.model.ShareVersion++
	if errText == "" {
		s.model.ShareDestinations = rooms
		selected := s.model.ShareDestinationID
		if selected != "" {
			found := false
			for _, room := range rooms {
				if room.ID == selected {
					found = true
					break
				}
			}
			if !found {
				s.model.ShareDestinationID = ""
			}
		}
	}
	return true
}

func (s *chatState) shareFilter(query string) {
	s.mu.Lock()
	if s.model.SharePending {
		s.mu.Unlock()
		return
	}
	// #3918: the filter only narrows the list. The checked destination is the
	// intended audience and stays until the person removes it.
	s.model.ShareQuery = query
	s.model.ShareVersion++
	s.mu.Unlock()
}

func (s *chatState) shareSelect(destination string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.model.SharePending || s.model.ShareLoading {
		return false
	}
	if destination == "" {
		// #3918: removing the checked destination before sharing.
		s.model.ShareDestinationID, s.model.ShareError = "", ""
		s.model.ShareVersion++
		s.shareAttemptDestination, s.shareAttemptKey = "", ""
		return true
	}
	for _, room := range s.model.ShareDestinations {
		if room.ID == destination && chatui.ShareDestinationEligible(room, s.model.ShareSourceRoomID) {
			s.model.ShareDestinationID, s.model.ShareError = destination, ""
			s.model.ShareVersion++
			s.shareAttemptDestination = destination
			s.shareAttemptKey = s.shareAttemptKeys[chatShareKey(destination, s.model.ShareNote)]
			return true
		}
	}
	return false
}

// shareNote records the note typed in the share dialog. The text is kept
// through a refusal and is dropped only when the dialog closes or the share is
// sent. A note over the limit is cut at the limit, as the field does.
func (s *chatState) shareNote(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.model.SharePending || s.model.SharePostID == "" {
		return
	}
	if runes := []rune(text); len(runes) > chatui.ShareNoteMaxRunes {
		text = string(runes[:chatui.ShareNoteMaxRunes])
	}
	s.model.ShareNote = text
	s.model.ShareVersion++
	s.shareAttemptKey = s.shareAttemptKeys[chatShareKey(s.model.ShareDestinationID, text)]
}

func (s *chatState) beginShare(mint func() string) (chatShareAttempt, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.model
	if m.SharePostID == "" || m.ShareDestinationID == "" || m.ShareSourceRoomID != m.SelectedID || m.SharePending || m.ShareLoading || m.ShareSource == nil || strings.TrimSpace(m.ShareSource.Body) == "" {
		return chatShareAttempt{}, false
	}
	eligible := false
	for _, room := range m.ShareDestinations {
		if room.ID == m.ShareDestinationID && chatui.ShareDestinationEligible(room, m.ShareSourceRoomID) {
			eligible = true
			break
		}
	}
	if !eligible {
		return chatShareAttempt{}, false
	}
	if s.shareAttemptKeys == nil {
		s.shareAttemptKeys = make(map[string]string)
	}
	note := strings.TrimSpace(m.ShareNote)
	keyed := chatShareKey(m.ShareDestinationID, note)
	if s.shareAttemptKeys[keyed] == "" {
		s.shareAttemptKeys[keyed] = mint()
	}
	s.shareAttemptKey, s.shareAttemptDestination = s.shareAttemptKeys[keyed], m.ShareDestinationID
	s.model.SharePending, s.model.ShareError = true, ""
	s.model.ShareVersion++
	return chatShareAttempt{Generation: s.shareGeneration, SourceRoom: m.ShareSourceRoomID, SourcePost: m.SharePostID, Destination: m.ShareDestinationID, Key: s.shareAttemptKey, Note: note, Config: s.cfg}, true
}

func (s *chatState) finishShare(attempt chatShareAttempt, errText string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if attempt.Generation != s.shareGeneration || s.model.SelectedID != attempt.SourceRoom || s.model.SharePostID != attempt.SourcePost || s.model.ShareSourceRoomID != attempt.SourceRoom || s.model.ShareDestinationID != attempt.Destination || s.cfg.Tenant != attempt.Config.Tenant || s.cfg.Subject != attempt.Config.Subject {
		return false
	}
	s.model.SharePending = false
	s.model.ShareVersion++
	if errText != "" {
		s.model.ShareError = errText
		return true
	}
	s.shareGeneration++
	s.model.SharePostID, s.model.ShareSourceRoomID, s.model.ShareDestinationID, s.model.ShareQuery, s.model.ShareError, s.model.ShareNote = "", "", "", "", "", ""
	s.model.ShareSource, s.model.ShareDestinations = nil, nil
	s.shareAttemptKey, s.shareAttemptDestination = "", ""
	s.shareAttemptKeys = nil
	return true
}
