package productui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
)

func TestTodo_AGENTDOC_005(t *testing.T) {
	persona := PersonaAdminPersona{
		ID: "policy-helper", StarterID: "starter", StarterVersion: 1, Handle: "policy-helper", Name: "Policy Helper", Purpose: "Answers policy questions.",
		ChannelClasses: []string{"PRIVATE"}, DocumentReferences: []PersonaAdminDocumentReference{{Title: "Benefits policy", DocumentID: "doc-benefits", VersionMode: "PINNED", PinnedVersion: 3, Label: "Benefits policy", Readable: true}},
	}
	markup := personaAdminRender(t, personaAdminVersionEditor(ResolveProductLocale("en-US"), &personaAdminTestClient{}, persona, true))
	for _, want := range []string{"Documents this agent reads", "Add a document", "Search by title", "Benefits policy", "Pinned to version 3", "Remove Benefits policy", "1 of 8 documents", "Type @ to mention a document it should follow.", "Each person only gets content from documents they can already read", `href="/workspace/app/docs?document=doc-benefits"`, `target="_blank"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("persona document editor missing %q:\n%s", want, markup)
		}
	}
	cleared := []agentdocref.Reference{}
	encoded, err := json.Marshal(PersonaAdminCommandRequest{Action: "CREATE_VERSION", DocumentReferences: &cleared})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"document_references":[]`) {
		t.Fatalf("explicit clear was omitted: %s", encoded)
	}
	card := personaAdminRender(t, personaAdminCard(ResolveProductLocale("en-US"), &personaAdminTestClient{}, persona, PersonaAdminSnapshot{DocumentServiceAvailable: true}))
	if !strings.Contains(card, "Documents this agent reads") || !strings.Contains(card, "Pinned to version 3") {
		t.Fatalf("catalog card omitted reference documents: %s", card)
	}
}

func TestTodo_AGENTDOC_005_Browser(t *testing.T) {
	localeStates := map[string]string{"en-US": "Documents this agent reads", "de-DE": "Dokumente, die dieser Agent liest", "ar": "المستندات التي يقرأها هذا الوكيل"}
	for locale, label := range localeStates {
		resolved := ResolveProductLocale(locale)
		states := map[AgentDocumentPickerState]string{AgentDocumentPickerIdle: "Search by title to add a document.", AgentDocumentPickerLoading: "Searching documents", AgentDocumentPickerEmpty: "No documents match", AgentDocumentPickerFailed: "Couldn&#39;t load documents."}
		for state, stateText := range states {
			markup := personaAdminRender(t, AgentDocumentReferencePicker(resolved, AgentDocumentReferencePickerModel{ID: "picker", Available: true, State: state}))
			hasExpectedControl := state == AgentDocumentPickerFailed || strings.Contains(markup, `role="combobox"`)
			hasExpectedStatus := (state == AgentDocumentPickerFailed && strings.Contains(markup, `role="alert"`)) || (state != AgentDocumentPickerFailed && strings.Contains(markup, `role="status"`))
			if !strings.Contains(markup, label) || !hasExpectedControl || !hasExpectedStatus || !strings.Contains(markup, `data-agentdoc-state="`+string(state)+`"`) || (locale == "en-US" && !strings.Contains(markup, stateText)) {
				t.Errorf("%s %s picker lacks localized accessible state: %s", locale, state, markup)
			}
			if locale == "ar" && !strings.Contains(markup, "البحث") {
				t.Errorf("Arabic picker copy is not localized: %s", markup)
			}
		}
	}
	ready := personaAdminRender(t, AgentDocumentReferencePicker(ResolveProductLocale("en-US"), AgentDocumentReferencePickerModel{ID: "ready", Available: true, State: AgentDocumentPickerReady, Suggestions: []AgentDocumentSuggestion{{DocumentID: "doc-policy", Title: "Benefits policy", Location: "People operations", Updated: "Updated Sep 29", PublishedVersion: 4}}}))
	for _, want := range []string{"Benefits policy", "People operations · Updated Sep 29", `role="listbox"`, `role="option"`, `data-agentdoc-version="4"`} {
		if !strings.Contains(ready, want) {
			t.Errorf("ready search result missing %q: %s", want, ready)
		}
	}
	refs := make([]PersonaAdminDocumentReference, 8)
	for index := range refs {
		refs[index] = PersonaAdminDocumentReference{Title: "Document", DocumentID: "doc-" + string(rune('a'+index)), VersionMode: "PINNED", PinnedVersion: 1, Label: "Document " + string(rune('a'+index)), Readable: true}
	}
	limit := personaAdminRender(t, AgentDocumentReferencePicker(ResolveProductLocale("en-US"), AgentDocumentReferencePickerModel{ID: "limit", Available: true, References: refs}))
	if !strings.Contains(limit, "You can add up to 8 documents.") || !strings.Contains(limit, `disabled`) || !strings.Contains(limit, "8 of 8 documents") {
		t.Fatalf("limit state is incomplete: %s", limit)
	}
	unavailable := personaAdminRender(t, AgentDocumentReferencePicker(ResolveProductLocale("en-US"), AgentDocumentReferencePickerModel{ID: "off", Available: false}))
	if !strings.Contains(unavailable, "documentation hub is not set up") || strings.Contains(unavailable, `role="combobox"`) {
		t.Fatalf("unconfigured hub left a dead control: %s", unavailable)
	}
	css := personaAdminStylesheet()
	for _, want := range []string{"grid-template-columns:minmax(0,1fr) minmax(12rem,.55fr) auto", "@media(max-width:50rem)", "var(--hcm-color-border)", "var(--hcm-shadow-raised)"} {
		if !strings.Contains(css, want) {
			t.Errorf("picker stylesheet missing %q", want)
		}
	}
}

