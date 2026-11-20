package chat

import (
	"testing"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// A project reference survives the wire both ways with its kind, so a post
// stores a typed pointer instead of dropping to an unspecified reference.
func TestIssue3752_ProjectReferenceKindRoundTripsTheWire(t *testing.T) {
	in := &chatv1.Reference{Kind: chatv1.ReferenceKind_REFERENCE_KIND_PROJECT_TASK, TenantId: "t1", Id: "p1/k1", Display: "Ship it"}
	got := referenceIn(in)
	if got.Kind != chatcore.ProjectWorkReference || got.ID != "p1/k1" || got.TenantID != "t1" {
		t.Fatalf("referenceIn = %+v", got)
	}
	out := reference(got)
	if out.GetKind() != chatv1.ReferenceKind_REFERENCE_KIND_PROJECT_TASK || out.GetId() != "p1/k1" {
		t.Fatalf("reference out = %+v", out)
	}
}
