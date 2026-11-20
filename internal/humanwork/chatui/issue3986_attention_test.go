package chatui

import (
	"strings"
	"testing"
	"time"
)

func issue3986Model(locale string) Model {
	room := Conversation{ID: "policy-dm", Name: "Policy Helper", Kind: DirectMessage, Agent: true, AgentID: "policy-helper", Joined: true}
	return Model{State: StateReady, Locale: locale, CurrentUser: "alice", CurrentTenantID: "tenant", SelectedID: room.ID,
		Conversations: []Conversation{room},
		Callbacks:     Callbacks{RequestAttention: func() {}, DraftChanged: func(string, string) {}}}
}

func issue3986Items() []AttentionItem {
	return []AttentionItem{
		{ID: "j-wait", Title: "Promotion for Dana", Source: "Promotion", Status: "Waiting for Finance", Responsibility: AttentionWaiting, Href: "/journeys/j-wait"},
		{ID: "j-late", Title: "Promotion for Sam", Source: "Promotion", Status: "Awaiting your approval", Responsibility: AttentionAction, Href: "/journeys/j-late", Due: time.Now().Add(-48 * time.Hour)},
		{ID: "j-soon", Title: "Promotion for Lee", Source: "Promotion", Status: "Awaiting your approval", Responsibility: AttentionAction, Href: "/journeys/j-soon", Due: time.Now().Add(72 * time.Hour)},
		{ID: "j-none", Title: "Promotion for Kim", Source: "Promotion", Status: "Awaiting your approval", Responsibility: AttentionAction, Href: "/journeys/j-none"},
		{ID: "j-odd", Title: "Promotion for Ray", Source: "Promotion", Status: "Under review", Responsibility: AttentionUnknown, Href: "/journeys/j-odd"},
	}
}

func TestIssue3986_OfferedOnlyInYourOwnAgentConversation(t *testing.T) {
	m := issue3986Model("en-US")
	if got := renderNode(t, issue3986Panel(m, localUI{})); !strings.Contains(got, "What needs my attention?") || !strings.Contains(got, `aria-expanded="false"`) {
		t.Fatalf("closed panel: %s", got)
	}
	channel := m
	channel.Conversations = []Conversation{{ID: "general", Name: "general", Kind: PublicChannel, Joined: true}}
	channel.SelectedID = "general"
	if issue3986Panel(channel, localUI{}) != nil {
		t.Fatal("offered in a channel, where others could be present")
	}
	none := m
	none.Callbacks.RequestAttention = nil
	if issue3986Panel(none, localUI{}) != nil {
		t.Fatal("offered without a way to read the work")
	}
	away := m
	away.Conversations = []Conversation{{ID: "policy-dm", Name: "Policy Helper", Kind: DirectMessage, Agent: true, Joined: true, HostTenantID: "other"}}
	if issue3986Panel(away, localUI{}) != nil {
		t.Fatal("offered for a conversation hosted by another tenant")
	}
}

func TestIssue3986_ShowsScopeBeforeAnythingIsRead(t *testing.T) {
	m := issue3986Model("en-US")
	got := renderNode(t, issue3986Panel(m, localUI{attention: issue3986UI{Open: true}}))
	for _, want := range []string{"My Work", "no date limit", "Include items waiting on others", "Only you can see this", "nothing is approved", "Read my work"} {
		if !strings.Contains(got, want) {
			t.Errorf("scope lacks %q: %s", want, got)
		}
	}
	if strings.Contains(got, "Needs your action") {
		t.Errorf("a result is shown before anything was read: %s", got)
	}
}

func TestIssue3986_SeparatesActionFromWaitingAndOrdersByDue(t *testing.T) {
	m := issue3986Model("en-US")
	m.Attention = AttentionBrief{Phase: AttentionReady, Items: issue3986Items(), LoadedAt: time.Now(), Total: 5}
	got := renderNode(t, issue3986Panel(m, localUI{attention: issue3986UI{Open: true}}))
	action := strings.Index(got, "Needs your action")
	wait := strings.Index(got, "Waiting on others")
	unknown := strings.Index(got, "Role not clear")
	if action < 0 || wait < action || unknown < wait {
		t.Fatalf("sections missing or out of order: %s", got)
	}
	late, soon, none := strings.Index(got, "Sam"), strings.Index(got, "Lee"), strings.Index(got, "Kim")
	if late < 0 || late > soon || soon > none {
		t.Fatalf("not ordered overdue, soon, undated: %d %d %d", late, soon, none)
	}
	for _, want := range []string{"Overdue", "No due date set", `href="/journeys/j-late"`, "suggestion", "Open it to see"} {
		if !strings.Contains(got, want) {
			t.Errorf("brief lacks %q", want)
		}
	}
	// Waiting items are not in the action section.
	if strings.Index(got, "Dana") < wait || strings.Index(got, "Dana") > unknown {
		t.Errorf("a waiting item is outside its section")
	}
	// The brief has nothing that decides or changes work.
	for _, banned := range []string{"Approve", "Decline", "Reassign", "Snooze"} {
		if strings.Contains(got, ">"+banned) {
			t.Errorf("brief offers %q", banned)
		}
	}
}

