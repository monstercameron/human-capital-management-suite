package productui

import (
	"strings"
	"testing"
)

// Agent cards show sealed labels: the badge carries the v-form and the
// sentence names the plain label, while an unlabelled draft keeps its
// sequence (AGENTUX-065).
func TestAgentUX065_CardShowsSealedLabel(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	persona := agentUXSetup2Persona()
	persona.Lifecycle, persona.Version, persona.VersionLabel = PersonaPublished, "6", "1.4.2"
	persona.Installations = []PersonaAdminInstallation{{Conversation: "general", Kind: "PUBLIC_CHANNEL", Version: "6", VersionLabel: "1.4.2"}}
	state := personaAdminLifecycleState(locale, persona)
	badges := personaAdminRender(t, personaAdminLifecycleBadges(locale, persona, state))
	if !strings.Contains(badges, "v1.4.2 · Published") {
		t.Fatalf("card badge missing v-form label: %s", badges)
	}
	if strings.Contains(badges, "Version 6") || strings.Contains(badges, "Version v1.4.2") {
		t.Fatalf("card badge kept the sequence: %s", badges)
	}
	sentence := personaAdminRender(t, personaAdminLifecycleSentence(locale, persona, state))
	if !strings.Contains(sentence, "Every conversation runs version 1.4.2.") {
		t.Fatalf("card sentence missing plain label: %s", sentence)
	}
	draft := agentUXSetup2Persona()
	draft.Lifecycle, draft.Version, draft.LiveVersion, draft.LiveVersionLabel = PersonaDraft, "7", "6", "1.4.2"
	draftState := personaAdminLifecycleState(locale, draft)
	draftBadges := personaAdminRender(t, personaAdminLifecycleBadges(locale, draft, draftState))
	if !strings.Contains(draftBadges, "Version 7 · Draft") {
		t.Fatalf("draft badge lost its sequence: %s", draftBadges)
	}
	draftSentence := personaAdminRender(t, personaAdminLifecycleSentence(locale, draft, draftState))
	if !strings.Contains(draftSentence, "People are using version 1.4.2. Version 7 is a draft.") {
		t.Fatalf("draft sentence missing labels: %s", draftSentence)
	}
	ops := personaAdminRender(t, agentUX073AgentRows(locale, AgentControlsSnapshot{Agents: []PersonaAdminPersona{persona}}))
	if !strings.Contains(ops, "v1.4.2") {
		t.Fatalf("operations row missing v-form label: %s", ops)
	}
	labelled := persona
	labelled.VersionHistory = []PersonaAdminVersionHistory{{Version: "6", VersionLabel: "1.4.2", Lifecycle: PersonaPublished}, {Version: "7", Lifecycle: PersonaDraft}}
	history := personaAdminRender(t, personaAdminVersionHistory(locale, labelled))
	if !strings.Contains(history, ">v1.4.2<") {
		t.Fatalf("history table missing v-form label: %s", history)
	}
}

// Rollout rows carry the target's v-form label and the completion sentence
// names its plain label (AGENTUX-065).
func TestAgentUX065_RolloutShowsSealedLabel(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	plan := AgentRolloutPlan{ID: "plan", Version: 2, VersionLabel: "1.1.0", Candidates: []AgentRolloutCandidate{{InstallationID: "i1"}}}
	snapshot := AgentRolloutSnapshot{
		Personas:      []AgentRolloutPersona{{ID: "policy", Name: "Policy Helper"}},
		Installations: []AgentRolloutInstallation{{ID: "i1", PersonaID: "policy", ConversationID: "general", Name: "general", ConversationKind: "PUBLIC_CHANNEL", MemberCount: 3, Visible: true, Version: 1, VersionLabel: "1.0.0"}},
		Progress:      &AgentRolloutProgress{Stage: "COMPLETE", Revision: 3},
	}
	markup := personaAdminRender(t, agentRolloutPlan(locale, plan, snapshot))
	if !strings.Contains(markup, "v1.1.0") {
		t.Fatalf("rollout rows missing v-form target: %s", markup)
	}
	if !strings.Contains(markup, "every selected conversation runs version 1.1.0.") {
		t.Fatalf("rollout completion missing plain label: %s", markup)
	}
}

