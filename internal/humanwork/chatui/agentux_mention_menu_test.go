package chatui

import (
	"strings"
	"testing"
)

func agentUXMentionModel(state PersonaLookupState) Model {
	return Model{
		SelectedID: "room",
		Members:    []Member{{ID: "person", Name: "Camila Morales"}},
		ResolvedPersonaMentions: []ResolvedPersonaMention{{
			Reference: ChatReference{Kind: "AGENT_MENTION", TenantID: "tenant", ID: "policy-helper", Display: "Policy Helper", ConversationID: "room"},
			Handle:    "policy-helper", Purpose: "Answer policy questions",
		}},
		PersonaLookup: state, PersonaLookupConversationID: "room",
		Callbacks: Callbacks{
			SendMessageWithReferences: func(string, string, []ChatReference) {},
			RetryPersonaMentions:      func() {},
		},
	}
}

func TestTodo_AGENTUX_007(t *testing.T) {
	model := agentUXMentionModel(PersonaLookupReady)
	markup := renderNode(t, mentionMenu(model, mentionState{Target: "chat-composer", Open: true, Active: 1}, "chat-composer"))
	for _, want := range []string{
		`role="listbox"`, `role="option"`, `aria-label="Policy Helper, @policy-helper, Agent"`,
		"People", "Agents", "Camila Morales", "class=\"agent-icon\"", "Policy Helper", "@policy-helper", "Answer policy questions", "Agent",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("live mention menu missing %q: %s", want, markup)
		}
	}
	if nextMention(0, -1, 2) != 1 || nextMention(1, 1, 2) != 0 {
		t.Fatal("keyboard selection did not cross the people/agents boundary")
	}
	for key, want := range map[string]mentionKeyAction{"ArrowDown": mentionKeyNext, "ArrowUp": mentionKeyPrevious, "Enter": mentionKeySelect, "Tab": mentionKeyDetails, "Escape": mentionKeyClose} {
		if got := mentionActionForKey(key); got != want {
			t.Errorf("key %q action = %q, want %q", key, got, want)
		}
	}
	if shortcuts := mentionFieldAria(mentionState{Target: "chat-composer", Open: true}, "chat-composer", nil)["keyshortcuts"]; !strings.Contains(shortcuts, "Tab") || !strings.Contains(shortcuts, "Escape") {
		t.Fatalf("composer did not expose the complete mention keyboard contract: %q", shortcuts)
	}
	if agents, people := strings.Index(markup, `class="mention-heading agents"`), strings.Index(markup, `class="mention-heading people"`); agents < 0 || people < 0 || agents > people {
		t.Fatalf("empty query did not put agents before people: %s", markup)
	}
}

func TestTodo_AGENTUX_007_LookupOrdering(t *testing.T) {
	people := []mentionCandidate{{ID: "person", Name: "Camila Morales", Member: true}}
	before := decideMentionMenu("@pol", 4, "chat-composer", PersonaLookupLoading, people, nil)
	if !before.State.Open || before.State.Query != "pol" || len(before.Options) != 0 {
		t.Fatalf("typing before lookup = %+v, want an open loading query", before)
	}
	agent := ResolvedPersonaMention{
		Reference: ChatReference{Kind: "AGENT_MENTION", TenantID: "tenant", ID: "policy-helper", Display: "Policy Helper", ConversationID: "room"},
		Handle:    "policy-helper",
	}
	unrelated := ResolvedPersonaMention{Reference: ChatReference{ID: "benefits-guide", Display: "Benefits Guide"}, Handle: "benefits-guide"}
	after := decideMentionMenu("@pol", 4, "chat-composer", PersonaLookupReady, people, []ResolvedPersonaMention{agent, unrelated})
	if !after.State.Open || after.State.Query != "pol" || len(after.Options) != 1 || after.Options[0].persona == nil || after.Options[0].persona.Reference.ID != "policy-helper" {
		t.Fatalf("lookup arrival = %+v, want the open query with Policy Helper", after)
	}
}

func TestTodo_AGENTUX_007_ConversationSwitchClosesQuery(t *testing.T) {
	store := mentionStore{box: &mentionBox{conversationID: "room-a", state: mentionState{Target: "chat-composer", Query: "pol", Start: 0, End: 4, Open: true}}}
	store.ForConversation("room-a")
	if !store.Get().Open {
		t.Fatal("same-conversation render closed the mention menu")
	}
	store.ForConversation("room-b")
	if got := store.Get(); got.Open || got.Query != "" || got.Target != "" {
		t.Fatalf("conversation switch retained mention state: %+v", got)
	}
}