func TestIssue3986_FilterLeavesWaitingOutAndSaysSo(t *testing.T) {
	m := issue3986Model("en-US")
	m.Attention = AttentionBrief{Phase: AttentionReady, Items: issue3986Items(), LoadedAt: time.Now(), Total: 5}
	got := renderNode(t, issue3986Panel(m, localUI{attention: issue3986UI{Open: true, NoWaiting: true}}))
	if strings.Contains(got, "Dana") || !strings.Contains(got, "left out by your filter") {
		t.Fatalf("filter not honest: %s", got)
	}
}

func TestIssue3986_FailedReadIsNeverNothingToDo(t *testing.T) {
	m := issue3986Model("en-US")
	m.Attention = AttentionBrief{Phase: AttentionFailed}
	got := renderNode(t, issue3986Panel(m, localUI{attention: issue3986UI{Open: true}}))
	for _, want := range []string{`role="alert"`, "could not be read", "not a list of what is left", "Try again"} {
		if !strings.Contains(got, want) {
			t.Errorf("failure lacks %q: %s", want, got)
		}
	}
	if strings.Contains(got, "Nothing needs you") {
		t.Errorf("a failure reads as nothing to do: %s", got)
	}
}

func TestIssue3986_EmptyReadAndPartialReadAreDistinct(t *testing.T) {
	m := issue3986Model("en-US")
	m.Attention = AttentionBrief{Phase: AttentionReady, LoadedAt: time.Now()}
	if got := renderNode(t, issue3986Panel(m, localUI{attention: issue3986UI{Open: true}})); !strings.Contains(got, "Nothing needs you in the work that was read") {
		t.Fatalf("empty: %s", got)
	}
	m.Attention = AttentionBrief{Phase: AttentionReady, LoadedAt: time.Now(), Items: issue3986Items()[:2], Total: 9}
	if got := renderNode(t, issue3986Panel(m, localUI{attention: issue3986UI{Open: true}})); !strings.Contains(got, "2 of 9") || !strings.Contains(got, "not everything") {
		t.Fatalf("partial read presented as complete: %s", got)
	}
}

func TestIssue3986_OldReadIsMarkedStale(t *testing.T) {
	m := issue3986Model("en-US")
	m.Attention = AttentionBrief{Phase: AttentionReady, Items: issue3986Items()[:2], LoadedAt: time.Now().Add(-time.Hour), Total: 2}
	if got := renderNode(t, issue3986Panel(m, localUI{attention: issue3986UI{Open: true}})); !strings.Contains(got, "may be out of date") {
		t.Fatalf("stale read not marked: %s", got)
	}
}

func TestIssue3986_RereadShowsChangedDueDateAndGoneItems(t *testing.T) {
	before := AttentionBrief{Items: []AttentionItem{
		{ID: "a", Title: "A", Status: "Awaiting your approval", Responsibility: AttentionAction, Due: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)},
		{ID: "b", Title: "B", Status: "Awaiting your approval", Responsibility: AttentionAction},
		{ID: "c", Title: "C", Status: "Awaiting your approval", Responsibility: AttentionAction},
	}}
	next := []AttentionItem{
		{ID: "a", Title: "A", Status: "Awaiting your approval", Responsibility: AttentionAction, Due: time.Date(2026, 1, 9, 0, 0, 0, 0, time.UTC)},
		{ID: "b", Title: "B", Status: "Waiting for Finance", Responsibility: AttentionWaiting},
		{ID: "d", Title: "D", Status: "Awaiting your approval", Responsibility: AttentionAction},
	}
	items, gone := AttentionMerge(before, next)
	if len(items) != 3 || items[0].Changed != "due" || items[1].Changed != "responsibility" || items[2].Changed != "" {
		t.Fatalf("changes = %+v", items)
	}
	if len(gone) != 1 || gone[0].ID != "c" {
		t.Fatalf("gone = %+v", gone)
	}
	m := issue3986Model("en-US")
	m.Attention = AttentionBrief{Phase: AttentionReady, Items: items, Gone: gone, LoadedAt: time.Now(), Total: 3}
	got := renderNode(t, issue3986Panel(m, localUI{attention: issue3986UI{Open: true}}))
	for _, want := range []string{"The due date changed since your last read", "Your role in this changed since your last read", "no longer in your work"} {
		if !strings.Contains(got, want) {
			t.Errorf("reread lacks %q: %s", want, got)
		}
	}
}

