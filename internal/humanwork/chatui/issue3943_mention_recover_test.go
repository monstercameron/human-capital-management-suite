package chatui

import (
	stdhtml "html"
	"strings"
	"testing"
)

// Issue 3943: a draft restored after a reload holds "@Evelyn Morgan" as text
// only. The mention store lives in the page, so the warning that she is not in
// the conversation, and the Add action, were gone. They are recomputed from the
// restored text, the directory and the current member list.
func issue3943Model() Model {
	m := chatbug071Model()
	m.Draft = "Please review @Evelyn Morgan "
	m.SearchDirectory = []SearchPerson{{ID: "evelyn", Name: "Evelyn Morgan"}, {ID: "loretta", Name: "Loretta Haynes"}}
	m.Callbacks.AddMembers = func([]string) {}
	return m
}

func TestIssue3943_RestoredDraftWarnsAboutAMentionedNonmember(t *testing.T) {
	store := mentionStore{box: &mentionBox{conversationID: "room"}}
	m := issue3943Model()
	outside := store.SettleOutsiders(m)
	if len(outside) != 1 || outside[0].Reference.ID != "evelyn" {
		t.Fatalf("outside = %+v, want Evelyn Morgan", outside)
	}
	note := stdhtml.UnescapeString(renderNode(t, chatbug071OutsideNote(m, outside, "chat-composer")))
	for _, want := range []string{"Evelyn Morgan is not in this conversation and will not be notified.", `data-action="mention-add-outside"`, ">Add Evelyn Morgan<"} {
		if !strings.Contains(note, want) {
			t.Errorf("restored draft note lacks %q: %s", want, note)
		}
	}
	if refs := store.PersonaReferences("chat-composer", "room", m.Draft); len(refs) != 0 {
		t.Fatalf("a nonmember was sent as a mention: %+v", refs)
	}
	// Settling again on the next render must not duplicate the warning.
	if again := store.SettleOutsiders(m); len(again) != 1 {
		t.Fatalf("second render outside = %+v", again)
	}
}

func TestIssue3943_RestoredDraftMentionOfAMemberIsAReference(t *testing.T) {
	store := mentionStore{box: &mentionBox{conversationID: "room"}}
	m := issue3943Model()
	m.Members = append(m.Members, Member{ID: "evelyn-sub", HomeTenantID: "tenant", Name: "Evelyn Morgan"})
	if outside := store.SettleOutsiders(m); len(outside) != 0 {
		t.Fatalf("a member is listed as outside: %+v", outside)
	}
	refs := store.PersonaReferences("chat-composer", "room", m.Draft)
	if len(refs) != 1 || refs[0].ID != "evelyn-sub" || refs[0].Kind != "PERSON_MENTION" {
		t.Fatalf("refs = %+v, want one mention of the member", refs)
	}
}

func TestIssue3943_RecoveryOnlyForRealMentions(t *testing.T) {
	for name, change := range map[string]func(*Model){
		"no @ before the name":     func(m *Model) { m.Draft = "Please review Evelyn Morgan" },
		"name continues as a word": func(m *Model) { m.Draft = "Please review @Evelyn Morganstern" },
		"members still loading":    func(m *Model) { m.Members = nil },
		"ambiguous directory names": func(m *Model) {
			m.SearchDirectory = append(m.SearchDirectory, SearchPerson{ID: "evelyn-2", Name: "Evelyn Morgan"})
		},
		"the viewer themselves": func(m *Model) {
			m.Draft = "note to @Walt Brennan"
			m.SearchDirectory = []SearchPerson{{ID: "walt", Name: "Walt Brennan"}}
		},
	} {
		store := mentionStore{box: &mentionBox{conversationID: "room"}}
		m := issue3943Model()
		change(&m)
		if outside := store.SettleOutsiders(m); len(outside) != 0 {
			t.Errorf("%s: warned about %+v", name, outside)
		}
	}
}

func TestIssue3943_NoteInEveryLanguage(t *testing.T) {
	for _, locale := range []string{"de-DE", "ar"} {
		store := mentionStore{box: &mentionBox{conversationID: "room"}}
		m := issue3943Model()
		m.Locale = locale
		outside := store.SettleOutsiders(m)
		note := stdhtml.UnescapeString(renderNode(t, chatbug071OutsideNote(m, outside, "chat-composer")))
		if !strings.Contains(note, "Evelyn Morgan") || strings.Contains(note, "will not be notified") {
			t.Errorf("%s: note is not localized: %s", locale, note)
		}
	}
}
