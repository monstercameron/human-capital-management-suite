package chatui

import (
	"strings"
	"testing"
)

func resolvedPersonaMention(tenant, id, display, conversation string) ResolvedPersonaMention {
	return ResolvedPersonaMention{Reference: ChatReference{Kind: "AGENT_MENTION", TenantID: tenant, ID: id, Display: display, ConversationID: conversation}}
}

func TestPersonaMentionSelectionKeepsCanonicalIdentityAcrossPeopleCollision(t *testing.T) {
	people := mentionCandidate{ID: "person-17", Name: "Policy Helper", Member: true}
	persona := resolvedPersonaMention("tenant-4", "agent-9", "Policy Helper", "room-1")
	if people.Name != persona.Reference.Display {
		t.Fatal("test requires colliding display labels")
	}
	got, ok := selectPersonaMention([]ResolvedPersonaMention{persona}, 0, "room-1")
	if !ok {
		t.Fatal("authorized persona was not selectable")
	}
	want := persona.Reference
	if got != want {
		t.Fatalf("persona reference = %+v, want %+v (people ID %q must not replace agent ID)", got, want, people.ID)
	}
}

func TestPersonaMentionKeyboardSelectionUsesHighlightedCandidate(t *testing.T) {
	candidates := []ResolvedPersonaMention{
		resolvedPersonaMention("t1", "agent-a", "Comp Analyst", "room"),
		resolvedPersonaMention("t1", "agent-b", "Policy Helper", "room"),
	}
	active := nextMention(0, 1, len(candidates))
	got, ok := selectPersonaMention(candidates, active, "room")
	if !ok || got.ID != "agent-b" || got.Display != "Policy Helper" {
		t.Fatalf("keyboard-selected reference = %+v, %v", got, ok)
	}
}

func TestPersonaMentionSelectionFailsClosedForUnknownOrUnavailable(t *testing.T) {
	candidates := []ResolvedPersonaMention{resolvedPersonaMention("t1", "agent-a", "Comp Analyst", "room")}
	if _, ok := selectPersonaMention(candidates, 1, "room"); ok {
		t.Fatal("unknown selection index was accepted")
	}
	candidates[0].Reference.ID = " "
	if _, ok := selectPersonaMention(candidates, 0, "room"); ok {
		t.Fatal("persona without canonical ID was accepted")
	}
	if _, ok := selectPersonaMention([]ResolvedPersonaMention{resolvedPersonaMention("t1", "a", "A", "other-room")}, 0, "room"); ok {
		t.Fatal("candidate resolved for another conversation was accepted")
	}
}

func TestApplyPersonaMentionInsertsLabelAndReturnsCanonicalReference(t *testing.T) {
	candidate := resolvedPersonaMention("tenant-4", "agent-9", "Policy Helper", "room-1")
	text, caret, reference, ok := applyPersonaMention("ask @Pol now", 4, 8, candidate, "room-1")
	if !ok {
		t.Fatal("authorized persona selection failed")
	}
	if text != "ask @Policy Helper  now" || caret != len("ask @Policy Helper ") {
		t.Fatalf("inserted text/caret = %q, %d", text, caret)
	}
	if reference != candidate.Reference {
		t.Fatalf("canonical reference = %+v", reference)
	}
	if _, _, _, ok := applyPersonaMention("@unknown", 0, 8, ResolvedPersonaMention{}, "room-1"); ok {
		t.Fatal("invalid persona selection was accepted")
	}
}