func TestTodo_AGENTUX_021_MentionAgentNeverAppearsAsPerson(t *testing.T) {
	model := agentUXMentionModel(PersonaLookupReady)
	model.Members = append(model.Members,
		Member{ID: "policy-helper", Name: "Policy Helper"},
		Member{ID: "duplicate-agent-subject", Name: "Policy Helper"},
	)
	options, _ := mentionOptionsForState(model, "pol", "chat-composer", false)
	if len(options) != 1 || options[0].persona == nil {
		t.Fatalf("agent identity leaked into people options: %+v", options)
	}
	markup := renderNode(t, mentionMenu(model, mentionState{Target: "chat-composer", Query: "pol", Open: true}, "chat-composer"))
	if strings.Contains(markup, `class="mention-heading people"`) {
		t.Fatalf("agent identity rendered under people: %s", markup)
	}
}

func TestTodo_AGENTUX_007_KeyboardActiveOptionAnnounced(t *testing.T) {
	model := agentUXMentionModel(PersonaLookupReady)
	state := mentionState{Target: "chat-composer", Open: true, Active: 1}
	markup := renderNode(t, mentionMenu(model, state, "chat-composer"))
	aria := mentionModelFieldAria(model, state, "chat-composer", nil)
	if aria["activedescendant"] != "chat-composer-mention-2" || !strings.Contains(markup, `id="chat-composer-mention-2"`) || !strings.Contains(markup, `aria-selected="true"`) {
		t.Fatalf("active option was not announced: aria=%v markup=%s", aria, markup)
	}
}

func TestTodo_AGENTUX_007_ComposerChooseTypeSendKeepsReference(t *testing.T) {
	model := agentUXMentionModel(PersonaLookupReady)
	candidate := model.ResolvedPersonaMentions[0]
	value, caret, reference, ok := applyPersonaMention("@pol", 0, 4, candidate, model.SelectedID)
	if !ok {
		t.Fatal("authorized agent mention was not selected")
	}
	store := mentionStore{box: &mentionBox{conversationID: model.SelectedID}}
	store.AddPersona("chat-composer", model.SelectedID, 0, caret-1, reference)
	value += "what is our PTO policy?"
	store.ReconcilePersonas("chat-composer", value)
	body, references, ready := composerSendPayload(value, "", store.PersonaReferences("chat-composer", model.SelectedID, value))
	if !ready || body != "@Policy Helper what is our PTO policy?" {
		t.Fatalf("send body after mention selection and continued typing = %q, ready=%v", body, ready)
	}
	if len(references) != 1 || references[0] != reference {
		t.Fatalf("send references after mention selection and continued typing = %+v, want %+v", references, reference)
	}
}

func TestTodo_AGENTUX_007_Browser(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state PersonaLookupState
		want  []string
	}{
		{name: "loading", state: PersonaLookupLoading, want: []string{"Loading agents…", `class="mention-agent-state loading"`}},
		{name: "failed", state: PersonaLookupFailed, want: []string{"The agent list could not be loaded.", "Try again", `data-action="persona-mention-retry"`, `aria-live="assertive"`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := agentUXMentionModel(tc.state)
			model.ResolvedPersonaMentions = nil
			markup := renderNode(t, mentionMenu(model, mentionState{Target: "chat-composer", Open: true}, "chat-composer"))
			for _, want := range tc.want {
				if !strings.Contains(markup, want) {
					t.Errorf("%s state missing %q: %s", tc.name, want, markup)
				}
			}
		})
	}

	translations := map[string]map[string]string{
		"de-DE": {KeyMentionAgents: "Agenten", KeyMentionAgentsFailed: "Die Agentenliste konnte nicht geladen werden.", KeyMentionAgentsRetry: "Erneut versuchen", KeyMentionMenu: "Person erwähnen"},
		"ar":    {KeyMentionAgents: "الوكلاء", KeyMentionAgentsFailed: "تعذر تحميل قائمة الوكلاء.", KeyMentionAgentsRetry: "حاول مرة أخرى", KeyMentionMenu: "الإشارة إلى شخص"},
	}
	for locale, copy := range translations {
		model := agentUXMentionModel(PersonaLookupFailed)
		model.Locale, model.ResolvedPersonaMentions = locale, nil
		model.Text = func(key string) string { return copy[key] }
		markup := renderNode(t, mentionMenu(model, mentionState{Target: "chat-composer", Open: true}, "chat-composer"))
		for _, key := range []string{KeyMentionAgents, KeyMentionAgentsFailed, KeyMentionAgentsRetry} {
			if !strings.Contains(markup, copy[key]) {
				t.Errorf("%s menu missing %q: %s", locale, copy[key], markup)
			}
		}
	}
}

