package chatui

import (
	"strings"
	"testing"
)

func issue3780Loaded() ChatAuditModel {
	return ChatAuditModel{Locale: "en-US", Loaded: true, Data: ChatAuditData{Entries: []ChatAuditEntry{
		{EventID: "e2", Sequence: 2, ActorID: "bob", Action: "moderation.remove", TargetType: "chat_post", TargetID: "post-9", Reason: "off topic", At: "2026-11-18T09:30:00Z"},
		{EventID: "e1", Sequence: 1, ActorID: "=cmd", Action: "chat.post.send", TargetType: "chat", TargetID: "post:1", Reason: "chat.post.send", At: "2026-11-18T08:00:00Z"},
	}, NextCursor: "next"}}
}

func TestIssue3780_AuditLogRendersEntriesInPlainWords(t *testing.T) {
	markup := renderNode(t, ChatAuditLogSettings(issue3780Loaded()))
	for _, want := range []string{
		"Audit log", "It never shows what a message said", `<th scope="col"`, "Time", "Person", "What happened", "Item", "Reason",
		"Removed a message (moderation)", "Sent a message", "November 18, 2026, 9:30 AM UTC", `datetime="2026-11-18T09:30:00Z"`, "off topic", "bob", "post-9",
		"2 events shown. More are available.", "Show more", "Download CSV", "Show events", "Clear filters",
		`name="actor"`, `name="action"`, `name="conversation"`, `name="fromdate"`, `name="todate"`, `aria-live="polite"`, "chatlangadmin-page",
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("audit log lacks %q: %s", want, markup)
		}
	}
	// Every label is tied to its control and the text is escaped, not interpreted.
	for _, id := range []string{"chataudit-actor", "chataudit-action", "chataudit-conversation", "chataudit-from", "chataudit-to"} {
		if !strings.Contains(markup, `for="`+id+`"`) || !strings.Contains(markup, `id="`+id+`"`) {
			t.Fatalf("control %s has no label", id)
		}
	}
	if strings.Contains(markup, "<script") {
		t.Fatal("markup contains a script")
	}
}

func TestIssue3780_AuditLogStates(t *testing.T) {
	cases := map[string]struct {
		m    ChatAuditModel
		want string
	}{
		"loading":     {ChatAuditModel{Locale: "en-US"}, "Loading the audit log"},
		"failed":      {ChatAuditModel{Locale: "en-US", Failed: true}, "The audit log could not load. Try again."},
		"signed out":  {ChatAuditModel{Locale: "en-US", Failed: true, SignedOut: true}, "You were signed out"},
		"denied":      {ChatAuditModel{Locale: "en-US", Loaded: true, Denied: true}, "Only a workspace administrator can read the audit log."},
		"unavailable": {ChatAuditModel{Locale: "en-US", Loaded: true, Unavailable: true}, "not set up on this server"},
		"empty":       {ChatAuditModel{Locale: "en-US", Loaded: true}, "Nothing has been recorded yet."},
		"no match":    {ChatAuditModel{Locale: "en-US", Loaded: true, Filter: ChatAuditFilter{Actor: "x"}}, "No events match these filters."},
		"exporting":   {ChatAuditModel{Locale: "en-US", Loaded: true, Exporting: true}, "Preparing the download"},
		"exported":    {ChatAuditModel{Locale: "en-US", Loaded: true, Exported: true}, "It has been recorded in the log."},
		"export fail": {ChatAuditModel{Locale: "en-US", Loaded: true, ExportFailed: true}, "so nothing was downloaded"},
		"more fail":   {func() ChatAuditModel { m := issue3780Loaded(); m.MoreFailed = true; return m }(), "More events could not load"},
	}
	for name, tc := range cases {
		if markup := renderNode(t, ChatAuditLogSettings(tc.m)); !strings.Contains(markup, tc.want) {
			t.Errorf("%s lacks %q: %s", name, tc.want, markup)
		}
	}
	// A refused reader sees no table and no data.
	denied := issue3780Loaded()
	denied.Denied = true
	if markup := renderNode(t, ChatAuditLogSettings(denied)); strings.Contains(markup, "bob") || strings.Contains(markup, "<table") || strings.Contains(markup, "Download CSV") {
		t.Fatalf("a refused reader was shown log content: %s", markup)
	}
	if markup := renderNode(t, ChatAuditLogSettings(ChatAuditModel{Locale: "en-US", Failed: true})); !strings.Contains(markup, "Try again") {
		t.Fatal("a failed read offers no retry")
	}
}

func TestIssue3780_AuditLogLanguagesAndDirection(t *testing.T) {
	for locale, want := range map[string][]string{
		"de-DE": {"Prüfprotokoll", "Nachricht entfernt", "CSV herunterladen", "Ereignisse angezeigt", `dir="ltr"`},
		"ar":    {"سجل التدقيق", "أزال رسالة", "تنزيل CSV", `dir="rtl"`},
	} {
		m := issue3780Loaded()
		m.Locale = locale
		markup := renderNode(t, ChatAuditLogSettings(m))
		for _, w := range want {
			if w == "Nachricht entfernt" {
				w = "Hat eine Nachricht entfernt"
			}
			if !strings.Contains(markup, w) {
				t.Errorf("%s lacks %q", locale, w)
			}
		}
		if strings.ContainsAny(markup, "⟦⟧") {
			t.Errorf("%s shows a raw key", locale)
		}
	}
	// Every key has all three languages and nothing falls back to a raw key.
	for key, v := range chatAuditCopy {
		for i, text := range v {
			if strings.TrimSpace(text) == "" {
				t.Errorf("%s has no text for language %d", key, i)
			}
		}
	}
	if got := ChatAuditWhat("en-US", "something.new"); got != "something.new" {
		t.Fatalf("an unknown action is shown as the server named it, got %q", got)
	}
}

func TestIssue3780_AuditAnsweredKeepsEntriesWhenMoreFails(t *testing.T) {
	m := chatAuditAnswered(issue3780Loaded(), ChatAuditData{}, 503, errChatAuditTest, ChatAuditFilter{}, true)
	if len(m.Data.Entries) != 2 || !m.MoreFailed || m.Failed {
		t.Fatalf("%+v", m)
	}
	m = chatAuditAnswered(issue3780Loaded(), ChatAuditData{Entries: []ChatAuditEntry{{EventID: "e0"}}}, 200, nil, ChatAuditFilter{}, true)
	if len(m.Data.Entries) != 3 || m.Data.NextCursor != "" {
		t.Fatalf("a next page is added to the page on show: %+v", m.Data)
	}
	m = chatAuditAnswered(ChatAuditModel{}, ChatAuditData{}, 403, errChatAuditTest, ChatAuditFilter{}, false)
	if !m.Denied || !m.Loaded {
		t.Fatalf("403 must read as refused: %+v", m)
	}
	m = chatAuditAnswered(ChatAuditModel{}, ChatAuditData{}, 401, errChatAuditTest, ChatAuditFilter{}, false)
	if !m.Failed || !m.SignedOut {
		t.Fatalf("401 must read as signed out: %+v", m)
	}
}

type chatAuditTestError struct{}

func (chatAuditTestError) Error() string { return "boom" }

var errChatAuditTest error = chatAuditTestError{}
