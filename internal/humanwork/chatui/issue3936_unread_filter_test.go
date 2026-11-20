package chatui

import (
	"strings"
	"testing"
)

func issue3936Rows(markup string) []string {
	var ids []string
	for _, part := range strings.Split(markup, `data-action="select"`)[1:] {
		if at := strings.Index(part, `data-id="`); at >= 0 {
			rest := part[at+len(`data-id="`):]
			if end := strings.Index(rest, `"`); end >= 0 {
				ids = append(ids, rest[:end])
			}
		}
	}
	return ids
}

func issue3936Model(locale string) Model {
	m := chatux007Model(locale)
	m.Sections = []SidebarSection{
		{ID: "channels", Name: "Channels", Chats: m.Conversations[:3]},
		{ID: "mine", Name: "Mine", Collapsed: true, Chats: m.Conversations[3:]},
	}
	return m
}

func TestIssue3936_FilterOffShowsEveryRowAndAToggle(t *testing.T) {
	m := issue3936Model("en-US")
	markup := renderNode(t, rail(m, handlers{}))
	if !strings.Contains(markup, `data-action="toggle-unread-filter"`) || !strings.Contains(markup, `aria-pressed="false"`) {
		t.Fatalf("no unpressed Unread toggle: %s", markup)
	}
	// general (open), unread, mention are unread; read-room is read; muted has
	// no mention.
	if !strings.Contains(markup, "3 unread") || !strings.Contains(markup, "including mentions of you") {
		t.Fatalf("toggle does not carry the count and the mention: %s", markup)
	}
	if got := strings.Join(issue3936Rows(markup), ","); got != "general,read,unread,mention,muted" {
		t.Fatalf("rows = %s", got)
	}
}

func TestIssue3936_FilterGathersUnreadAcrossSectionsIncludingFolded(t *testing.T) {
	m := issue3936Model("en-US")
	m.SelectedID = "read"
	markup := renderNode(t, rail(m, handlers{local: localUI{unreadOnly: true}}))
	got := strings.Join(issue3936Rows(markup), ",")
	// general, unread and mention (folded section) are listed; the open read-room
	// stays; the muted row is not.
	if got != "general,read,unread,mention" {
		t.Fatalf("rows = %s", got)
	}
	if !strings.Contains(markup, `aria-pressed="true"`) || !strings.Contains(markup, "Muted conversations without a mention are not shown here.") {
		t.Fatalf("pressed state or muted note missing: %s", markup)
	}
	if strings.Contains(markup, "chatbug092-archived") {
		t.Fatalf("archived section shown in the filter")
	}
}

func TestIssue3936_RowThatBecomesReadStaysUntilFilterIsToggled(t *testing.T) {
	m := issue3936Model("en-US")
	m.SelectedID = "mention"
	for i := range m.Sections[0].Chats {
		if m.Sections[0].Chats[i].ID == "unread" {
			m.Sections[0].Chats[i].Unread = 0
		}
	}
	h := handlers{local: localUI{unreadOnly: true, unreadKept: map[string]bool{"unread": true}}}
	if got := strings.Join(issue3936Rows(renderNode(t, rail(m, h))), ","); got != "general,unread,mention" {
		t.Fatalf("a row that became read vanished: %s", got)
	}
	h.local.unreadKept = nil
	if got := strings.Join(issue3936Rows(renderNode(t, rail(m, h))), ","); got != "general,mention" {
		t.Fatalf("a read row that was never kept is listed: %s", got)
	}
}

func TestIssue3936_EmptyLoadingAndErrorStatesOfferTheWayBack(t *testing.T) {
	m := issue3936Model("en-US")
	m.SelectedID = ""
	for _, s := range m.Sections {
		for i := range s.Chats {
			s.Chats[i].Unread, s.Chats[i].Mentions = 0, 0
		}
	}
	h := handlers{local: localUI{unreadOnly: true}}
	empty := renderNode(t, rail(m, h))
	if !strings.Contains(empty, "all caught up") || !strings.Contains(empty, "Show all conversations") || len(issue3936Rows(empty)) != 0 {
		t.Fatalf("empty state: %s", empty)
	}
	m.State = StateLoading
	if got := renderNode(t, rail(m, h)); !strings.Contains(got, "Loading your conversations") || strings.Contains(got, "all caught up") {
		t.Fatalf("loading state: %s", got)
	}
	m.State = StateError
	if got := renderNode(t, rail(m, h)); !strings.Contains(got, `role="alert"`) || !strings.Contains(got, "Show all conversations") || strings.Contains(got, "all caught up") {
		t.Fatalf("error state: %s", got)
	}
}

func TestIssue3936_CopyIsTranslatedAndLive(t *testing.T) {
	for _, locale := range []string{"de-DE", "ar"} {
		m := issue3936Model(locale)
		markup := renderNode(t, rail(m, handlers{local: localUI{unreadOnly: true}}))
		for key, en := range issue3936Copy["en-US"] {
			if issue3936Copy[locale][key] == "" || issue3936Copy[locale][key] == en {
				t.Fatalf("%s: %s is not translated", locale, key)
			}
		}
		if !strings.Contains(markup, issue3936Copy[locale][keyIssue3936Label]) || !strings.Contains(markup, `aria-live="polite"`) || strings.Contains(markup, "chat.issue3936") {
			t.Fatalf("%s: %s", locale, markup)
		}
	}
}

func TestIssue3936_SnapshotIsTheUnreadRowsAcrossFoldedSections(t *testing.T) {
	got := issue3936Snapshot(issue3936Model("en-US"))
	if len(got) != 3 || !got["general"] || !got["unread"] || !got["mention"] || got["read"] || got["muted"] {
		t.Fatalf("snapshot = %v", got)
	}
}

func TestIssue3936_NoMutedNoteWhenNothingIsMutedAndToggleCanBeFocused(t *testing.T) {
	m := issue3936Model("en-US")
	for _, s := range m.Sections {
		for i := range s.Chats {
			s.Chats[i].Muted = false
			s.Chats[i].Unread = 0
		}
	}
	for _, s := range m.Sections {
		for i := range s.Chats {
			if s.Chats[i].ID == "unread" {
				s.Chats[i].Unread = 2
			}
		}
	}
	markup := renderNode(t, rail(m, handlers{local: localUI{unreadOnly: true}}))
	if strings.Contains(markup, "Muted conversations without a mention are not shown") {
		t.Fatalf("muted note shown with nothing muted: %s", markup)
	}
	if !strings.Contains(markup, `id="issue3936-toggle"`) || strings.Contains(markup, "Showing") {
		t.Fatalf("toggle id or status wording: %s", markup)
	}
}
