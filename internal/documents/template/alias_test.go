package template

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/doctemplate"
)

func aliasDefinition() Definition {
	return Definition{ID: "offer", Version: "1", Type: "OFFER", Purpose: "employment", SourceLocale: "en-US", Locale: "en-US", Jurisdiction: "US-NC", Classification: "CONFIDENTIAL", AccessibilityVersion: "wcag-2.2", Format: "text/html", Source: "Hello {{.name}}", Placeholders: []Placeholder{{Name: "name", Kind: String, Required: true}}}
}

// The alias path must produce exactly what the leaf produces: same digests,
// same rendered bytes, same sentinels, and the same state constants.
func TestAliasPublishesAndRendersLikeTheLeaf(t *testing.T) {
	bindings := map[string]Binding{"name": {Kind: String, Value: "Ada"}}

	draft, err := New(aliasDefinition())
	if err != nil {
		t.Fatalf("New via alias: %v", err)
	}
	leafDraft, err := doctemplate.New(aliasDefinition())
	if err != nil {
		t.Fatalf("New via leaf: %v", err)
	}
	if draft.Digest() == "" || draft.Digest() != leafDraft.Digest() {
		t.Fatalf("digest via alias = %q, via leaf = %q", draft.Digest(), leafDraft.Digest())
	}

	publication := func(tpl Template) Publication {
		return Publication{
			TemplateDigest: tpl.Digest(),
			Fixtures:       []Fixture{{Name: "golden", Bindings: bindings}},
			Approval:       Approval{ApprovalID: "approval-1", ApprovedBy: "hr"},
			Applicability:  Applicability{Locales: []string{"en-US"}, Jurisdictions: []string{"US-NC"}, Classifications: []string{"CONFIDENTIAL"}},
		}
	}
	published, err := draft.Publish(publication(draft))
	if err != nil {
		t.Fatalf("Publish via alias: %v", err)
	}
	if Published != doctemplate.Published || published.State != doctemplate.Published {
		t.Fatalf("state = %q, want PUBLISHED from both paths", published.State)
	}
	artifact, err := published.Render(bindings)
	if err != nil {
		t.Fatalf("Render via alias: %v", err)
	}
	if string(artifact.Content) != "Hello Ada" {
		t.Fatalf("content = %q, want %q", artifact.Content, "Hello Ada")
	}
}

func TestAliasRefusesInvalidDefinitionWithTheLeafSentinel(t *testing.T) {
	_, err := New(Definition{})
	if !errors.Is(err, ErrInvalid) || !errors.Is(err, doctemplate.ErrInvalid) {
		t.Fatalf("err = %v, want the shared ErrInvalid sentinel", err)
	}
}

func TestAliasValueKindsAndStatesMatchTheLeaf(t *testing.T) {
	if String != doctemplate.String || Date != doctemplate.Date || DateTime != doctemplate.DateTime {
		t.Fatal("value kind constants diverged from the leaf")
	}
	if Draft != doctemplate.Draft || Retired != doctemplate.Retired {
		t.Fatal("state constants diverged from the leaf")
	}
}
