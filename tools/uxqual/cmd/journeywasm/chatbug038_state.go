package main

import "github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"

// CHATBUG-038. A preview shown under the composer belongs to the draft. The
// resolved previews of share links are kept in the client's state, keyed by the
// link's token; they must go when the draft that held the link is sent,
// cleared or edited so the link is gone, unless a message or thread still on
// screen holds the same link.

// pruneChatEmbedsAfterDraftLocked is called by setDraft, with s.mu held, after
// a conversation's draft went from previous to value. It does nothing when the
// set of links in the draft did not change, so ordinary typing costs one
// signature comparison.
func (s *chatState) pruneChatEmbedsAfterDraftLocked(previous, value string) {
	origin := s.model.EmbedOrigin
	if chatEmbedDraftSignature(previous, origin) == chatEmbedDraftSignature(value, origin) {
		return
	}
	if len(s.embedEntries) == 0 && len(s.model.Embeds) == 0 {
		return
	}
	wanted := map[string]bool{}
	for _, token := range chatEmbedTokens(s.model) {
		wanted[token] = true
	}
	if len(s.model.Embeds) > 0 {
		// A new map: a snapshot of the model may be being read.
		kept := make(map[string]chatui.LinkEmbed, len(s.model.Embeds))
		for token, embed := range s.model.Embeds {
			if wanted[token] {
				kept[token] = embed
			}
		}
		s.model.Embeds = kept
	}
	for token := range s.embedEntries {
		if !wanted[token] {
			delete(s.embedEntries, token)
		}
	}
	if len(s.embedOrder) > 0 {
		kept := make([]string, 0, len(s.embedOrder))
		for _, token := range s.embedOrder {
			if wanted[token] {
				kept = append(kept, token)
			}
		}
		s.embedOrder = kept
	}
}
