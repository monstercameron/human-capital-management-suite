package productui

import (
	"html"
	"strings"
	"testing"
)

func TestAgentUXOps5_RunningErrorRetryAndContact(t *testing.T) {
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(localeID)
		markup := renderAgentOperationsTest(t, RenderAgentControls(locale, AgentControlsSnapshot{TechnicalContact: "Maya Chen"}, "error"))
		for _, want := range []string{`role="alert"`, `data-owner-refresh="true"`, agentControlsText(locale, "retry"), "Maya Chen"} {
			if !strings.Contains(markup, want) {
				t.Fatalf("%s running error missing %q: %s", localeID, want, markup)
			}
		}
		fallback := renderAgentOperationsTest(t, RenderAgentControls(locale, AgentControlsSnapshot{}, "error"))
		if !strings.Contains(fallback, agentControlsText(locale, "unavailable_help")) || strings.Contains(fallback, "technical contact") {
			t.Fatalf("%s fallback contact wrong: %s", localeID, fallback)
		}
	}
}

func TestAgentUXOps5_TabsAndAdministratorNav(t *testing.T) {
	view := ApplyLocale(NewView(PageAgentOperations, "tenant", "admin", ""), ResolveProductLocale("en-US"))
	view.AgentsProjection = &AgentsAvailabilityProjection{ViewerIsAdmin: true}
	view.EffectivePermissions = []RolePagePermission{{Page: PageAgents, View: true}, {Page: PagePersonaAdmin, View: true}, {Page: PageAgentOperations, View: true}}
	markup := renderAgentOperationsTest(t, BuildAgentOperationsPage(view))
	for _, want := range []string{
		`aria-label="Agent pages"`, ">Ask</a>", ">Setup</a>", `aria-current="page"`, ">Operations</span>",
		`tab=rollout`, `tab=move`,
		`role="tabpanel"`, `aria-labelledby="agent-operations-tab-running"`, `data-agent-operations-panel="move"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("operations tabs/nav missing %q: %s", want, markup)
		}
	}
	for _, forbidden := range []string{"Agent administration", "Monitor running agents", "Define who an agent is"} {
		if strings.Contains(markup, forbidden) {
			t.Fatalf("operations hero retained retired intro %q: %s", forbidden, markup)
		}
	}
}

func TestAgentUXOps5_RolloutStatesAndBidi(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	allCurrent := AgentRolloutSnapshot{Available: true, CanPreview: true,
		Personas: []AgentRolloutPersona{{ID: "policy-helper", Name: "Policy Helper"}},
		Versions: []AgentRolloutVersion{{PersonaID: "policy-helper", Version: 4, PublishedAt: "2026-09-30", Current: true}},
		Installations: []AgentRolloutInstallation{
			{ID: "general", PersonaID: "policy-helper", Name: "general", ConversationKind: "PUBLIC_CHANNEL", MemberCount: 18, Visible: true, Version: 4},
			{ID: "private", PersonaID: "policy-helper", MemberCount: 3, Version: 4},
		},
	}
	currentMarkup := html.UnescapeString(renderAgentOperationsTest(t, AgentRolloutPortableMount(locale, allCurrent, AgentPortableSnapshot{})))
	for _, want := range []string{"Version 4 (current) · published Sep 30", "All 2 conversations already run version 4. Nothing to roll out."} {
		if !strings.Contains(currentMarkup, want) {
			t.Fatalf("all-current rollout missing %q: %s", want, currentMarkup)
		}
	}
	if !strings.Contains(currentMarkup, `class="agent-rollout-target-fields" hidden`) {
		t.Fatal("all-current rollout exposes conversation controls")
	}

	several := allCurrent
	several.NotListedCount = 1
	several.Versions = []AgentRolloutVersion{{PersonaID: "policy-helper", Version: 5, PublishedAt: "2026-10-01"}}
	several.Installations[0].Version = 4
	several.Installations[0].CanaryEligible = true
	several.Installations[1].Version = 4
	several.Installations[1].UpdateBlocked = true
	markup := html.UnescapeString(renderAgentOperationsTest(t, AgentRolloutPortableMount(locale, several, AgentPortableSnapshot{})))
	for _, want := range []string{`<bdi dir="ltr">#general</bdi>`, `data-rollout-canary-container="true" hidden`, `data-rollout-canary-row="general" hidden`, "2 private conversation is not listed; its owner must update it."} {
		if !strings.Contains(markup, want) {
			t.Fatalf("several rollout missing %q: %s", want, markup)
		}
	}
}

func TestAgentUXOps5_RolloutLocalizedLabels(t *testing.T) {
	ar := ResolveProductLocale("ar")
	label := agentPortableManifestLabel(ar, AgentPortableManifest{ID: "policy-helper", Name: "Agent.starter.policy Helper", Version: "2"})
	if strings.Contains(label, "Agent.starter") || strings.Contains(label, "2") || !strings.Contains(label, "Policy Helper") {
		t.Fatalf("Arabic manifest label leaked raw name or Western digit: %q", label)
	}
	de := ResolveProductLocale("de-DE")
	markup := renderAgentOperationsTest(t, AgentRolloutPortableMount(de, AgentRolloutSnapshot{Available: true, CanPreview: true,
		Personas:      []AgentRolloutPersona{{ID: "policy-helper", Name: "Policy Helper"}},
		Versions:      []AgentRolloutVersion{{PersonaID: "policy-helper", Version: 4, PublishedAt: "2026-09-30", Current: true}},
		Installations: []AgentRolloutInstallation{{ID: "general", PersonaID: "policy-helper", Name: "general", ConversationKind: "PUBLIC_CHANNEL", MemberCount: 18, Visible: true, Version: 4}},
	}, AgentPortableSnapshot{}))
	for _, want := range []string{"Version 4 (aktuell) · veröffentlicht 30. Sep.", "Versionswechsel"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("German rollout missing %q: %s", want, markup)
		}
	}
}
