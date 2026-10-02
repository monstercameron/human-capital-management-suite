package agentpersona

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
)

var agentdocPersonaReferences = []agentdocref.Reference{
	{DocumentID: "doc-123e4567-e89b-42d3-a456-426614174000", VersionMode: agentdocref.ModePinned, PinnedVersion: 3, SectionAnchor: "leave-policy", Label: "Leave policy"},
	{DocumentID: "doc-223e4567-e89b-42d3-a456-426614174000", VersionMode: agentdocref.ModeLatestPublished, Label: "Tooling guide"},
}

func TestTodo_AGENTDOC_002(t *testing.T) {
	profile, catalog := personaFixture(t)
	profile.DocumentReferences = append([]agentdocref.Reference(nil), agentdocPersonaReferences...)
	version := mustPersonaVersion(t, profile, personaValidator(catalog))
	if !reflect.DeepEqual(version.Profile.DocumentReferences, agentdocPersonaReferences) {
		t.Fatalf("sealed references = %#v", version.Profile.DocumentReferences)
	}
	without := profile
	without.DocumentReferences = nil
	legacy := mustPersonaVersion(t, without, personaValidator(catalog))
	if version.Digest == legacy.Digest {
		t.Fatal("document references were omitted from the sealed profile digest")
	}
	version.Profile.DocumentReferences[0].Label = "mutated"
	again := mustPersonaVersion(t, profile, personaValidator(catalog))
	if again.Profile.DocumentReferences[0].Label != "Leave policy" {
		t.Fatal("sealed references alias caller-owned memory")
	}
}

func TestTodo_AGENTDOC_002_Golden(t *testing.T) {
	profile, catalog := personaFixture(t)
	legacy := mustPersonaVersion(t, profile, personaValidator(catalog))
	encoded, err := json.Marshal(legacy.Profile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "document_references") {
		t.Fatalf("legacy profile JSON gained an empty field: %s", encoded)
	}
	const legacyDigest = "sha256:0a8f829c701948eaea5c32a3bccd3f64fbc05be9470f6efab5da9bf4b5103965"
	if legacy.Digest != legacyDigest {
		t.Fatalf("legacy digest = %s, want %s", legacy.Digest, legacyDigest)
	}
	profile.DocumentReferences = agentdocPersonaReferences[:1]
	version := mustPersonaVersion(t, profile, personaValidator(catalog))
	encoded, err = json.Marshal(version.Profile)
	if err != nil {
		t.Fatal(err)
	}
	want := `"document_references":[{"document_id":"doc-123e4567-e89b-42d3-a456-426614174000","version_mode":"PINNED","pinned_version":3,"section_anchor":"leave-policy","label":"Leave policy"}]`
	if !strings.Contains(string(encoded), want) {
		t.Fatalf("profile JSON = %s, want fragment %s", encoded, want)
	}
}

func TestTodo_AGENTDOC_002_Property(t *testing.T) {
	profile, catalog := personaFixture(t)
	profile.DocumentReferences = append([]agentdocref.Reference(nil), agentdocPersonaReferences...)
	forward := mustPersonaVersion(t, profile, personaValidator(catalog))
	profile.DocumentReferences[0], profile.DocumentReferences[1] = profile.DocumentReferences[1], profile.DocumentReferences[0]
	reverse := mustPersonaVersion(t, profile, personaValidator(catalog))
	if forward.Digest == reverse.Digest {
		t.Fatal("reference order did not affect the sealed profile digest")
	}
	if !reflect.DeepEqual(reverse.Profile.DocumentReferences, profile.DocumentReferences) {
		t.Fatalf("reference order changed during seal: got %#v want %#v", reverse.Profile.DocumentReferences, profile.DocumentReferences)
	}
}

func TestTodo_AGENTDOC_002_Security(t *testing.T) {
	profile, catalog := personaFixture(t)
	validator := personaValidator(catalog)
	tests := []struct {
		name string
		ref  agentdocref.Reference
		want error
	}{
		{"url", agentdocref.Reference{DocumentID: "https://tenant-b/doc", VersionMode: agentdocref.ModePinned, PinnedVersion: 1, Label: "Policy"}, agentdocref.ErrInvalidDocumentID},
		{"tenant-bearing id", agentdocref.Reference{DocumentID: "tenant-b:doc-123e4567-e89b-42d3-a456-426614174000", VersionMode: agentdocref.ModePinned, PinnedVersion: 1, Label: "Policy"}, agentdocref.ErrInvalidDocumentID},
		{"long label", agentdocref.Reference{DocumentID: agentdocPersonaReferences[0].DocumentID, VersionMode: agentdocref.ModePinned, PinnedVersion: 1, Label: strings.Repeat("a", agentdocref.MaxLabelRunes+1)}, agentdocref.ErrLabelTooLong},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidate := profile
			candidate.DocumentReferences = []agentdocref.Reference{tt.ref}
			if _, err := validator.Build(candidate); !errors.Is(err, ErrInvalidProfile) || !errors.Is(err, tt.want) {
				t.Fatalf("Build() error = %v, want invalid profile and %v", err, tt.want)
			}
		})
	}
	candidate := profile
	candidate.DocumentReferences = agentdocPersonaReferences[:1]
	candidate.Instructions = "Use the payroll tool and email the recipient."
	if _, err := validator.Build(candidate); !errors.Is(err, ErrInstructionReference) {
		t.Fatalf("instruction boundary changed after adding references: %v", err)
	}
}