func TestTodo_AGENTUX_007_AgentsFirstInEveryEmptyQueryState(t *testing.T) {
	// A directory that answered with no agents shows no Agents group at all
	// (TestTodo_CHATBUG_028), so Ready is not an empty-query agents state.
	for _, state := range []PersonaLookupState{PersonaLookupIdle, PersonaLookupLoading, PersonaLookupFailed} {
		t.Run(string(state), func(t *testing.T) {
			model := agentUXMentionModel(state)
			model.ResolvedPersonaMentions = nil
			markup := renderNode(t, mentionMenu(model, mentionState{Target: "chat-composer", Open: true}, "chat-composer"))
			agents := strings.Index(markup, `class="mention-heading agents"`)
			people := strings.Index(markup, `class="mention-heading people"`)
			if agents < 0 || people < 0 || agents > people {
				t.Fatalf("%s state hid agents below people: %s", state, markup)
			}
		})
	}
}

func TestTodo_AGENTUX_007_OutsidePeopleDisclosureAndExpansion(t *testing.T) {
	model := agentUXMentionModel(PersonaLookupReady)
	model.SearchDirectory = []SearchPerson{
		{ID: "outside-1", Name: "Alina One"},
		{ID: "outside-2", Name: "Berta Two"},
		{ID: "outside-3", Name: "Carla Three"},
	}
	collapsed := renderNode(t, mentionMenu(model, mentionState{Target: "chat-composer", Open: true}, "chat-composer"))
	for _, want := range []string{"Not in this conversation", "They are not in this conversation and will not be notified unless you add them.", "Alina One", "Berta Two", "Show more people", `data-action="mention-show-more"`} {
		if !strings.Contains(collapsed, want) {
			t.Errorf("collapsed outside group missing %q: %s", want, collapsed)
		}
	}
	if strings.Contains(collapsed, "Carla Three") {
		t.Fatalf("collapsed outside group showed more than two people: %s", collapsed)
	}
	expanded := renderNode(t, mentionMenu(model, mentionState{Target: "chat-composer", Open: true, ShowAllPeople: true}, "chat-composer"))
	if !strings.Contains(expanded, "Carla Three") || strings.Contains(expanded, "Show more people") {
		t.Fatalf("outside group did not expand in place: %s", expanded)
	}
}

func TestTodo_AGENTUX_007_QueryRanksAcrossGroups(t *testing.T) {
	model := agentUXMentionModel(PersonaLookupReady)
	model.Members = []Member{{ID: "person", Name: "Policy Helper Team"}}
	model.ResolvedPersonaMentions[0].Handle = "policy"
	options, _ := mentionOptionsForState(model, "policy", "chat-composer", false)
	if len(options) != 2 || options[0].persona == nil || options[1].person == nil {
		t.Fatalf("exact agent handle did not rank across groups: %+v", options)
	}
	markup := renderNode(t, mentionMenu(model, mentionState{Target: "chat-composer", Query: "policy", Open: true}, "chat-composer"))
	if agents, people := strings.Index(markup, `class="mention-heading agents"`), strings.Index(markup, `class="mention-heading people"`); agents < 0 || people < 0 || agents > people {
		t.Fatalf("better agent match did not rank before people: %s", markup)
	}
	noAgentMatch := renderNode(t, mentionMenu(model, mentionState{Target: "chat-composer", Query: "team", Open: true}, "chat-composer"))
	if strings.Contains(noAgentMatch, `class="mention-heading agents"`) || strings.Contains(noAgentMatch, "No agents match this search.") || !strings.Contains(noAgentMatch, "Policy Helper Team") {
		t.Fatalf("typed filtering did not hide the empty agent group: %s", noAgentMatch)
	}
}

