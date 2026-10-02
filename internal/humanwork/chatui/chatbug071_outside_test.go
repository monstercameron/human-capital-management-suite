package chatui

import (
	stdhtml "html"
	"strings"
	"testing"
)

func chatbug071Model() Model {
	return Model{
		State: StateReady, Locale: "en-US", SelectedID: "room", CurrentUser: "walt", CurrentTenantID: "tenant",
		Conversations: []Conversation{{ID: "room", Name: "general", Kind: PublicChannel, Joined: true}, {ID: "dm", Name: "Loretta Haynes", Kind: DirectMessage}},
		Members:       []Member{{ID: "walt", HomeTenantID: "tenant", Name: "Walt Brennan"}, {ID: "loretta", HomeTenantID: "tenant", Name: "Loretta Haynes"}},
	}
}

// TestTodo_CHATBUG_071_Outside: a person chosen from "Not in this conversation"
// is not sent as a mention, is named under the draft with the offer to add
// them, and becomes a mention like any other once they are a member.
func TestTodo_CHATBUG_071_Outside(t *testing.T) {
	sofia := mentionCandidate{ID: "sofia", HomeTenantID: "tenant", Name: "Sofia Beltran"}
	const draft = "hello @Sofia Beltran "
	newStore := func() mentionStore {
		store := mentionStore{box: &mentionBox{conversationID: "room"}}
		store.AddOutsider("chat-composer", "room", 6, 20, sofia)
		return store
	}

	t.Run("not sent, and named under the draft", func(t *testing.T) {
		store := newStore()
		m := chatbug071Model()
		added := []string{}
		m.Callbacks.AddMembers = func(ids []string) { added = append(added, ids...) }
		if refs := store.PersonaReferences("chat-composer", "room", draft); len(refs) != 0 {
			t.Fatalf("someone outside the conversation was sent as a mention: %+v", refs)
		}
		if name := store.PersonaDisplay("chat-composer", "room"); name != "" {
			t.Fatalf("someone outside the conversation was named as the agent being asked: %q", name)
		}
		outside := store.SettleOutsiders(m)
		if len(outside) != 1 || outside[0].Reference.ID != "sofia" {
			t.Fatalf("outside = %+v, want Sofia Beltran", outside)
		}
		note := stdhtml.UnescapeString(renderNode(t, chatbug071OutsideNote(m, outside, "chat-composer")))
		for _, want := range []string{"Sofia Beltran is not in this conversation and will not be notified.", `data-action="mention-add-outside"`, `data-id="sofia"`, ">Add Sofia Beltran<", `role="status"`} {
			if !strings.Contains(note, want) {
				t.Errorf("the line under the draft lacks %q: %s", want, note)
			}
		}
		if chatbug071OutsideNote(m, outside, "thread-composer") != nil {
			t.Error("the thread composer shows a note for the main composer's draft")
		}
		chatbug071Add(m, "sofia")
		if len(added) != 1 || added[0] != "sofia" {
			t.Fatalf("Add asked for %v, want sofia", added)
		}
		m.AddMembersPending = true
		chatbug071Add(m, "sofia")
		if len(added) != 1 {
			t.Fatal("a second Add was sent while the first was still running")
		}
		if pending := renderNode(t, chatbug071OutsideNote(m, outside, "chat-composer")); !strings.Contains(pending, "disabled") {
			t.Errorf("the Add button stays pressable while the add runs: %s", pending)
		}
		m.AddMembersPending, m.AddMembersError = false, "We couldn't add everyone you picked."
		if failed := renderNode(t, chatbug071OutsideNote(m, outside, "chat-composer")); !strings.Contains(failed, `role="alert"`) || !strings.Contains(stdhtml.UnescapeString(failed), "We couldn't add everyone you picked.") {
			t.Errorf("a refused add is not reported under the draft: %s", failed)
		}
	})

	t.Run("no offer where the viewer may not add people", func(t *testing.T) {
		for name, change := range map[string]func(*Model){
			"a direct message":                    func(m *Model) { m.SelectedID = "dm" },
			"a private channel someone else owns": func(m *Model) { m.Conversations[0].Kind, m.Conversations[0].OwnerID = PrivateChannel, "loretta" },
			"adding is not available":             func(m *Model) { m.Callbacks.AddMembers = nil },
		} {
			m := chatbug071Model()
			m.Callbacks.AddMembers = func([]string) { t.Errorf("%s: Add was sent", name) }
			change(&m)
			outside := []personaDraftMention{{Target: "chat-composer", ConversationID: m.SelectedID, Reference: ChatReference{Kind: "PERSON_MENTION", ID: "sofia", Display: "Sofia Beltran"}}}
			note := stdhtml.UnescapeString(renderNode(t, chatbug071OutsideNote(m, outside, "chat-composer")))
			if !strings.Contains(note, "is not in this conversation") || strings.Contains(note, "mention-add-outside") {
				t.Errorf("%s: want the note without the offer: %s", name, note)
			}
			chatbug071Add(m, "sofia")
		}
		owned := chatbug071Model()
		owned.Conversations[0].Kind, owned.Conversations[0].OwnerID = PrivateChannel, "walt"
		owned.Callbacks.AddMembers = func([]string) {}
		if !chatbug071MayAdd(owned) {
			t.Error("the owner of a private channel is not offered Add")
		}
	})

	t.Run("a mention once the person is a member", func(t *testing.T) {
		store := newStore()
		m := chatbug071Model()
		// The member list can carry the person under another identifier than
		// the directory did; the name joins the two.
		m.Members = append(m.Members, Member{ID: "hc-077", HomeTenantID: "home", Name: "Sofia Beltran"})
		if outside := store.SettleOutsiders(m); len(outside) != 0 {
			t.Fatalf("a member is still listed as outside: %+v", outside)
		}
		refs := store.PersonaReferences("chat-composer", "room", draft)
		if len(refs) != 1 || refs[0].Kind != "PERSON_MENTION" || refs[0].ID != "hc-077" || refs[0].TenantID != "home" || refs[0].ConversationID != "room" {
			t.Fatalf("the added person is not sent as a mention of the member: %+v", refs)
		}
		if name := store.PersonaDisplay("chat-composer", "room"); name != "" {
			t.Fatalf("the added person was named as the agent being asked: %q", name)
		}
	})

	t.Run("the note follows the draft", func(t *testing.T) {
		store := newStore()
		m := chatbug071Model()
		store.ReconcilePersonas("chat-composer", "hello @Sofia Beltra")
		if outside := store.SettleOutsiders(m); len(outside) != 0 {
			t.Fatalf("the name was edited away and the note stayed: %+v", outside)
		}
		store = newStore()
		store.AddOutsider("chat-composer", "other-room", 0, 14, sofia)
		store.RemovePersonas("chat-composer", "room")
		if outside := store.SettleOutsiders(m); len(outside) != 0 {
			t.Fatalf("the draft was sent and the note stayed: %+v", outside)
		}
		if len(store.box.outsiders) != 1 || store.box.outsiders[0].ConversationID != "other-room" {
			t.Fatalf("sending in one conversation dropped another conversation's draft: %+v", store.box.outsiders)
		}
		if chatbug071OutsideNote(m, nil, "chat-composer") != nil {
			t.Error("a draft that names nobody outside draws a note")
		}
	})
}

