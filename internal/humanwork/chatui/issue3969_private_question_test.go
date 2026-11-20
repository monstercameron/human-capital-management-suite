package chatui

import (
	"strings"
	"testing"
)

// issue3969Fixture is a public channel asking Policy Helper, with the person's
// own conversation with that agent in the list.
func issue3969Fixture(locale string, withDirect bool) (Model, ChatReference, *[]string) {
	model := chat4Fixture(locale, "answered", false)
	if withDirect {
		model.Conversations = append(model.Conversations, Conversation{ID: "policy-dm", Name: "Policy Helper", Kind: DirectMessage, Agent: true, AgentID: "policy-helper", Joined: true})
	}
	calls := &[]string{}
	model.Callbacks.SendMessage = func(id, body string) { *calls = append(*calls, "send:"+id+":"+body) }
	model.Callbacks.SendMessageWithReferences = func(id, body string, _ []ChatReference) { *calls = append(*calls, "send:"+id+":"+body) }
	model.Callbacks.SelectConversation = func(id string) { *calls = append(*calls, "select:"+id) }
	model.Callbacks.DraftChanged = func(id, body string) { *calls = append(*calls, "draft:"+id+":"+body) }
	return model, model.ResolvedPersonaMentions[0].Reference, calls
}

// The Private answer row says the question stays visible to the channel, and
// offers a separate way to ask privately, in every locale.
func TestIssue3969_RowExplainsQuestionStaysPublicAndOffersPrivateRoute(t *testing.T) {
	model, agent, _ := issue3969Fixture("en-US", true)
	markup := renderNode(t, agentux070Row(model, localUI{privateAnswer: true}, []ChatReference{agent}))
	for _, want := range []string{"Your question will still be visible to everyone in", "general", "Ask this agent privately", `data-action="agent-ask-private"`, `data-id="policy-helper"`, `aria-describedby="composer-private-ask-help"`, "no messages or files from this channel come with it"} {
		if !strings.Contains(markup, want) {
			t.Errorf("row lacks %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, " disabled") {
		t.Errorf("the private route is disabled although a private conversation exists: %s", markup)
	}
	for _, locale := range []string{"de-DE", "ar"} {
		m, ref, _ := issue3969Fixture(locale, true)
		got := renderNode(t, agentux070Row(m, localUI{}, []ChatReference{ref}))
		for _, english := range []string{"Your question will still be visible", "Ask this agent privately", "no messages or files", "issue3969"} {
			if strings.Contains(got, english) {
				t.Errorf("%s: English or a raw key %q on the page: %s", locale, english, got)
			}
		}
		if !strings.Contains(got, `data-action="agent-ask-private"`) {
			t.Errorf("%s: no private route: %s", locale, got)
		}
	}
}

// With no private conversation to move into, the route is disabled and says so.
func TestIssue3969_RouteUnavailableWithoutPrivateConversation(t *testing.T) {
	model, agent, _ := issue3969Fixture("en-US", false)
	markup := renderNode(t, agentux070Row(model, localUI{}, []ChatReference{agent}))
	if !strings.Contains(markup, "disabled") || !strings.Contains(markup, "is not available, so nothing was moved or sent") {
		t.Errorf("the unavailable route is not explained: %s", markup)
	}
}

// Moving puts the text, without the agent's @name, in the private draft for
// review; sends nothing; leaves nothing in the channel draft; and carries no
// context.
func TestIssue3969_MoveIsReviewNotSend(t *testing.T) {
	model, _, calls := issue3969Fixture("en-US", true)
	store := localStore{box: &localUI{}}
	drafts := &browserDrafts{}
	cleared := false
	if !issue3969Move(model, store, drafts, func() { cleared = true }, "policy-helper", "@Policy Helper what is my salary band?") {
		t.Fatal("the text did not move although a private conversation exists")
	}
	got := strings.Join(*calls, "|")
	for _, want := range []string{"draft:general:", "draft:policy-dm:what is my salary band?", "select:policy-dm"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
	if strings.Contains(got, "send:") {
		t.Errorf("a message was sent: %s", got)
	}
	if !cleared {
		t.Error("the agent token stayed in the channel composer")
	}
	if drafts.values["policy-dm"] != "what is my salary band?" || drafts.values["general"] != "" {
		t.Errorf("drafts = %#v", drafts.values)
	}
	// In the private conversation, the banner says nothing is sent yet.
	direct := model
	direct.SelectedID, direct.Draft = "policy-dm", "what is my salary band?"
	banner := renderNode(t, issue3969Banner(direct, store.get()))
	for _, want := range []string{"Not sent yet", "general", "No messages or files from the channel came with it", `data-action="agent-ask-private-cancel"`, "Back to channel"} {
		if !strings.Contains(banner, want) {
			t.Errorf("banner lacks %q: %s", want, banner)
		}
	}
	// Nothing for a draft that is in no way moved.
	if issue3969Banner(model, store.get()) != nil {
		t.Error("a banner on a conversation nothing was moved into")
	}
}

// Cancel returns the text to the channel draft exactly as it was.
func TestIssue3969_CancelRestoresChannelDraft(t *testing.T) {
	model, _, calls := issue3969Fixture("en-US", true)
	store := localStore{box: &localUI{}}
	drafts := &browserDrafts{}
	raw := "@Policy Helper what is my salary band?"
	issue3969Move(model, store, drafts, nil, "policy-helper", raw)
	*calls = nil
	direct := model
	direct.SelectedID = "policy-dm"
	if !issue3969Cancel(direct, store, drafts) {
		t.Fatal("cancel did nothing")
	}
	if drafts.values["general"] != raw || drafts.values["policy-dm"] != "" {
		t.Errorf("drafts after cancel = %#v", drafts.values)
	}
	got := strings.Join(*calls, "|")
	if !strings.Contains(got, "draft:general:"+raw) || !strings.Contains(got, "select:general") || strings.Contains(got, "send:") {
		t.Errorf("calls = %s", got)
	}
	if _, still := store.get().privateAsk["policy-dm"]; still {
		t.Error("the moved-question state survived cancel")
	}
}

// An unavailable private conversation moves nothing, sends nothing, and keeps
// the channel draft.
func TestIssue3969_UnavailablePrivateConversationKeepsChannelDraft(t *testing.T) {
	model, _, calls := issue3969Fixture("en-US", false)
	store := localStore{box: &localUI{}}
	drafts := &browserDrafts{}
	drafts.set("general", "@Policy Helper hello")
	if issue3969Move(model, store, drafts, nil, "policy-helper", "@Policy Helper hello") {
		t.Fatal("moved with no private conversation")
	}
	if len(*calls) != 0 {
		t.Errorf("callbacks ran: %v", *calls)
	}
	if drafts.values["general"] != "@Policy Helper hello" {
		t.Errorf("the channel draft was lost: %#v", drafts.values)
	}
	if !strings.Contains(store.get().composerNotice, "not available") {
		t.Errorf("notice = %q", store.get().composerNotice)
	}
}