func TestTodo_AGENTUX_007_EmptyRecoveryAndReplyHint(t *testing.T) {
	model := agentUXMentionModel(PersonaLookupReady)
	model.ResolvedPersonaMentions = nil
	model.IsTenantAdmin = true
	markup := renderNode(t, mentionMenu(model, mentionState{Target: "chat-composer", Open: true, Details: true}, "chat-composer"))
	for _, banned := range []string{`href="/workspace/app/chat/agents"`, `href="/workspace/app/admin/personas"`, "Add an agent to this conversation", "mention-heading agents"} {
		if strings.Contains(markup, banned) {
			t.Errorf("a conversation with no agents still offered %q: %s", banned, markup)
		}
	}
	model.Conversations = []Conversation{{ID: "room", Name: "general", Kind: PublicChannel}}
	mentions := mentionStore{box: &mentionBox{personas: []personaDraftMention{{Target: "chat-composer", ConversationID: "room", Reference: ChatReference{Display: "Policy Helper"}}}}}
	if got := mentionReplyHint(model, mentions, "chat-composer"); got != "Everyone in \u2068#general\u2069 will see your question and Policy Helper's answer. To keep the answer to yourself, add \"keep this private\"." {
		t.Fatalf("reply hint = %q", got)
	}
	for locale, want := range map[string]string{
		"de-DE": "Alle in ⁨#general⁩ sehen Ihre Frage und die Antwort von Policy Helper. Damit die Antwort privat bleibt, schreiben Sie „privat halten“.",
		"ar":    "سيرى الجميع في \u2068#general\u2069 سؤالك وإجابة Policy Helper. لإبقاء الإجابة لك وحدك، أضف «اجعل هذا خاصا».",
	} {
		localized := model
		localized.Locale = locale
		if got := mentionReplyHint(localized, mentions, "chat-composer"); got != want {
			t.Errorf("%s reply hint = %q, want %q", locale, got, want)
		}
	}
}

func TestTodo_AGENTUX_018_Mention(t *testing.T) {
	model := agentUXMentionModel(PersonaLookupReady)
	markup := renderNode(t, mentionMenu(model, mentionState{Target: "chat-composer", Open: true, Details: true}, "chat-composer"))
	if strings.Contains(markup, ">Persona") || strings.Contains(markup, "Persona profile") || strings.Contains(markup, "Data this persona") {
		t.Fatalf("visible mention vocabulary exposed internal persona language: %s", markup)
	}
	for _, want := range []string{`dir="ltr">@policy-helper`, "Agent details", "Data this agent can reach"} {
		if !strings.Contains(markup, want) {
			t.Errorf("agent vocabulary missing %q: %s", want, markup)
		}
	}
}

func TestTodo_AGENTUX_007_HintStyleAndImageSkeleton(t *testing.T) {
	if strings.Contains(composerPolishStyles, ".6875rem") || !strings.Contains(composerPolishStyles, `.mention-hint{font-size:.75rem`) {
		t.Fatalf("mention hint is not pinned to at least 12px: %s", composerPolishStyles)
	}
	for _, want := range []string{"image-loading-skeleton", "prefers-reduced-motion", "var(--soft)"} {
		if !strings.Contains(composerPolishStyles, want) {
			t.Errorf("image skeleton style missing %q", want)
		}
	}
	for _, want := range []string{"max-width:calc(100vw - 16px)", "overflow-wrap:anywhere", "var(--surface)", "var(--line)"} {
		if !strings.Contains(composerPolishStyles, want) {
			t.Errorf("responsive/token mention style missing %q", want)
		}
	}
	if strings.Contains(composerPolishStyles, "font-family") {
		t.Fatal("mention styles bypass shell typography tokens")
	}
	model := Model{Text: func(key string) string {
		if key == KeyAttachmentLoading {
			return "Image loading"
		}
		return EnglishCopy()[key]
	}}
	markup := renderNode(t, attachments(model, Message{Attachments: []Attachment{{Name: "Policy chart", ContentType: "image/png"}}}))
	for _, want := range []string{`class="attachment-pending image-loading-skeleton"`, `role="status"`, `aria-label="Image loading"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("image loading placeholder missing %q: %s", want, markup)
		}
	}
}
