package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_AGENT_044_RendersExactSelectionsAndFencedActions(t *testing.T) {
	view := ApplyLocale(NewView(PageAgents, "tenant-1", "principal-1", "scope-1"), ResolveProductLocale("en-US"))
	markup, err := ui.RenderToString(AgentRolloutPortableMount(view.Locale, AgentRolloutSnapshot{
		Available: true, CanPreview: true, CanApprove: true, CanPromote: true, CanAdvance: true,
		Personas:      []AgentRolloutPersona{{ID: "persona-7", Name: "Payroll assistant"}},
		Versions:      []AgentRolloutVersion{{Version: 3, Digest: "sha256:plan", ProfileDigest: "sha256:profile"}},
		Installations: []AgentRolloutInstallation{{ID: "inst-a", ConversationID: "conv-a", CanaryEligible: true}, {ID: "inst-b", ConversationID: "conv-b"}},
		Active:        &AgentRolloutPlan{ID: "rollout-1", Digest: "sha256:plan", Version: 3, Candidates: []AgentRolloutCandidate{{InstallationID: "inst-a", ConversationID: "conv-a", Version: 2, Revision: 9, RevocationEpoch: 4, AuthorityRevision: 12, PolicyDigest: "sha256:policy"}}},
		Progress:      &AgentRolloutProgress{Revision: 3, Stage: "CANARY"},
	}, AgentPortableSnapshot{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`data-rollout-action="PREVIEW"`, `value="inst-a"`, `value="inst-b"`, `data-rollout-action="APPROVE"`, `data-rollout-digest="sha256:plan"`, `sha256:policy`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("markup missing %q: %s", want, markup)
		}
	}
}

func TestTodo_AGENT_045_PortableImportStatesNoAuthority(t *testing.T) {
	view := ApplyLocale(NewView(PageAgents, "tenant-1", "principal-1", "scope-1"), ResolveProductLocale("de-DE"))
	markup, err := ui.RenderToString(AgentRolloutPortableMount(view.Locale, AgentRolloutSnapshot{}, AgentPortableSnapshot{Available: true, CanImport: true, Destinations: []AgentPortableDestination{{ID: "src-1", Label: "Knowledge source"}}}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`id="agent-portable-import"`, `name="manifest"`, `name="destination_mapping[src-1]"`, "Der Import erstellt einen prüfbaren Entwurf"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("markup missing %q", want)
		}
	}
}

func TestTodo_AGENT_044_LocalizesRTL(t *testing.T) {
	view := ApplyLocale(NewView(PageAgents, "tenant-1", "principal-1", "scope-1"), ResolveProductLocale("ar"))
	markup, err := ui.RenderToString(AgentRolloutPortableMount(view.Locale, AgentRolloutSnapshot{Available: true}, AgentPortableSnapshot{Available: true}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `dir="rtl"`) || !strings.Contains(markup, "توزيع الإصدار") {
		t.Fatalf("rtl Arabic surface missing: %s", markup)
	}
}

func TestTodo_AGENT_044_EmptyCatalogDoesNotOfferRollout(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := ResolveProductLocale(locale)
		markup, err := ui.RenderToString(AgentRolloutPortableMount(view, AgentRolloutSnapshot{Available: true, CanPreview: true}, AgentPortableSnapshot{}))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(markup, `id="agent-rollout-preview"`) || !strings.Contains(markup, agentRPText(view, "setup_required")) || strings.Contains(markup, agentRPText(view, "ready")) {
			t.Fatalf("empty rollout presented as ready in %s: %s", locale, markup)
		}
	}
}

func TestTodo_AGENT_045_RequiresCompleteExplicitMappings(t *testing.T) {
	destinations := []AgentPortableDestination{{Kind: "source", SourceID: "src-1", ID: "dest-1"}, {Kind: "capability", SourceID: "cap-1", ID: "dest-2"}}
	if CompletePortableMappings(destinations, []AgentPortableMapping{{Kind: "source", SourceID: "src-1", DestinationID: "dest-1"}}) {
		t.Fatal("partial mapping accepted")
	}
	if !CompletePortableMappings(destinations, []AgentPortableMapping{{Kind: "source", SourceID: "src-1", DestinationID: "dest-1"}, {Kind: "capability", SourceID: "cap-1", DestinationID: "dest-2"}}) {
		t.Fatal("complete mapping rejected")
	}
}

func TestTodo_AGENT_045_ImportNamesExplicitDestinationManifest(t *testing.T) {
	view := ApplyLocale(NewView(PageAgents, "tenant-1", "principal-1", "scope-1"), ResolveProductLocale("en-US"))
	markup, err := ui.RenderToString(AgentRolloutPortableMount(view.Locale, AgentRolloutSnapshot{}, AgentPortableSnapshot{Available: true, CanImport: true, Manifests: []AgentPortableManifest{{ID: "manifest-dest", Version: "7", Name: "Destination"}}}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `id="agent-portable-target"`) || !strings.Contains(markup, `value="manifest-dest"`) || !strings.Contains(markup, `data-manifest-version="7"`) {
		t.Fatalf("destination manifest selector missing: %s", markup)
	}
}

func TestTodo_AGENT_045_PreservesExportedDefinitionOnRerender(t *testing.T) {
	view := ApplyLocale(NewView(PageAgents, "tenant-1", "principal-1", "scope-1"), ResolveProductLocale("en-US"))
	markup, err := ui.RenderToString(AgentRolloutPortableMount(view.Locale, AgentRolloutSnapshot{}, AgentPortableSnapshot{Available: true, CanImport: true, Definition: `{"instructions":"canonical"}`}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "canonical") {
		t.Fatalf("definition was lost on rerender: %s", markup)
	}
	textarea := markup[strings.Index(markup, `<textarea`):]
	textarea = textarea[:strings.Index(textarea, `</textarea>`)]
	if !strings.Contains(textarea[strings.Index(textarea, `>`)+1:], "canonical") {
		t.Fatal("definition stored as an attribute rather than editable textarea content")
	}
}

func TestTodo_AGENT_045_Conformance_LocalizedStatuses(t *testing.T) {
	for _, key := range []string{"refused", "invalid", "mapping_required", "exported", "imported"} {
		en := AgentRolloutPortableStatus(ResolveProductLocale("en-US"), key)
		de := AgentRolloutPortableStatus(ResolveProductLocale("de-DE"), key)
		ar := AgentRolloutPortableStatus(ResolveProductLocale("ar"), key)
		if en == "" || de == "" || ar == "" || en == de || de == ar || en == ar {
			t.Fatalf("missing status translation %s", key)
		}
	}
}

func TestTodo_AGENT_045_ReviewImportedDraft(t *testing.T) {
	for _, loc := range []string{"en-US", "de-DE", "ar"} {
		markup, err := ui.RenderToString(AgentRolloutPortableMount(ResolveProductLocale(loc), AgentRolloutSnapshot{}, AgentPortableSnapshot{Available: true, CanImport: true, Draft: &AgentPortableReviewDraft{ID: "portable:actual-draft", Version: 1, Purpose: "Help payroll", Instructions: "Exact body <safe>"}}))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{`id="agent-portable-review"`, `data-portable-action="read"`, `id="agent-portable-draft"`, `portable:actual-draft`, `Help payroll`, `Exact body &lt;safe&gt;`} {
			if !strings.Contains(markup, want) {
				t.Fatalf("%s missing %s: %s", loc, want, markup)
			}
		}
	}
}