// TestTodo_CHATBUG_071_Browser renders the line under the draft in the three
// languages with a catalog that answers every key with a bracketed key, the
// way the product's does for a key it does not hold.
func TestTodo_CHATBUG_071_Browser(t *testing.T) {
	outside := []personaDraftMention{{Target: "chat-composer", ConversationID: "room", Reference: ChatReference{Kind: "PERSON_MENTION", ID: "sofia", Display: "Sofia Beltran"}}}
	for locale, want := range map[string]struct{ note, add string }{
		"en-US": {"Sofia Beltran is not in this conversation and will not be notified.", "Add Sofia Beltran"},
		"de-DE": {"Sofia Beltran ist nicht in dieser Unterhaltung und wird nicht benachrichtigt.", "Sofia Beltran hinzufügen"},
		"ar":    {"Sofia Beltran ليس في هذه المحادثة ولن يتلقى إشعارًا.", "إضافة Sofia Beltran"},
	} {
		m := chatbug071Model()
		m.Locale = locale
		m.Text = func(key string) string { return "⟦" + key + "⟧" }
		m.Callbacks.AddMembers = func([]string) {}
		note := stdhtml.UnescapeString(renderNode(t, chatbug071OutsideNote(m, outside, "chat-composer")))
		if strings.Contains(note, "⟦") {
			t.Errorf("%s: the note prints a copy key: %s", locale, note)
		}
		if !strings.Contains(note, ">"+want.note+"<") || !strings.Contains(note, ">"+want.add+"<") {
			t.Errorf("%s: want %q and the button %q: %s", locale, want.note, want.add, note)
		}
	}
}
