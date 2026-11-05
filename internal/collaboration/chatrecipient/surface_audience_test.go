package chatrecipient

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

// surfaceFloorTestAuthority answers surface audience reads for the AGENTP-025
// floor tests. Denied principals fail disclosure authorization; a denied
// class fails the surface policy check. The chat half of the authority is
// intentionally unusable: surface decisions must never consult it.
type surfaceFloorTestAuthority struct {
	snapshot AudienceSnapshot
	err      error
	denied   map[string]bool
	classErr bool
}

func (a surfaceFloorTestAuthority) CurrentAudience(_ context.Context, _ chat.Conversation) (AudienceSnapshot, error) {
	return AudienceSnapshot{}, errors.New("chat audience is not consulted for a surface decision")
}

func (a surfaceFloorTestAuthority) CurrentSurfaceAudience(_ context.Context, surface SurfaceRef) (AudienceSnapshot, error) {
	if a.err != nil {
		return AudienceSnapshot{}, a.err
	}
	if surface.TenantID == "" || surface.ResourceID == "" || surface.Revision == 0 || !surface.Kind.Valid() {
		return AudienceSnapshot{}, errors.New("unexpected surface input")
	}
	return a.snapshot, nil
}

func (a surfaceFloorTestAuthority) AuthorizeDisclosure(_ context.Context, principal AudiencePrincipal, disclosure Disclosure) error {
	if a.denied[principal.SubjectID] {
		return errors.New("not authorized")
	}
	if disclosure.SourceID == "" || disclosure.RecordID == "" || disclosure.Field == "" {
		return errors.New("unexpected disclosure input")
	}
	return nil
}

func (a surfaceFloorTestAuthority) AllowDataClass(_ context.Context, _ chat.Conversation, _ dlp.DataClass) error {
	return errors.New("chat policy is not consulted for a surface decision")
}

func (a surfaceFloorTestAuthority) AllowSurfaceDataClass(_ context.Context, _ SurfaceRef, class dlp.DataClass) error {
	if a.classErr {
		return errors.New("class not allowed")
	}
	if !class.Valid() {
		return errors.New("unexpected class input")
	}
	return nil
}

func surfaceFloorFixture() (AudienceFloorRequest, surfaceFloorTestAuthority) {
	request := AudienceFloorRequest{
		Surface:     &SurfaceRef{TenantID: "tenant-a", Kind: SurfaceHubDocument, ResourceID: "doc-9", Revision: 11},
		Disclosures: []Disclosure{{SourceID: "doc-9", RecordID: "v12", Field: "section-2", CitationTitle: "Section 2", DataClass: dlp.ClassPublic}},
	}
	authority := surfaceFloorTestAuthority{snapshot: AudienceSnapshot{
		TenantID: "tenant-a", Surface: SurfaceRef{TenantID: "tenant-a", Kind: SurfaceHubDocument, ResourceID: "doc-9", Revision: 11}, Revision: 11,
		CurrentMembers: []AudiencePrincipal{{TenantID: "tenant-a", SubjectID: "alice"}, {TenantID: "tenant-a", SubjectID: "bob"}},
		Complete:       true, GuestAndExternalComplete: true,
	}}
	return request, authority
}