// The publish action offers the author's three plain choices with their
// computed labels, preselecting the smallest the change allows; a first
// publication names its one computed label (AGENTUX-065).
func TestAgentUX065_PublishChoicePicker(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	persona := agentUXSetup2Persona()
	persona.PublishPreview = PersonaPublishPreview{Available: true, Required: "minor", Greatest: "1.4.2", Patch: "1.4.3", Minor: "1.5.0", Major: "2.0.0"}
	markup := personaAdminRender(t, personaAdminPublishChoice(locale, persona))
	for _, want := range []string{`value="patch"`, `value="minor"`, `value="major"`, "Fixes wording", "Adds or clarifies", "Changes what it says or does", "v1.4.3", "v1.5.0", "v2.0.0"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("publish picker missing %q: %s", want, markup)
		}
	}
	around := func(value string) string {
		i := strings.Index(markup, `value="`+value+`"`)
		if i < 0 {
			return ""
		}
		start := strings.LastIndex(markup[:i], "<input")
		end := strings.Index(markup[i:], ">")
		if start < 0 || end < 0 {
			return ""
		}
		return markup[start : i+end+1]
	}
	if !strings.Contains(around("minor"), "checked") {
		t.Fatalf("picker did not preselect the required choice: %s", markup)
	}
	if !strings.Contains(around("patch"), "disabled") {
		t.Fatalf("picker left a too-small choice enabled: %s", markup)
	}
	first := agentUXSetup2Persona()
	first.PublishPreview = PersonaPublishPreview{Available: true, Required: "patch", Patch: "1.0.0", Minor: "1.0.0", Major: "1.0.0"}
	firstMarkup := personaAdminRender(t, personaAdminPublishChoice(locale, first))
	if !strings.Contains(firstMarkup, "v1.0.0") || strings.Contains(firstMarkup, `value="patch"`) {
		t.Fatalf("first publication picker should name one label, not choices: %s", firstMarkup)
	}
	off := agentUXSetup2Persona()
	if node := personaAdminPublishChoice(locale, off); node != nil {
		t.Fatalf("picker rendered without a preview: %s", personaAdminRender(t, node))
	}
	if got := PersonaAdminCommandStatusText(locale, "version_bump_too_small"); got == PersonaAdminCommandStatusText(locale, "unavailable") || !strings.Contains(got, "version") {
		t.Fatalf("refusal copy missing: %q", got)
	}
}

// A major document change shows the owner what changed, what the agent
// still reads, and a way to review and update (AGENTUX-065).
func TestAgentUX065_DocumentDriftNotice(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	persona := agentUXSetup2Persona()
	persona.DocumentNotices = []PersonaDocumentDriftNotice{{DocumentID: "pto", Title: "Paid time off policy", Reading: "1.4.2", Latest: "2.0.0"}}
	markup := personaAdminRender(t, personaAdminDocumentNotices(locale, persona))
	if !strings.Contains(markup, "Paid time off policy changed to v2.0.0; this agent still reads v1.4.2.") {
		t.Fatalf("drift notice missing: %s", markup)
	}
	if !strings.Contains(markup, "Review and update") {
		t.Fatalf("drift notice missing review link: %s", markup)
	}
	quiet := agentUXSetup2Persona()
	if node := personaAdminDocumentNotices(locale, quiet); node != nil {
		t.Fatalf("notice rendered without drift: %s", personaAdminRender(t, node))
	}
}

// TestTodo_AGENTUX_065_Browser reads the agent card as the owner: the badge
// carries the sealed label, the publish action offers the three computed
// choices, and a drifted document names what changed and what still reads.
func TestTodo_AGENTUX_065_Browser(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	persona := agentUXSetup2Persona()
	persona.Lifecycle, persona.Version, persona.VersionLabel = PersonaPublished, "6", "1.4.2"
	persona.PublishPreview = PersonaPublishPreview{Available: true, Required: "minor", Greatest: "1.4.2", Patch: "1.4.3", Minor: "1.5.0", Major: "2.0.0"}
	persona.DocumentReferences = []PersonaAdminDocumentReference{{DocumentID: "pto", Title: "Paid time off policy", Label: "PTO", VersionMode: "COMPATIBLE", CompatibleBase: "1.0.0", Readable: true}}
	persona.DocumentNotices = []PersonaDocumentDriftNotice{{DocumentID: "pto", Title: "Paid time off policy", Reading: "1.4.2", Latest: "2.0.0"}}
	snapshot := PersonaAdminSnapshot{CommandPermissionsAvailable: true, AllowedCommands: []string{"PUBLISH"}}
	markup := personaAdminRender(t, personaAdminCard(locale, &personaAdminTestClient{}, persona, snapshot))
	for _, want := range []string{"v1.4.2", "Follow ^1", "Paid time off policy changed to v2.0.0; this agent still reads v1.4.2.", "Review and update"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("owner card missing %q", want)
		}
	}
	draft := agentUXSetup2Persona()
	draft.Lifecycle, draft.Version = PersonaInReview, "7"
	draft.ReviewApproved, draft.EvaluationRef = true, "suite"
	draft.PublishPreview = PersonaPublishPreview{Available: true, Required: "patch", Greatest: "1.4.2", Patch: "1.4.3", Minor: "1.5.0", Major: "2.0.0"}
	draftMarkup := personaAdminRender(t, personaAdminCard(locale, &personaAdminTestClient{}, draft, snapshot))
	for _, want := range []string{"Publish as", "Fixes wording", "v1.4.3", "v1.5.0", "v2.0.0", "Publish version 7"} {
		if !strings.Contains(draftMarkup, want) {
			t.Fatalf("publish card missing %q", want)
		}
	}
}
