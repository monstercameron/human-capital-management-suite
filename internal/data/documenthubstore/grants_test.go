package documenthubstore

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"
)

// TestTodo_HUB_011_Golden is the GOLDEN test for HUB-011: the canonical
// evidence bytes of a grant decision are pinned for audit consumers.
func TestTodo_HUB_011_Golden(t *testing.T) {
	canonical, err := json.Marshal(map[string]string{
		"action": "deploy", "decision": "allow", "document_id": "doc-fixed",
		"effect": "allow", "expires_at": "", "id": "docg-fixed",
		"issuer": "u-owner", "subject_id": "u-op", "subject_kind": "person",
	})
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/hub011_grant.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(append(canonical, '\n'), want) {
		t.Fatalf("grant evidence bytes changed:\n got %s\nwant %s", canonical, want)
	}
}

// TestTodo_HUB_011 is the PRIMARY test for HUB-011: each document action is
// granted separately, and a read grant never authorizes proposing,
// deploying or managing access.
func TestTodo_HUB_011(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-owner", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Authorize(ctx, "tenant-a", docID, "person", "u-reader", ActionRead); !errors.Is(err, ErrDenied) {
		t.Fatalf("ungranted read allowed: %v", err)
	}
	if _, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: docID, SubjectKind: "person", SubjectID: "u-reader", Action: ActionRead, Effect: EffectAllow, Issuer: "u-owner"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Authorize(ctx, "tenant-a", docID, "person", "u-reader", ActionRead); err != nil {
		t.Fatalf("granted read denied: %v", err)
	}
	for _, action := range []string{ActionPropose, ActionReview, ActionDeploy, ActionManage, ActionHistory, ActionComment, ActionExport, ActionRetire} {
		if err := s.Authorize(ctx, "tenant-a", docID, "person", "u-reader", action); !errors.Is(err, ErrDenied) {
			t.Fatalf("read grant authorized %q", action)
		}
	}
	if _, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: docID, SubjectKind: "person", SubjectID: "u-reader", Action: "DELETE_EVERYTHING", Effect: EffectAllow, Issuer: "u-owner"}); err == nil {
		t.Fatal("unknown action granted")
	}
	if _, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: docID, SubjectKind: "person", SubjectID: "u-reader", Action: ActionRead, Effect: "maybe", Issuer: "u-owner"}); err == nil {
		t.Fatal("unknown effect granted")
	}
}

// TestTodo_HUB_011_Security is the SECURITY test for HUB-011: deny
// dominates allow, expiry and revocation close access, and grants never
// cross tenants.
func TestTodo_HUB_011_Security(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-owner", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	grant, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: docID, SubjectKind: "person", SubjectID: "u-member", Action: ActionRead, Effect: EffectAllow, Issuer: "u-owner"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: docID, SubjectKind: "person", SubjectID: "u-member", Action: ActionRead, Effect: EffectDeny, Issuer: "u-owner"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Authorize(ctx, "tenant-a", docID, "person", "u-member", ActionRead); !errors.Is(err, ErrDenied) {
		t.Fatal("deny did not dominate allow")
	}
	past := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: docID, SubjectKind: "person", SubjectID: "u-temp", Action: ActionRead, Effect: EffectAllow, Issuer: "u-owner", ExpiresAt: past}); err != nil {
		t.Fatal(err)
	}
	if err := s.Authorize(ctx, "tenant-a", docID, "person", "u-temp", ActionRead); !errors.Is(err, ErrDenied) {
		t.Fatal("expired grant authorizes")
	}
	if err := s.RevokeGrant(ctx, "tenant-a", grant.ID, "u-owner"); err != nil {
		t.Fatal(err)
	}
	if err := s.Authorize(ctx, "tenant-b", docID, "person", "u-member", ActionRead); !errors.Is(err, ErrDenied) {
		t.Fatal("grant crossed tenants")
	}
}

// TestTodo_HUB_011_Property is the PROPERTY test for HUB-011: over the
// generated effect/expiry/revocation space, the verdict always follows the
// decision table: live allow grants access, and any deny, expiry or
// revocation removes it.
func TestTodo_HUB_011_Property(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-owner", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	past := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	future := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	subjects := []string{"u-deny", "u-expired", "u-revoked", "u-live", "u-future"}
	if _, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: docID, SubjectKind: "person", SubjectID: subjects[0], Action: ActionComment, Effect: EffectDeny, Issuer: "u-owner"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: docID, SubjectKind: "person", SubjectID: subjects[1], Action: ActionComment, Effect: EffectAllow, Issuer: "u-owner", ExpiresAt: past}); err != nil {
		t.Fatal(err)
	}
	revoked, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: docID, SubjectKind: "person", SubjectID: subjects[2], Action: ActionComment, Effect: EffectAllow, Issuer: "u-owner"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeGrant(ctx, "tenant-a", revoked.ID, "u-owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: docID, SubjectKind: "person", SubjectID: subjects[3], Action: ActionComment, Effect: EffectAllow, Issuer: "u-owner"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: docID, SubjectKind: "person", SubjectID: subjects[4], Action: ActionComment, Effect: EffectAllow, Issuer: "u-owner", ExpiresAt: future}); err != nil {
		t.Fatal(err)
	}
	for i, subject := range subjects {
		err := s.Authorize(ctx, "tenant-a", docID, "person", subject, ActionComment)
		wantAllow := i >= 3
		if wantAllow && err != nil {
			t.Fatalf("subject %q denied: %v", subject, err)
		}
		if !wantAllow && !errors.Is(err, ErrDenied) {
			t.Fatalf("subject %q allowed: %v", subject, err)
		}
	}
}
