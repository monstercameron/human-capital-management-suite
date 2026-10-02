package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_AGENTP_011_RenderFiltersRecipientAndExpiry(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	posts := []EphemeralMessage{
		{ID: "visible", Body: "private answer", OnlyVisibleToYou: true, ExpiresAt: now.Add(time.Hour)},
		{ID: "public", Body: "must not render", OnlyVisibleToYou: false, ExpiresAt: now.Add(time.Hour)},
		{ID: "expired", Body: "too late", OnlyVisibleToYou: true, ExpiresAt: now},
		{ID: "missing-expiry", Body: "invalid", OnlyVisibleToYou: true},
	}
	got := VisibleEphemeralMessages(posts, now)
	if len(got) != 1 || got[0].ID != "visible" {
		t.Fatalf("visible posts = %+v", got)
	}
}

func TestTodo_AGENTP_011_RenderAccessiblePrivateAgentAnswer(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	model := Model{Locale: "de-DE"}
	markup, err := ui.RenderToString(RenderEphemeralMessage(model, EphemeralMessage{
		ID: "e1", ThreadID: "root", Body: "private answer", OnlyVisibleToYou: true,
		ExpiresAt: now.Add(24 * time.Hour), ThreadLink: "/chat/room/root",
	}, now))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"chat-ephemeral", "Nur für Sie sichtbar", "private answer", "agent-reply-name", `aria-live="polite"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("markup missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, "Quellthread") || strings.Contains(markup, `href="/chat/room/root"`) {
		t.Fatalf("private answer kept the obsolete source-thread action: %s", markup)
	}
}

func TestTodo_AGENTP_011_RenderExpiredIsHidden(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	markup, err := ui.RenderToString(RenderEphemeralMessage(Model{}, EphemeralMessage{
		ID: "e1", Body: "expired answer", OnlyVisibleToYou: true, ExpiresAt: now,
	}, now))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "expired answer") || !strings.Contains(markup, `aria-hidden="true"`) {
		t.Fatalf("expired ephemeral content leaked into markup: %s", markup)
	}
}

func TestTodo_AGENTP_011_ReducedMotionStyles(t *testing.T) {
	if !strings.Contains(EphemeralStyles, "prefers-reduced-motion:reduce") || !strings.Contains(EphemeralStyles, "animation:none") || !strings.Contains(EphemeralStyles, "transition:none") {
		t.Fatalf("reduced-motion rule missing: %s", EphemeralStyles)
	}
}

func TestTodo_AGENTP_011_WorkspaceRendersEphemeralOutsideHistory(t *testing.T) {
	markup, err := ui.RenderToString(Build(Model{
		State: StateReady, SelectedID: "room",
		Conversations: []Conversation{{ID: "room", Name: "Room"}},
		EphemeralMessages: []EphemeralMessage{{
			ID: "private-1", Body: "private persona answer", OnlyVisibleToYou: true,
			ExpiresAt: time.Now().Add(time.Hour), ThreadID: "root",
		}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "private persona answer") || !strings.Contains(markup, "Only visible to you") {
		t.Fatalf("workspace omitted visible ephemeral delivery: %s", markup)
	}
}

func TestTodo_AGENTP_011_ExpiredEphemeralDoesNotReplaceEmptyState(t *testing.T) {
	now := time.Now()
	markup, err := ui.RenderToString(Build(Model{
		State: StateReady, SelectedID: "room",
		Conversations: []Conversation{{ID: "room", Name: "Room"}},
		EphemeralMessages: []EphemeralMessage{{
			ID: "expired", Body: "private answer", OnlyVisibleToYou: true, ExpiresAt: now,
		}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "private answer") || !strings.Contains(markup, "No messages yet") {
		t.Fatalf("expired delivery remained visible: %s", markup)
	}
}