func TestTodo_AGENTP_019(t *testing.T) {
	model := Model{
		SelectedID:    "room-1",
		Conversations: []Conversation{{ID: "room-1", Kind: PublicChannel}},
		Members:       []Member{{ID: "person-17", Name: "Policy Helper"}},
		ResolvedPersonaMentions: []ResolvedPersonaMention{
			{Reference: ChatReference{Kind: "AGENT_MENTION", TenantID: "tenant-4", ID: "agent-9", Display: "Policy Helper", ConversationID: "room-1"}, Purpose: "Answer policy questions", Owner: "People Operations", Version: "v3", Skills: []PersonaMentionSkill{{Name: "Read policy", Tier: "T0"}}, DataClasses: []string{"Policy data"}, CannotDo: []string{"Change records"}, ReplyPlacement: PersonaReplyInThread},
		},
		Callbacks: Callbacks{SendMessageWithReferences: func(string, string, []ChatReference) {}},
	}
	options := mentionOptions(model, "policy", "chat-composer")
	if len(options) != 2 || options[0].person == nil || options[1].persona == nil {
		t.Fatalf("mixed suggestions = %+v", options)
	}
	if options[0].person.ID != "person-17" || options[1].persona.Reference.ID != "agent-9" {
		t.Fatalf("same-label identities were merged: %+v", options)
	}
	markup := renderNode(t, mentionMenu(model, mentionState{Target: "chat-composer", Query: "policy", Open: true}, "chat-composer"))
	for _, want := range []string{`class="mention-option persona"`, "Policy Helper", "Agent", "Agents", "Answer policy questions", "Purpose", "People Operations", "Read only", "Policy data", "Change records", "Acts with your current access.", "in this thread", "mention-profile-trigger"} {
		if !strings.Contains(markup, want) {
			t.Errorf("persona menu/profile missing %q: %s", want, markup)
		}
	}
	withoutTypedSend := model
	withoutTypedSend.Callbacks.SendMessageWithReferences = nil
	for _, option := range mentionOptions(withoutTypedSend, "policy", "chat-composer") {
		if option.persona != nil {
			t.Fatal("persona suggestion appeared without a typed send callback")
		}
	}
}

func TestTodo_AGENTP_019_Security(t *testing.T) {
	model := Model{
		SelectedID:    "room-1",
		Conversations: []Conversation{{ID: "room-1", Kind: PublicChannel}},
		ResolvedPersonaMentions: []ResolvedPersonaMention{
			resolvedPersonaMention("t1", "visible", "Visible Agent", "room-1"),
		},
		Callbacks: Callbacks{SendMessageWithReferences: func(string, string, []ChatReference) {}},
	}
	options := mentionOptions(model, "", "chat-composer")
	if len(options) != 1 || options[0].persona == nil || options[0].persona.Reference.ID != "visible" {
		t.Fatalf("menu payload projection exposed unauthorized personas: %+v", options)
	}
	markup := renderNode(t, mentionMenu(model, mentionState{Target: "chat-composer", Open: true}, "chat-composer"))
	for _, hidden := range []string{"Comp Analyst", "Payroll Agent", "Uninstalled Agent"} {
		if strings.Contains(markup, hidden) {
			t.Errorf("unauthorized persona %q leaked into menu markup: %s", hidden, markup)
		}
	}
	withoutProjection := Model{SelectedID: "room-1", Conversations: model.Conversations, Callbacks: model.Callbacks}
	if got := personaMentionCandidates(withoutProjection, ""); len(got) != 0 {
		t.Fatalf("missing server projection returned %d personas", len(got))
	}
	wrongConversation := model
	wrongConversation.ResolvedPersonaMentions = []ResolvedPersonaMention{resolvedPersonaMention("t1", "other", "Other Room Agent", "room-2")}
	if got := personaMentionCandidates(wrongConversation, ""); len(got) != 0 {
		t.Fatalf("server candidate bound to another conversation was exposed: %+v", got)
	}
	legacy := model
	legacy.ResolvedPersonaMentions = nil
	legacy.PersonaMentions = []PersonaMentionSuggestion{{TenantID: "t1", ID: "fake", Display: "Fake Invocable", Invocable: true, AudienceIncludesViewer: true, InstalledInConversation: true}}
	if got := mentionOptions(legacy, "", "chat-composer"); len(got) != 0 {
		t.Fatalf("legacy client booleans granted invocability: %+v", got)
	}
}

