package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"strings"
	"testing"
)

func TestTodo_AGENTP_020_ThreadBinding(t *testing.T) {
	model := Model{Locale: "ar", ThreadParentID: "root", PersonaInvocations: []PersonaThreadInvocation{{PostID: "root", Projection: personaProgressFixture()}, {PostID: "other", Projection: personaProgressFixture()}}}
	markup, err := ui.RenderToString(personaThreadProgress(model))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(markup, "persona-progress-surface") != 1 || !strings.Contains(markup, `dir="rtl"`) {
		t.Fatalf("wrong thread projection: %s", markup)
	}
}

func TestTodo_AGENTP_019_PostProfile(t *testing.T) {
	persona := resolvedPersonaMention("tenant", "agent", "Policy Helper", "room")
	persona.Purpose = "Answer policy questions"
	model := Model{Locale: "de-DE", ResolvedPersonaMentions: []ResolvedPersonaMention{persona}}
	plain, _ := ui.RenderToString(personaPostProfiles(model, Message{Body: "@Policy Helper"}))
	if strings.Contains(plain, "Answer policy") {
		t.Fatal("profile inferred from text")
	}
	canonical, _ := ui.RenderToString(personaPostProfiles(model, Message{PersonaReferences: []ChatReference{persona.Reference}}))
	if !strings.Contains(canonical, "Answer policy questions") || !strings.Contains(canonical, "Agentendetails") {
		t.Fatalf("canonical profile missing: %s", canonical)
	}
}

func TestTodo_AGENTP_020_ActivityLocales(t *testing.T) {
	for _, tc := range []struct{ locale, want string }{{"en-US", "Reading sources"}, {"de-DE", "Quellen werden gelesen"}, {"ar", "جارٍ قراءة المصادر"}} {
		if got := personaActivityLabel(tc.locale, "Reading sources"); got != tc.want {
			t.Fatalf("%s activity = %q", tc.locale, got)
		}
	}
	if got := personaActivityLabel("de-DE", "reading 3 authorized sources"); got != "reading 3 authorized sources" {
		t.Fatalf("unknown activity invented: %q", got)
	}
}

func TestTodo_AGENTP_019_TrustedBadgeLocales(t *testing.T) {
	actor := &PersonaActor{PersonaID: "policy", AgentID: "registered-agent", InvokerHandle: "walt", Trusted: true}
	for _, tc := range []struct{ locale, want string }{{"de-DE", "handelt für @walt"}, {"ar", "يعمل نيابة عن @walt"}} {
		markup, err := ui.RenderToString(PersonaBadgeLocalized(tc.locale, actor))
		if err != nil || !strings.Contains(markup, tc.want) || !strings.Contains(markup, `dir="auto"`) {
			t.Fatalf("%s badge: %s %v", tc.locale, markup, err)
		}
	}
	actor.Trusted = false
	markup, _ := ui.RenderToString(PersonaBadgeLocalized("ar", actor))
	if strings.Contains(markup, "@walt") {
		t.Fatalf("untrusted identity attributed: %s", markup)
	}
}

func TestTodo_AGENTP_019_ReceiptIdentity(t *testing.T) {
	model := Model{PersonaPostActors: map[string]PersonaPostActor{"delivered-post": {Display: "Policy Helper", Actor: PersonaActor{PersonaID: "policy", AgentID: "registered", InvokerHandle: "walt", Trusted: true}}}}
	message := personaTrustedMessage(model, Message{ID: "delivered-post", Author: "raw-actor"})
	if message.PersonaActor == nil || message.PersonaActor.PersonaID != "policy" || message.Author != "Policy Helper" {
		t.Fatalf("receipt identity lost: %+v", message)
	}
	if message := personaTrustedMessage(model, Message{ID: "human-post", Author: "Policy Helper"}); message.PersonaActor != nil {
		t.Fatal("name impersonation received badge")
	}
}

func TestTodo_AGENTP_019_DeliveredVersion(t *testing.T) {
	persona := resolvedPersonaMention("tenant", "agent", "Policy Helper", "room")
	persona.Version, persona.Purpose = "2", "Latest confidential capability"
	model := Model{Locale: "en-US", ResolvedPersonaMentions: []ResolvedPersonaMention{persona}}
	message := Message{Author: "Policy Helper", PersonaActor: &PersonaActor{PersonaID: "persona", PersonaVersion: "1", AgentID: "agent", Trusted: true}}
	markup, _ := ui.RenderToString(personaPostProfiles(model, message))
	if !strings.Contains(markup, "Version: 1") || strings.Contains(markup, persona.Purpose) {
		t.Fatalf("delivered version substituted: %s", markup)
	}
}
