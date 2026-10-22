package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
)

// Document references pin an exact label or follow a compatible line, and
// new references default to the compatible line (AGENTUX-065).
func TestAgentUX065_PickerLabelModes(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	model := AgentDocumentReferencePickerModel{
		ID: "persona-reference-new", Available: true, State: AgentDocumentPickerIdle,
		DefaultVersionMode: string(agentdocref.ModeCompatible),
		References: []PersonaAdminDocumentReference{
			{DocumentID: "pto", Title: "Paid time off policy", Label: "PTO", VersionMode: string(agentdocref.ModePinnedLabel), PinnedLabel: "1.2.0", Readable: true},
			{DocumentID: "handbook", Title: "Handbook", Label: "Handbook", VersionMode: string(agentdocref.ModeCompatible), CompatibleBase: "1.0.0", Readable: true},
		},
		Suggestions: []AgentDocumentSuggestion{{DocumentID: "guide", Title: "Guide", PublishedVersion: 3, PublishedLabel: "1.4.2"}},
	}
	markup := personaAdminRender(t, AgentDocumentReferencePicker(locale, model))
	for _, want := range []string{
		`data-agentdoc-default-mode="COMPATIBLE"`,
		`=1.2.0`, `^1`, `data-agentdoc-published-label="1.4.2"`,
		`value="PINNED_LABEL"`, `value="COMPATIBLE"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("picker missing %q: %s", want, markup)
		}
	}
}
