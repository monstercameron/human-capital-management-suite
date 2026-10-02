package documenthubstore

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestTodo_CHATBUG_020_TitleReader pins the title-only lookup an agent answer's
// source needs: exact title, deployed version, and only what the named reader
// may read now. Placement is not required, and a live deny wins.
func TestTodo_CHATBUG_020_TitleReader(t *testing.T) {
	store, _ := documentFixture(t)
	ctx := context.Background()
	const tenant = "chatbug020-title-reader"

	deploy := func(title, scope string, readers ...string) string {
		t.Helper()
		documentID, err := store.CreateDocument(ctx, tenant, "author", "PERSONAL")
		if err != nil {
			t.Fatal(err)
		}
		version, err := store.SubmitCandidate(ctx, tenant, Version{DocumentID: documentID, CreatorID: "author", Title: title, Markdown: "Current guide."}, "")
		if err != nil {
			t.Fatal(err)
		}
		if scope != "" {
			if _, err := store.RecordReview(ctx, tenant, ReviewInput{DocumentID: documentID, VersionID: version.ID, ScopeKind: "placement", ScopeID: scope, ReviewerID: "reviewer", Authority: "policy-owner", Decision: "approved"}); err != nil {
				t.Fatal(err)
			}
			for _, action := range []string{ActionManage, ActionDeploy} {
				if _, err := store.GrantAction(ctx, tenant, GrantInput{DocumentID: documentID, SubjectKind: "person", SubjectID: "deployer", Action: action, Effect: EffectAllow, Issuer: "author"}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := store.PlaceDocument(ctx, tenant, PlaceInput{DocumentID: documentID, VersionID: version.ID, ScopeKind: "placement", ScopeID: scope, ActorID: "deployer", CustodianID: "deployer", ReviewDueAt: time.Now().UTC().Add(24 * time.Hour)}); err != nil {
				t.Fatal(err)
			}
		}
		for _, reader := range readers {
			if _, err := store.ShareDocument(ctx, tenant, documentID, "author", GrantInput{SubjectKind: "person", SubjectID: reader, Action: ActionRead, Effect: EffectAllow}); err != nil {
				t.Fatal(err)
			}
		}
		return documentID
	}

	guide := deploy("2026 holiday guide", "general", "walt")
	_ = deploy("2026 holiday guide", "general", "someone-else")
	candidateOnly := deploy("Draft handbook", "", "walt")
	_ = candidateOnly

	rows, err := store.ListReadableDeployedDocumentsByTitle(ctx, tenant, "2026 holiday guide", "person", "walt")
	if err != nil || len(rows) != 1 || rows[0].DocumentID != guide || rows[0].Title != "2026 holiday guide" || rows[0].VersionID == "" {
		t.Fatalf("title lookup for the reader = %+v, %v", rows, err)
	}
	if rows, err := store.ListReadableDeployedDocumentsByTitle(ctx, tenant, "2026 holiday guide", "person", "nobody"); err != nil || len(rows) != 0 {
		t.Fatalf("a reader the hub does not allow was given %+v, %v", rows, err)
	}
	for _, title := range []string{"2026 Holiday Guide", "holiday", "Draft handbook"} {
		if rows, err := store.ListReadableDeployedDocumentsByTitle(ctx, tenant, title, "person", "walt"); err != nil || len(rows) != 0 {
			t.Fatalf("title %q matched %+v, %v: exact titles of deployed versions only", title, rows, err)
		}
	}
	if rows, err := store.ListReadableDeployedDocumentsByTitle(ctx, "another-tenant", "2026 holiday guide", "person", "walt"); err != nil || len(rows) != 0 {
		t.Fatalf("a document crossed tenants: %+v, %v", rows, err)
	}
	if _, err := store.GrantAction(ctx, tenant, GrantInput{DocumentID: guide, SubjectKind: "person", SubjectID: "walt", Action: ActionRead, Effect: EffectDeny, Issuer: "author"}); err != nil {
		t.Fatal(err)
	}
	if rows, err := store.ListReadableDeployedDocumentsByTitle(ctx, tenant, "2026 holiday guide", "person", "walt"); err != nil || len(rows) != 0 {
		t.Fatalf("a live deny did not win: %+v, %v", rows, err)
	}
	for _, args := range [][4]string{{"", "t", "person", "r"}, {tenant, "", "person", "r"}, {tenant, "t", "company", "r"}, {tenant, "t", "person", ""}} {
		if _, err := store.ListReadableDeployedDocumentsByTitle(ctx, args[0], args[1], args[2], args[3]); !errors.Is(err, ErrDenied) {
			t.Fatalf("arguments %v: %v", args, err)
		}
	}
	if _, err := (*Store)(nil).ListReadableDeployedDocumentsByTitle(ctx, tenant, "t", "person", "r"); !errors.Is(err, ErrDenied) {
		t.Fatalf("nil store: %v", err)
	}
}