func TestTodo_AGENTDOC_005_Security(t *testing.T) {
	ref := PersonaAdminDocumentReference{DocumentID: "doc-secret", VersionMode: "PINNED", PinnedVersion: 2, Label: "Restricted handbook", Readable: false}
	markup := personaAdminRender(t, AgentDocumentReferencePicker(ResolveProductLocale("en-US"), AgentDocumentReferencePickerModel{ID: "picker", Available: true, References: []PersonaAdminDocumentReference{ref}}))
	if !strings.Contains(markup, "Restricted handbook") || !strings.Contains(markup, "Cannot be read by you") || !strings.Contains(markup, "Remove Restricted handbook") {
		t.Fatalf("unreadable reference did not preserve safe label and removal: %s", markup)
	}
	if strings.Contains(markup, `href="/workspace/app/docs?document=doc-secret"`) || strings.Contains(markup, `data-agentdoc-mode`) {
		t.Fatalf("unreadable reference exposed navigation or editing: %s", markup)
	}
}

func TestTodo_AGENTDOC_005_ConfigurableRequestBounds(t *testing.T) {
	refs := []PersonaAdminDocumentReference{{Title: "Benefits", DocumentID: "benefits", VersionMode: "LATEST_PUBLISHED", Label: "Benefits", Readable: true}}
	markup := personaAdminRender(t, AgentDocumentReferencePicker(ResolveProductLocale("en-US"), AgentDocumentReferencePickerModel{
		ID: "request-documents", Available: true, DefaultVersionMode: "LATEST_PUBLISHED", ReferenceLimit: 5, LockedVersionMode: true, References: refs,
	}))
	for _, want := range []string{`data-agentdoc-limit-value="5"`, `data-agentdoc-locked-mode="true"`, `data-agentdoc-default-mode="LATEST_PUBLISHED"`, `data-agentdoc-mode-locked="true"`, "Always latest published", "1 of 5 documents"} {
		if !strings.Contains(markup, want) {
			t.Errorf("configurable request picker missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, `data-agentdoc-mode="true"`) || strings.Contains(markup, `<select`) {
		t.Fatalf("locked latest-published picker exposed a version selector: %s", markup)
	}
	limitRefs := make([]PersonaAdminDocumentReference, 5)
	for index := range limitRefs {
		limitRefs[index] = PersonaAdminDocumentReference{Title: "Document", DocumentID: "doc", VersionMode: "LATEST_PUBLISHED", Label: "Document", Readable: true}
	}
	limit := personaAdminRender(t, AgentDocumentReferencePicker(ResolveProductLocale("en-US"), AgentDocumentReferencePickerModel{ID: "request-limit", Available: true, ReferenceLimit: 5, LockedVersionMode: true, References: limitRefs}))
	if !strings.Contains(limit, "You can add up to 5 documents.") || !strings.Contains(limit, "5 of 5 documents") {
		t.Fatalf("request picker did not enforce its configured limit: %s", limit)
	}
}
