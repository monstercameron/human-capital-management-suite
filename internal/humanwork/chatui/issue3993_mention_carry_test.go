package chatui

import (
	stdhtml "html"
	"strings"
	"testing"
)

func issue3993Evelyn(conversation string) personaDraftMention {
	return personaDraftMention{Target: "chat-composer", ConversationID: conversation, Start: 24, End: 38,
		Reference: ChatReference{Kind: "PERSON_MENTION", TenantID: "tenant", ID: "evelyn", Display: "Evelyn Morgan", ConversationID: conversation}}
}

const issue3993Draft = "Could you review this, @Evelyn Morgan "

// Typing a different draft in another room used to drop the first room's
// mentions: the reconcile ran the typed text against every conversation's.
func TestIssue3993_TypingInAnotherRoomKeepsTheFirstRoomsMentions(t *testing.T) {
	store := mentionStore{box: &mentionBox{conversationID: "design"}}
	store.box.outsiders = []personaDraftMention{issue3993Evelyn("benefits")}
	member := issue3993Evelyn("benefits")
	member.Reference.ID, member.Reference.Display = "loretta", "Loretta Haynes"
	chip := personaDraftMention{Target: "chat-composer", ConversationID: "benefits", Detached: true, Reference: ChatReference{Kind: "AGENT_MENTION", TenantID: "tenant", ID: "policy", Display: "Policy Helper", ConversationID: "benefits"}}
	store.box.personas = []personaDraftMention{member, chip}

	store.ReconcilePersonas("chat-composer", "A temporary draft for design")

	if len(store.box.outsiders) != 1 || store.box.outsiders[0].Reference.ID != "evelyn" {
		t.Errorf("typing in design dropped the nonmember warning of benefits: %+v", store.box.outsiders)
	}
	if len(store.box.personas) != 2 {
		t.Errorf("typing in design dropped benefits' references: %+v", store.box.personas)
	}
}

// Editing the open draft in place still follows the name and drops it once the
// name is gone.
func TestIssue3993_EditingTheOpenDraftStillFollowsTheName(t *testing.T) {
	store := mentionStore{box: &mentionBox{conversationID: "design"}}
	store.box.outsiders = []personaDraftMention{issue3993Evelyn("design")}
	store.ReconcilePersonas("chat-composer", "Quickly: "+issue3993Draft)
	if len(store.box.outsiders) != 1 {
		t.Fatalf("inserting text before the name dropped the warning: %+v", store.box.outsiders)
	}
	store.ReconcilePersonas("chat-composer", "Could you review this")
	if len(store.box.outsiders) != 0 {
		t.Errorf("a removed name kept its warning: %+v", store.box.outsiders)
	}
}

// Chat, Agents, Chat: the workspace unmounts and a new one mounts with an empty
// box. The warning, the colleague reference and the agent chip come back with
// the drafts.
func TestIssue3993_MentionsSurviveLeavingAndReturningToChat(t *testing.T) {
	clearMentionCarry()
	t.Cleanup(clearMentionCarry)
	drafts := map[string]string{"benefits": issue3993Draft, "design": "Hello @Loretta Haynes "}
	draftOf := func(id string) string { return drafts[id] }
	loretta := personaDraftMention{Target: "chat-composer", ConversationID: "design", Start: 6, End: 21,
		Reference: ChatReference{Kind: "PERSON_MENTION", TenantID: "tenant", ID: "loretta", Display: "Loretta Haynes", ConversationID: "design"}}
	chip := personaDraftMention{Target: "chat-composer", ConversationID: "benefits", Detached: true,
		Reference: ChatReference{Kind: "AGENT_MENTION", TenantID: "tenant", ID: "policy", Display: "Policy Helper", ConversationID: "benefits"}}

	first := mentionStore{box: &mentionBox{conversationID: "benefits"}}
	first.RestoreCarried("tenant\x00walt", draftOf)
	first.box.outsiders = []personaDraftMention{issue3993Evelyn("benefits")}
	first.box.personas = []personaDraftMention{loretta, chip}
	first.Carry()

	second := mentionStore{box: &mentionBox{}}
	second.ForConversation("benefits")
	second.RestoreCarried("tenant\x00walt", draftOf)
	m := chatbug071Model()
	m.SelectedID = "benefits"
	m.Conversations = append(m.Conversations, Conversation{ID: "benefits", Name: "benefits", Kind: PublicChannel, Joined: true})
	m.Callbacks.AddMembers = func([]string) {}
	outside := second.SettleOutsiders(m)
	if len(outside) != 1 || outside[0].Reference.ID != "evelyn" {
		t.Fatalf("the nonmember warning did not come back: %+v", outside)
	}
	note := stdhtml.UnescapeString(renderNode(t, chatbug071OutsideNote(m, outside, "chat-composer")))
	if !strings.Contains(note, "Evelyn Morgan is not in this conversation and will not be notified.") || !strings.Contains(note, ">Add Evelyn Morgan<") {
		t.Errorf("the restored warning and Add control are not drawn: %s", note)
	}
	if refs := second.PersonaReferences("chat-composer", "benefits", drafts["benefits"]); len(refs) != 1 || refs[0].ID != "policy" {
		t.Errorf("the agent chip did not come back with its draft: %+v", refs)
	}
	if refs := second.PersonaReferences("chat-composer", "design", drafts["design"]); len(refs) != 1 || refs[0].ID != "loretta" {
		t.Errorf("the colleague reference did not come back with its draft: %+v", refs)
	}
	// A second render of the same mounted workspace restores nothing twice.
	second.RestoreCarried("tenant\x00walt", draftOf)
	if len(second.box.outsiders) != 1 || len(second.box.personas) != 2 {
		t.Errorf("restoring twice duplicated mentions: %+v %+v", second.box.outsiders, second.box.personas)
	}
}

