package chatui

import (
	"strings"
	"testing"
)

func holdLoaded(locale string) ChatHoldModel {
	return ChatHoldModel{Locale: locale, Loaded: true, Data: ChatHoldData{
		Holds: []ChatHoldItem{
			{ID: "h1", ConversationID: "c1", ConversationName: "Finance", Matter: "M-7", Reason: "Preserve for the claim", PlacedAt: "2026-09-22T12:00:00Z", PlacedByMe: true, Active: true},
			{ID: "h2", ConversationID: "c2", Reason: "Audit request", PlacedAt: "2026-08-01T09:00:00Z", Active: false, ReleasedAt: "2026-09-01T09:00:00Z", ReleasedByMe: true, ReleaseReason: "Settled"},
		},
		Conversations: []ChatHoldChoice{{ID: "c1", Name: "Finance", Kind: "PRIVATE_CHANNEL"}, {ID: "c3", Name: "", Kind: "DIRECT"}},
	}}
}

func TestIssue3779_HoldSettingsListAndForm(t *testing.T) {
	markup := renderNode(t, ChatHoldSettings(holdLoaded("en-US")))
	for _, want := range []string{
		"Legal holds", "Holds in force", "Finance", "Matter: M-7", "Reason: Preserve for the claim", "Placed by you on",
		`aria-label="Release the hold on Finance"`, `name="id"`, `value="h1"`, `name="reason"`, "Reason for releasing the hold on Finance",
		"Released holds", "A conversation you cannot open", "Released by you on", "Release reason: Settled",
		"Place a hold", `name="conversation"`, "Choose a conversation", "Direct or group message", `name="matter"`, "Reason for the hold",
		"Place hold", "recorded in the audit log", `aria-live="polite"`, `role="status"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("settings lack %q: %s", want, markup)
		}
	}
	for _, id := range []string{"chathold-conversation", "chathold-matter", "chathold-reason", "chathold-release-h1"} {
		if !strings.Contains(markup, `for="`+id+`"`) || !strings.Contains(markup, `id="`+id+`"`) {
			t.Fatalf("field %s is not labelled", id)
		}
	}
	// A released hold offers no release form again, and nothing leaks a person id.
	if strings.Contains(markup, `value="h2"`) {
		t.Fatalf("a released hold must not offer release again: %s", markup)
	}
}

func TestIssue3779_HoldSettingsStatesAndRefusals(t *testing.T) {
	empty := holdLoaded("en-US")
	empty.Data.Holds = nil
	if markup := renderNode(t, ChatHoldSettings(empty)); !strings.Contains(markup, "No legal holds are in force.") || strings.Contains(markup, "Released holds") {
		t.Fatal("empty list", markup)
	}
	noChoices := holdLoaded("en-US")
	noChoices.Data.Conversations = nil
	if markup := renderNode(t, ChatHoldSettings(noChoices)); !strings.Contains(markup, "You cannot open any conversation") || !strings.Contains(markup, "disabled") {
		t.Fatal("no conversations to choose", markup)
	}
	for state, want := range map[string]string{"placed": "Hold placed.", "released": "Hold released."} {
		m := holdLoaded("en-US")
		m.Notice = state
		if markup := renderNode(t, ChatHoldSettings(m)); !strings.Contains(markup, want) {
			t.Fatalf("notice %s: %s", state, markup)
		}
	}
	for refusal, want := range map[string]string{"invalid": "Check the reason", "missing": "Choose a conversation you can open.", "gone": "no hold was placed"} {
		m := holdLoaded("en-US")
		m.FormError = refusal
		markup := renderNode(t, ChatHoldSettings(m))
		if !strings.Contains(markup, want) || !strings.Contains(markup, `aria-invalid="true"`) || !strings.Contains(markup, `role="alert"`) {
			t.Fatalf("refusal %s: %s", refusal, markup)
		}
	}
	m := holdLoaded("en-US")
	m.ReleaseError, m.ReleaseID = "release_missing", "h1"
	if markup := renderNode(t, ChatHoldSettings(m)); !strings.Contains(markup, "Say why the hold is being released.") || !strings.Contains(markup, `id="chathold-release-h1-error"`) {
		t.Fatalf("release refusal: %s", markup)
	}
	for name, model := range map[string]ChatHoldModel{
		"denied":      {Locale: "en-US", Loaded: true, Denied: true},
		"unavailable": {Locale: "en-US", Loaded: true, Unavailable: true},
		"loading":     {Locale: "en-US"},
		"failed":      {Locale: "en-US", Failed: true},
		"signed out":  {Locale: "en-US", Failed: true, SignedOut: true},
	} {
		markup := renderNode(t, ChatHoldSettings(model))
		if strings.Contains(markup, `name="reason"`) || !strings.Contains(markup, "Legal holds") {
			t.Fatalf("%s must show the shell only: %s", name, markup)
		}
	}
	if markup := renderNode(t, ChatHoldSettings(ChatHoldModel{Locale: "en-US", Failed: true})); !strings.Contains(markup, "Try again") {
		t.Fatal("a failure offers Try again", markup)
	}
}

func TestIssue3779_HoldSettingsLocalisedAndRTL(t *testing.T) {
	de := renderNode(t, ChatHoldSettings(holdLoaded("de-DE")))
	for _, want := range []string{"Rechtliche Aufbewahrungspflichten", "Geltende Pflichten", "Pflicht festlegen", "Grund der Pflicht"} {
		if !strings.Contains(de, want) {
			t.Fatalf("de-DE lacks %q: %s", want, de)
		}
	}
	ar := renderNode(t, ChatHoldSettings(holdLoaded("ar")))
	for _, want := range []string{`dir="rtl"`, "الحجوزات القانونية", "الحجوزات السارية", "فرض الحجز"} {
		if !strings.Contains(ar, want) {
			t.Fatalf("ar lacks %q: %s", want, ar)
		}
	}
	// Every string exists in all three languages.
	for key, values := range chatHoldCopy {
		for i, v := range values {
			if strings.TrimSpace(v) == "" {
				t.Errorf("hold copy %q has no text for language %d", key, i)
			}
		}
	}
	for _, key := range []string{"w_hold.place", "w_hold.release"} {
		for _, locale := range []string{"en-US", "de-DE", "ar"} {
			if got := ChatAuditWhat(locale, key[2:]); got == key[2:] {
				t.Errorf("the audit log has no words for %s in %s", key[2:], locale)
			}
		}
	}
}

func TestIssue3779_HoldRefusalMapping(t *testing.T) {
	for _, c := range []struct {
		action string
		status int
		want   string
	}{
		{"place", 400, "invalid"}, {"place", 404, "gone"}, {"place", 500, ""}, {"place", 403, ""},
		{"release", 400, "release_missing"}, {"release", 404, "release_gone"}, {"release", 409, "release_gone"}, {"release", 503, ""},
	} {
		if got := chatHoldRefusal(c.action, c.status); got != c.want {
			t.Errorf("%s %d: %q, want %q", c.action, c.status, got, c.want)
		}
	}
	m := chatHoldReadAnswered(ChatHoldModel{Locale: "en-US", Loaded: true, Data: holdLoaded("en-US").Data}, ChatHoldData{}, 403, errBarrierTestOffline, "")
	if !m.Denied {
		t.Fatal("a 403 reads as denied")
	}
	m = chatHoldReadAnswered(ChatHoldModel{Locale: "en-US", Loaded: true, Data: holdLoaded("en-US").Data}, ChatHoldData{}, 500, errBarrierTestOffline, "")
	if !m.Failed || len(m.Data.Holds) != 2 {
		t.Fatal("a failed refresh keeps the holds on show")
	}
}
