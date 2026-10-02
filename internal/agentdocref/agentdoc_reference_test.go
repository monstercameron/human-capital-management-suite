package agentdocref

import (
	"errors"
	"strings"
	"testing"
)

const testDocumentID = "doc-123e4567-e89b-42d3-a456-426614174000"

func TestTodo_AGENTDOC_002(t *testing.T) {
	refs := []Reference{{DocumentID: testDocumentID, VersionMode: ModePinned, PinnedVersion: 2, SectionAnchor: "email-and-messaging", Label: "Email and messaging policy"}}
	if err := Validate(refs, MaxPersonaReferences); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
	latest := []Reference{{DocumentID: "doc-223e4567-e89b-42d3-a456-426614174000", VersionMode: ModeLatestPublished, Label: "Tooling guide"}}
	if err := Validate(latest, MaxRequestReferences); err != nil {
		t.Fatalf("Validate() rejected an ordinary display title: %v", err)
	}
	if err := Validate([]Reference{{DocumentID: "policy_1", VersionMode: ModeLatestPublished, Label: "Imported policy"}}, MaxRequestReferences); err != nil {
		t.Fatalf("Validate() rejected a documentation-hub link id: %v", err)
	}
}

func TestTodo_AGENTDOC_002_Property(t *testing.T) {
	first := Reference{DocumentID: testDocumentID, VersionMode: ModePinned, PinnedVersion: 1, Label: "Policy"}
	second := Reference{DocumentID: "doc-223e4567-e89b-42d3-a456-426614174000", VersionMode: ModeLatestPublished, Label: "Guide"}
	if err := Validate([]Reference{first, second}, MaxPersonaReferences); err != nil {
		t.Fatalf("ordered references rejected: %v", err)
	}
	if err := Validate([]Reference{second, first}, MaxPersonaReferences); err != nil {
		t.Fatalf("reverse-ordered references rejected: %v", err)
	}
	if err := Validate(append(make([]Reference, MaxPersonaReferences), first), MaxPersonaReferences); !errors.Is(err, ErrReferenceLimit) {
		t.Fatalf("over-limit error = %v", err)
	}
}

func TestTodo_AGENTDOC_002_Security(t *testing.T) {
	valid := Reference{DocumentID: testDocumentID, VersionMode: ModePinned, PinnedVersion: 2, Label: "Policy"}
	tests := []struct {
		name string
		edit func(*Reference)
		want error
	}{
		{"foreign tenant or URL id", func(r *Reference) { r.DocumentID = "tenant-b://" + testDocumentID }, ErrInvalidDocumentID},
		{"path id", func(r *Reference) { r.DocumentID = "doc/1" }, ErrInvalidDocumentID},
		{"unknown mode", func(r *Reference) { r.VersionMode = "CURRENT" }, ErrInvalidVersionMode},
		{"missing pin", func(r *Reference) { r.PinnedVersion = 0 }, ErrPinnedVersionRequired},
		{"pin on latest", func(r *Reference) { r.VersionMode = ModeLatestPublished }, ErrUnexpectedPinnedVersion},
		{"invalid anchor", func(r *Reference) { r.SectionAnchor = "Policy/2026" }, ErrInvalidSectionAnchor},
		{"empty label", func(r *Reference) { r.Label = "  " }, ErrEmptyLabel},
		{"long label", func(r *Reference) { r.Label = strings.Repeat("界", MaxLabelRunes+1) }, ErrLabelTooLong},
		{"invalid utf8 label", func(r *Reference) { r.Label = string([]byte{0xff}) }, ErrInvalidLabelUTF8},
		{"control label", func(r *Reference) { r.Label = "Policy\nsecret" }, ErrLabelControl},
		{"url label", func(r *Reference) { r.Label = "Policy https://example.invalid" }, ErrLabelURL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref := valid
			tt.edit(&ref)
			if err := Validate([]Reference{ref}, MaxPersonaReferences); !errors.Is(err, tt.want) {
				t.Fatalf("Validate() error = %v, want %v", err, tt.want)
			}
		})
	}
	duplicateDocument := []Reference{valid, valid}
	duplicateDocument[1].Label = "Other"
	if err := Validate(duplicateDocument, MaxPersonaReferences); !errors.Is(err, ErrDuplicateDocument) {
		t.Fatalf("duplicate document error = %v", err)
	}
	duplicateLabel := []Reference{valid, {DocumentID: "doc-223e4567-e89b-42d3-a456-426614174000", VersionMode: ModeLatestPublished, Label: " policy "}}
	if err := Validate(duplicateLabel, MaxPersonaReferences); !errors.Is(err, ErrDuplicateLabel) {
		t.Fatalf("duplicate label error = %v", err)
	}
}
