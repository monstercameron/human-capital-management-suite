package documenthubstore

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
)

// fakeAudience answers eligibility for one tenant/scope from a fixed
// membership set, and can be told to fail or to record every call it saw.
type fakeAudience struct {
	members map[string]bool
	err     error
	calls   []string
}

func (f *fakeAudience) Eligible(_ context.Context, tenantID, scopeKind, scopeID, subjectID string) (bool, error) {
	f.calls = append(f.calls, tenantID+"/"+scopeKind+"/"+scopeID+"/"+subjectID)
	if f.err != nil {
		return false, f.err
	}
	return f.members[subjectID], nil
}

// TestTodo_HUB_013 is the PRIMARY test for HUB-013: a placement path
// resolves only while the live audience authority currently admits the
// subject, and it rechecks eligibility on every call rather than caching
// a stale answer.
func TestTodo_HUB_013(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-author", Markdown: "guide\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordReview(ctx, "tenant-a", ReviewInput{DocumentID: docID, VersionID: v.ID, ScopeKind: "placement", ScopeID: "chan-A", ReviewerID: "u-reviewer", Authority: "team:leads", Decision: "approved"}); err != nil {
		t.Fatal(err)
	}
	for _, g := range []GrantInput{
		{SubjectKind: "person", SubjectID: "u-manager", Action: ActionDeploy, Effect: EffectAllow, Issuer: "u-author"},
		{SubjectKind: "person", SubjectID: "u-manager", Action: ActionManage, Effect: EffectAllow, Issuer: "u-author"},
	} {
		g.DocumentID = docID
		if _, err := s.GrantAction(ctx, "tenant-a", g); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.PlaceDocument(ctx, "tenant-a", PlaceInput{DocumentID: docID, VersionID: v.ID, ScopeKind: "placement", ScopeID: "chan-A", ActorID: "u-manager", CustodianID: "u-manager", ReviewDueAt: fixedReviewDue}); err != nil {
		t.Fatal(err)
	}

	aud := &fakeAudience{members: map[string]bool{"u-member": true}}
	got, err := s.ResolvePlacementDeployment(ctx, "tenant-a", docID, "placement", "chan-A", "u-member", aud)
	if err != nil || got.VersionID != v.ID {
		t.Fatalf("eligible member refused: %+v err=%v", got, err)
	}
	if _, err := s.ResolvePlacementDeployment(ctx, "tenant-a", docID, "placement", "chan-A", "u-departed", aud); !errors.Is(err, ErrIneligibleAudience) {
		t.Fatalf("ineligible subject resolved placement: %v", err)
	}
	// A departure between two resolutions must be seen immediately: no
	// caching of the earlier "eligible" answer.
	delete(aud.members, "u-member")
	if _, err := s.ResolvePlacementDeployment(ctx, "tenant-a", docID, "placement", "chan-A", "u-member", aud); !errors.Is(err, ErrIneligibleAudience) {
		t.Fatalf("stale eligibility served after departure: %v", err)
	}
	if len(aud.calls) != 3 {
		t.Fatalf("eligibility not rechecked per call: %v", aud.calls)
	}
}

// TestTodo_HUB_013_Security is the SECURITY test for HUB-013: a subject
// holding only a direct personal-share grant, and never live team/channel
// membership, cannot resolve an official placement; a nil or erroring
// authority fails closed rather than falling back to the grant.
func TestTodo_HUB_013_Security(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-author", Markdown: "guide\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordReview(ctx, "tenant-a", ReviewInput{DocumentID: docID, VersionID: v.ID, ScopeKind: "placement", ScopeID: "chan-A", ReviewerID: "u-reviewer", Authority: "team:leads", Decision: "approved"}); err != nil {
		t.Fatal(err)
	}
	for _, g := range []GrantInput{
		{SubjectKind: "person", SubjectID: "u-author", Action: ActionDeploy, Effect: EffectAllow, Issuer: "u-author"},
		{SubjectKind: "person", SubjectID: "u-author", Action: ActionManage, Effect: EffectAllow, Issuer: "u-author"},
	} {
		g.DocumentID = docID
		if _, err := s.GrantAction(ctx, "tenant-a", g); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.PlaceDocument(ctx, "tenant-a", PlaceInput{DocumentID: docID, VersionID: v.ID, ScopeKind: "placement", ScopeID: "chan-A", ActorID: "u-author", CustodianID: "u-author", ReviewDueAt: fixedReviewDue}); err != nil {
		t.Fatal(err)
	}
	// u-outsider holds a direct personal share (read) but is never in the
	// channel's live audience.
	if _, err := s.ShareDocument(ctx, "tenant-a", docID, "u-author", GrantInput{SubjectKind: "person", SubjectID: "u-outsider", Action: ActionRead, Effect: EffectAllow}); err != nil {
		t.Fatal(err)
	}
	aud := &fakeAudience{members: map[string]bool{}}
	if _, err := s.ResolvePlacementDeployment(ctx, "tenant-a", docID, "placement", "chan-A", "u-outsider", aud); !errors.Is(err, ErrIneligibleAudience) {
		t.Fatalf("direct grant substituted for live eligibility: %v", err)
	}
	failing := &fakeAudience{err: errors.New("authority unavailable")}
	if _, err := s.ResolvePlacementDeployment(ctx, "tenant-a", docID, "placement", "chan-A", "u-outsider", failing); err == nil {
		t.Fatal("erroring authority resolved placement")
	}
	if _, err := s.ResolvePlacementDeployment(ctx, "tenant-a", docID, "placement", "chan-A", "u-outsider", nil); err == nil {
		t.Fatal("nil authority resolved placement")
	}
}

// TestTodo_HUB_013_Integration is the INTEGRATION test for HUB-013: the
// placement path resolves the same live deployment ResolveDeployment does,
// and refuses an unknown scope kind before touching the database.
func TestTodo_HUB_013_Integration(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-author", Markdown: "guide\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordReview(ctx, "tenant-a", ReviewInput{DocumentID: docID, VersionID: v.ID, ScopeKind: "placement", ScopeID: "team-1", ReviewerID: "u-reviewer", Authority: "team:leads", Decision: "approved"}); err != nil {
		t.Fatal(err)
	}
	for _, g := range []GrantInput{
		{SubjectKind: "person", SubjectID: "u-author", Action: ActionDeploy, Effect: EffectAllow, Issuer: "u-author"},
		{SubjectKind: "person", SubjectID: "u-author", Action: ActionManage, Effect: EffectAllow, Issuer: "u-author"},
	} {
		g.DocumentID = docID
		if _, err := s.GrantAction(ctx, "tenant-a", g); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.PlaceDocument(ctx, "tenant-a", PlaceInput{DocumentID: docID, VersionID: v.ID, ScopeKind: "placement", ScopeID: "team-1", ActorID: "u-author", CustodianID: "u-author", ReviewDueAt: fixedReviewDue}); err != nil {
		t.Fatal(err)
	}
	aud := &fakeAudience{members: map[string]bool{"u-teammate": true}}
	viaEligibility, err := s.ResolvePlacementDeployment(ctx, "tenant-a", docID, "placement", "team-1", "u-teammate", aud)
	if err != nil {
		t.Fatal(err)
	}
	viaDirect, err := s.ResolveDeployment(ctx, "tenant-a", docID, "placement", "team-1")
	if err != nil {
		t.Fatal(err)
	}
	if viaEligibility != viaDirect {
		t.Fatalf("placement path diverged from direct resolution: %+v vs %+v", viaEligibility, viaDirect)
	}
	if _, err := s.ResolvePlacementDeployment(ctx, "tenant-a", docID, "default", "", "u-teammate", aud); !errors.Is(err, ErrRouteInvalid) {
		t.Fatalf("non-placement scope accepted: %v", err)
	}
}

// TestTodo_HUB_013_Golden is the GOLDEN test for HUB-013: the canonical
// eligibility-gated resolution evidence is pinned for audit consumers.
func TestTodo_HUB_013_Golden(t *testing.T) {
	canonical, err := json.Marshal(map[string]any{
		"custodian_id": "u-custodian", "document_id": "doc-fixed", "eligible": true,
		"scope_id": "chan-A", "scope_kind": "placement", "subject_id": "u-member", "version_id": "docv-fixed",
	})
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/hub013_placement_resolution.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(append(canonical, '\n'), want) {
		t.Fatalf("placement resolution evidence changed:\n got %s\nwant %s", canonical, want)
	}
}
