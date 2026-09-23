package main

import (
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

const chatEmbedLimit = 24
const chatEmbedFreshFor = 30 * time.Second

type chatEmbedEntry struct {
	value   chatui.LinkEmbed
	at      time.Time
	pending bool
}

func (s *chatState) startChatEmbedTimer() (uint64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.embedTimerRunning || len(chatEmbedTokens(s.model)) == 0 {
		return s.embedEpoch, false
	}
	s.embedTimerRunning = true
	return s.embedEpoch, true
}

func (s *chatState) continueChatEmbedTimer(epoch uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if epoch != s.embedEpoch {
		return false
	}
	if len(chatEmbedTokens(s.model)) == 0 {
		s.embedTimerRunning = false
		return false
	}
	return true
}

func chatEmbedTokens(model chatui.Model) []string {
	seen := map[string]bool{}
	out := make([]string, 0, chatEmbedLimit)
	add := func(body string) {
		for _, l := range chatui.ShareLocators(body, model.EmbedOrigin) {
			if l.Token != "" && !seen[l.Token] && len(out) < chatEmbedLimit {
				out = append(out, l.Token)
				seen[l.Token] = true
			}
		}
	}
	add(model.Draft)
	for i := len(model.ThreadMessages) - 1; i >= 0 && len(out) < chatEmbedLimit; i-- {
		add(model.ThreadMessages[i].Body)
	}
	for i := len(model.Messages) - 1; i >= 0 && len(out) < chatEmbedLimit; i-- {
		add(model.Messages[i].Body)
	}
	return out
}

func chatEmbedFingerprint(model chatui.Model) string {
	return model.SelectedID + "|" + strconv.FormatUint(model.EmbedRevision, 10) + "|" + strings.Join(chatEmbedKeys(model), "|")
}

func chatEmbedKeys(model chatui.Model) []string {
	seen := map[string]bool{}
	out := make([]string, 0, chatEmbedLimit)
	add := func(body string) {
		for _, locator := range chatui.ShareLocators(body, model.EmbedOrigin) {
			key := locator.Token
			if locator.LegacyPost != "" {
				key = "legacy:" + locator.LegacyPost
			}
			if key != "" && !seen[key] && len(out) < chatEmbedLimit {
				out = append(out, key)
				seen[key] = true
			}
		}
	}
	add(model.Draft)
	for i := len(model.ThreadMessages) - 1; i >= 0 && len(out) < chatEmbedLimit; i-- {
		add(model.ThreadMessages[i].Body)
	}
	for i := len(model.Messages) - 1; i >= 0 && len(out) < chatEmbedLimit; i-- {
		add(model.Messages[i].Body)
	}
	return out
}

func chatEmbedDraftSignature(body, origin string) string {
	sig := strings.Join(chatEmbedKeys(chatui.Model{Draft: body, EmbedOrigin: origin}), "|")
	for _, ref := range chatui.ChannelReferences(body, origin) {
		sig += "|channel:" + strconv.Itoa(len(ref.ID)) + ":" + ref.ID
	}
	return sig
}

// claimChatEmbeds returns tokens that need a fresh authorization read. A
// cached body expires quickly and is hidden while revalidation is pending.
func (s *chatState) claimChatEmbeds(now time.Time) ([]string, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tokens := chatEmbedTokens(s.model)
	if s.embedEntries == nil {
		s.embedEntries = make(map[string]chatEmbedEntry)
	}
	if s.model.Embeds == nil {
		s.model.Embeds = make(map[string]chatui.LinkEmbed)
	}
	claims := []string{}
	for _, token := range tokens {
		entry, exists := s.embedEntries[token]
		if exists && (entry.pending || now.Sub(entry.at) < chatEmbedFreshFor) {
			s.model.Embeds[token] = entry.value
			continue
		}
		if !exists && len(s.embedOrder) >= chatEmbedLimit {
			old := s.embedOrder[0]
			s.embedOrder = s.embedOrder[1:]
			delete(s.embedEntries, old)
			delete(s.model.Embeds, old)
		}
		if !exists {
			s.embedOrder = append(s.embedOrder, token)
		}
		value := chatui.LinkEmbed{Token: token, State: "loading"}
		s.embedEntries[token] = chatEmbedEntry{value: value, at: now, pending: true}
		s.model.Embeds[token] = value
		claims = append(claims, token)
	}
	return claims, s.embedEpoch
}

func (s *chatState) finishChatEmbed(epoch uint64, token string, value chatui.LinkEmbed, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if epoch != s.embedEpoch {
		return false
	}
	entry, ok := s.embedEntries[token]
	if !ok || !entry.pending {
		return false
	}
	entry.value, entry.at, entry.pending = value, now, false
	s.embedEntries[token] = entry
	if s.model.Embeds == nil {
		s.model.Embeds = make(map[string]chatui.LinkEmbed)
	}
	s.model.Embeds[token] = value
	return true
}

func (s *chatState) invalidateChatEmbeds() {
	s.mu.Lock()
	s.embedEpoch++
	s.embedTimerRunning = false
	s.embedEntries, s.embedOrder = nil, nil
	s.model.Embeds = nil
	s.model.EmbedRevision++
	s.mu.Unlock()
}

// markChatEmbedDraftChange reports a change in recognized locator set, so
// ordinary typing never repaints the controlled composer.
func (s *chatState) markChatEmbedDraftChange(room, body string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if room == "" || s.model.SelectedID != room {
		return false
	}
	sig := chatEmbedDraftSignature(body, s.model.EmbedOrigin)
	if sig == s.embedDraftSignature {
		return false
	}
	s.embedDraftSignature = sig
	return true
}

func (s *chatState) embedEpochActive(epoch uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.embedEpoch == epoch
}

func (s *chatState) claimOpenShareFragment(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if token == "" || s.openedShareFragment == token {
		return false
	}
	s.openedShareFragment = token
	return true
}
