package chatui

import "sync"

// Issue 3993. A mention picked from the composer menu is kept apart from the
// draft text: an agent as a detached chip, a colleague as a reference, and a
// person outside the conversation as the warning under the draft. All of that
// lived in the mounted workspace, so leaving Chat (Chat, Agents, Chat) kept the
// text and lost who it was addressed to, and typing in another room using the
// same composer dropped the first room's mentions.
//
// Two rules fix it. Reconciling a keystroke only touches the open
// conversation's mentions (mention.go, chatbug071_outside.go). And the mounted
// workspace hands its mentions to mentionCarry when it unmounts, and the next
// one takes them back, after checking each against the draft and the people and
// agents it can now see. The carry holds only what the signed-in person already
// saw in their own composer, is kept for that person alone, and is dropped with
// their drafts (browserDrafts.clear).

var mentionCarry = struct {
	sync.Mutex
	owner     string
	personas  []personaDraftMention
	outsiders []personaDraftMention
}{}

func clearMentionCarry() {
	mentionCarry.Lock()
	mentionCarry.owner, mentionCarry.personas, mentionCarry.outsiders = "", nil, nil
	mentionCarry.Unlock()
}

// inOpenConversation reports whether a keystroke in the composer can speak for
// item: it belongs to the conversation the composer is showing. An unscoped box
// or item, as in older callers, is treated as belonging to the open one.
func (s mentionStore) inOpenConversation(item personaDraftMention) bool {
	return s.box.conversationID == "" || item.ConversationID == "" || item.ConversationID == s.box.conversationID
}

// Carry saves the mentions for the next mount of the workspace.
func (s mentionStore) Carry() {
	if s.box == nil || s.box.carryOwner == "" {
		return
	}
	mentionCarry.Lock()
	defer mentionCarry.Unlock()
	mentionCarry.owner = s.box.carryOwner
	mentionCarry.personas = append([]personaDraftMention(nil), s.box.personas...)
	mentionCarry.outsiders = append([]personaDraftMention(nil), s.box.outsiders...)
}

// RestoreCarried gives a newly mounted workspace the mentions its drafts were
// written with. owner names the signed-in person; draftOf returns the text of a
// conversation's draft. It runs once per mounted box and, like SettleOutsiders,
// before the render reads the store.
//
// A person is kept only while their name is still in that conversation's draft.
// An agent chip is kept, and marked Restored so the first answer from the agent
// lookup can confirm it is still installed and available here
// (SettleRestoredAgents); the service checks it again when the message is sent.
func (s mentionStore) RestoreCarried(owner string, draftOf func(string) string) {
	if s.box == nil {
		return
	}
	s.box.carryOwner = owner
	if s.box.restored || owner == "" {
		return
	}
	s.box.restored = true
	mentionCarry.Lock()
	defer mentionCarry.Unlock()
	if mentionCarry.owner != owner {
		return
	}
	for _, item := range mentionCarry.personas {
		if item.Detached {
			item.Restored = true
		} else if !draftMentionPresent(draftOf(item.ConversationID), item) {
			continue
		}
		s.box.personas = append(s.box.personas, item)
	}
	for _, item := range mentionCarry.outsiders {
		if chatbug071NamedIn(draftOf(item.ConversationID), item.Reference.Display) {
			s.box.outsiders = append(s.box.outsiders, item)
		}
	}
}

// SettleRestoredAgents drops a restored agent chip of the open conversation
// that the answered agent lookup no longer offers: the agent was removed from
// the conversation, uninstalled, or is not available to this person any more.
// Until the lookup answers the chip stays, and the service refuses it at send
// if it is gone. It reports whether it dropped one.
func (s mentionStore) SettleRestoredAgents(m Model) bool {
	if s.box == nil || m.PersonaLookup != PersonaLookupReady || (m.PersonaLookupConversationID != "" && m.PersonaLookupConversationID != m.SelectedID) {
		return false
	}
	dropped := false
	kept := s.box.personas[:0]
	for _, item := range s.box.personas {
		if item.Restored && item.ConversationID == m.SelectedID && item.Reference.Kind != "PERSON_MENTION" {
			offered := false
			for _, persona := range m.ResolvedPersonaMentions {
				if persona.Reference.ID == item.Reference.ID && persona.Reference.TenantID == item.Reference.TenantID {
					offered = true
					break
				}
			}
			if !offered {
				dropped = true
				continue
			}
			item.Restored = false
		}
		kept = append(kept, item)
	}
	s.box.personas = kept
	return dropped
}
