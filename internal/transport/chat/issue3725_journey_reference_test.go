package chat

import (
	"testing"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// A journey reference survives the wire both ways with its kind, so a post
// stores a typed pointer to the workflow request instead of dropping to an
// unspecified reference.
func TestIssue3725_JourneyReferenceKindRoundTripsTheWire(t *testing.T) {
	in := &chatv1.Reference{Kind: chatv1.ReferenceKind_REFERENCE_KIND_JOURNEY, TenantId: "t1", Id: "j1", Display: "Promotion"}
	got := referenceIn(in)
	if got.Kind != chatcore.JourneyReference || got.ID != "j1" || got.TenantID != "t1" {
		t.Fatalf("referenceIn = %+v", got)
	}
	out := reference(got)
	if out.GetKind() != chatv1.ReferenceKind_REFERENCE_KIND_JOURNEY || out.GetId() != "j1" {
		t.Fatalf("reference out = %+v", out)
	}
}
