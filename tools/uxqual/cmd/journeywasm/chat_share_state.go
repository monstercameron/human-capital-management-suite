package main

import (
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

type chatShareAttempt struct {
	Generation                               uint64
	SourceRoom, SourcePost, Destination, Key string
	Config                                   journeyclient.Config
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
		s.model.ShareDestinationID, s.model.ShareQuery, s.model.ShareError = "", "", ""
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
	s.model.SharePostID, s.model.ShareSourceRoomID, s.model.ShareDestinationID, s.model.ShareQuery, s.model.ShareError = "", "", "", "", ""
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
	s.model.ShareQuery = query
	if s.model.ShareDestinationID != "" {
		visible := false
		for _, room := range s.model.ShareDestinations {
			if room.ID == s.model.ShareDestinationID && strings.Contains(strings.ToLower(room.Name), strings.ToLower(strings.TrimSpace(query))) {
				visible = true
				break
			}
		}
		if !visible {
			s.model.ShareDestinationID = ""
		}
	}
	s.model.ShareVersion++
	s.mu.Unlock()
}

func (s *chatState) shareSelect(destination string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.model.SharePending || s.model.ShareLoading {
		return false
	}
	for _, room := range s.model.ShareDestinations {
		if room.ID == destination && room.ID != s.model.ShareSourceRoomID && (room.Kind == chatui.PublicChannel || room.Kind == chatui.PrivateChannel) {
			s.model.ShareDestinationID, s.model.ShareError = destination, ""
			s.model.ShareVersion++
			s.shareAttemptDestination = destination
			s.shareAttemptKey = s.shareAttemptKeys[destination]
			return true
		}
	}
	return false
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
		if room.ID == m.ShareDestinationID && (room.Kind == chatui.PublicChannel || room.Kind == chatui.PrivateChannel) {
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
	if s.shareAttemptKeys[m.ShareDestinationID] == "" {
		s.shareAttemptKeys[m.ShareDestinationID] = mint()
	}
	s.shareAttemptKey, s.shareAttemptDestination = s.shareAttemptKeys[m.ShareDestinationID], m.ShareDestinationID
	s.model.SharePending, s.model.ShareError = true, ""
	s.model.ShareVersion++
	return chatShareAttempt{Generation: s.shareGeneration, SourceRoom: m.ShareSourceRoomID, SourcePost: m.SharePostID, Destination: m.ShareDestinationID, Key: s.shareAttemptKey, Config: s.cfg}, true
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
	s.model.SharePostID, s.model.ShareSourceRoomID, s.model.ShareDestinationID, s.model.ShareQuery, s.model.ShareError = "", "", "", "", ""
	s.model.ShareSource, s.model.ShareDestinations = nil, nil
	s.shareAttemptKey, s.shareAttemptDestination = "", ""
	s.shareAttemptKeys = nil
	return true
}