func TestIssue3986_UnsafeLinkIsNotRendered(t *testing.T) {
	m := issue3986Model("en-US")
	m.Attention = AttentionBrief{Phase: AttentionReady, LoadedAt: time.Now(), Total: 1, Items: []AttentionItem{
		{ID: "x", Title: "Odd", Status: "Awaiting", Responsibility: AttentionAction, Href: "javascript:alert(1)"}}}
	got := renderNode(t, issue3986Panel(m, localUI{attention: issue3986UI{Open: true}}))
	if strings.Contains(got, "javascript:") {
		t.Fatalf("unsafe link rendered: %s", got)
	}
}

func TestIssue3986_StepsReadOnlyOnRequest(t *testing.T) {
	m := issue3986Model("en-US")
	state, effect := issue3986Step(m, issue3986UI{}, issue3986OpenAction)
	if !state.Open || effect != "open" {
		t.Fatalf("opening: %+v %q", state, effect)
	}
	state, effect = issue3986Step(m, state, issue3986ReadAction)
	if effect != "read" {
		t.Fatalf("read effect = %q", effect)
	}
	state, _ = issue3986Step(m, state, issue3986FilterAction)
	if !state.NoWaiting {
		t.Fatalf("filter not toggled")
	}
	state, effect = issue3986Step(m, state, issue3986CloseAction)
	if state.Open || effect != "close" {
		t.Fatalf("close: %+v %q", state, effect)
	}
	none := m
	none.Callbacks.RequestAttention = nil
	if _, effect := issue3986Step(none, issue3986UI{Open: true}, issue3986ReadAction); effect != "" {
		t.Fatalf("read without a reader: %q", effect)
	}
}

func TestIssue3986_DraftTextIsAPlainEditableBrief(t *testing.T) {
	m := issue3986Model("en-US")
	m.Attention = AttentionBrief{Phase: AttentionReady, Items: issue3986Items(), LoadedAt: time.Now(), Total: 5}
	got := issue3986DraftText(m, issue3986UI{})
	for _, want := range []string{"What needs my attention?", "Needs my action", "Promotion for Sam", "Waiting on others", "/journeys/j-late", "Overdue"} {
		if !strings.Contains(got, want) {
			t.Errorf("draft lacks %q: %s", want, got)
		}
	}
	if got := issue3986DraftText(m, issue3986UI{NoWaiting: true}); strings.Contains(got, "Dana") {
		t.Errorf("filtered item in draft: %s", got)
	}
	if issue3986DraftText(issue3986Model("en-US"), issue3986UI{}) != "" {
		t.Error("a draft was made without a read")
	}
}

func TestIssue3986_CopyInEveryLanguage(t *testing.T) {
	for _, locale := range []string{"de-DE", "ar"} {
		m := issue3986Model(locale)
		m.Attention = AttentionBrief{Phase: AttentionReady, Items: issue3986Items(), LoadedAt: time.Now(), Total: 5}
		for _, local := range []localUI{{}, {attention: issue3986UI{Open: true}}} {
			got := renderNode(t, issue3986Panel(m, local))
			if strings.Contains(got, "What needs my attention") || strings.Contains(got, "Needs your action") || strings.Contains(got, "Read my work") {
				t.Errorf("%s shows English: %s", locale, got)
			}
		}
	}
	if renderNode(t, issue3986Panel(issue3986Model("ar"), localUI{})) == renderNode(t, issue3986Panel(issue3986Model("de-DE"), localUI{})) {
		t.Error("German and Arabic copy are identical")
	}
}

func TestIssue3986_NotOfferedUntilJoinedAndFocusableAfterRead(t *testing.T) {
	m := issue3986Model("en-US")
	m.Conversations[0].Joined = false
	if issue3986Panel(m, localUI{}) != nil {
		t.Fatal("offered in an agent conversation the person has not joined")
	}
	m = issue3986Model("en-US")
	m.Attention = AttentionBrief{Phase: AttentionLoading}
	got := renderNode(t, issue3986Panel(m, localUI{attention: issue3986UI{Open: true}}))
	// The read button is replaced while reading, so focus moves to the region.
	if !strings.Contains(got, `tabIndex="-1"`) || !strings.Contains(got, `role="region"`) || !strings.Contains(got, `role="status"`) {
		t.Fatalf("loading panel cannot take focus or announce: %s", got)
	}
}
