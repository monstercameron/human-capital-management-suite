package chatui

import (
	"errors"
	"strings"
	"testing"
)

// errBarrierTestOffline stands for any failed call; it is defined here so the
// test also compiles under js/wasm, where the native stub file is not built.
var errBarrierTestOffline = errors.New("offline")

func barrierLoaded() ChatBarrierModel {
	return ChatBarrierModel{Locale: "en-US", Loaded: true, Data: ChatBarrierData{
		Rules: []ChatBarrierRuleView{{ID: "r1", GroupA: "Advisory", GroupB: "Sales"}},
		Teams: []string{"Advisory", "People", "Sales"},
	}}
}

func TestIssue3783_BarrierSettingsListAndForm(t *testing.T) {
	markup := renderNode(t, ChatBarrierSettings(barrierLoaded()))
	for _, want := range []string{
		"Information barriers", "Advisory and Sales cannot share a conversation", "Barriers in force",
		`aria-label="Remove the barrier between Advisory and Sales"`, `name="id"`, `value="r1"`,
		"First team", "Second team", `name="groupa"`, `name="groupb"`, `list="chatbarrier-teams"`,
		`<datalist id="chatbarrier-teams">`, `value="People"`, "Add barrier", "recorded in the audit log",
		`role="status"`, `aria-live="polite"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("settings lack %q: %s", want, markup)
		}
	}
	// Each field is labelled, and the status line is a live region.
	for _, id := range []string{"chatbarrier-team-a", "chatbarrier-team-b"} {
		if !strings.Contains(markup, `for="`+id+`"`) || !strings.Contains(markup, `id="`+id+`"`) {
			t.Fatalf("field %s is not labelled", id)
		}
	}
}

func TestIssue3783_BarrierSettingsEmptyAndNotices(t *testing.T) {
	none := barrierLoaded()
	none.Data.Rules = nil
	if markup := renderNode(t, ChatBarrierSettings(none)); !strings.Contains(markup, "No barriers are set. Any team can share a conversation with any other team.") || strings.Contains(markup, "chatbarrier-remove") {
		t.Fatal("empty list", markup)
	}
	for notice, want := range map[string]string{"added": "Barrier added.", "removed": "Barrier removed."} {
		m := barrierLoaded()
		m.Notice = notice
		if markup := renderNode(t, ChatBarrierSettings(m)); !strings.Contains(markup, want) {
			t.Fatalf("notice %s: %s", notice, markup)
		}
	}
	for refusal, want := range map[string]string{
		"invalid":   "Choose two different teams.",
		"unknown":   "One of those is not a team in the people directory.",
		"duplicate": "There is already a barrier between those two teams.",
		"limit":     "This workspace has reached its limit of 200 barriers.",
	} {
		m := barrierLoaded()
		m.FormError = refusal
		markup := renderNode(t, ChatBarrierSettings(m))
		if !strings.Contains(markup, want) || !strings.Contains(markup, `aria-invalid="true"`) || !strings.Contains(markup, `id="chatbarrier-error"`) || !strings.Contains(markup, `role="alert"`) {
			t.Fatalf("refusal %s is not announced against the form: %s", refusal, markup)
		}
	}
}

func TestIssue3783_BarrierSettingsStates(t *testing.T) {
	if markup := renderNode(t, ChatBarrierSettings(ChatBarrierModel{Locale: "en-US"})); !strings.Contains(markup, "Loading information barriers. Please wait.") || !strings.Contains(markup, `aria-busy="true"`) {
		t.Fatal("loading", markup)
	}
	if markup := renderNode(t, ChatBarrierSettings(ChatBarrierModel{Locale: "en-US", Loaded: true, Denied: true})); !strings.Contains(markup, "Only a workspace administrator can see or change information barriers.") || strings.Contains(markup, "<form") || strings.Contains(markup, "Advisory") {
		t.Fatal("denied must show nothing else", markup)
	}
	if markup := renderNode(t, ChatBarrierSettings(ChatBarrierModel{Locale: "en-US", Loaded: true, Unavailable: true})); !strings.Contains(markup, "not set up on this server") || strings.Contains(markup, "<form") {
		t.Fatal("unavailable", markup)
	}
	if markup := renderNode(t, ChatBarrierSettings(ChatBarrierModel{Locale: "en-US", Failed: true})); !strings.Contains(markup, "could not load or save") || !strings.Contains(markup, "Try again") || !strings.Contains(markup, `role="alert"`) {
		t.Fatal("failure", markup)
	}
	if markup := renderNode(t, ChatBarrierSettings(ChatBarrierModel{Locale: "en-US", Failed: true, SignedOut: true})); !strings.Contains(markup, "You were signed out") {
		t.Fatal("signed out", markup)
	}
	// A failed change over good data keeps the barriers on show and says so.
	failed := barrierLoaded()
	failed.Failed = true
	if markup := renderNode(t, ChatBarrierSettings(failed)); !strings.Contains(markup, "Advisory and Sales cannot share a conversation") || !strings.Contains(markup, "could not load or save") {
		t.Fatal("failed change", markup)
	}
	// Changes are disabled while one is on its way.
	busy := barrierLoaded()
	busy.Loading = true
	if markup := renderNode(t, ChatBarrierSettings(busy)); strings.Count(markup, "disabled") < 3 {
		t.Fatal("controls must be disabled while saving", markup)
	}
}

// Every string of the page exists in English, German and Arabic, and the page
// lays out right to left in Arabic.
func TestIssue3783_BarrierCopyInAllLanguages(t *testing.T) {
	for key, values := range chatBarrierCopy {
		for i, v := range values {
			if strings.TrimSpace(v) == "" {
				t.Errorf("%s has no text for language %d", key, i)
			}
		}
		if values[1] == values[0] && key != "retry" {
			t.Errorf("%s is not translated into German", key)
		}
		if values[2] == values[0] {
			t.Errorf("%s is not translated into Arabic", key)
		}
	}
	de := renderNode(t, ChatBarrierSettings(func() ChatBarrierModel { m := barrierLoaded(); m.Locale = "de-DE"; return m }()))
	if !strings.Contains(de, "Informationsbarrieren") || !strings.Contains(de, "Advisory und Sales können keine Unterhaltung teilen") {
		t.Fatal("German page", de)
	}
	ar := renderNode(t, ChatBarrierSettings(func() ChatBarrierModel { m := barrierLoaded(); m.Locale = "ar"; return m }()))
	if !strings.Contains(ar, "حواجز المعلومات") || !strings.Contains(ar, `dir="rtl"`) {
		t.Fatal("Arabic page is not right to left", ar)
	}
	for locale, want := range map[string]string{"en-US": "An information barrier does not allow these people", "de-DE": "Eine Informationsbarriere erlaubt es nicht", "ar": "يمنع حاجز معلومات"} {
		if got := BarrierRefusalNotice(locale); !strings.Contains(got, want) {
			t.Errorf("refusal notice in %s = %q", locale, got)
		}
		if strings.Contains(strings.ToLower(BarrierRefusalNotice(locale)), "sales") {
			t.Errorf("the refusal must name no team")
		}
	}
}

func TestIssue3783_BarrierChangeRefusalsMapToFormErrors(t *testing.T) {
	for _, c := range []struct {
		action string
		status int
		code   string
		want   string
	}{
		{"add", 400, "unknown_group", "unknown"},
		{"add", 400, "invalid_request", "invalid"},
		{"add", 409, "already_exists", "duplicate"},
		{"add", 409, "limit_reached", "limit"},
		{"add", 500, "", ""},
		{"add", 403, "permission_denied", ""},
		{"remove", 404, "not_found", ""},
	} {
		if got := chatBarrierChangeRefusal(c.action, c.status, c.code); got != c.want {
			t.Errorf("%s %d %q = %q, want %q", c.action, c.status, c.code, got, c.want)
		}
	}
	m := chatBarrierReadAnswered(barrierLoaded(), ChatBarrierData{}, 503, errBarrierTestOffline, "")
	if !m.Failed || len(m.Data.Rules) != 1 {
		t.Fatalf("a failed read must keep the barriers on show: %+v", m)
	}
	if m := chatBarrierReadAnswered(ChatBarrierModel{}, ChatBarrierData{}, 503, errBarrierTestOffline, ""); !m.Unavailable || !m.Loaded {
		t.Fatalf("503 on the first read is 'not set up': %+v", m)
	}
	if m := chatBarrierReadAnswered(ChatBarrierModel{}, ChatBarrierData{}, 403, errBarrierTestOffline, ""); !m.Denied {
		t.Fatalf("403 is a refusal: %+v", m)
	}
}
