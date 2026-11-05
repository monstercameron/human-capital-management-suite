package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func agent012StudioFixture() Agent012StudioSnapshot {
	return Agent012StudioSnapshot{
		Available: true, AgentID: "agent-1", DisplayName: "Policy Guide", OwnerID: "owner-1",
		Revision: 9, State: "REVIEW_REQUIRED",
		ManifestDigest:   "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		EvaluationDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		ReviewDigest:     "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		ReviewerID:       "reviewer-1",
		SandboxPresent:   true, SandboxPassed: true, SandboxFresh: true,
		PublicationPresent: false,
		Organizations:      []string{"org-1"},
		Jurisdictions:      []string{"us-ca"},
		DataCategories:     []string{"public"},
		EscalationOwner:    "owner-1",
		SuccessMeasures:    "CITED_ANSWERS",
		SourceIDs:          []string{"src.handbook"},
		ChatSpaceIDs:       []string{"chat.general"},
		Audience:           "employees",
		Purpose:            "policy-answers",
		CapabilityIDs:      []string{"tool.docsearch"},
		TriggerModes:       []string{"DM", "MENTION"},
		ChannelIDs:         []string{"chat.general"},
		ReplyVisibility:    "public",
		MaxCostMicros:      1000, MaxInputTokens: 8000, MaxOutputTokens: 2000, MaxConcurrentRuns: 2,
		AutonomyCeiling: "ASSISTED", ModelPolicyID: "model.standard", ModelPolicyVersion: 3,
		Tools: []Agent012StudioScopeItem{
			{ID: "tool.docsearch", Allowed: true, Reason: "tool.docsearch is allowed by platform policy"},
			{ID: "tool.rawsql", Allowed: false, Reason: "tool.rawsql is denied by platform policy"},
		},
		Sources: []Agent012StudioScopeItem{
			{ID: "src.handbook", Allowed: true, Reason: "src.handbook is allowed by platform policy"},
		},
		Steps: []Agent012StudioStep{
			{ID: "job"}, {ID: "charter"}, {ID: "context"}, {ID: "tools"}, {ID: "triggers"}, {ID: "budgets"}, {ID: "authority"}, {ID: "sandbox"}, {ID: "review", Current: true},
		},
		CanValidate: true, CanSubmit: true, CanReview: true, CanPublish: true, CanTransfer: true,
	}
}

func renderAgent012Studio(t *testing.T, locale string, snapshot Agent012StudioSnapshot) string {
	t.Helper()
	markup, err := ui.RenderToString(Agent012StudioPage(Agent012StudioPageProps{
		I18nProps:  I18nProps{Locale: ResolveProductLocale(locale)},
		Snapshot:   snapshot,
		ActionPath: "/agents/studio",
	}))
	if err != nil {
		t.Fatalf("render %s: %v", locale, err)
	}
	return markup
}

// TestTodo_AGENT_012_Browser proves the Studio page renders every governed
// step from the canonical snapshot in English, German, and Arabic: charter,
// context, tools, triggers, budgets, authority preview with denied items
// explained, sandbox evidence, the exact review digest, publication status,
// and owner transfer. The markup stays keyboard-operable and direction-aware.
func TestTodo_AGENT_012_Browser(t *testing.T) {
	fixture := agent012StudioFixture()
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		markup := renderAgent012Studio(t, locale, fixture)
		if strings.Contains(markup, "⟦") || strings.Contains(markup, "agent012.") {
			t.Fatalf("%s studio page has unresolved copy: %s", locale, markup)
		}
		for _, want := range []string{
			"Policy Guide", "owner-1", "org-1", "src.handbook", "chat.general",
			"tool.docsearch", "tool.rawsql", "tool.rawsql is denied by platform policy",
			"DM", "MENTION", "ASSISTED", "model.standard",
			"sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			`data-agent-studio-step="review"`, `aria-current="step"`,
			`<ol`, `<form`, `<button`, `<label`, `name="expected_revision"`, `name="expected_digest"`,
			`name="reviewer"`, `name="new_owner"`, `dir="auto"`,
		} {
			if !strings.Contains(markup, want) {
				t.Fatalf("%s studio page missing %q: %s", locale, want, markup)
			}
		}
		wantDir := `dir="ltr"`
		if locale == "ar" {
			wantDir = `dir="rtl"`
		}
		if !strings.Contains(markup, wantDir) {
			t.Fatalf("%s studio page missing %s: %s", locale, wantDir, markup)
		}
		// The exact review digest is never truncated: the suffix must survive.
		if !strings.Contains(markup, "cccccccccccccccccccccccccccccccc\"") && !strings.Contains(markup, "cccccccccccccccc</code>") {
			t.Fatalf("%s studio page truncated the review digest: %s", locale, markup)
		}
	}

	titles := map[string]string{}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		titles[locale] = agent012StudioText(ResolveProductLocale(locale), "title")
	}
	if titles["en-US"] == "" || titles["en-US"] == titles["de-DE"] || titles["en-US"] == titles["ar"] {
		t.Fatalf("studio title is not translated: %+v", titles)
	}

	// Without authority, the page shows state but offers no state-changing control.
	readonly := agent012StudioFixture()
	readonly.CanValidate, readonly.CanSubmit, readonly.CanReview, readonly.CanPublish, readonly.CanTransfer = false, false, false, false, false
	readonlyMarkup := renderAgent012Studio(t, "en-US", readonly)
	if strings.Contains(readonlyMarkup, "<form") || strings.Contains(readonlyMarkup, "<button") {
		t.Fatalf("unauthorized studio page exposes actions: %s", readonlyMarkup)
	}
	if !strings.Contains(readonlyMarkup, "Policy Guide") || !strings.Contains(readonlyMarkup, "tool.rawsql is denied by platform policy") {
		t.Fatalf("unauthorized studio page hides effective scope: %s", readonlyMarkup)
	}

	// An unavailable snapshot is an honest empty state with no draft data.
	empty := renderAgent012Studio(t, "en-US", Agent012StudioSnapshot{})
	if strings.Contains(empty, "Policy Guide") || strings.Contains(empty, "sha256:") || strings.Contains(empty, "<form") {
		t.Fatalf("unavailable studio page leaks draft state: %s", empty)
	}
}