// Nothing comes back for another person, for a name the draft no longer holds,
// or after the drafts were discarded.
func TestIssue3993_RestoredMentionsAreCheckedAgainstTheDraftAndThePerson(t *testing.T) {
	t.Cleanup(clearMentionCarry)
	save := func() {
		clearMentionCarry()
		s := mentionStore{box: &mentionBox{conversationID: "benefits"}}
		s.RestoreCarried("tenant\x00walt", func(string) string { return "" })
		s.box.outsiders = []personaDraftMention{issue3993Evelyn("benefits")}
		s.Carry()
	}
	restore := func(owner, draft string) *mentionBox {
		s := mentionStore{box: &mentionBox{conversationID: "benefits"}}
		s.RestoreCarried(owner, func(string) string { return draft })
		return s.box
	}
	save()
	if box := restore("tenant\x00someone-else", issue3993Draft); len(box.outsiders) != 0 {
		t.Errorf("another person got the mentions: %+v", box.outsiders)
	}
	save()
	if box := restore("tenant\x00walt", "the name was deleted"); len(box.outsiders) != 0 {
		t.Errorf("a name no longer in the draft kept its warning: %+v", box.outsiders)
	}
	save()
	(&browserDrafts{}).clear()
	if box := restore("tenant\x00walt", issue3993Draft); len(box.outsiders) != 0 {
		t.Errorf("discarding drafts left their mentions behind: %+v", box.outsiders)
	}
}

// A restored agent chip is checked once the agent lookup has answered: an agent
// no longer offered is dropped, one still offered is kept and confirmed, and
// before the lookup answers nothing is dropped.
func TestIssue3993_RestoredAgentChipIsRevalidated(t *testing.T) {
	chip := func() mentionStore {
		return mentionStore{box: &mentionBox{personas: []personaDraftMention{{Target: "chat-composer", ConversationID: "room", Detached: true, Restored: true,
			Reference: ChatReference{Kind: "AGENT_MENTION", TenantID: "tenant", ID: "policy", Display: "Policy Helper", ConversationID: "room"}}}}}
	}
	m := chatbug071Model()
	m.PersonaLookup, m.PersonaLookupConversationID = PersonaLookupLoading, "room"
	s := chip()
	if s.SettleRestoredAgents(m) || len(s.box.personas) != 1 {
		t.Fatal("a chip was dropped before the lookup answered")
	}
	m.PersonaLookup = PersonaLookupReady
	if !s.SettleRestoredAgents(m) || len(s.box.personas) != 0 {
		t.Errorf("a chip for an agent that is no longer offered was kept: %+v", s.box.personas)
	}
	s = chip()
	m.ResolvedPersonaMentions = []ResolvedPersonaMention{{Reference: ChatReference{Kind: "AGENT_MENTION", TenantID: "tenant", ID: "policy", Display: "Policy Helper", ConversationID: "room"}}}
	if s.SettleRestoredAgents(m) || len(s.box.personas) != 1 || s.box.personas[0].Restored {
		t.Errorf("an agent that is still offered was dropped or left unconfirmed: %+v", s.box.personas)
	}
}
