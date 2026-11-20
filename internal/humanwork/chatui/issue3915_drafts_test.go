package chatui

import (
	"strings"
	"testing"
)

func issue3915Model(locale string) Model {
	m := chatux007Model(locale)
	m.Sections = []SidebarSection{
		{ID: "channels", Name: "Channels", Collapsed: true, Chats: m.Conversations[:3]},
		{ID: "mine", Name: "Mine", Collapsed: true, Chats: m.Conversations[3:]},
	}
	m.SelectedID = "general"
	m.railDrafts = map[string]bool{"unread": true, "mention": true}
	m.railDraftBodies = map[string]string{
		"unread":  "Remember to confirm\n  the rota swap",
		"mention": "Thanks, I will take the late shift",
	}
	return m
}

func TestIssue3915_ListsEveryDraftWithItsDestinationEvenWhenSectionsAreCollapsed(t *testing.T) {
	m := issue3915Model("en-US")
	markup := renderNode(t, rail(m, handlers{local: localUI{draftsOpen: true}}))
	for _, want := range []string{
		`data-action="drafts-toggle"`, `aria-expanded="true"`,
		`data-action="drafts-open" data-id="unread"`, `data-action="drafts-open" data-id="mention"`,
		`data-action="drafts-discard" data-id="unread"`, `data-action="drafts-discard" data-id="mention"`,
		"unread-room", "mention-room", "Public channel", "Remember to confirm the rota swap",
		"Discard draft",
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("Drafts panel is missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, `data-action="drafts-open" data-id="read"`) || strings.Contains(markup, `data-action="drafts-open" data-id="muted"`) {
		t.Fatalf("a conversation without a draft is listed: %s", markup)
	}
	// The per-row DRAFT markers stay.
	if !strings.Contains(markup, "chat-row-draft") {
		t.Fatalf("the sidebar Draft marker is gone: %s", markup)
	}
}

func TestIssue3915_ClosedEntryShowsOnlyTheCountNeverTheText(t *testing.T) {
	m := issue3915Model("en-US")
	markup := renderNode(t, rail(m, handlers{}))
	if !strings.Contains(markup, `aria-expanded="false"`) || !strings.Contains(markup, `aria-label="Drafts, 2 unsent"`) {
		t.Fatalf("closed entry has no labelled count: %s", markup)
	}
	if strings.Contains(markup, "rota swap") || strings.Contains(markup, "late shift") {
		t.Fatalf("draft text rendered while the list is closed: %s", markup)
	}
	if strings.Contains(markup, "unread-count") && strings.Contains(markup, "issue3915-count has-unread") {
		t.Fatalf("a draft count is styled as unread: %s", markup)
	}
}

func TestIssue3915_EmptyStateAndNoCount(t *testing.T) {
	m := issue3915Model("en-US")
	m.railDrafts, m.railDraftBodies = nil, nil
	markup := renderNode(t, rail(m, handlers{local: localUI{draftsOpen: true}}))
	if !strings.Contains(markup, "No unsent drafts") || strings.Contains(markup, "issue3915-count") {
		t.Fatalf("empty state or count wrong: %s", markup)
	}
}

func TestIssue3915_OnlyConversationsThePersonCanStillOpenAreListed(t *testing.T) {
	m := issue3915Model("en-US")
	m.railDrafts["revoked-room"] = true
	m.railDraftBodies["revoked-room"] = "secret words"
	markup := renderNode(t, rail(m, handlers{local: localUI{draftsOpen: true}}))
	if strings.Contains(markup, "revoked-room") || strings.Contains(markup, "secret words") {
		t.Fatalf("a draft for a conversation that is no longer accessible is shown: %s", markup)
	}
	if !strings.Contains(markup, `aria-label="Drafts, 2 unsent"`) {
		t.Fatalf("the count includes an inaccessible draft: %s", markup)
	}
}

func TestIssue3915_ThreadDraftsAreNamedAsNotListedYet(t *testing.T) {
	m := issue3915Model("en-US")
	markup := renderNode(t, rail(m, handlers{local: localUI{draftsOpen: true}}))
	if !strings.Contains(markup, "Drafts inside a thread are not listed here yet") {
		t.Fatalf("the list silently promises thread drafts: %s", markup)
	}
}

func TestIssue3915_DiscardNeedsAConfirmingSecondPress(t *testing.T) {
	m := issue3915Model("en-US")
	markup := renderNode(t, rail(m, handlers{local: localUI{draftsOpen: true, draftsAsk: "unread"}}))
	if strings.Count(markup, ">Confirm discard<") != 1 || strings.Count(markup, ">Discard draft<") != 1 {
		t.Fatalf("exactly one entry should ask for confirmation: %s", markup)
	}
	// The accessible name of each discard button contains its visible text, so
	// voice control and screen readers agree (WCAG 2.5.3).
	for _, want := range []string{`aria-label="Confirm discard of the draft in unread-room, for good"`, `aria-label="Discard draft in mention-room"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("missing %q: %s", want, markup)
		}
	}
	for _, locale := range []string{"de-DE", "ar"} {
		lm := issue3915Model(locale)
		for _, key := range []string{keyIssue3915Discard, keyIssue3915Confirm} {
			visible := issue3915Copy[locale][key]
			named := issue3915Copy[locale][keyIssue3915DiscardN]
			if key == keyIssue3915Confirm {
				named = issue3915Copy[locale][keyIssue3915ConfirmN]
			}
			if !strings.Contains(named, visible) {
				t.Fatalf("%s: accessible name %q does not contain visible text %q (%s)", locale, named, visible, lm.Locale)
			}
		}
	}
}

func TestIssue3915_DiscardOfTheOpenConversationClearsItsComposer(t *testing.T) {
	m := issue3915Model("en-US")
	m.Draft = "typing now"
	cleared, mentions := []string{}, 0
	m.Callbacks.DraftChanged = func(id, v string) {
		if v == "" {
			cleared = append(cleared, id)
		}
	}
	drafts := &browserDrafts{}
	drafts.prepare(&m)
	local := localStore{box: &localUI{draftsOpen: true, draftsAsk: "general"}}
	issue3915Discard(m, local, drafts, "general", func() { mentions++ })
	if strings.Join(cleared, ",") != "general" || mentions != 1 {
		t.Fatalf("cleared=%v mentionResets=%d", cleared, mentions)
	}
	if v, ok := drafts.values["general"]; ok && v != "" {
		t.Fatalf("browser draft survived: %v", drafts.values)
	}
	if note := local.get().draftsNote; !strings.Contains(note, "general") {
		t.Fatalf("no polite status for the discard: %q", note)
	}
}

func TestIssue3915_OpenOfTheCurrentConversationDoesNotReselect(t *testing.T) {
	m := issue3915Model("en-US")
	selected := 0
	m.Callbacks.SelectConversation = func(string) { selected++ }
	drafts := &browserDrafts{}
	drafts.prepare(&m)
	issue3915Open(m, localStore{box: &localUI{}}, drafts, "general")
	if selected != 0 {
		t.Fatalf("opening the draft of the open conversation reselected it %d times", selected)
	}
}

func TestIssue3915_IdentityChangeAndUnjoinedChannelsDropDrafts(t *testing.T) {
	m := issue3915Model("en-US")
	m.CurrentTenantID, m.CurrentUser = "tenant-a", "ana"
	m.Preferences.Drafts = map[string]string{"unread": "mine", "mention": "also mine"}
	d := &browserDrafts{}
	d.prepare(&m)
	if got := issue3915Bodies(m, d); got["unread"] != "mine" {
		t.Fatalf("own drafts missing: %v", got)
	}
	// Another person (or tenant) signs in on this tab: nothing carries over.
	other := issue3915Model("en-US")
	other.CurrentTenantID, other.CurrentUser = "tenant-b", "ana"
	d.prepare(&other)
	if got := issue3915Bodies(other, d); len(got) != 0 {
		t.Fatalf("drafts survived an identity change: %v", got)
	}
	// A public channel the person left is not an entry even if text lingers.
	left := issue3915Model("en-US")
	for i := range left.Conversations {
		if left.Conversations[i].ID == "unread" {
			left.Conversations[i].Joined = false
		}
	}
	for _, e := range issue3915Entries(left) {
		if e.Conversation.ID == "unread" {
			t.Fatalf("an unjoined channel is listed")
		}
	}
}

func TestIssue3915_OpenSelectsTheConversationAndNeverSends(t *testing.T) {
	m := issue3915Model("en-US")
	sent, selected := 0, ""
	saved := map[string]string{}
	m.Callbacks.SelectConversation = func(id string) { selected = id }
	m.Callbacks.SendMessage = func(string, string) { sent++ }
	m.Callbacks.DraftChanged = func(id, v string) { saved[id] = v }
	m.Draft = "half a sentence"
	drafts := &browserDrafts{}
	drafts.prepare(&m)
	local := localStore{box: &localUI{draftsOpen: true, draftsAsk: "mention"}}
	issue3915Open(m, local, drafts, "unread")
	if selected != "unread" || sent != 0 {
		t.Fatalf("selected=%q sent=%d", selected, sent)
	}
	if saved["general"] != "half a sentence" {
		t.Fatalf("the draft being left was not kept: %#v", saved)
	}
	if got := local.get().draftsAsk; got != "" {
		t.Fatalf("a half-confirmed discard survived opening: %q", got)
	}
}

func TestIssue3915_DiscardClearsOnlyThatDraft(t *testing.T) {
	m := issue3915Model("en-US")
	m.Preferences.Drafts = map[string]string{"unread": "one", "mention": "two"}
	cleared := []string{}
	m.Callbacks.DraftChanged = func(id, v string) {
		if v == "" {
			cleared = append(cleared, id)
		}
	}
	drafts := &browserDrafts{}
	drafts.prepare(&m)
	local := localStore{box: &localUI{draftsOpen: true}}
	// First press only asks.
	issue3915Discard(m, local, drafts, "unread", nil)
	if len(cleared) != 0 || m.Preferences.Drafts["unread"] != "one" || local.get().draftsAsk != "unread" {
		t.Fatalf("first press discarded: cleared=%v prefs=%v ask=%q", cleared, m.Preferences.Drafts, local.get().draftsAsk)
	}
	issue3915Discard(m, local, drafts, "unread", nil)
	if strings.Join(cleared, ",") != "unread" {
		t.Fatalf("cleared = %v", cleared)
	}
	if _, ok := m.Preferences.Drafts["unread"]; ok || m.Preferences.Drafts["mention"] != "two" {
		t.Fatalf("prefs after discard = %v", m.Preferences.Drafts)
	}
	if drafts.values["mention"] != "two" || drafts.values["unread"] != "" {
		t.Fatalf("browser drafts after discard = %v", drafts.values)
	}
	if local.get().draftsAsk != "" {
		t.Fatalf("confirmation left behind")
	}
}

func TestIssue3915_BodiesMergeStoredAndOpenDraftsAndIgnoreBlank(t *testing.T) {
	m := issue3915Model("en-US")
	m.Preferences.Drafts = map[string]string{"unread": "stored", "muted": "   "}
	m.Draft = "typing now"
	d := &browserDrafts{values: map[string]string{"mention": "browser", "unread": ""}}
	got := issue3915Bodies(m, d)
	if got["mention"] != "browser" || got["general"] != "typing now" {
		t.Fatalf("bodies = %v", got)
	}
	if _, ok := got["unread"]; ok {
		t.Fatalf("a cleared draft is still listed: %v", got)
	}
	if _, ok := got["muted"]; ok {
		t.Fatalf("a blank draft is listed: %v", got)
	}
}

func TestIssue3915_CopyInEveryLocaleAndRTL(t *testing.T) {
	for _, locale := range []string{"de-DE", "ar"} {
		m := issue3915Model(locale)
		markup := renderNode(t, rail(m, handlers{local: localUI{draftsOpen: true}}))
		if strings.Contains(markup, "Discard draft") || strings.Contains(markup, "Drafts, ") {
			t.Fatalf("%s falls back to English: %s", locale, markup)
		}
	}
	if !strings.Contains(Issue3915Styles, "margin-inline") || strings.Contains(Issue3915Styles, "margin-left") || !strings.Contains(Issue3915Styles, "min-height:44px") {
		t.Fatalf("styles are not logical or lack the phone target")
	}
}
