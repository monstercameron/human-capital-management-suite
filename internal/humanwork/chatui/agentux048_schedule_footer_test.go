package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func agentux048Render(t *testing.T, locale string, message AgentAnnouncementMessage) string {
	t.Helper()
	markup, err := ui.RenderToString(RenderAgentAnnouncementMessage(Model{Locale: locale}, message))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

// TestTodo_AGENTUX_048_Browser reads an announcement the way a second member
// does: a normal public message from the agent with its Sources, and under a
// scheduled one the line that says who set the schedule, in each language. A
// post made at once has no such line.
func TestTodo_AGENTUX_048_Browser(t *testing.T) {
	TestAgentUXProactive_RenderedPublicMessage(t)
	TestAgentUXProactive_NormalMessageProjection(t)
	TestAgentUXProactiveLive_PostedMessage_Browser(t)
	for locale, want := range map[string]string{
		"en-US": "Posted on a schedule set by Walt Brennan",
		"de-DE": "Nach einem Zeitplan von Walt Brennan veröffentlicht",
		"ar":    "نُشر وفق جدول أعدّه Walt Brennan",
	} {
		scheduled := AgentAnnouncementMessage{AgentName: "Assistant", OwnerName: "Walt Brennan", Text: "The next company holiday is Thanksgiving Day on Thursday, Nov 26.", Scheduled: true, PostedAt: time.Now(), Sources: []AgentAnnouncementSource{{Title: "2026 holiday guide", Href: "/workspace/app/docs?document=holiday-guide"}}}
		markup := agentux048Render(t, locale, scheduled)
		if !strings.Contains(markup, want) || !strings.Contains(markup, `data-agent-announcement-footer="scheduled"`) {
			t.Errorf("%s: a scheduled post does not say who set the schedule (%q): %s", locale, want, markup)
		}
		if strings.Index(markup, "2026 holiday guide") > strings.Index(markup, want) {
			t.Errorf("%s: the schedule line comes before the Sources", locale)
		}
		scheduled.Scheduled = false
		if strings.Contains(agentux048Render(t, locale, scheduled), "agent-announcement-footer") {
			t.Errorf("%s: a post made at once carries a schedule line", locale)
		}
	}
	// The line survives the stored body a second member loads.
	body, err := AnnouncementMessageBody(AgentAnnouncementMessage{AgentName: "Agent", OwnerName: "Walt Brennan", Scheduled: true, Text: "Thanksgiving is the next company holiday."})
	if err != nil {
		t.Fatal(err)
	}
	markup := render(t, Model{Locale: "en-US", State: StateReady, SelectedID: "general", Conversations: []Conversation{{ID: "general", Name: "General"}}, Messages: []Message{{ID: "announcement", AuthorID: "assistant", Author: "Assistant", Body: body, PersonaActor: &PersonaActor{PersonaID: "assistant", AgentID: "assistant", Trusted: true}}}})
	if !strings.Contains(markup, "Posted on a schedule set by Walt Brennan") {
		t.Fatalf("a second member does not read who set the schedule: %s", markup)
	}
}

// TestTodo_AGENTUX_048_Accessibility: the schedule line is quiet text that
// follows the reader's direction, Sources keep their labelled list, and the
// announcement carries no alert role that would interrupt a screen reader.
func TestTodo_AGENTUX_048_Accessibility(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		markup := agentux048Render(t, locale, AgentAnnouncementMessage{AgentName: "Assistant", OwnerName: "Walt Brennan", Text: "Thanksgiving is the next company holiday.", Scheduled: true, Sources: []AgentAnnouncementSource{{Title: "2026 holiday guide", Href: "/workspace/app/docs?document=holiday-guide"}}})
		footer := markup[strings.Index(markup, "<p class=\"muted agent-announcement-footer\""):]
		footer = footer[:strings.Index(footer, "</p>")]
		if !strings.Contains(footer, `dir="auto"`) {
			t.Errorf("%s: the schedule line does not follow the reader's direction: %s", locale, footer)
		}
		if !strings.Contains(markup, `aria-label="`+agentReplyFallback(locale, "chat.agent.sources", "Sources")+`"`) {
			t.Errorf("%s: Sources lost their label: %s", locale, markup)
		}
		if strings.Contains(markup, `role="alert"`) || strings.Contains(markup, `aria-live`) {
			t.Errorf("%s: a posted announcement interrupts assistive technology: %s", locale, markup)
		}
	}
}

// TestTodo_AGENTUX_053_Browser reads #general as a second member: a holiday
// announcement from Assistant with its source, a birthday post, and two ordinary
// questions, all as normal public messages with no private marker.
func TestTodo_AGENTUX_053_Browser(t *testing.T) {
	holiday, err := AnnouncementMessageBody(AgentAnnouncementMessage{AgentName: "Agent", OwnerName: "Walt Brennan", Scheduled: true, Text: "Thanksgiving Day is Thursday, Nov 26.", Sources: []AgentAnnouncementSource{{Title: "2026 holiday guide", Href: "/workspace/app/docs?document=holiday-guide"}}})
	if err != nil {
		t.Fatal(err)
	}
	birthday, err := AnnouncementMessageBody(AgentAnnouncementMessage{AgentName: "Agent", OwnerName: "Walt Brennan", Scheduled: true, Text: "Happy birthday, Priya! Today is June 3."})
	if err != nil {
		t.Fatal(err)
	}
	model := Model{Locale: "en-US", State: StateReady, SelectedID: "general", Conversations: []Conversation{{ID: "general", Name: "General"}},
		ResolvedPersonaMentions: []ResolvedPersonaMention{
			{Reference: ChatReference{Kind: "AGENT_MENTION", ID: "assistant", Display: "Assistant"}, Initials: "AS"},
			{Reference: ChatReference{Kind: "AGENT_MENTION", ID: "birthday-buddy", Display: "Birthday Buddy"}, Initials: "BB"},
		},
		Messages: []Message{
			{ID: "m1", AuthorID: "assistant", Author: "Assistant", Body: holiday, PersonaActor: &PersonaActor{PersonaID: "assistant", AgentID: "assistant", Trusted: true}},
			{ID: "m2", AuthorID: "birthday-buddy", Author: "Birthday Buddy", Body: birthday, PersonaActor: &PersonaActor{PersonaID: "birthday-buddy", AgentID: "birthday-buddy", Trusted: true}},
			{ID: "m3", AuthorID: "alice", Author: "Alice Smith", Body: "How many PTO hours carry over?"},
		}}
	markup := render(t, model)
	for _, want := range []string{"Thanksgiving Day is Thursday, Nov 26.", "2026 holiday guide", "Happy birthday, Priya! Today is June 3.", "Assistant", "Birthday Buddy", "How many PTO hours carry over?"} {
		if !strings.Contains(markup, want) {
			t.Errorf("#general history missing %q: %s", want, markup)
		}
	}
	if strings.Count(markup, `data-agent-announcement="true"`) != 2 || strings.Contains(markup, "Only visible to you") {
		t.Fatalf("the agents' posts are not two normal public messages: %s", markup)
	}
}
