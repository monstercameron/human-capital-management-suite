package documenthubstore

import (
	"context"
	"testing"
	"time"
)

func TestAgentUXSetup3_PlacementDocuments(t *testing.T) {
	store, _ := documentFixture(t)
	ctx := context.Background()
	const tenant = "agentux-setup3-documents"

	place := func(title, reader string) string {
		t.Helper()
		documentID, err := store.CreateDocument(ctx, tenant, "author", "PERSONAL")
		if err != nil {
			t.Fatal(err)
		}
		version, err := store.SubmitCandidate(ctx, tenant, Version{DocumentID: documentID, CreatorID: "author", Title: title, Markdown: "Current policy."}, "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.RecordReview(ctx, tenant, ReviewInput{DocumentID: documentID, VersionID: version.ID, ScopeKind: "placement", ScopeID: "general", ReviewerID: "reviewer", Authority: "policy-owner", Decision: "approved"}); err != nil {
			t.Fatal(err)
		}
		for _, action := range []string{ActionManage, ActionDeploy} {
			if _, err := store.GrantAction(ctx, tenant, GrantInput{DocumentID: documentID, SubjectKind: "person", SubjectID: "deployer", Action: action, Effect: EffectAllow, Issuer: "author"}); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := store.PlaceDocument(ctx, tenant, PlaceInput{DocumentID: documentID, VersionID: version.ID, ScopeKind: "placement", ScopeID: "general", ActorID: "deployer", CustodianID: "deployer", ReviewDueAt: time.Now().UTC().Add(24 * time.Hour)}); err != nil {
			t.Fatal(err)
		}
		if reader != "" {
			if _, err := store.ShareDocument(ctx, tenant, documentID, "author", GrantInput{SubjectKind: "person", SubjectID: reader, Action: ActionRead, Effect: EffectAllow}); err != nil {
				t.Fatal(err)
			}
		}
		return documentID
	}

	readableID := place("Paid time off policy", "admin")
	_ = place("Executive succession plan", "executive")
	rows, err := store.ListReadableOfficialPlacementDocuments(ctx, tenant, "general", "person", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].DocumentID != readableID || rows[0].Title != "Paid time off policy" || rows[0].VersionID == "" {
		t.Fatalf("authorized placement projection = %+v", rows)
	}
	if rows, err := store.ListReadableOfficialPlacementDocuments(ctx, tenant, "other-room", "person", "admin"); err != nil || len(rows) != 0 {
		t.Fatalf("cross-placement documents leaked: rows=%+v err=%v", rows, err)
	}
}