func TestTodo_AGENTP_019_Accessibility(t *testing.T) {
	aria := mentionFieldAria(mentionState{Target: "chat-composer", Open: true}, "chat-composer", nil)
	if aria["expanded"] != "true" || aria["controls"] != "chat-composer-mentions" || aria["haspopup"] != "listbox" {
		t.Fatalf("composer announcement: %+v", aria)
	}
	if aria := mentionModelFieldAria(Model{}, mentionState{Target: "chat-composer", Open: true}, "chat-composer", nil); aria["activedescendant"] != "" {
		t.Fatalf("empty suggestions name nonexistent option: %+v", aria)
	}
	model := Model{
		SelectedID: "room-1", Conversations: []Conversation{{ID: "room-1", Kind: PublicChannel}},
		ResolvedPersonaMentions: []ResolvedPersonaMention{{Reference: ChatReference{Kind: "AGENT_MENTION", TenantID: "t1", ID: "agent", Display: "Policy Helper", ConversationID: "room-1"}, Purpose: "Answer policy questions"}},
		Callbacks:               Callbacks{SendMessageWithReferences: func(string, string, []ChatReference) {}},
	}
	markup := renderNode(t, mentionMenu(model, mentionState{Target: "chat-composer", Open: true, Active: 0}, "chat-composer"))
	for _, want := range []string{`role="listbox"`, `role="option"`, `aria-selected="true"`, "<details", "<summary", `aria-label="Persona profile"`, `dir="ltr"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("accessible menu/profile missing %q: %s", want, markup)
		}
	}
}

func TestTodo_AGENTP_019_I18n(t *testing.T) {
	for _, tc := range []struct {
		locale, group, profile, tier, direction string
	}{
		{locale: "en-US", group: "Agents", profile: "Persona profile", tier: "Read only", direction: `dir="ltr"`},
		{locale: "de-DE", group: "Agenten", profile: "Persona-Profil", tier: "Nur lesen", direction: `dir="ltr"`},
		{locale: "ar", group: "الوكلاء", profile: "ملف الشخصية", tier: "قراءة فقط", direction: `dir="rtl"`},
	} {
		model := Model{Locale: tc.locale, SelectedID: "room", Conversations: []Conversation{{ID: "room", Kind: PublicChannel}},
			ResolvedPersonaMentions: []ResolvedPersonaMention{{Reference: ChatReference{Kind: "AGENT_MENTION", TenantID: "t1", ID: "agent", Display: "Policy Helper", ConversationID: "room"}, Purpose: "Purpose text", Skills: []PersonaMentionSkill{{Name: "Search", Tier: "T0"}}}},
			Callbacks:               Callbacks{SendMessageWithReferences: func(string, string, []ChatReference) {}}}
		markup := renderNode(t, mentionMenu(model, mentionState{Target: "chat-composer", Open: true}, "chat-composer"))
		for _, want := range []string{tc.group, tc.profile, tc.tier, tc.direction} {
			if !strings.Contains(markup, want) {
				t.Errorf("locale %q menu/profile missing %q: %s", tc.locale, want, markup)
			}
		}
	}
}

func TestPersonaProfileReplyPlacementFailsClosedAndLocalizes(t *testing.T) {
	if got := personaReplyLabel("en", "unknown"); got != "Not provided" {
		t.Fatalf("unknown reply placement = %q", got)
	}
	markup := renderNode(t, personaProfileCard("ar", ResolvedPersonaMention{Reference: ChatReference{Display: "Policy Helper"}, ReplyPlacement: PersonaReplyPrivateAudience}))
	if !strings.Contains(markup, "في سلسلة المحادثة عندما يستطيع الجمهور الوصول إلى الإجابة؛ وإلا بشكل خاص") || !strings.Contains(markup, "dir=\"rtl\"") {
		t.Fatalf("Arabic RTL profile card missing localized placement/direction: %s", markup)
	}
}

func TestPersonaReferenceIsInvalidatedWhenSelectedLabelChanges(t *testing.T) {
	item := personaDraftMention{Target: "chat-composer", ConversationID: "room-1", Start: 2, End: 14,
		Reference: ChatReference{Kind: "AGENT_MENTION", TenantID: "t1", ID: "agent-1", Display: "Policy Help"}}
	if !personaReferenceAt("x @Policy Help rest", item) {
		t.Fatal("selected persona label was not recognized")
	}
	if personaReferenceAt("x @Policy Helper rest", item) {
		t.Fatal("edited label retained its canonical persona reference")
	}
}