// TestTodo_AGENTP_025_SurfaceAudienceFloor proves workflow cases and hub
// documents share the one AGENTP-012 evaluator: the public route needs every
// current case participant or document reader to authorize every disclosure,
// and anything else routes privately to the invoker. Surfaces have no
// eligible-future-joiner population, so the audience is exactly the current
// readers; the returned revision is the commit boundary the committer must
// compare atomically, exactly as on chat.
func TestTodo_AGENTP_025_SurfaceAudienceFloor(t *testing.T) {
	ctx := context.Background()
	request, authority := surfaceFloorFixture()
	if decision := EvaluateAudienceFloor(ctx, request, authority); decision.Route != RoutePublic || decision.Reason != AudienceFloorAllowed || decision.SnapshotRevision != 11 || decision.AudienceSize != 2 {
		t.Fatalf("unanimous readers decision = %+v, want the public route", decision)
	}
	cases := []struct {
		name   string
		mutate func(*AudienceFloorRequest, *surfaceFloorTestAuthority)
		reason AudienceFloorReason
	}{
		{"one reader cannot read a part", func(_ *AudienceFloorRequest, authority *surfaceFloorTestAuthority) {
			authority.denied = map[string]bool{"bob": true}
		}, AudienceFloorUnauthorized},
		{"data class refused on the surface", func(_ *AudienceFloorRequest, authority *surfaceFloorTestAuthority) { authority.classErr = true }, AudienceFloorUnauthorized},
		{"always-private surface", func(request *AudienceFloorRequest, _ *surfaceFloorTestAuthority) { request.AlwaysPrivate = true }, AudienceFloorAlwaysPrivate},
		{"incomplete enumeration", func(_ *AudienceFloorRequest, authority *surfaceFloorTestAuthority) {
			authority.snapshot.Complete = false
		}, AudienceFloorIncomplete},
		{"guest enumeration incomplete", func(_ *AudienceFloorRequest, authority *surfaceFloorTestAuthority) {
			authority.snapshot.GuestAndExternalComplete = false
		}, AudienceFloorIncomplete},
		{"snapshot answers another resource", func(_ *AudienceFloorRequest, authority *surfaceFloorTestAuthority) {
			authority.snapshot.Surface.ResourceID = "doc-1"
		}, AudienceFloorIncomplete},
		{"snapshot answers another kind", func(_ *AudienceFloorRequest, authority *surfaceFloorTestAuthority) {
			authority.snapshot.Surface.Kind = SurfaceWorkflowCase
		}, AudienceFloorIncomplete},
		{"authority failed", func(_ *AudienceFloorRequest, authority *surfaceFloorTestAuthority) {
			authority.err = errors.New("store unavailable")
		}, AudienceFloorIncomplete},
		{"no readers", func(_ *AudienceFloorRequest, authority *surfaceFloorTestAuthority) {
			authority.snapshot.CurrentMembers = nil
		}, AudienceFloorIncomplete},
		{"malformed reader", func(_ *AudienceFloorRequest, authority *surfaceFloorTestAuthority) {
			authority.snapshot.CurrentMembers[1].SubjectID = ""
		}, AudienceFloorIncomplete},
		{"no disclosures", func(request *AudienceFloorRequest, _ *surfaceFloorTestAuthority) { request.Disclosures = nil }, AudienceFloorInvalid},
		{"archived surface", func(request *AudienceFloorRequest, _ *surfaceFloorTestAuthority) { request.Surface.Archived = true }, AudienceFloorInvalid},
		{"unknown surface kind", func(request *AudienceFloorRequest, _ *surfaceFloorTestAuthority) {
			request.Surface.Kind = "CHAT_ROOM"
		}, AudienceFloorInvalid},
		{"missing resource", func(request *AudienceFloorRequest, _ *surfaceFloorTestAuthority) { request.Surface.ResourceID = "" }, AudienceFloorInvalid},
		{"missing revision", func(request *AudienceFloorRequest, _ *surfaceFloorTestAuthority) { request.Surface.Revision = 0 }, AudienceFloorInvalid},
		{"conversation and surface together", func(request *AudienceFloorRequest, _ *surfaceFloorTestAuthority) {
			request.Conversation = floorConversation()
		}, AudienceFloorInvalid},
	}
	for _, tc := range cases {
		request, authority := surfaceFloorFixture()
		tc.mutate(&request, &authority)
		if decision := EvaluateAudienceFloor(ctx, request, authority); decision.Route != RoutePrivate || decision.Reason != tc.reason {
			t.Fatalf("%s: decision = %+v, want private/%s", tc.name, decision, tc.reason)
		}
	}
	// A revision that moved under the request does not fail the decision:
	// the evaluator returns the authority's current revision and the
	// committer compares it atomically before writing publicly. That is
	// the same rule as on chat.
	request, authority = surfaceFloorFixture()
	authority.snapshot.Revision, authority.snapshot.Surface.Revision = 12, 12
	if decision := EvaluateAudienceFloor(ctx, request, authority); decision.Route != RoutePublic || decision.Reason != AudienceFloorAllowed || decision.SnapshotRevision != 12 {
		t.Fatalf("moved revision decision = %+v, want the public route at revision 12", decision)
	}
	// Eligibility is ignored on surfaces: there is no future-joiner
	// population to enumerate, so the audience is exactly the current
	// readers even with eligibility unset.
	request, authority = surfaceFloorFixture()
	authority.snapshot.EligibilityComplete = false
	authority.snapshot.EligibilityPopulation = nil
	if decision := EvaluateAudienceFloor(ctx, request, authority); decision.Route != RoutePublic || decision.AudienceSize != 2 {
		t.Fatalf("surface decision consulted eligibility: %+v", decision)
	}
	// The same evaluator serves workflow cases: the kind changes, the rule
	// does not.
	request, authority = surfaceFloorFixture()
	request.Surface.Kind, request.Surface.ResourceID = SurfaceWorkflowCase, "case-1"
	request.Disclosures = []Disclosure{{SourceID: "case-1", RecordID: "note-7", Field: "body", CitationTitle: "Case note", DataClass: dlp.ClassPublic}}
	authority.snapshot.Surface.Kind, authority.snapshot.Surface.ResourceID = SurfaceWorkflowCase, "case-1"
	if decision := EvaluateAudienceFloor(ctx, request, authority); decision.Route != RoutePublic || decision.AudienceSize != 2 {
		t.Fatalf("workflow case decision = %+v, want the public route", decision)
	}
	// A chat-only authority cannot decide a surface: the surface source
	// port is missing, so the decision fails closed.
	if decision := EvaluateAudienceFloor(ctx, request, floorAuthority()); decision.Route != RoutePrivate || decision.Reason != AudienceFloorIncomplete {
		t.Fatalf("chat-only authority decision = %+v, want private/incomplete", decision)
	}
	if decision := EvaluateAudienceFloor(ctx, request, nil); decision.Route != RoutePrivate || decision.Reason != AudienceFloorInvalid {
		t.Fatalf("nil authority decision = %+v, want private/invalid", decision)
	}
}
