package documenthubstore

import (
	"context"
	"testing"
	"time"
)

func TestTodo_AGENTP_011_PersonaPlacementSearchScopeAndRevocation(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	const tenant = "persona-placement-test"
	makePlacement := func(scope, body string) string {
		doc, err := s.CreateDocument(ctx, tenant, "author", "PERSONAL")
		if err != nil {
			t.Fatal(err)
		}
		v, err := s.SubmitCandidate(ctx, tenant, Version{DocumentID: doc, CreatorID: "author", Title: "Policy", Markdown: body, Classification: "INTERNAL"}, "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.RecordReview(ctx, tenant, ReviewInput{DocumentID: doc, VersionID: v.ID, ScopeKind: "placement", ScopeID: scope, ReviewerID: "reviewer", Authority: "team:policy", Decision: ReviewApproved}); err != nil {
			t.Fatal(err)
		}
		for _, action := range []string{ActionManage, ActionDeploy} {
			if _, err = s.GrantAction(ctx, tenant, GrantInput{DocumentID: doc, SubjectKind: "person", SubjectID: "deployer", Action: action, Effect: EffectAllow, Issuer: "author"}); err != nil {
				t.Fatal(err)
			}
		}
		if _, err = s.PlaceDocument(ctx, tenant, PlaceInput{DocumentID: doc, VersionID: v.ID, ScopeKind: "placement", ScopeID: scope, ActorID: "deployer", CustodianID: "deployer", ReviewDueAt: time.Now().Add(24 * time.Hour)}); err != nil {
			t.Fatal(err)
		}
		if _, err = s.ShareDocument(ctx, tenant, doc, "author", GrantInput{SubjectKind: "person", SubjectID: "reader", Action: ActionRead, Effect: EffectAllow}); err != nil {
			t.Fatal(err)
		}
		if err = s.IndexDeployedVersion(ctx, tenant, doc, v.ID); err != nil {
			t.Fatal(err)
		}
		return doc
	}
	own := makePlacement("room-a", "The policy requires written approval.")
	makePlacement("room-b", "The policy requires secret unrelated procedures.")
	hits, err := s.SearchOfficialPlacementLexical(ctx, tenant, "room-a", "policy", "person", "reader")
	if err != nil || len(hits) != 1 || hits[0].DocumentID != own {
		t.Fatalf("scope leaked hits=%+v err=%v", hits, err)
	}
	for _, args := range [][3]string{{tenant, "room-a", "stranger"}, {"other-tenant", "room-a", "reader"}, {tenant, "missing-room", "reader"}} {
		hits, err = s.SearchOfficialPlacementLexical(ctx, args[0], args[1], "policy", "person", args[2])
		if err != nil || len(hits) != 0 {
			t.Fatalf("foreign scope visible %+v %v", hits, err)
		}
	}
	if _, err = s.GrantAction(ctx, tenant, GrantInput{DocumentID: own, SubjectKind: "person", SubjectID: "reader", Action: ActionRead, Effect: EffectDeny, Issuer: "author"}); err != nil {
		t.Fatal(err)
	}
	hits, err = s.SearchOfficialPlacementLexical(ctx, tenant, "room-a", "policy", "person", "reader")
	if err != nil || len(hits) != 0 {
		t.Fatalf("revoked visibility %+v %v", hits, err)
	}
}
