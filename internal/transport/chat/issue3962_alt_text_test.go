package chat

import (
	"testing"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
)

// TestIssue3962_ImageDescriptionRoundTripsTheWire proves the description and the
// decorative mark survive the conversion in both directions, beside the name.
func TestIssue3962_ImageDescriptionRoundTripsTheWire(t *testing.T) {
	in := &chatv1.Reference{Kind: chatv1.ReferenceKind_REFERENCE_KIND_MEDIA, TenantId: "t1", Id: "a1", Display: "q3.png", ContentType: "image/png", AltText: "Line chart of Q3 hires"}
	got := referenceIn(in)
	if got.AltText != "Line chart of Q3 hires" || got.Display != "q3.png" {
		t.Fatalf("referenceIn = %+v", got)
	}
	if out := reference(got); out.GetAltText() != "Line chart of Q3 hires" || out.GetDecorative() {
		t.Fatalf("reference = %+v", out)
	}
	dec := referenceIn(&chatv1.Reference{Kind: chatv1.ReferenceKind_REFERENCE_KIND_MEDIA, TenantId: "t1", Id: "a1", Decorative: true})
	if !dec.Decorative || !reference(dec).GetDecorative() {
		t.Fatalf("decorative lost: %+v", dec)
	}
}
