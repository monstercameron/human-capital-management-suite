package documenthubstore

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
)

// TestTodo_HUB_010_Golden is the GOLDEN test for HUB-010: the canonical
// withdrawal event body is pinned for outbox consumers.
func TestTodo_HUB_010_Golden(t *testing.T) {
	payload, err := withdrawalEventPayload("tenant-a", "docd-fixed", WithdrawInput{
		DocumentID: "doc-fixed", ScopeKind: "placement", ScopeID: "chan-A",
		ActorID: "u-deployer", ExpectedLive: "docv-fixed", Reason: "major error",
	})
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/hub010_withdraw.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(payload+"\n", string(want)) {
		t.Fatalf("withdraw evidence bytes changed:\n got %s\nwant %s", payload, want)
	}
}

// TestTodo_HUB_010 is the PRIMARY test for HUB-010: authorized withdrawal
// removes the live pointer but preserves every immutable record, and
// redeploying an old version appends fresh evidence.
func TestTodo_HUB_010(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v1, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-author", Markdown: "one\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	v2, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-author", Markdown: "two\n"}, v1.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []Version{v1, v2} {
		if _, err := s.RecordReview(ctx, "tenant-a", ReviewInput{DocumentID: docID, VersionID: v.ID, ScopeKind: "default", ScopeID: "", ReviewerID: "u-reviewer", Authority: "team:leads", Decision: "approved"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, g := range []GrantInput{
		{DocumentID: docID, SubjectKind: "person", SubjectID: "u-deployer", Action: ActionDeploy, Effect: EffectAllow, Issuer: "u-owner"},
		{DocumentID: docID, SubjectKind: "person", SubjectID: "u-deployer", Action: ActionRetire, Effect: EffectAllow, Issuer: "u-owner"},
	} {
		if _, err := s.GrantAction(ctx, "tenant-a", g); err != nil {
			t.Fatal(err)
		}
	}
	deploy := func(versionID, expected string) {
		t.Helper()
		if _, err := s.Deploy(ctx, "tenant-a", DeployInput{DocumentID: docID, VersionID: versionID, ScopeKind: "default", ScopeID: "", DeployerID: "u-deployer", ExpectedLive: expected}); err != nil {
			t.Fatal(err)
		}
	}
	deploy(v1.ID, "")
	deploy(v2.ID, v1.ID)
	if _, err := s.Withdraw(ctx, "tenant-a", WithdrawInput{DocumentID: docID, ScopeKind: "default", ScopeID: "", ActorID: "u-deployer", ExpectedLive: v1.ID, Reason: "wrong version"}); !errors.Is(err, ErrStalePointer) {
		t.Fatalf("withdraw with stale base: %v", err)
	}
	w, err := s.Withdraw(ctx, "tenant-a", WithdrawInput{DocumentID: docID, ScopeKind: "default", ScopeID: "", ActorID: "u-deployer", ExpectedLive: v2.ID, Reason: "major error"})
	if err != nil {
		t.Fatalf("authorized withdrawal refused: %v", err)
	}
	if w.VersionID != v2.ID {
		t.Fatalf("withdrawal names wrong version: %+v", w)
	}
	if _, err := s.ResolveDeployment(ctx, "tenant-a", docID, "default", ""); !errors.Is(err, ErrNoDeployment) {
		t.Fatalf("withdrawn scope still resolves: %v", err)
	}
	live, events, statuses := deployState(t, s, "tenant-a", docID)
	if live != "" || events != 2 || statuses[v1.ID] != "stale" || statuses[v2.ID] != "retired" {
		t.Fatalf("withdrawal rewrote history: live=%q events=%d statuses=%v", live, events, statuses)
	}
	deploy(v1.ID, "")
	got, err := s.ResolveDeployment(ctx, "tenant-a", docID, "default", "")
	if err != nil || got.VersionID != v1.ID {
		t.Fatalf("redeploy failed: %+v err=%v", got, err)
	}
	live, events, statuses = deployState(t, s, "tenant-a", docID)
	if live != v1.ID || events != 3 || statuses[v1.ID] != "deployed" {
		t.Fatalf("redeploy did not append fresh evidence: live=%q events=%d statuses=%v", live, events, statuses)
	}
}

// TestTodo_HUB_010_Security is the SECURITY test for HUB-010: withdrawal
// needs its own capability, never touches other scopes, and never leaks
// history.
func TestTodo_HUB_010_Security(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-author", Markdown: "one\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordReview(ctx, "tenant-a", ReviewInput{DocumentID: docID, VersionID: v.ID, ScopeKind: "default", ScopeID: "", ReviewerID: "u-reviewer", Authority: "team:leads", Decision: "approved"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: docID, SubjectKind: "person", SubjectID: "u-deployer", Action: ActionDeploy, Effect: EffectAllow, Issuer: "u-owner"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Deploy(ctx, "tenant-a", DeployInput{DocumentID: docID, VersionID: v.ID, ScopeKind: "default", ScopeID: "", DeployerID: "u-deployer"}); err != nil {
		t.Fatal(err)
	}
	in := WithdrawInput{DocumentID: docID, ScopeKind: "default", ScopeID: "", ActorID: "u-deployer", ExpectedLive: v.ID, Reason: "oops"}
	if _, err := s.Withdraw(ctx, "tenant-a", in); !errors.Is(err, ErrDenied) {
		t.Fatalf("withdrawal without retire grant: %v", err)
	}
	got, err := s.ResolveDeployment(ctx, "tenant-a", docID, "default", "")
	if err != nil || got.VersionID != v.ID {
		t.Fatalf("refused withdrawal moved the pointer: %+v err=%v", got, err)
	}
	if _, err := s.Withdraw(ctx, "tenant-b", in); !errors.Is(err, ErrDenied) {
		t.Fatalf("cross-tenant withdrawal: %v", err)
	}
}
